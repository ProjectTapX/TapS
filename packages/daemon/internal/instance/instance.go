package instance

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aymanbagabas/go-pty"

	"github.com/taps/daemon/internal/docker"
	"github.com/taps/shared/protocol"
)

// audit-2026-04-25 MED7: defence-in-depth UUID gate. Every site that
// builds a docker container/argument from cfg.UUID via "taps-"+UUID
// must call this before exec.Command. The panel layer already
// validates instance UUIDs at create/import time, but a stale row
// from a hand-edited DB or a future RPC that forgets validation
// would otherwise let "evil --rm" pass straight through to docker
// as a global flag.
var instanceUUIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validInstanceUUID(s string) error {
	if !instanceUUIDRe.MatchString(s) {
		return fmt.Errorf("instance.invalid_uuid: %q", s)
	}
	return nil
}

// Instance is a single managed process on this Daemon. It runs inside a real
// pseudo-terminal so that interactive programs (vim, htop, MC server consoles
// with ANSI control codes) work as users expect. Docker-typed instances run
// via `docker run -i ...` instead, with plain stdio.
type Instance struct {
	cfg protocol.InstanceConfig

	mu       sync.Mutex
	status   protocol.InstanceStatus
	pty      pty.Pty
	cmd      *pty.Cmd       // PTY-backed command
	plainCmd *exec.Cmd      // exec.Cmd path (docker fallback)
	stdin    io.WriteCloser // for plainCmd path
	pid      int
	exitCode int
	ptyMode  bool

	mgr *Manager // for restart trigger
	bus *Bus
}

func newInstance(cfg protocol.InstanceConfig, bus *Bus, mgr *Manager) *Instance {
	return &Instance{cfg: cfg, status: protocol.StatusStopped, bus: bus, mgr: mgr}
}

func (i *Instance) Info() protocol.InstanceInfo {
	i.mu.Lock()
	defer i.mu.Unlock()
	return protocol.InstanceInfo{Config: i.cfg, Status: i.status, PID: i.pid, ExitCode: i.exitCode}
}

func (i *Instance) Status() protocol.InstanceStatus {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.status
}

func (i *Instance) Config() protocol.InstanceConfig {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.cfg
}

func (i *Instance) UpdateConfig(cfg protocol.InstanceConfig) {
	i.mu.Lock()
	defer i.mu.Unlock()
	cfg.UUID = i.cfg.UUID
	i.cfg = cfg
}

func (i *Instance) setStatus(s protocol.InstanceStatus) {
	i.status = s
	i.bus.Publish(i.cfg.UUID, EventStatus, s)
	if i.mgr != nil && i.mgr.hib != nil {
		// Hib hooks call back into MutateConfig which takes this instance's
		// i.mu. setStatus is called while the caller already holds i.mu, so
		// we must hand off to a goroutine to avoid self-deadlock.
		hib := i.mgr.hib
		uuid := i.cfg.UUID
		switch s {
		case protocol.StatusRunning:
			go hib.MarkStarted(uuid)
		case protocol.StatusStopped, protocol.StatusCrashed:
			go hib.MarkStopped(uuid)
		}
	}
}

// Start launches the configured command.
func (i *Instance) Start(baseDir string) error {
	i.mu.Lock()
	if i.status == protocol.StatusRunning || i.status == protocol.StatusStarting {
		i.mu.Unlock()
		return errors.New("already running")
	}
	if i.cfg.Command == "" {
		i.mu.Unlock()
		return errors.New("empty command")
	}
	if i.mgr != nil && i.mgr.RequireDocker() && i.cfg.Type != "docker" {
		i.mu.Unlock()
		return errors.New("docker-only mode: this daemon refuses to spawn non-docker instances. Set type=docker.")
	}
	// If we're hibernating, the fake SLP listener still owns the host
	// port. Close it synchronously so the docker run below can bind.
	// Hib state cleanup (HibernationActive=false, idle counter reset) is
	// handled by setStatus(Running) → MarkStarted later in this Start.
	wasHibernating := i.status == protocol.StatusHibernating
	i.mu.Unlock()
	if wasHibernating && i.mgr != nil && i.mgr.hib != nil {
		i.mgr.hib.CloseFake(i.cfg.UUID)
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	wd := i.cfg.WorkingDir
	if wd == "" {
		wd = "."
	}
	if !filepath.IsAbs(wd) {
		wd = filepath.Join(baseDir, wd)
	}

	if i.cfg.Type == "docker" {
		return i.startDocker(wd)
	}
	return i.startPty(wd)
}

func (i *Instance) startPty(wd string) error {
	pt, err := pty.New()
	if err != nil {
		return err
	}

	var name string
	var args []string
	if runtime.GOOS == "windows" {
		name = "cmd"
		args = append([]string{"/c", i.cfg.Command}, i.cfg.Args...)
	} else {
		name = "sh"
		args = append([]string{"-lc", i.cfg.Command}, i.cfg.Args...)
	}
	if abs, err := exec.LookPath(name); err == nil {
		name = abs
	}
	cmd := pt.Command(name, args...)
	cmd.Dir = wd

	if err := cmd.Start(); err != nil {
		pt.Close()
		return err
	}
	i.pty = pt
	i.cmd = cmd
	i.ptyMode = true
	if cmd.Process != nil {
		i.pid = cmd.Process.Pid
	}
	i.exitCode = 0
	i.setStatus(protocol.StatusRunning)

	uuid := i.cfg.UUID
	enc := i.cfg.OutputEncoding
	go i.pumpReader(uuid, enc, pt)
	go i.waitPty(cmd, pt)
	return nil
}

func (i *Instance) startDocker(wd string) error {
	// Audit-2026-04-24-v3 H5: validate the image reference before
	// it lands as a positional in `docker run`. Without this, an
	// image of "--config=/tmp/x" gets parsed as a docker global
	// flag and silently retargets the daemon socket / auth dir.
	// `docker run` doesn't support `--` flag-termination the way
	// `docker exec` does, so strict validation is the only line of
	// defence at this site.
	if err := docker.ValidImage(i.cfg.Command); err != nil {
		return err
	}
	// audit-2026-04-25 MED7: container name is "taps-"+UUID; UUID
	// must match the canonical hex shape so it can't smuggle a
	// flag-shaped token through `--name`.
	if err := validInstanceUUID(i.cfg.UUID); err != nil {
		return err
	}
	dockerArgs := []string{"run", "-i", "--rm", "--pull", "never", "--name", "taps-" + i.cfg.UUID}
	dockerArgs = append(dockerArgs, "-w", "/data")
	for _, kv := range i.cfg.DockerEnv {
		if strings.TrimSpace(kv) != "" {
			dockerArgs = append(dockerArgs, "-e", kv)
		}
	}
	for _, v := range i.cfg.DockerVolumes {
		if strings.TrimSpace(v) != "" {
			dockerArgs = append(dockerArgs, "-v", v)
		}
	}
	for _, p := range i.cfg.DockerPorts {
		if strings.TrimSpace(p) != "" {
			dockerArgs = append(dockerArgs, "-p", p)
		}
	}
	if cpu := strings.TrimSpace(i.cfg.DockerCPU); cpu != "" {
		dockerArgs = append(dockerArgs, "--cpus", cpu)
	}
	if mem := strings.TrimSpace(i.cfg.DockerMemory); mem != "" {
		dockerArgs = append(dockerArgs, "--memory", mem)
	}
	dockerArgs = append(dockerArgs, i.cfg.Command)
	dockerArgs = append(dockerArgs, i.cfg.Args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Dir = wd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	i.plainCmd = cmd
	i.stdin = stdin
	i.ptyMode = false
	i.pid = cmd.Process.Pid
	i.exitCode = 0
	i.setStatus(protocol.StatusRunning)

	uuid := i.cfg.UUID
	enc := i.cfg.OutputEncoding
	go i.pumpReader(uuid, enc, stdout)
	go i.pumpReader(uuid, enc, stderr)
	go i.waitPlain(cmd)
	return nil
}

func (i *Instance) pumpReader(uuid, encName string, r io.Reader) {
	br := bufio.NewReader(r)
	buf := make([]byte, 4096)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			i.bus.Publish(uuid, EventOutput, decodeToUTF8(encName, buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

func (i *Instance) waitPty(cmd *pty.Cmd, pt pty.Pty) {
	err := cmd.Wait()
	pt.Close()
	i.afterExit(err)
}

func (i *Instance) waitPlain(cmd *exec.Cmd) {
	err := cmd.Wait()
	i.afterExit(err)
}

func (i *Instance) afterExit(err error) {
	i.mu.Lock()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			i.exitCode = ee.ExitCode()
		} else {
			i.exitCode = -1
		}
	} else {
		i.exitCode = 0
	}
	wasStopping := i.status == protocol.StatusStopping
	if wasStopping {
		i.setStatus(protocol.StatusStopped)
	} else {
		i.setStatus(protocol.StatusCrashed)
	}
	i.pid = 0
	i.stdin = nil
	i.pty = nil
	i.cmd = nil
	i.plainCmd = nil
	uuid := i.cfg.UUID
	autoRestart := i.cfg.AutoRestart && !wasStopping
	delay := i.cfg.RestartDelay
	if delay <= 0 {
		delay = 5
	}
	i.mu.Unlock()

	if autoRestart && i.mgr != nil {
		log.Printf("instance %s exited; auto-restart in %ds", uuid, delay)
		time.Sleep(time.Duration(delay) * time.Second)
		if err := i.Start(i.mgr.BaseDir()); err != nil {
			log.Printf("instance %s auto-restart failed: %v", uuid, err)
		}
	}
}

func (i *Instance) Input(data string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	encoded := encodeFromUTF8(i.cfg.OutputEncoding, data)
	if i.ptyMode {
		if i.pty == nil {
			return errors.New("not running")
		}
		_, err := i.pty.Write(encoded)
		return err
	}
	if i.stdin == nil {
		return errors.New("not running")
	}
	_, err := i.stdin.Write(encoded)
	return err
}

func (i *Instance) Resize(cols, rows uint16) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.ptyMode || i.pty == nil {
		return nil
	}
	return i.pty.Resize(int(cols), int(rows))
}

// Stop sends the configured stop command via stdin if any, else delegates to Kill.
func (i *Instance) Stop() error {
	i.mu.Lock()
	if i.status != protocol.StatusRunning {
		i.mu.Unlock()
		return errors.New("not running")
	}
	if i.cfg.Type == "docker" {
		i.setStatus(protocol.StatusStopping)
		uuid := i.cfg.UUID
		i.mu.Unlock()
		// audit-2026-04-25 MED7: defence-in-depth UUID check before
		// piping into docker stop.
		if err := validInstanceUUID(uuid); err != nil {
			return err
		}
		_ = exec.Command("docker", "stop", "taps-"+uuid).Run()
		return nil
	}
	stop := i.cfg.StopCmd
	i.setStatus(protocol.StatusStopping)
	canWrite := i.pty != nil || i.stdin != nil
	i.mu.Unlock()
	if stop != "" && canWrite {
		_ = i.Input(stop + "\n")
		go func() {
			t := time.NewTimer(15 * time.Second)
			defer t.Stop()
			<-t.C
			i.mu.Lock()
			if i.status == protocol.StatusStopping {
				i.mu.Unlock()
				_ = i.Kill()
				return
			}
			i.mu.Unlock()
		}()
		return nil
	}
	return i.Kill()
}

func (i *Instance) Kill() error {
	i.mu.Lock()
	isDocker := i.cfg.Type == "docker"
	uuid := i.cfg.UUID
	cmd := i.cmd
	plain := i.plainCmd
	if i.status == protocol.StatusRunning {
		i.setStatus(protocol.StatusStopping)
	}
	// A crashed instance has no live process to signal but the user is
	// almost always asking us to "make this not be in a broken state" so
	// they can edit / restart / resize. Treat Kill on crashed as a state
	// reset to Stopped so downstream operations (disk resize, etc.) that
	// require StatusStopped go through.
	if i.status == protocol.StatusCrashed {
		i.setStatus(protocol.StatusStopped)
		i.pid = 0
		i.exitCode = 0
		i.mu.Unlock()
		// Belt-and-suspenders: in case the docker container is somehow
		// still around (orphaned by a daemon restart), tell docker to
		// kill it. Errors are ignored — we already returned a clean
		// state to the user.
		// audit-2026-04-25 MED7: skip the docker calls if UUID is
		// somehow malformed; the container could not have been ours.
		if isDocker && validInstanceUUID(uuid) == nil {
			_ = exec.Command("docker", "kill", "taps-"+uuid).Run()
			_ = exec.Command("docker", "rm", "-f", "taps-"+uuid).Run()
		}
		return nil
	}
	// Hibernating means the real container is gone but the fake SLP
	// listener is holding the host port. Treat Kill as "release the
	// port and go to Stopped". setStatus(Stopped) fires MarkStopped on
	// a goroutine which closes the fake listener and clears hib state.
	if i.status == protocol.StatusHibernating {
		i.setStatus(protocol.StatusStopped)
		i.pid = 0
		i.exitCode = 0
		i.mu.Unlock()
		return nil
	}
	i.mu.Unlock()
	if isDocker {
		// audit-2026-04-25 MED7: validate UUID before exec.Command so a
		// bad row can't sneak a flag-shaped token through docker kill.
		if err := validInstanceUUID(uuid); err != nil {
			return err
		}
		_ = exec.Command("docker", "kill", "taps-"+uuid).Run()
		return nil
	}
	if cmd != nil && cmd.Process != nil {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	if plain != nil && plain.Process != nil {
		if runtime.GOOS == "windows" {
			return plain.Process.Kill()
		}
		return plain.Process.Signal(syscall.SIGTERM)
	}
	return errors.New("not running")
}

// ----- Manager -----

type Manager struct {
	baseDir       string
	storePath     string
	bus           *Bus
	requireDocker bool
	vols          VolumeProvider
	hib           HibernationHook
	mu            sync.Mutex
	items         map[string]*Instance
}

// HibernationHook is the slice of hibernation.Manager the instance
// manager calls to keep hibernation state in sync with start/stop.
// Optional — nil means hibernation is disabled.
type HibernationHook interface {
	MarkStarted(uuid string)
	MarkStopped(uuid string)
	IsHibernating(uuid string) bool
	CloseFake(uuid string)
}

func (m *Manager) SetHibernation(h HibernationHook) { m.hib = h }

func NewManager(baseDir string, bus *Bus) *Manager {
	_ = os.MkdirAll(baseDir, 0o755)
	return &Manager{
		baseDir:   baseDir,
		storePath: filepath.Join(baseDir, "..", "instances.json"),
		bus:       bus,
		items:     map[string]*Instance{},
	}
}

func (m *Manager) SetRequireDocker(v bool) { m.requireDocker = v }
func (m *Manager) RequireDocker() bool     { return m.requireDocker }

func (m *Manager) Bus() *Bus       { return m.bus }
func (m *Manager) BaseDir() string { return m.baseDir }

func (m *Manager) List() []protocol.InstanceInfo {
	// Snapshot the items map under m.mu, then release the lock before
	// touching each instance's i.mu via Info(). Without this, one stuck
	// instance (i.mu held by a slow Start/Input/Stop) would freeze the
	// entire Manager since List would block on it.Info() while holding
	// m.mu, queuing every other List/Update/Delete behind it.
	m.mu.Lock()
	items := make([]*Instance, 0, len(m.items))
	for _, it := range m.items {
		items = append(items, it)
	}
	m.mu.Unlock()
	out := make([]protocol.InstanceInfo, 0, len(items))
	for _, it := range items {
		out = append(out, it.Info())
	}
	return out
}

func (m *Manager) Get(uuid string) (*Instance, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[uuid]
	return it, ok
}

// AutoStartAll starts every instance with cfg.AutoStart=true. Called once
// by the daemon at startup, after Load.
func (m *Manager) AutoStartAll() {
	m.mu.Lock()
	candidates := make([]*Instance, 0)
	for _, it := range m.items {
		if it.Config().AutoStart {
			candidates = append(candidates, it)
		}
	}
	m.mu.Unlock()
	for _, it := range candidates {
		if err := it.Start(m.baseDir); err != nil {
			log.Printf("autoStart %s: %v", it.Config().UUID, err)
		}
	}
}
