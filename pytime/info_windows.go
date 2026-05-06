//go:build windows

package pytime

// fillMonotonicInfo populates info to match CPython's
// py_get_monotonic_clock on Windows.
//
// CPython: Python/pytime.c:1070 implementation = "QueryPerformanceCounter()"
func fillMonotonicInfo(info *ClockInfo) {
	if info == nil {
		return
	}
	info.Implementation = "QueryPerformanceCounter()"
	info.Monotonic = true
	info.Adjustable = false
	info.Resolution = 1e-7
}

// fillTimeInfo populates info to match CPython's py_get_system_clock
// on Windows.
//
// CPython: Python/pytime.c:924 implementation = "GetSystemTimePreciseAsFileTime()"
func fillTimeInfo(info *ClockInfo) {
	if info == nil {
		return
	}
	info.Implementation = "GetSystemTimePreciseAsFileTime()"
	info.Monotonic = false
	info.Adjustable = true
	info.Resolution = 1e-7
}
