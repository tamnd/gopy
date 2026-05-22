//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package selectmod

import "syscall"

// doSelect wraps syscall.Select on the BSD family. The signature here
// returns only an error; bitmasks are updated in place.
//
// CPython: Modules/selectmodule.c:359 select(...) (POSIX arm)
func doSelect(nfds int, r, w, e *syscall.FdSet, t *syscall.Timeval) error {
	return syscall.Select(nfds, r, w, e, t)
}
