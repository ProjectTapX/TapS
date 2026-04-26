//go:build linux || darwin || freebsd

package fs

import "syscall"

// nofollowFlag is OR'd into os.OpenFile when extracting archive
// entries (audit-2026-04-24-v3 H2). Stops a pre-planted symlink at
// the target path from causing the write to follow out of the sandbox.
const nofollowFlag = syscall.O_NOFOLLOW
