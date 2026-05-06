package pytime

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestFromSecondsRoundFloor(t *testing.T) {
	got, err := FromSeconds(1.5, RoundFloor)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1_500_000_000 {
		t.Errorf("got %d, want 1500000000", got)
	}
}

func TestFromSecondsRoundCeiling(t *testing.T) {
	got, err := FromSeconds(0.0000000001, RoundCeiling)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestFromSecondsRoundUp(t *testing.T) {
	// _PyTime_ROUND_UP rounds away from zero so a tiny negative
	// value becomes -1 ns rather than 0.
	got, err := FromSeconds(-0.0000000001, RoundUp)
	if err != nil {
		t.Fatal(err)
	}
	if got != -1 {
		t.Errorf("got %d, want -1", got)
	}
}

// TestPytimeRoundHalfEven pins the canonical banker's-rounding cases.
// We exercise the unexported helper directly because FromSeconds folds
// in a float64 multiply that loses precision below the integer ns
// scale.
//
// CPython: Python/pytime.c:282 pytime_round_half_even
func TestPytimeRoundHalfEven(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0.5, 0},
		{1.5, 2},
		{2.5, 2},
		{-0.5, 0},
		{-1.5, -2},
		{0.4, 0},
		{0.6, 1},
		{-0.6, -1},
	}
	for _, c := range cases {
		got := pytimeRoundHalfEven(c.in)
		if got != c.want {
			t.Errorf("pytimeRoundHalfEven(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFromSecondsOverflow(t *testing.T) {
	_, err := FromSeconds(1e20, RoundFloor)
	if !errors.Is(err, ErrOverflow) {
		t.Errorf("got %v, want ErrOverflow", err)
	}
}

func TestFromSecondsNaN(t *testing.T) {
	_, err := FromSeconds(float64NaN(), RoundFloor)
	if err == nil {
		t.Error("expected error on NaN")
	}
}

// float64NaN mirrors math.NaN without forcing the test file to import
// math just for the constant.
func float64NaN() float64 {
	var z float64
	return z / z
}

func TestAsSecondsDoubleRoundTrip(t *testing.T) {
	in := 1.5
	t1, err := FromSeconds(in, RoundFloor)
	if err != nil {
		t.Fatal(err)
	}
	got := AsSecondsDouble(t1)
	if got != in {
		t.Errorf("got %v, want %v", got, in)
	}
}

func TestMonotonicNonDecreasing(t *testing.T) {
	var a, b Time
	if err := Monotonic(&a); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1 * time.Millisecond)
	if err := Monotonic(&b); err != nil {
		t.Fatal(err)
	}
	if b <= a {
		t.Errorf("monotonic clock went backwards or stalled: a=%d b=%d", a, b)
	}
	delta := b - a
	if delta < 500_000 { // 0.5ms slack for sleep granularity
		t.Errorf("monotonic gap %dns < 500us; sleep wasn't observed", delta)
	}
}

func TestMonotonicWithInfo(t *testing.T) {
	var t1 Time
	var info ClockInfo
	if err := MonotonicWithInfo(&t1, &info); err != nil {
		t.Fatal(err)
	}
	if !info.Monotonic {
		t.Error("info.Monotonic = false, want true")
	}
	want := monotonicImplName()
	if info.Implementation != want {
		t.Errorf("info.Implementation = %q, want %q", info.Implementation, want)
	}
}

// monotonicImplName returns the platform-expected string so the test
// stays aligned with CPython's per-OS values.
func monotonicImplName() string {
	switch runtime.GOOS {
	case "linux", "freebsd":
		return "clock_gettime(CLOCK_MONOTONIC)"
	case "darwin":
		return "mach_absolute_time()"
	case "windows":
		return "QueryPerformanceCounter()"
	}
	return ""
}

func TestTimeWithInfo(t *testing.T) {
	var t1 Time
	var info ClockInfo
	if err := TimeWithInfo(&t1, &info); err != nil {
		t.Fatal(err)
	}
	if info.Monotonic {
		t.Error("wall clock should not be monotonic")
	}
	if info.Implementation == "" {
		t.Error("info.Implementation empty")
	}
}

func TestPerfCounterMatchesMonotonic(t *testing.T) {
	var m, p Time
	if err := Monotonic(&m); err != nil {
		t.Fatal(err)
	}
	if err := PerfCounter(&p); err != nil {
		t.Fatal(err)
	}
	// Both come from the same source; the second reading is at least
	// the first reading.
	if p < m {
		t.Errorf("perf counter went backwards: m=%d p=%d", m, p)
	}
}

func TestDeadlineIsAhead(t *testing.T) {
	timeout := Time(10 * time.Millisecond)
	d := Deadline(timeout)
	var now Time
	_ = Monotonic(&now)
	if d <= now {
		t.Errorf("deadline %d should be after now %d", d, now)
	}
	if d-now > timeout+Time(time.Millisecond) {
		t.Errorf("deadline %d further than timeout %d from now %d", d, timeout, now)
	}
}
