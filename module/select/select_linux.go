//go:build linux

package selectmod

import "syscall"

// doSelect wraps syscall.Select on Linux. The Linux signature returns
// the number of ready fds alongside the error; the bitmasks are
// updated in place so the count is redundant for this caller.
//
// CPython: Modules/selectmodule.c:359 select(...) (POSIX arm)
func doSelect(nfds int, r, w, e *syscall.FdSet, t *syscall.Timeval) error {
	_, err := syscall.Select(nfds, r, w, e, t)
	return err
}
