//go:build linux || darwin || freebsd

package backup

import "syscall"

// nofollowFlag is OR'd into os.OpenFile's flag arg when extracting
// archive entries, so a hostile symlink that already exists at the
// target path causes open() to fail rather than silently write through
// it. Linux/macOS/BSD support O_NOFOLLOW directly; on other platforms
// (Windows in dev builds) it falls back to 0.
const nofollowFlag = syscall.O_NOFOLLOW
