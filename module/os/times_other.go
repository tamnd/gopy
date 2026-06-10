//go:build !darwin && !freebsd && !linux && !windows

// CPython: Modules/posixmodule.c:10755 os_times_impl (fallback stub)

package os

import "time"

// processTimes returns zeros for CPU time on unsupported platforms and
// the monotonic wall clock for elapsed.
//
// CPython: Modules/posixmodule.c:10755 os_times_impl
func processTimes() (user, system, childUser, childSystem, elapsed float64) {
	return 0, 0, 0, 0, float64(time.Now().UnixNano()) / 1e9
}
