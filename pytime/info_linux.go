//go:build linux || freebsd

package pytime

// fillMonotonicInfo populates info to match CPython's
// py_get_monotonic_clock on Linux / FreeBSD.
//
// CPython: Python/pytime.c:1190 implementation = "clock_gettime(CLOCK_MONOTONIC)"
func fillMonotonicInfo(info *ClockInfo) {
	if info == nil {
		return
	}
	info.Implementation = "clock_gettime(CLOCK_MONOTONIC)"
	info.Monotonic = true
	info.Adjustable = false
	info.Resolution = 1e-9
}

// fillTimeInfo populates info to match CPython's py_get_system_clock
// on Linux / FreeBSD.
//
// CPython: Python/pytime.c:959 implementation = "clock_gettime(CLOCK_REALTIME)"
func fillTimeInfo(info *ClockInfo) {
	if info == nil {
		return
	}
	info.Implementation = "clock_gettime(CLOCK_REALTIME)"
	info.Monotonic = false
	info.Adjustable = true
	info.Resolution = 1e-9
}
