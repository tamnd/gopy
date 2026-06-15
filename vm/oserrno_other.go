//go:build !windows

package vm

// winerrorToErrno is the identity on non-Windows platforms: Go's
// syscall.Errno already carries the POSIX errno there.
func winerrorToErrno(errno int) int { return errno }
