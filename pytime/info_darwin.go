//go:build darwin

package pytime

// fillMonotonicInfo populates info to match CPython's
// py_get_monotonic_clock on macOS.
//
// CPython: Python/pytime.c:1151 implementation = "mach_absolute_time()"
func fillMonotonicInfo(info *ClockInfo) {
	if info == nil {
		return
	}
	info.Implementation = "mach_absolute_time()"
	info.Monotonic = true
	info.Adjustable = false
	info.Resolution = 1e-9
}

// fillTimeInfo populates info to match CPython's py_get_system_clock
// on macOS.
//
// CPython: Python/pytime.c:992 implementation = "gettimeofday()"
func fillTimeInfo(info *ClockInfo) {
	if info == nil {
		return
	}
	info.Implementation = "gettimeofday()"
	info.Monotonic = false
	info.Adjustable = true
	info.Resolution = 1e-6
}
