//go:build !linux && !darwin && !freebsd

package backup

// nofollowFlag fallback for non-Unix dev builds. Production daemon
// runs on Linux where the syscall constant is honored; this stub keeps
// `go build ./...` working under Windows during development.
const nofollowFlag = 0
