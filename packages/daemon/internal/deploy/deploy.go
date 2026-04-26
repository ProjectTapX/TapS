// Package deploy turns a panel-supplied DeployStartReq into a fully
// installed Minecraft server inside one instance's /data volume. The
// panel does the per-provider API hits up front; this package just
// validates, clears, downloads, optionally runs an installer container,
// writes eula.txt, and updates the instance's launch Args.
//
// All long work runs on a goroutine keyed by instance UUID. Status is
// stored in memory and read back via ActionInstanceDeployStatus —
// browsers poll once a second.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProjectTapX/TapS/packages/daemon/internal/instance"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/volumes"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// Manager owns active deploys (one per instance UUID at a time).
type Manager struct {
	mgr  *instance.Manager
	vols *volumes.Manager

	mu     sync.Mutex
	active map[string]*deployState
	last   map[string]*protocol.DeployStatus
}

type deployState struct {
	status protocol.DeployStatus
	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(mgr *instance.Manager, vm *volumes.Manager) *Manager {
	return &Manager{
		mgr:    mgr,
		vols:   vm,
		active: map[string]*deployState{},
		last:   map[string]*protocol.DeployStatus{},
	}
}

// Status returns the latest snapshot for one uuid (active or finished).
func (m *Manager) Status(uuid string) protocol.DeployStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.active[uuid]; ok {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.status
	}
	if s, ok := m.last[uuid]; ok {
		return *s
	}
	return protocol.DeployStatus{UUID: uuid, Active: false}
}

// Start kicks off a deploy. Returns an error if one is already running
// for this uuid, or the request is invalid.
func (m *Manager) Start(req protocol.DeployStartReq) error {
	if req.UUID == "" {
		return errors.New("uuid required")
	}
	if req.DownloadURL == "" || req.DownloadName == "" {
		return errors.New("downloadUrl and downloadName required")
	}
	it, ok := m.mgr.Get(req.UUID)
	if !ok {
		return errors.New("instance not found")
	}
	cfg := it.Config()
	if cfg.Type != "docker" {
		return errors.New("server deploy is docker-only for now")
	}
	if cfg.ManagedVolume == "" || m.vols == nil {
		return errors.New("instance has no managed volume; deploy needs a writable /data")
	}

	m.mu.Lock()
	if _, busy := m.active[req.UUID]; busy {
		m.mu.Unlock()
		return errors.New("a deploy is already running for this instance")
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &deployState{
		status: protocol.DeployStatus{
			UUID: req.UUID, Active: true, Stage: protocol.DeployStageQueued,
			StartedAt: time.Now().Unix(),
		},
		cancel: cancel,
	}
	m.active[req.UUID] = st
	delete(m.last, req.UUID)
	m.mu.Unlock()

	go m.run(ctx, st, req, cfg)
	return nil
}

// finish moves the state out of `active` into `last` so subsequent Get
// calls still see the result for a while after completion.
func (m *Manager) finish(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.active[uuid]; ok {
		st.mu.Lock()
		st.status.Active = false
		st.status.FinishedAt = time.Now().Unix()
		final := st.status
		st.mu.Unlock()
		m.last[uuid] = &final
		delete(m.active, uuid)
	}
}

func (m *Manager) update(st *deployState, fn func(s *protocol.DeployStatus)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.status)
}

func (m *Manager) fail(st *deployState, err error) {
	m.update(st, func(s *protocol.DeployStatus) {
		s.Stage = protocol.DeployStageError
		s.Error = err.Error()
	})
}

// volumeMount returns the host path corresponding to /data for this
// managed volume.
func (m *Manager) volumeMount(name string) string {
	root := m.vols.Root()
	if root == "" {
		return ""
	}
	return filepath.Join(root, name)
}

// needsSetup mirrors the frontend's web/src/components/SetupWizard.tsx
// `needsSetup` predicate: a docker instance whose image / launch
// command / stop directive / port mapping isn't fully configured yet
// is shown to the user as "Setup pending" (configuring) and shouldn't
// accept a deploy. Non-docker instances always have a Command at
// create time so they're never flagged.
func needsSetup(cfg protocol.InstanceConfig) bool {
	if cfg.Type != "docker" {
		return false
	}
	if cfg.Command == "" {
		return true
	}
	if len(cfg.Args) == 0 {
		return true
	}
	if cfg.StopCmd == "" {
		return true
	}
	if len(cfg.DockerPorts) == 0 {
		return true
	}
	return false
}

func (m *Manager) run(ctx context.Context, st *deployState, req protocol.DeployStartReq, cfg protocol.InstanceConfig) {
	defer m.finish(req.UUID)

	dataDir := m.volumeMount(cfg.ManagedVolume)
	if dataDir == "" {
		m.fail(st, errors.New("could not resolve volume mount path"))
		return
	}

	// ----- 1. validate -----
	m.update(st, func(s *protocol.DeployStatus) {
		s.Stage = protocol.DeployStageValidating
		s.MessageKey = "msg.checkingState"
		s.Message = "checking instance state"
	})
	if it, ok := m.mgr.Get(req.UUID); ok {
		switch it.Status() {
		case protocol.StatusRunning, protocol.StatusStarting, protocol.StatusStopping:
			m.fail(st, errors.New("stop the instance before deploying"))
			return
		}
	}
	// "Setup pending" instances haven't been fully configured by the
	// user yet (docker image / command / args / stopCmd / port mapping
	// missing). Deploying onto such an instance would land server jars
	// into an unusable container that can't be started — block here so
	// the user finishes the setup wizard first. Mirrors the UI's
	// "configuring" pseudo-status (web/src/components/SetupWizard.tsx
	// `needsSetup`).
	if needsSetup(cfg) {
		m.fail(st, errors.New("instance is still pending setup; finish the setup wizard before deploying"))
		return
	}
	// Forge / NeoForge need java to run their installer in a one-shot
	// container. Detect the configured image and warn loudly.
	if req.PostInstallCmd != "" {
		image := strings.ToLower(cfg.Command)
		looksJava := strings.Contains(image, "java") ||
			strings.Contains(image, "jre") ||
			strings.Contains(image, "jdk") ||
			strings.Contains(image, "temurin") ||
			strings.Contains(image, "corretto") ||
			strings.Contains(image, "zulu")
		if !looksJava {
			m.fail(st, fmt.Errorf("forge/neoforge installer needs a Java image (current: %q). Pick a temurin/openjdk/corretto image and retry.", cfg.Command))
			return
		}
	}

	// ----- 2. clear /data, preserving nothing (backups live elsewhere) -----
	m.update(st, func(s *protocol.DeployStatus) {
		s.Stage = protocol.DeployStageClearing
		s.MessageKey = "msg.clearing"
		s.Message = "clearing /data"
	})
	if err := clearDir(dataDir); err != nil {
		m.fail(st, fmt.Errorf("clear /data: %w", err))
		return
	}

	// ----- 3. download -----
	m.update(st, func(s *protocol.DeployStatus) {
		s.Stage = protocol.DeployStageDownloading
		s.Percent = 0
		s.BytesDone = 0
		s.BytesTotal = 0
		s.MessageKey = "msg.downloading"
		s.Message = "downloading " + req.DownloadName
	})
	dst := filepath.Join(dataDir, req.DownloadName)
	if err := downloadWithProgress(ctx, req.DownloadURL, dst, st, m); err != nil {
		m.fail(st, fmt.Errorf("download: %w", err))
		return
	}

	// ----- 4. optional installer (Forge / NeoForge) -----
	if req.PostInstallCmd != "" {
		m.update(st, func(s *protocol.DeployStatus) {
			s.Stage = protocol.DeployStageInstalling
			s.Percent = 0
			s.MessageKey = "msg.installer"
			s.Message = "running installer (this can take a minute)"
		})
		if err := runInstallerContainer(ctx, dataDir, cfg.Command, req.PostInstallCmd); err != nil {
			m.fail(st, fmt.Errorf("installer: %w", err))
			return
		}
	}

	// ----- 5. eula.txt + write Args back to instance config -----
	m.update(st, func(s *protocol.DeployStatus) {
		s.Stage = protocol.DeployStageConfiguring
		s.Percent = 100
		s.MessageKey = "msg.writingConfig"
		s.Message = "writing eula.txt and launch args"
	})
	if req.AcceptEula {
		eula := "# generated by TapS\neula=true\n"
		if err := os.WriteFile(filepath.Join(dataDir, "eula.txt"), []byte(eula), 0o644); err != nil {
			m.fail(st, fmt.Errorf("write eula: %w", err))
			return
		}
	}
	if len(req.LaunchArgs) > 0 {
		// Substitute ${MEM} with the instance's container memory limit
		// (e.g. "2g" → "2G" for -Xmx). Falls back to "2G" if unset.
		mem := strings.ToUpper(strings.TrimSpace(cfg.DockerMemory))
		if mem == "" {
			mem = "2G"
		}
		args := make([]string, len(req.LaunchArgs))
		for i, a := range req.LaunchArgs {
			args[i] = strings.ReplaceAll(a, "${MEM}", mem)
		}
		m.mgr.MutateConfig(req.UUID, func(c *protocol.InstanceConfig) {
			c.Args = args
			if c.StopCmd == "" {
				c.StopCmd = "stop"
			}
		})
	}

	// ----- done -----
	m.update(st, func(s *protocol.DeployStatus) {
		s.Stage = protocol.DeployStageDone
		s.Percent = 100
		s.MessageKey = "msg.done"
		s.Message = "deploy complete"
	})
}

// clearDir removes everything inside `dir` but keeps the directory
// itself. Preserves `.taps-backups`, which the backup manager writes
// inside the volume for managed-volume instances so backups count
// against the per-instance disk quota.
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == ".taps-backups" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// downloadWithProgress streams `url` into `dst`, ticking the deploy
// status with bytes-done / total / percent. Big files are common
// (Mojang Vanilla 1.21 jar is ~50MB; Forge installer ~30MB), so the
// 256KB buffer + 200ms throttle keeps status updates cheap.
func downloadWithProgress(ctx context.Context, url, dst string, st *deployState, mgr *Manager) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "TapS-deploy/1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("upstream %d", resp.StatusCode)
	}
	total := resp.ContentLength
	mgr.update(st, func(s *protocol.DeployStatus) { s.BytesTotal = total })

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	var done atomic.Int64
	pr := &progressReader{r: resp.Body, n: &done}
	last := time.Now()
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				d := done.Load()
				mgr.update(st, func(s *protocol.DeployStatus) {
					s.BytesDone = d
					if total > 0 {
						s.Percent = int(d * 100 / total)
					}
				})
				if d > 0 && time.Since(last) > 0 {
					last = time.Now()
				}
				if mgr.Status(st.status.UUID).Stage != protocol.DeployStageDownloading {
					return
				}
			}
		}
	}()

	if _, err := io.Copy(f, pr); err != nil {
		return err
	}
	mgr.update(st, func(s *protocol.DeployStatus) {
		s.Percent = 100
		s.BytesDone = done.Load()
	})
	return nil
}

type progressReader struct {
	r io.Reader
	n *atomic.Int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.n.Add(int64(n))
	}
	return n, err
}

// runInstallerContainer spawns a one-shot container against the
// instance's volume, runs `cmd` via /bin/sh -c. Used for Forge /
// NeoForge installers. The installer typically expands a libraries
// tree and writes a run.sh next to the jar.
func runInstallerContainer(ctx context.Context, dataDir, image, cmd string) error {
	args := []string{
		"run", "--rm", "-w", "/data",
		"-v", dataDir + ":/data",
		image,
		"sh", "-c", cmd,
	}
	c := exec.CommandContext(ctx, "docker", args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run installer: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
