// Clock backends. Go's time.Now() calls the same underlying syscalls
// CPython does: clock_gettime on linux/freebsd, mach_absolute_time on
// darwin, QueryPerformanceCounter on windows. We surface the raw int64
// nanoseconds and let the per-platform info_*.go files name the source.
//
// CPython: Python/pytime.c py_get_system_clock / py_get_monotonic_clock

package pytime

import "time"

// processStart is captured once at package init so Monotonic returns
// nanoseconds since process start, mirroring CPython's monotonic clock
// epoch.
var processStart = time.Now()

// Time_ writes the current wall-clock time (ns since the Unix epoch)
// to *out. The trailing underscore avoids colliding with the type.
//
// CPython: Python/pytime.c:1010 PyTime_Time
//
//nolint:revive // Time_ keeps the trailing underscore to avoid shadowing the package's Time type
func Time_(out *Time) error {
	*out = Time(time.Now().UnixNano())
	return nil
}

// Monotonic writes a monotonic clock reading to *out. The reading is
// nanoseconds since process start; it never decreases on a single
// machine.
//
// CPython: Python/pytime.c:1223 PyTime_Monotonic
func Monotonic(out *Time) error {
	*out = Time(time.Since(processStart).Nanoseconds())
	return nil
}

// PerfCounter writes a high-resolution monotonic counter to *out. On
// every supported platform CPython aliases this to monotonic, so we do
// the same.
//
// CPython: Python/pytime.c:1266 PyTime_PerfCounterRaw
func PerfCounter(out *Time) error { return Monotonic(out) }

// TimeWithInfo writes the wall-clock time and fills info.
//
// CPython: Python/pytime.c:1031 _PyTime_TimeWithInfo
func TimeWithInfo(out *Time, info *ClockInfo) error {
	if err := Time_(out); err != nil {
		return err
	}
	fillTimeInfo(info)
	return nil
}

// MonotonicWithInfo writes a monotonic reading and fills info.
//
// CPython: Python/pytime.c:1245 _PyTime_MonotonicWithInfo
func MonotonicWithInfo(out *Time, info *ClockInfo) error {
	if err := Monotonic(out); err != nil {
		return err
	}
	fillMonotonicInfo(info)
	return nil
}

// PerfCounterWithInfo writes a high-resolution monotonic counter and
// fills info.
//
// CPython: Python/pytime.c:1255 _PyTime_PerfCounterWithInfo
func PerfCounterWithInfo(out *Time, info *ClockInfo) error {
	return MonotonicWithInfo(out, info)
}
