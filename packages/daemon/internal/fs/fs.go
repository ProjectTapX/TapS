// Package fs implements filesystem operations for the daemon's file manager.
//
// The file manager is a virtual tree with named mount points at the top level:
//
//	/files/...           → <DataDir>/files       (general user files / instance working dirs)
//	/data/<vol>/...      → <DataDir>/volumes/<vol>  (managed loopback volumes)
//
// Listing "/" yields the mount names as synthetic directories. All other
// operations (read/write/upload/download/zip/unzip/move/copy/delete/mkdir)
// take a virtual path that begins with a mount name and resolve it to an
// absolute on-disk path bounded by the mount root (no "..": every Resolve
// guards against escaping its mount).
package fs

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

type mount struct {
	root        string
	hideNonDirs bool // for /data: hide .img / .json sibling files alongside mount dirs
}

type FS struct {
	mu     sync.RWMutex
	mounts map[string]*mount
}

// New creates an empty FS. Call Mount to add named roots.
func New() *FS {
	return &FS{mounts: map[string]*mount{}}
}

// Mount registers a named root. hideNonDirs causes List on the mount root to
// skip non-directory entries (used for /data so the .img backing files are
// hidden behind their mount-point dirs).
func (f *FS) Mount(name, root string, hideNonDirs bool) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mounts[name] = &mount{root: abs, hideNonDirs: hideNonDirs}
	return nil
}

// MountRoot returns the on-disk root for a mount name. Used by callers that
// need the physical path (e.g. instance working dirs anchored under /files).
func (f *FS) MountRoot(name string) (string, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	m, ok := f.mounts[name]
	if !ok {
		return "", false
	}
	return m.root, true
}

// split parses a virtual path into (mountName, subPath). Empty mountName means
// the path refers to the synthetic root "/".
func (f *FS) split(rel string) (string, string, error) {
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimPrefix(rel, "\\")
	rel = filepath.ToSlash(rel)
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return "", "", nil
	}
	parts := strings.SplitN(rel, "/", 2)
	name := parts[0]
	f.mu.RLock()
	_, ok := f.mounts[name]
	f.mu.RUnlock()
	if !ok {
		return "", "", fmt.Errorf("unknown mount %q", name)
	}
	if len(parts) == 1 {
		return name, "", nil
	}
	return name, parts[1], nil
}

// Resolve maps a virtual path to an absolute on-disk path under the
// appropriate mount root. The root "/" itself has no physical location and
// is rejected here — callers that want to list it should use List.
//
// Audit-2026-04-24-v3 H2: lexical containment alone is not enough —
// once any symlink lands inside a mount (via uploaded archive,
// restore, manual mkdir+link), naively joining paths follows it
// silently and lets writes/deletes escape the sandbox. After the
// HasPrefix check, walk the existing prefix through EvalSymlinks
// and re-verify containment.
func (f *FS) Resolve(rel string) (string, error) {
	name, sub, err := f.split(rel)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("path refers to virtual root")
	}
	f.mu.RLock()
	m := f.mounts[name]
	f.mu.RUnlock()
	clean := filepath.Clean("/" + filepath.ToSlash(sub))
	abs := filepath.Join(m.root, clean)
	rp, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if rp != m.root && !strings.HasPrefix(rp, m.root+string(filepath.Separator)) {
		return "", errors.New("path escapes mount root")
	}
	// Symlink-aware second pass. The deepest existing prefix is
	// resolved to its symlink-free form; the unresolved (yet-to-exist)
	// suffix is re-attached afterwards. We use the *real* mount root
	// (also EvalSymlinks'd) so the comparison stays apples-to-apples
	// when the daemon's data dir itself sits under a symlink.
	realRoot, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		// Mount root vanished — refuse rather than fall back to the
		// pre-EvalSymlinks rp which can't be containment-checked.
		return "", errors.New("mount root unavailable")
	}
	resolved, err := evalExistingPrefix(rp)
	if err != nil {
		return "", err
	}
	if resolved != realRoot && !strings.HasPrefix(resolved, realRoot+string(filepath.Separator)) {
		return "", errors.New("path escapes mount root via symlink")
	}
	// Audit-2026-04-24-v3 H3: surgical refusal of paths that point at a
	// managed-volume backing file. A real volume named "myvol" lives
	// in the mount root as both `myvol/` (the mounted dir, what users
	// see) and `myvol.img` + `myvol.json` (loopback file + metadata,
	// also at root). Without this check `fs.write?path=/data/myvol.img`
	// resolves to the backing file itself; an attacker overwrites the
	// ext4 image and the next mount feeds a hostile superblock to the
	// kernel (daemon runs as root → kernel-fuzz / suid escalation).
	// The check only fires when the leaf basename matches `<X>.img`
	// or `<X>.json` AND a sibling directory `<X>/` exists at the mount
	// root — so user-named files like `mywallpaper.img` (in instance
	// data dir) and `config.json` (anywhere) pass through unaffected.
	if isProtectedBackingFile(resolved, m) {
		return "", errors.New("path is a managed-volume backing file; access blocked")
	}
	return resolved, nil
}

// isProtectedBackingFile reports whether `rp` (already containment-
// checked, EvalSymlinks-resolved) is a backing file `<vol>.img` or
// metadata `<vol>.json` for a real managed volume on this mount. The
// hideNonDirs flag is the same flag the volumes-aware /data mount
// sets at registration time; mounts without it (per-instance dirs,
// arbitrary admin mounts) skip the check entirely so they don't
// accidentally block user files.
func isProtectedBackingFile(rp string, m *mount) bool {
	if m == nil || !m.hideNonDirs {
		return false
	}
	parent := filepath.Dir(rp)
	if parent != m.root {
		return false
	}
	base := filepath.Base(rp)
	var sibling string
	switch {
	case strings.HasSuffix(base, ".img"):
		sibling = strings.TrimSuffix(base, ".img")
	case strings.HasSuffix(base, ".json"):
		sibling = strings.TrimSuffix(base, ".json")
	default:
		return false
	}
	if sibling == "" || sibling == "." || sibling == ".." {
		return false
	}
	if st, err := os.Stat(filepath.Join(m.root, sibling)); err == nil && st.IsDir() {
		return true
	}
	return false
}

// evalExistingPrefix walks up from `p` until it finds a component
// that exists on disk, runs EvalSymlinks on that, then re-attaches
// the not-yet-existing suffix verbatim. This lets callers Resolve
// paths to files they're about to *create* (uploads, mkdir) without
// EvalSymlinks failing on the missing leaf, while still defeating
// any symlink in the existing parents.
func evalExistingPrefix(p string) (string, error) {
	cur := p
	var suffix []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		next := filepath.Dir(cur)
		if next == cur {
			// Walked off the top without finding anything that exists.
			// Nothing to evaluate; return the original — caller will
			// fail the next Stat/Open and that's the right error.
			return p, nil
		}
		suffix = append([]string{filepath.Base(cur)}, suffix...)
		cur = next
	}
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", err
	}
	for _, s := range suffix {
		real = filepath.Join(real, s)
	}
	return real, nil
}

func (f *FS) List(rel string) (protocol.FsListResp, error) {
	name, sub, err := f.split(rel)
	if err != nil {
		return protocol.FsListResp{}, err
	}
	if name == "" {
		// synthetic root — list mount names sorted
		f.mu.RLock()
		names := make([]string, 0, len(f.mounts))
		for n := range f.mounts {
			names = append(names, n)
		}
		f.mu.RUnlock()
		sort.Strings(names)
		out := make([]protocol.FsEntry, 0, len(names))
		for _, n := range names {
			out = append(out, protocol.FsEntry{Name: n, IsDir: true, Mode: "drwxr-xr-x"})
		}
		return protocol.FsListResp{Path: "/", Entries: out}, nil
	}
	abs, err := f.Resolve(rel)
	if err != nil {
		return protocol.FsListResp{}, err
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return protocol.FsListResp{}, err
	}
	f.mu.RLock()
	m := f.mounts[name]
	f.mu.RUnlock()
	atMountRoot := sub == "" || sub == "."
	out := make([]protocol.FsEntry, 0, len(ents))
	for _, e := range ents {
		if atMountRoot && m.hideNonDirs && !e.IsDir() {
			continue
		}
		// Hide daemon-internal directories (e.g. backup zips that live
		// inside the managed volume so they count against the disk
		// quota). Users see and manage them through dedicated tabs.
		if e.Name() == ".taps-backups" || strings.HasPrefix(e.Name(), ".taps-") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, protocol.FsEntry{
			Name:     e.Name(),
			IsDir:    e.IsDir(),
			Size:     fi.Size(),
			Modified: fi.ModTime().Unix(),
			Mode:     fi.Mode().String(),
		})
	}
	// normalize the response path back to a leading-slash form
	respPath := "/" + name
	if sub != "" {
		respPath = respPath + "/" + sub
	}
	return protocol.FsListResp{Path: respPath, Entries: out}, nil
}

const maxReadBytes = 4 * 1024 * 1024 // 4 MiB

func (f *FS) Read(rel string) (protocol.FsReadResp, error) {
	abs, err := f.Resolve(rel)
	if err != nil {
		return protocol.FsReadResp{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return protocol.FsReadResp{}, err
	}
	if fi.IsDir() {
		return protocol.FsReadResp{}, errors.New("is a directory")
	}
	if fi.Size() > maxReadBytes {
		return protocol.FsReadResp{}, errors.New("file too large; use download endpoint")
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return protocol.FsReadResp{}, err
	}
	return protocol.FsReadResp{Content: string(b), Size: fi.Size()}, nil
}

func (f *FS) Write(rel, content string) error {
	abs, err := f.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, abs)
}

func (f *FS) Mkdir(rel string) error {
	abs, err := f.Resolve(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0o755)
}

func (f *FS) Delete(rel string) error {
	abs, err := f.Resolve(rel)
	if err != nil {
		return err
	}
	// Refuse to delete a mount root itself.
	for _, m := range f.snapshotMounts() {
		if abs == m.root {
			return errors.New("refuse to delete mount root")
		}
	}
	return os.RemoveAll(abs)
}

func (f *FS) snapshotMounts() []*mount {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*mount, 0, len(f.mounts))
	for _, m := range f.mounts {
		out = append(out, m)
	}
	return out
}

func (f *FS) Rename(from, to string) error {
	a, err := f.Resolve(from)
	if err != nil {
		return err
	}
	b, err := f.Resolve(to)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		return err
	}
	return os.Rename(a, b)
}

// Copy supports cross-directory file/folder copy (and cross-mount).
func (f *FS) Copy(from, to string) error {
	a, err := f.Resolve(from)
	if err != nil {
		return err
	}
	b, err := f.Resolve(to)
	if err != nil {
		return err
	}
	// audit-2026-04-25 MED2: copy is bounded by the source mount root
	// — symlinks pointing outside it are skipped rather than followed,
	// otherwise an attacker who slipped a symlink into /files/foo->
	// /etc/passwd could exfiltrate arbitrary host files via Copy.
	srcRootName, _, _ := f.split(from)
	srcRoot, _ := f.MountRoot(srcRootName)
	return copyTreeBounded(a, b, srcRoot)
}

// Move is rename with cross-device fallback (copy + delete source). Cross-
// mount moves always go through the copy path because rename can't span
// filesystems (and managed volumes are separate filesystems).
func (f *FS) Move(from, to string) error {
	a, err := f.Resolve(from)
	if err != nil {
		return err
	}
	b, err := f.Resolve(to)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		return err
	}
	if err := os.Rename(a, b); err == nil {
		return nil
	}
	srcRootName, _, _ := f.split(from)
	srcRoot, _ := f.MountRoot(srcRootName)
	if err := copyTreeBounded(a, b, srcRoot); err != nil {
		return err
	}
	return os.RemoveAll(a)
}

// copyTreeBounded mirrors copyTree but uses Lstat at every step so a
// symlink under `src` doesn't get silently followed. When a symlink
// target stays within `mountRoot` we treat it as a normal copy of the
// resolved file/dir; targets that escape `mountRoot` are logged and
// skipped. mountRoot may be empty (older callers / unknown mount) —
// in that case we keep the legacy follow-everything behaviour.
func copyTreeBounded(src, dst, mountRoot string) error {
	rootAbs := ""
	if mountRoot != "" {
		if a, err := filepath.Abs(mountRoot); err == nil {
			rootAbs = a
		}
	}
	li, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if li.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(src)
		if err != nil {
			log.Printf("[fs-copy] skipping broken symlink %q: %v", src, err)
			return nil
		}
		if rootAbs != "" {
			realAbs, _ := filepath.Abs(real)
			if !strings.HasPrefix(realAbs, rootAbs+string(filepath.Separator)) && realAbs != rootAbs {
				log.Printf("[fs-copy] skipping symlink %q -> %q (escapes mount %q)", src, realAbs, rootAbs)
				return nil
			}
		}
		// Recurse with the real target so directory copies still work
		// and so we re-check any nested symlinks against the same root.
		return copyTreeBounded(real, dst, mountRoot)
	}
	if li.IsDir() {
		if err := os.MkdirAll(dst, li.Mode().Perm()); err != nil {
			return err
		}
		ents, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := copyTreeBounded(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), mountRoot); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, li.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
			return err
		}
		ents, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (f *FS) Zip(paths []string, dest string) error {
	destAbs, err := f.Resolve(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destAbs)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()

	for _, p := range paths {
		abs, err := f.Resolve(p)
		if err != nil {
			return err
		}
		// audit-2026-04-25 MED2: bound symlink resolution to the source
		// mount root. Files pointed to by symlinks that escape the
		// mount get logged + skipped instead of being silently zipped
		// (which would let a user exfiltrate /etc/passwd by linking it
		// inside their working directory).
		mountName, _, _ := f.split(p)
		mountRoot, _ := f.MountRoot(mountName)
		rootAbs := ""
		if mountRoot != "" {
			if a, e := filepath.Abs(mountRoot); e == nil {
				rootAbs = a
			}
		}
		parent := filepath.Dir(abs)
		err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(parent, path)
			zname := filepath.ToSlash(rel)

			li, lerr := os.Lstat(path)
			if lerr != nil {
				return lerr
			}
			if li.Mode()&os.ModeSymlink != 0 {
				real, e := filepath.EvalSymlinks(path)
				if e != nil {
					log.Printf("[fs-zip] skipping broken symlink %q: %v", path, e)
					return nil
				}
				if rootAbs != "" {
					realAbs, _ := filepath.Abs(real)
					if !strings.HasPrefix(realAbs, rootAbs+string(filepath.Separator)) && realAbs != rootAbs {
						log.Printf("[fs-zip] skipping symlink %q -> %q (escapes mount %q)", path, realAbs, rootAbs)
						return nil
					}
				}
				ri, e := os.Stat(real)
				if e != nil {
					log.Printf("[fs-zip] skipping symlink %q (stat target failed): %v", path, e)
					return nil
				}
				info = ri
				path = real
			}

			if info.IsDir() {
				if zname == "." {
					return nil
				}
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
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()
			_, err = io.Copy(w, src)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *FS) Unzip(src, destDir string) error {
	srcAbs, err := f.Resolve(src)
	if err != nil {
		return err
	}
	destAbs, err := f.Resolve(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return err
	}
	zr, err := zip.OpenReader(srcAbs)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, ze := range zr.File {
		// Audit-2026-04-24-v3 H2: refuse symlink entries; same logic
		// as backup.unzipInto.
		if ze.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip entry %q is a symlink; refused", ze.Name)
		}
		if strings.HasPrefix(ze.Name, "/") || strings.Contains(ze.Name, "..") {
			return fmt.Errorf("zip entry %q has illegal name", ze.Name)
		}
		// guard against zip-slip
		target := filepath.Join(destAbs, filepath.FromSlash(ze.Name))
		if !strings.HasPrefix(target, destAbs+string(filepath.Separator)) && target != destAbs {
			return errors.New("zip entry escapes dest")
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
