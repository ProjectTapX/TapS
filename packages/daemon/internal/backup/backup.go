// Package backup creates and restores zip snapshots of one instance's
// working directory. Backups live alongside the instance dir under
// <baseDir>/backups/<uuid>/<name>.zip.
package backup

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

type Manager struct {
	instancesRoot string // where instance working dirs live
	backupsRoot   string // where backup zips are written for instances without a custom dir

	// volumesRoot is the on-disk root of managed volumes (typically
	// <DataDir>/volumes). Set via SetVolumesRoot at daemon boot. Used
	// only by Restore's double-root containment check (audit-2026-04-25
	// MED1) — left empty when the daemon doesn't expose volumes.
	volumesRoot string

	// dirResolver, when set, is asked for the per-instance backup directory
	// before falling back to backupsRoot/<uuid>. We use it to redirect
	// backups for instances that own a managed loopback volume so the zips
	// land inside the volume and count against the disk quota.
	dirResolver func(uuid string) string
}

func New(instancesRoot, backupsRoot string) *Manager {
	_ = os.MkdirAll(backupsRoot, 0o755)
	return &Manager{instancesRoot: instancesRoot, backupsRoot: backupsRoot}
}

// SetVolumesRoot wires the managed-volumes root for the Restore
// containment check (audit-2026-04-25 MED1). Restore will refuse a
// dest path that lives outside both instancesRoot and volumesRoot
// even when the panel layer was attacker-controlled.
func (m *Manager) SetVolumesRoot(root string) { m.volumesRoot = root }

// SetDirResolver registers a callback used to override the storage location
// of backups for a given instance. The callback returns "" when the
// instance has no special location (use the default backupsRoot/uuid).
func (m *Manager) SetDirResolver(fn func(uuid string) string) { m.dirResolver = fn }

func (m *Manager) backupsDir(uuid string) string {
	if m.dirResolver != nil {
		if d := m.dirResolver(uuid); d != "" {
			return d
		}
	}
	return filepath.Join(m.backupsRoot, uuid)
}

// instanceDir resolves the instance working directory used by Manager.Start.
// We accept the working dir from the caller because backups are agnostic to
// what's inside; we just zip whatever is there.
func (m *Manager) resolveInstanceDir(workingDir string) string {
	if workingDir == "" {
		return m.instancesRoot
	}
	if filepath.IsAbs(workingDir) {
		return workingDir
	}
	return filepath.Join(m.instancesRoot, workingDir)
}

func (m *Manager) List(uuid string) ([]protocol.BackupEntry, error) {
	dir := m.backupsDir(uuid)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []protocol.BackupEntry{}, nil
		}
		return nil, err
	}
	out := make([]protocol.BackupEntry, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, protocol.BackupEntry{
			Name:         e.Name(),
			Size:         fi.Size(),
			Created:      fi.ModTime().Unix(),
			InstanceUUID: uuid,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

func (m *Manager) Create(uuid, workingDir, note string) (protocol.BackupEntry, error) {
	src := m.resolveInstanceDir(workingDir)
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return protocol.BackupEntry{}, errors.New("instance dir not found")
	}
	if err := os.MkdirAll(m.backupsDir(uuid), 0o755); err != nil {
		return protocol.BackupEntry{}, err
	}
	stamp := time.Now().Format("20060102-150405")
	name := stamp
	if note != "" {
		name += "-" + sanitize(note)
	}
	name += ".zip"
	out := filepath.Join(m.backupsDir(uuid), name)

	if err := zipDir(src, out); err != nil {
		_ = os.Remove(out)
		return protocol.BackupEntry{}, err
	}
	fi, err := os.Stat(out)
	if err != nil {
		return protocol.BackupEntry{}, err
	}
	return protocol.BackupEntry{
		Name:         name,
		Size:         fi.Size(),
		Created:      fi.ModTime().Unix(),
		InstanceUUID: uuid,
	}, nil
}

// backupNameRe restricts backup file names to a safe character set
// (mirrors the panel-side validator). Defense-in-depth on the daemon
// in case a future caller path forgets to validate.
var backupNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}\.zip$`)

func validBackupName(name string) error {
	if !backupNameRe.MatchString(name) {
		return errors.New("invalid backup name")
	}
	return nil
}

func (m *Manager) Restore(uuid, workingDir, name string) error {
	if err := validUUID(uuid); err != nil {
		return err
	}
	if err := validBackupName(name); err != nil {
		return err
	}
	src := filepath.Join(m.backupsDir(uuid), name)
	dest := m.resolveInstanceDir(workingDir)
	// audit-2026-04-25 MED1: refuse to unzip into a path outside the
	// daemon's two managed roots. Without this an attacker who'd
	// already taken the panel could set instance.workingDir = "/etc"
	// and trigger Restore to overwrite system files. zip-entry
	// containment alone (in unzipInto) only stops escapes *out of*
	// dest — it can't reject a hostile dest itself.
	if err := m.containedInManaged(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return unzipInto(src, dest)
}

// containedInManaged reports nil when `dest` resolves under either
// instancesRoot or volumesRoot. Returns the structured error code
// "instance.dest_outside_managed_roots" so daemon RPC + panel
// proxy + frontend can route it through the standard i18n table.
func (m *Manager) containedInManaged(dest string) error {
	if err := containedIn(dest, m.instancesRoot); err == nil {
		return nil
	}
	if m.volumesRoot != "" {
		if err := containedIn(dest, m.volumesRoot); err == nil {
			return nil
		}
	}
	return errors.New("instance.dest_outside_managed_roots: backup restore destination must live under <DataDir>/files or <DataDir>/volumes")
}

func (m *Manager) Delete(uuid, name string) error {
	if err := validUUID(uuid); err != nil {
		return err
	}
	if err := validBackupName(name); err != nil {
		return err
	}
	return os.Remove(filepath.Join(m.backupsDir(uuid), name))
}

// validUUID rejects anything outside the canonical 8-4-4-4-12 hex
// shape used by `model.Instance.UUID`. This is the gate that stops
// `?uuid=../../var/log` from reaching `filepath.Join` and pivoting
// into arbitrary directories on the daemon host (audit-2026-04-24-v3
// finding H4). Lower- and uppercase both accepted because both forms
// have appeared in older instance imports.
var validUUIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validUUID(s string) error {
	if !validUUIDRe.MatchString(s) {
		return fmt.Errorf("invalid uuid")
	}
	return nil
}

// Path returns the absolute path of one backup zip. Used by the daemon's
// HTTP download endpoint. Validates the name shape so the caller can't
// traverse out with "../" or hit unrelated files. The uuid is also
// validated to a strict regex AND a defence-in-depth `filepath.Rel`
// check confirms the resolved path stays under the configured backups
// root regardless of any future regex regression.
func (m *Manager) Path(uuid, name string) (string, error) {
	if err := validUUID(uuid); err != nil {
		return "", err
	}
	if err := validBackupName(name); err != nil {
		return "", err
	}
	abs := filepath.Join(m.backupsDir(uuid), name)
	// Belt-and-suspenders: even if a custom dirResolver returns a path
	// outside backupsRoot/instancesRoot for some uuid, refuse anything
	// that escapes BOTH known roots via `..` traversal. This catches
	// any future bug in dirResolver wiring.
	if err := containedIn(abs, m.backupsRoot); err != nil {
		if err2 := containedIn(abs, m.instancesRoot); err2 != nil {
			return "", fmt.Errorf("backup path escapes managed roots")
		}
	}
	return abs, nil
}

// containedIn returns nil iff `target` (after Clean) is at or under
// `root`. Uses filepath.Rel to detect "../"-leading relative paths.
func containedIn(target, root string) error {
	rt, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	tt, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rt, tt)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes root")
	}
	return nil
}

func zipDir(src, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	// audit-2026-04-25 MED2: walk with Lstat so symlinks aren't
	// silently followed. Resolve any symlink we hit; if the target
	// stays inside `src` the link is a legitimate intra-tree
	// reference (e.g. plugins/-> ../shared/foo.jar in MC servers)
	// and we zip its real content. If it points outside `src` we
	// log + skip so a hostile link can't make zip exfiltrate
	// /etc/passwd.
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		zname := filepath.ToSlash(rel)

		// Lstat-aware re-evaluation: filepath.Walk passes Lstat info for
		// the entry already, but to be explicit and bullet-proof against
		// future Walk semantic changes we re-Lstat here.
		li, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if li.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				log.Printf("[backup-zip] skipping broken symlink %q: %v", path, err)
				return nil
			}
			realAbs, _ := filepath.Abs(real)
			if !strings.HasPrefix(realAbs, srcAbs+string(filepath.Separator)) && realAbs != srcAbs {
				log.Printf("[backup-zip] skipping symlink %q -> %q (escapes backup root)", path, realAbs)
				return nil
			}
			ri, err := os.Stat(real)
			if err != nil {
				log.Printf("[backup-zip] skipping symlink %q (stat target failed): %v", path, err)
				return nil
			}
			info = ri
			path = real
		}

		if info.IsDir() {
			_, err := zw.Create(zname + "/")
			return err
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = zname
		fh.Method = zip.Deflate
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
}

func unzipInto(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, ze := range zr.File {
		// Audit-2026-04-24-v3 H2 (archive variant): refuse symlink
		// entries entirely. Without this, an attacker-uploaded zip
		// can plant a symlink under dest, and the next fs.write to
		// "dest/link" follows it out of the sandbox (daemon root).
		// Hard symlinks in zip carry the Unix mode bit Symlink set;
		// gzipped tar restores would need similar handling but we
		// only support zip today.
		if ze.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip entry %q is a symlink; refused", ze.Name)
		}
		// Refuse entries with a leading "/" or absolute path component
		// — defence-in-depth on top of HasPrefix below.
		if strings.HasPrefix(ze.Name, "/") || strings.Contains(ze.Name, "..") {
			return fmt.Errorf("zip entry %q has illegal name", ze.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(ze.Name))
		if !strings.HasPrefix(target, dest+string(filepath.Separator)) && target != dest {
			return fmt.Errorf("zip entry escapes dest: %s", ze.Name)
		}
		if ze.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := ze.Open()
		if err != nil {
			return err
		}
		// O_NOFOLLOW so that even if a symlink slipped past the mode
		// check (e.g. predicted by a TOCTOU race against a sibling
		// symlink already on disk), open() refuses rather than writing
		// through the link.
		w, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|nofollowFlag, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		w.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
		if b.Len() > 32 {
			break
		}
	}
	return b.String()
}
