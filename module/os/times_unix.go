//go:build darwin || freebsd || linux

// CPython: Modules/posixmodule.c:10755 os_times_impl (non-MS_WINDOWS branch)

package os

import (
	"syscall"
	"time"
)

// tvSeconds converts a syscall.Timeval to fractional seconds. Timeval's
// microsecond field is int32 on Darwin and int64 on Linux; the int64
// conversion covers both.
func tvSeconds(tv syscall.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1e6
}

// processTimes returns the five os.times fields: user / system CPU time
// for this process and its children, plus elapsed wall-clock time. POSIX
// CPython reads these from times(2) and divides by sysconf(_SC_CLK_TCK);
// Go's getrusage gives the same user/system split directly, and the
// monotonic wall clock fills the elapsed slot ("since an arbitrary point
// in the past").
//
// CPython: Modules/posixmodule.c:10755 os_times_impl
func processTimes() (user, system, childUser, childSystem, elapsed float64) {
	var self, children syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &self)
	_ = syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children)
	user = tvSeconds(self.Utime)
	system = tvSeconds(self.Stime)
	childUser = tvSeconds(children.Utime)
	childSystem = tvSeconds(children.Stime)
	elapsed = float64(time.Now().UnixNano()) / 1e9
	return user, system, childUser, childSystem, elapsed
}
