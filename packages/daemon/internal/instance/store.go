package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// VolumeProvider is the subset of the volumes manager that instance.Manager
// needs. We use an interface so the instance package doesn't depend on the
// linux-only volumes implementation directly.
type VolumeProvider interface {
	Available() bool
	Root() string
	Create(req protocol.VolumeCreateReq) (protocol.Volume, error)
	Remove(name string) error
	Resize(name string, newSize int64) error
}

// SetVolumes wires a volume provider so that creating a docker instance with a
// non-empty DockerDiskSize auto-allocates a managed volume.
func (m *Manager) SetVolumes(v VolumeProvider) { m.vols = v }

// Load reads instances.json from disk.
func (m *Manager) Load() error {
	b, err := os.ReadFile(m.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfgs []protocol.InstanceConfig
	if err := json.Unmarshal(b, &cfgs); err != nil {
		return err
	}
	// Backfill CreatedAt for instances created before this field existed
	// so the panel can sort them stably. Stagger by index so the order in
	// the on-disk file is preserved (older entries get smaller timestamps).
	now := time.Now().Unix()
	dirty := false
	for i := range cfgs {
		if cfgs[i].CreatedAt == 0 {
			cfgs[i].CreatedAt = now - int64(len(cfgs)-i)
			dirty = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range cfgs {
		m.items[c.UUID] = newInstance(c, m.bus, m)
	}
	if dirty {
		// Persist the backfill so subsequent loads don't have to repeat
		// it (and so the timestamps don't shift on every restart).
		_ = m.saveLocked()
	}
	return nil
}

// saveLocked writes the current items to disk; caller must hold m.mu.
func (m *Manager) saveLocked() error {
	out := make([]protocol.InstanceConfig, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, it.Config())
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.storePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.storePath)
}

func (m *Manager) save() error {
	out := make([]protocol.InstanceConfig, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, it.Config())
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.storePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.storePath)
}

func (m *Manager) Create(cfg protocol.InstanceConfig) (*Instance, error) {
	// Empty command is allowed for docker instances at create time so the
	// user can run the new-user setup wizard afterwards. Start() refuses
	// to launch with empty command in either path.
	if cfg.Command == "" && cfg.Type != "docker" {
		return nil, errors.New("command required")
	}
	if m.requireDocker && cfg.Type != "docker" {
		return nil, errors.New("docker-only mode: only type=docker instances are allowed on this daemon")
	}
	// Resource limits are required for docker instances — running an
	// unbounded container can starve the host. The panel form enforces
	// this in the UI but defence in depth.
	if cfg.Type == "docker" {
		if strings.TrimSpace(cfg.DockerMemory) == "" {
			return nil, errors.New("dockerMemory required for docker instances (e.g. \"2g\")")
		}
		if strings.TrimSpace(cfg.DockerCPU) == "" {
			return nil, errors.New("dockerCpu required for docker instances (e.g. \"1.5\")")
		}
		if strings.TrimSpace(cfg.DockerDiskSize) == "" {
			return nil, errors.New("dockerDiskSize required for docker instances (e.g. \"10g\")")
		}
	}
	if cfg.UUID == "" {
		cfg.UUID = uuid.NewString()
	}
	if cfg.Name == "" {
		cfg.Name = "inst-" + shortUUID(cfg.UUID)
	}
	if cfg.CreatedAt == 0 {
		cfg.CreatedAt = time.Now().Unix()
	}

	// Auto-create a /data target for every docker instance so users can
	// upload files via the file manager and reference them with relative
	// paths inside the container (e.g. `java -jar server.jar`).
	//
	//   * DockerDiskSize set → fixed-size loopback volume (managed)
	//   * else              → plain host directory under <volumesRoot>/inst-<short>
	//
	// Skipped if the user already specified their own bind ending in ":/data".
	if cfg.Type == "docker" && !hasDataMount(cfg.DockerVolumes) && cfg.ManagedVolume == "" && !cfg.AutoDataDir && m.vols != nil {
		short := shortUUID(cfg.UUID)
		volName := "inst-" + short
		if strings.TrimSpace(cfg.DockerDiskSize) != "" && m.vols.Available() {
			size, err := parseSize(cfg.DockerDiskSize)
			if err != nil {
				return nil, fmt.Errorf("dockerDiskSize: %w", err)
			}
			v, err := m.vols.Create(protocol.VolumeCreateReq{Name: volName, SizeBytes: size, FsType: "ext4"})
			if err != nil {
				return nil, fmt.Errorf("auto-create volume: %w", err)
			}
			cfg.ManagedVolume = v.Name
			cfg.DockerVolumes = append(cfg.DockerVolumes, v.MountPath+":/data")
		} else if root := m.vols.Root(); root != "" {
			dir := filepath.Join(root, volName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("auto-create instance dir: %w", err)
			}
			cfg.AutoDataDir = true
			cfg.DockerVolumes = append(cfg.DockerVolumes, dir+":/data")
		}
	}

	m.mu.Lock()
	if _, ok := m.items[cfg.UUID]; ok {
		m.mu.Unlock()
		// Roll back the volume / auto dir we just created on uuid collision.
		if cfg.ManagedVolume != "" && m.vols != nil {
			_ = m.vols.Remove(cfg.ManagedVolume)
		}
		if cfg.AutoDataDir && m.vols != nil {
			if root := m.vols.Root(); root != "" {
				_ = os.RemoveAll(filepath.Join(root, "inst-"+shortUUID(cfg.UUID)))
			}
		}
		return nil, errors.New("uuid exists")
	}
	it := newInstance(cfg, m.bus, m)
	m.items[cfg.UUID] = it
	err := m.save()
	m.mu.Unlock()
	return it, err
}

func (m *Manager) Update(cfg protocol.InstanceConfig) (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[cfg.UUID]
	if !ok {
		return nil, errors.New("not found")
	}
	// Preserve the managed-volume name across updates: editing the form
	// shouldn't drop or rename the auto-created volume.
	prev := it.Config()
	if cfg.Name == "" {
		cfg.Name = prev.Name
	}
	if cfg.Type == "" {
		cfg.Type = prev.Type
	}
	if prev.ManagedVolume != "" && cfg.ManagedVolume == "" {
		cfg.ManagedVolume = prev.ManagedVolume
	}
	if prev.AutoDataDir && !cfg.AutoDataDir {
		cfg.AutoDataDir = true
	}
	// If the user changed dockerDiskSize and we own a managed loopback for
	// this instance, attempt to grow it. Shrinks are rejected by the
	// volumes layer; resize requires the instance to be stopped because
	// we have to umount the loopback.
	if prev.ManagedVolume != "" && cfg.DockerDiskSize != prev.DockerDiskSize && cfg.DockerDiskSize != "" && m.vols != nil {
		newSize, err := parseSize(cfg.DockerDiskSize)
		if err != nil {
			return nil, fmt.Errorf("dockerDiskSize: %w", err)
		}
		if it.Status() != protocol.StatusStopped && it.Status() != protocol.StatusCrashed {
			return nil, errors.New("stop the instance before resizing the disk")
		}
		if err := m.vols.Resize(prev.ManagedVolume, newSize); err != nil {
			return nil, err
		}
	}
	it.UpdateConfig(cfg)
	if err := m.save(); err != nil {
		return nil, err
	}
	return it, nil
}

func (m *Manager) Delete(u string) error {
	m.mu.Lock()
	it, ok := m.items[u]
	m.mu.Unlock()
	if !ok {
		return errors.New("not found")
	}
	_ = it.Kill()
	cfg := it.Config()
	m.mu.Lock()
	delete(m.items, u)
	err := m.save()
	m.mu.Unlock()
	if cfg.ManagedVolume != "" && m.vols != nil {
		_ = m.vols.Remove(cfg.ManagedVolume)
	}
	if cfg.AutoDataDir && m.vols != nil {
		if root := m.vols.Root(); root != "" {
			_ = os.RemoveAll(filepath.Join(root, "inst-"+shortUUID(u)))
		}
	}
	m.bus.ClearHistory(u)
	return err
}

// hasDataMount reports whether any of the user's docker volume binds already
// targets /data inside the container. We use this to skip auto-allocation
// when the user has explicitly wired their own /data.
func hasDataMount(vols []string) bool {
	for _, v := range vols {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		// "host:container[:mode]" — pull container side
		parts := strings.Split(v, ":")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) == "/data" {
			return true
		}
	}
	return false
}

// shortUUID returns the first 12 hex chars of a UUID (with dashes stripped),
// suitable for use as a volume name (sanitize allows letters and digits).
func shortUUID(u string) string {
	clean := strings.ReplaceAll(u, "-", "")
	if len(clean) > 12 {
		clean = clean[:12]
	}
	return clean
}

// InstanceDirName returns the canonical "inst-<short>" name used both as
// the managed-volume name and the auto-allocated directory name. Exported
// so other packages (backup) can compute the same path without depending
// on the volume metadata.
func InstanceDirName(uuid string) string { return "inst-" + shortUUID(uuid) }

// parseSize accepts strings like "10g", "500M", "2GB", "1024" (bytes).
// Returns the size in bytes. Suffixes: k, m, g, t (case-insensitive, optional B).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	s = strings.TrimSuffix(strings.ToLower(s), "b")
	mult := int64(1)
	switch s[len(s)-1] {
	case 'k':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'm':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'g':
		mult = 1 << 30
		s = s[:len(s)-1]
	case 't':
		mult = 1 << 40
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	if n <= 0 {
		return 0, errors.New("size must be > 0")
	}
	return int64(n * float64(mult)), nil
}

// ----- HibernationHook helpers used by hibernation.Manager via the
// InstanceProvider interface defined in that package. -----

// StartByUUID is the wake-on-connect entry point — same as Instance.Start
// but lookup-by-uuid since the hibernation manager only has the uuid.
func (m *Manager) StartByUUID(uuid string) error {
	it, ok := m.Get(uuid)
	if !ok {
		return errors.New("not found")
	}
	return it.Start(m.baseDir)
}

// StopByUUID kicks off a graceful Stop (via cfg.StopCmd if any).
func (m *Manager) StopByUUID(uuid string) error {
	it, ok := m.Get(uuid)
	if !ok {
		return errors.New("not found")
	}
	return it.Stop()
}

// SetExternalStatus lets the hibernation manager mark an instance as
// hibernating. We bypass the normal setStatus path so the hib callbacks
// don't fire recursively.
func (m *Manager) SetExternalStatus(uuid string, s protocol.InstanceStatus) {
	it, ok := m.Get(uuid)
	if !ok {
		return
	}
	it.mu.Lock()
	it.status = s
	uuidCopy := it.cfg.UUID
	it.mu.Unlock()
	it.bus.Publish(uuidCopy, EventStatus, s)
}

// MutateConfig applies a function to the in-memory cfg of one instance
// and persists the result to instances.json. Used by hibernation to
// flip HibernationActive on/off.
func (m *Manager) MutateConfig(uuid string, mutate func(*protocol.InstanceConfig)) {
	it, ok := m.Get(uuid)
	if !ok {
		return
	}
	it.mu.Lock()
	mutate(&it.cfg)
	it.mu.Unlock()
	m.mu.Lock()
	_ = m.save()
	m.mu.Unlock()
}
