//go:build !linux && !darwin && !freebsd

package fs

// nofollowFlag fallback for non-Unix dev builds. Production daemon
// runs on Linux where the syscall constant is honored.
const nofollowFlag = 0
