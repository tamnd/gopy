// Package pytime ports cpython/Python/pytime.c. PyTime_t lands as the
// Go type Time (a signed int64 count of nanoseconds). The conversion
// helpers, rounding modes, and clock surface mirror CPython's so that
// callers in the GIL switch interval, the user-facing time module,
// and asyncio deadline math all share one nanosecond representation.
//
// CPython: Python/pytime.c

package pytime

import (
	"errors"
	"fmt"
	"math"
)

// Time is the gopy equivalent of PyTime_t. The epoch depends on the
// clock that produced the value: Monotonic returns nanoseconds since
// process start, Time_ returns nanoseconds since the Unix epoch.
//
// CPython: Include/cpython/pytime.h:10 typedef int64_t PyTime_t
type Time int64

// MinTime and MaxTime mirror PyTime_MIN / PyTime_MAX.
//
// CPython: Include/cpython/pytime.h:11 PyTime_MIN
const (
	MinTime Time = math.MinInt64
	MaxTime Time = math.MaxInt64

	nsPerSec = 1_000_000_000
)

// Rounding selects the float-to-int conversion strategy used by
// FromSeconds and friends.
//
// CPython: Include/internal/pycore_time.h:69 _PyTime_round_t
type Rounding int

const (
	RoundFloor    Rounding = 0
	RoundCeiling  Rounding = 1
	RoundHalfEven Rounding = 2
	RoundUp       Rounding = 3
)

// ErrOverflow matches CPython's "timestamp out of range for platform"
// OverflowError raised by pytime_time_t_overflow.
//
// CPython: Python/pytime.c pytime_time_t_overflow
var ErrOverflow = errors.New("OverflowError: timestamp out of range for platform Py_TIME")

// FromSeconds converts a float-seconds value to a Time using the given
// rounding. Returns ErrOverflow if the value falls outside the int64 ns
// range; returns a ValueError-shaped error on NaN.
//
// CPython: Python/pytime.c:626 _PyTime_FromSecondsObject (float branch)
func FromSeconds(s float64, round Rounding) (Time, error) {
	if math.IsNaN(s) {
		return 0, fmt.Errorf("ValueError: Invalid value NaN (not a number)")
	}
	ns := s * nsPerSec
	rounded := pytimeRound(ns, round)
	if math.IsInf(rounded, 0) || rounded >= float64(math.MaxInt64) || rounded < float64(math.MinInt64) {
		return 0, ErrOverflow
	}
	return Time(rounded), nil
}

// FromNanoseconds wraps an int64 ns count as Time.
func FromNanoseconds(ns int64) Time { return Time(ns) }

// AsSecondsDouble converts Time to a float-seconds value.
//
// CPython: Include/cpython/pytime.h:14 PyTime_AsSecondsDouble
func AsSecondsDouble(t Time) float64 { return float64(t) / nsPerSec }

// pytimeRoundHalfEven mirrors pytime_round_half_even. round(x) in Go
// (math.Round) ties away from zero, matching C99 round; the half-even
// adjustment is the same two-step that CPython does.
//
// CPython: Python/pytime.c:282 pytime_round_half_even
func pytimeRoundHalfEven(x float64) float64 {
	rounded := math.Round(x)
	if math.Abs(x-rounded) == 0.5 {
		rounded = 2.0 * math.Round(x/2.0)
	}
	return rounded
}

// pytimeRound mirrors pytime_round.
//
// CPython: Python/pytime.c:294 pytime_round
func pytimeRound(x float64, round Rounding) float64 {
	switch round {
	case RoundHalfEven:
		return pytimeRoundHalfEven(x)
	case RoundCeiling:
		return math.Ceil(x)
	case RoundFloor:
		return math.Floor(x)
	case RoundUp:
		if x >= 0 {
			return math.Ceil(x)
		}
		return math.Floor(x)
	}
	return x
}

// Deadline returns Monotonic() + timeout, the absolute time at which a
// timeout-bounded wait should give up.
//
// CPython: Python/pytime.c _PyDeadline_Init
func Deadline(timeout Time) Time {
	var now Time
	_ = Monotonic(&now)
	return now + timeout
}

// ClockInfo mirrors _Py_clock_info_t. time.get_clock_info() returns a
// named tuple with these four fields.
//
// CPython: Include/internal/pycore_time.h _Py_clock_info_t
type ClockInfo struct {
	Implementation string
	Monotonic      bool
	Adjustable     bool
	Resolution     float64
}
