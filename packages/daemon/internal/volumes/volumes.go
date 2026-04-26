// Package volumes manages fixed-size loopback volumes on the daemon host.
//
// A "managed volume" is a regular file on the host (an .img) that has been
// formatted with a filesystem and mounted at a known path. Users then map
// that path into a container in the instance's DockerVolumes config — the
// container effectively gets a hard quota equal to the file size.
//
// Layout (under <daemonData>/volumes/):
//
//	<name>.img    the loopback image file
//	<name>.json   metadata (size, fs type, created)
//	<name>/       mount point
//
// All operations require the daemon to run as root because mount/mkfs need
// root. Our systemd unit already runs as root.

//go:build linux

package volumes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/taps/shared/protocol"
)

type Manager struct {
	root string
	mu   sync.Mutex
}

func New(daemonData string) *Manager {
	root := filepath.Join(daemonData, "volumes")
	_ = os.MkdirAll(root, 0o755)
	return &Manager{root: root}
}

// Root returns the directory where managed volume mount points live. Other
// daemon subsystems use this to drop sibling per-instance directories that
// should appear under /data in the file manager.
func (m *Manager) Root() string { return m.root }

// Available reports whether the host has the tools needed for managed volumes
// (mkfs.ext4 + mount + umount + losetup) and the daemon process can mount.
func (m *Manager) Available() bool {
	for _, b := range []string{"mkfs.ext4", "mount", "umount", "losetup"} {
		if _, err := exec.LookPath(b); err != nil {
			return false
		}
	}
	return os.Geteuid() == 0
}

// resizeAvailable additionally requires e2fsck + resize2fs (only ext4 grows
// are supported through this path).
func (m *Manager) resizeAvailable() bool {
	for _, b := range []string{"e2fsck", "resize2fs"} {
		if _, err := exec.LookPath(b); err != nil {
			return false
		}
	}
	return m.Available()
}

// Resize grows an existing managed volume to newSize. Shrinking is rejected
// because resize2fs needs a smaller-than-current target that we'd have to
// compute from used space, with real risk of truncating live data. The
// instance must be stopped before calling — unmounting a loopback that's
// bind-mounted into a running container fails.
func (m *Manager) Resize(name string, newSize int64) error {
	if !m.resizeAvailable() {
		return errors.New("resize requires e2fsck + resize2fs + root")
	}
	if _, err := sanitize(name); err != nil {
		return err
	}
	meta, err := m.loadMeta(name)
	if err != nil {
		return err
	}
	if newSize == meta.SizeBytes {
		return nil
	}
	if newSize < meta.SizeBytes {
		return fmt.Errorf("shrink not supported (current %d B, requested %d B)", meta.SizeBytes, newSize)
	}
	if meta.FsType != "" && meta.FsType != "ext4" {
		return fmt.Errorf("resize only supported on ext4, this volume is %s", meta.FsType)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	mp := m.mountPath(name)
	if isMounted(mp) {
		if out, err := exec.Command("umount", mp).CombinedOutput(); err != nil {
			return fmt.Errorf("umount: %s", strings.TrimSpace(string(out)))
		}
	}
	if err := exec.Command("truncate", "-s", fmt.Sprintf("%d", newSize), m.imagePath(name)).Run(); err != nil {
		// Best-effort remount before returning so the instance dir comes back.
		_ = exec.Command("mount", "-o", "loop", m.imagePath(name), mp).Run()
		return fmt.Errorf("truncate: %w", err)
	}
	// e2fsck exit codes: 0 = clean, 1 = errors corrected, 2 = reboot needed,
	// >=4 = fatal. Treat 0 and 1 as success.
	if out, err := exec.Command("e2fsck", "-fy", m.imagePath(name)).CombinedOutput(); err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() > 1 {
			_ = exec.Command("mount", "-o", "loop", m.imagePath(name), mp).Run()
			return fmt.Errorf("e2fsck: %s", strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("resize2fs", m.imagePath(name)).CombinedOutput(); err != nil {
		_ = exec.Command("mount", "-o", "loop", m.imagePath(name), mp).Run()
		return fmt.Errorf("resize2fs: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("mount", "-o", "loop", m.imagePath(name), mp).CombinedOutput(); err != nil {
		return fmt.Errorf("mount: %s", strings.TrimSpace(string(out)))
	}
	meta.SizeBytes = newSize
	return m.saveMeta(meta)
}

func (m *Manager) imagePath(name string) string  { return filepath.Join(m.root, name+".img") }
func (m *Manager) metaPath(name string) string   { return filepath.Join(m.root, name+".json") }
func (m *Manager) mountPath(name string) string  { return filepath.Join(m.root, name) }

func sanitize(name string) (string, error) {
	if name == "" {
		return "", errors.New("name required")
	}
	if len(name) > 48 {
		return "", errors.New("name too long")
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return "", errors.New("name may only contain letters, digits, '-' and '_'")
		}
	}
	return name, nil
}

type meta struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	FsType    string `json:"fsType"`
	CreatedAt int64  `json:"createdAt"`
}

func (m *Manager) loadMeta(name string) (meta, error) {
	var x meta
	b, err := os.ReadFile(m.metaPath(name))
	if err != nil {
		return x, err
	}
	err = json.Unmarshal(b, &x)
	return x, err
}

func (m *Manager) saveMeta(x meta) error {
	b, _ := json.MarshalIndent(x, "", "  ")
	return os.WriteFile(m.metaPath(x.Name), b, 0o644)
}

// List returns every managed volume the daemon knows about.
func (m *Manager) List() (protocol.VolumeListResp, error) {
	if !m.Available() {
		return protocol.VolumeListResp{Available: false, Error: "managed volumes require root + mkfs.ext4 / mount / losetup"}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ents, err := os.ReadDir(m.root)
	if err != nil {
		return protocol.VolumeListResp{Available: true, Error: err.Error()}, nil
	}
	out := protocol.VolumeListResp{Available: true, Volumes: []protocol.Volume{}}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		x, err := m.loadMeta(name)
		if err != nil {
			continue
		}
		v := protocol.Volume{
			Name:      x.Name,
			SizeBytes: x.SizeBytes,
			FsType:    x.FsType,
			ImagePath: m.imagePath(name),
			MountPath: m.mountPath(name),
			CreatedAt: x.CreatedAt,
			Mounted:   isMounted(m.mountPath(name)),
		}
		if v.Mounted {
			if free, used := diskUsage(v.MountPath); free > 0 {
				v.UsedBytes = used
			}
		}
		out.Volumes = append(out.Volumes, v)
	}
	sort.Slice(out.Volumes, func(i, j int) bool { return out.Volumes[i].CreatedAt > out.Volumes[j].CreatedAt })
	return out, nil
}

func (m *Manager) Create(req protocol.VolumeCreateReq) (protocol.Volume, error) {
	if !m.Available() {
		return protocol.Volume{}, errors.New("managed volumes not available")
	}
	name, err := sanitize(req.Name)
	if err != nil {
		return protocol.Volume{}, err
	}
	if req.SizeBytes < 64*1024*1024 {
		return protocol.Volume{}, errors.New("size must be at least 64 MiB")
	}
	if req.FsType == "" {
		req.FsType = "ext4"
	}
	if req.FsType != "ext4" && req.FsType != "xfs" {
		return protocol.Volume{}, errors.New("unsupported fsType (use ext4 or xfs)")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := os.Stat(m.imagePath(name)); err == nil {
		return protocol.Volume{}, errors.New("volume already exists")
	}

	// 1. allocate the image file (sparse, fast)
	if err := exec.Command("truncate", "-s", fmt.Sprintf("%d", req.SizeBytes), m.imagePath(name)).Run(); err != nil {
		return protocol.Volume{}, fmt.Errorf("truncate: %w", err)
	}
	// 2. mkfs
	mkfs := "mkfs." + req.FsType
	if out, err := exec.Command(mkfs, "-F", m.imagePath(name)).CombinedOutput(); err != nil {
		_ = os.Remove(m.imagePath(name))
		return protocol.Volume{}, fmt.Errorf("%s: %s", mkfs, strings.TrimSpace(string(out)))
	}
	// 3. mkdir + mount
	if err := os.MkdirAll(m.mountPath(name), 0o755); err != nil {
		return protocol.Volume{}, err
	}
	if out, err := exec.Command("mount", "-o", "loop", m.imagePath(name), m.mountPath(name)).CombinedOutput(); err != nil {
		_ = os.Remove(m.imagePath(name))
		_ = os.Remove(m.mountPath(name))
		return protocol.Volume{}, fmt.Errorf("mount: %s", strings.TrimSpace(string(out)))
	}
	// 4. metadata
	x := meta{Name: name, SizeBytes: req.SizeBytes, FsType: req.FsType, CreatedAt: time.Now().Unix()}
	if err := m.saveMeta(x); err != nil {
		return protocol.Volume{}, err
	}
	return protocol.Volume{
		Name:      name,
		SizeBytes: req.SizeBytes,
		FsType:    req.FsType,
		ImagePath: m.imagePath(name),
		MountPath: m.mountPath(name),
		Mounted:   true,
		CreatedAt: x.CreatedAt,
	}, nil
}

func (m *Manager) Remove(name string) error {
	if !m.Available() {
		return errors.New("managed volumes not available")
	}
	if _, err := sanitize(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mp := m.mountPath(name)
	if isMounted(mp) {
		if out, err := exec.Command("umount", mp).CombinedOutput(); err != nil {
			return fmt.Errorf("umount: %s", strings.TrimSpace(string(out)))
		}
	}
	_ = os.Remove(m.imagePath(name))
	_ = os.Remove(m.metaPath(name))
	_ = os.Remove(mp)
	return nil
}

// MountAll re-mounts every known volume. Called on daemon startup.
func (m *Manager) MountAll() {
	if !m.Available() {
		return
	}
	resp, _ := m.List()
	for _, v := range resp.Volumes {
		if v.Mounted {
			continue
		}
		_ = os.MkdirAll(v.MountPath, 0o755)
		_ = exec.Command("mount", "-o", "loop", v.ImagePath, v.MountPath).Run()
	}
}

// UnmountAll lazy-unmounts every currently-mounted managed volume.
// Called from daemon graceful shutdown (audit-2026-04-25 MED4) so a
// systemctl restart leaves no dangling loop devices that would block
// the next MountAll. -l (lazy) so an in-flight container doesn't
// pin us; the loop is detached as soon as no fd holds it open.
func (m *Manager) UnmountAll() error {
	if !m.Available() {
		return nil
	}
	resp, _ := m.List()
	var firstErr error
	for _, v := range resp.Volumes {
		if !v.Mounted {
			continue
		}
		if out, err := exec.Command("umount", "-l", v.MountPath).CombinedOutput(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("umount %s: %v (%s)", v.MountPath, err, strings.TrimSpace(string(out)))
			}
		}
	}
	return firstErr
}

// isMounted: look up in /proc/self/mountinfo for an entry whose mount point
// equals path. Avoid shelling out to `findmnt` to keep deps minimal.
func isMounted(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	target := " " + abs + " "
	return strings.Contains(string(b), target)
}

// diskUsage returns (free, used) bytes for a mount point.
func diskUsage(path string) (free, used int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	free = int64(st.Bavail) * bsize
	used = total - free
	return
}

// FreeBytesAt is a public wrapper around diskUsage so callers outside
// the package (the upload-session quota check) can ask "how much room
// does the volume holding this path have left?" without re-implementing
// the syscall.
func FreeBytesAt(path string) int64 {
	free, _ := diskUsage(path)
	return free
}
