// Tests for the _datetime module. Six areas are covered: timedelta
// arithmetic, date creation / validation, datetime.now(), isoformat
// round-trip, timezone UTC handling, and timezone.fromutc().
//
// CPython: Modules/_datetimemodule.c

package _datetime

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// ---------------------------------------------------------------------------
// Helper utilities.
// ---------------------------------------------------------------------------

func intVal(t *testing.T, o objects.Object) int64 {
	t.Helper()
	i, ok := o.(*objects.Int)
	if !ok {
		t.Fatalf("expected *objects.Int, got %T", o)
	}
	v, _ := i.Int64()
	return v
}

func floatVal(t *testing.T, o objects.Object) float64 {
	t.Helper()
	f, ok := o.(*objects.Float)
	if !ok {
		t.Fatalf("expected *objects.Float, got %T", o)
	}
	return f.Float64()
}

func strVal(t *testing.T, o objects.Object) string {
	t.Helper()
	s, err := objects.Str(o)
	if err != nil {
		t.Fatalf("Str: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// 1. timedelta arithmetic.
// ---------------------------------------------------------------------------

// TestTimedeltaBasic verifies that timedelta(days=1, hours=2) normalizes
// correctly and total_seconds returns the right value.
func TestTimedeltaBasic(t *testing.T) {
	// timedelta(days=1, hours=2) = 1 day + 7200 seconds.
	td, err := newTimedelta(1, 2*3600, 0)
	if err != nil {
		t.Fatalf("newTimedelta: %v", err)
	}
	if td.Days != 1 {
		t.Errorf("days: want 1, got %d", td.Days)
	}
	if td.Seconds != 2*3600 {
		t.Errorf("seconds: want 7200, got %d", td.Seconds)
	}
	if td.Microseconds != 0 {
		t.Errorf("microseconds: want 0, got %d", td.Microseconds)
	}

	// total_seconds = 86400 + 7200 = 93600.
	totalSecs, err := timedeltaTotalSecondsMethod([]objects.Object{td}, nil)
	if err != nil {
		t.Fatalf("total_seconds: %v", err)
	}
	if got := floatVal(t, totalSecs); got != 93600.0 {
		t.Errorf("total_seconds: want 93600.0, got %f", got)
	}
}

// TestTimedeltaArithmetic tests add, sub, mul, neg, abs.
func TestTimedeltaArithmetic(t *testing.T) {
	a, _ := newTimedelta(2, 0, 0)
	b, _ := newTimedelta(1, 0, 0)

	// a + b = 3 days.
	sum, err := timedeltaAdd(a, b)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if sum.(*Timedelta).Days != 3 {
		t.Errorf("add days: want 3, got %d", sum.(*Timedelta).Days)
	}

	// a - b = 1 day.
	diff, err := timedeltaSub(a, b)
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	if diff.(*Timedelta).Days != 1 {
		t.Errorf("sub days: want 1, got %d", diff.(*Timedelta).Days)
	}

	// a * 3 = 6 days.
	prod, err := timedeltaMul(a, objects.NewInt(3))
	if err != nil {
		t.Fatalf("mul: %v", err)
	}
	if prod.(*Timedelta).Days != 6 {
		t.Errorf("mul days: want 6, got %d", prod.(*Timedelta).Days)
	}

	// -a = -2 days.
	neg, err := timedeltaNeg(a)
	if err != nil {
		t.Fatalf("neg: %v", err)
	}
	if neg.(*Timedelta).Days != -2 {
		t.Errorf("neg days: want -2, got %d", neg.(*Timedelta).Days)
	}

	// abs(-a) = 2 days.
	absVal, err := timedeltaAbs(neg.(*Timedelta))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if absVal.(*Timedelta).Days != 2 {
		t.Errorf("abs days: want 2, got %d", absVal.(*Timedelta).Days)
	}
}

// TestTimedeltaNormalization verifies that overflowing seconds and
// microseconds are carried up into the higher units.
func TestTimedeltaNormalization(t *testing.T) {
	// 0 days + 86401 seconds = 1 day + 1 second.
	td, err := newTimedelta(0, 86401, 0)
	if err != nil {
		t.Fatalf("newTimedelta: %v", err)
	}
	if td.Days != 1 {
		t.Errorf("days: want 1, got %d", td.Days)
	}
	if td.Seconds != 1 {
		t.Errorf("seconds: want 1, got %d", td.Seconds)
	}

	// 0 days + 0 secs + 1_000_001 us = 0 days + 1 sec + 1 us.
	td2, err := newTimedelta(0, 0, 1_000_001)
	if err != nil {
		t.Fatalf("newTimedelta: %v", err)
	}
	if td2.Seconds != 1 {
		t.Errorf("seconds: want 1, got %d", td2.Seconds)
	}
	if td2.Microseconds != 1 {
		t.Errorf("microseconds: want 1, got %d", td2.Microseconds)
	}
}

// ---------------------------------------------------------------------------
// 2. date creation and validation.
// ---------------------------------------------------------------------------

// TestDateCreation checks a valid date and its fields.
func TestDateCreation(t *testing.T) {
	d, err := newDate(2024, 3, 15)
	if err != nil {
		t.Fatalf("newDate: %v", err)
	}
	if d.Year != 2024 || d.Month != 3 || d.Day != 15 {
		t.Errorf("fields: got %d-%d-%d", d.Year, d.Month, d.Day)
	}
}

// TestDateValidation checks that out-of-range values are rejected.
func TestDateValidation(t *testing.T) {
	cases := []struct {
		y, m, d int64
	}{
		{0, 1, 1},    // year below MINYEAR
		{10000, 1, 1}, // year above MAXYEAR
		{2024, 0, 1},  // month < 1
		{2024, 13, 1}, // month > 12
		{2024, 2, 30}, // day out of range for Feb
	}
	for _, c := range cases {
		if _, err := newDate(c.y, c.m, c.d); err == nil {
			t.Errorf("newDate(%d,%d,%d) expected error", c.y, c.m, c.d)
		}
	}
}

// TestDateIsoformat checks that isoformat() returns YYYY-MM-DD.
func TestDateIsoformat(t *testing.T) {
	d, _ := newDate(2024, 12, 1)
	got, err := dateStr(d)
	if err != nil {
		t.Fatalf("dateStr: %v", err)
	}
	if got != "2024-12-01" {
		t.Errorf("isoformat: want 2024-12-01, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// 3. datetime.now().
// ---------------------------------------------------------------------------

// TestDatetimeNow checks that datetime.now() returns a valid datetime with
// year >= 2024 (future-proof enough for CI).
func TestDatetimeNow(t *testing.T) {
	// Simulate calling datetime.now() with no args (class is first arg for
	// method descriptors).
	result, err := datetimeNowMethod([]objects.Object{DatetimeType}, nil)
	if err != nil {
		t.Fatalf("datetime.now(): %v", err)
	}
	dt, ok := result.(*Datetime)
	if !ok {
		t.Fatalf("expected *Datetime, got %T", result)
	}
	if dt.Year < 2024 {
		t.Errorf("year too old: %d", dt.Year)
	}
	if dt.Month < 1 || dt.Month > 12 {
		t.Errorf("month out of range: %d", dt.Month)
	}
	if dt.Day < 1 || dt.Day > 31 {
		t.Errorf("day out of range: %d", dt.Day)
	}
}

// ---------------------------------------------------------------------------
// 4. isoformat round-trip.
// ---------------------------------------------------------------------------

// TestDatetimeIsoformatRoundtrip encodes a datetime to isoformat and parses
// it back, verifying the values survive the trip.
func TestDatetimeIsoformatRoundtrip(t *testing.T) {
	orig, err := newDatetime(2025, 6, 15, 10, 30, 45, 123456, nil, 0)
	if err != nil {
		t.Fatalf("newDatetime: %v", err)
	}
	iso, err := datetimeIsoformatImpl(orig, "T", "auto")
	if err != nil {
		t.Fatalf("isoformat: %v", err)
	}
	// Parse back via fromisoformat.
	result, err := datetimeFromisoformatMethod(
		[]objects.Object{DatetimeType, objects.NewStr(iso)}, nil)
	if err != nil {
		t.Fatalf("fromisoformat(%q): %v", iso, err)
	}
	back := result.(*Datetime)
	if back.Year != orig.Year || back.Month != orig.Month || back.Day != orig.Day ||
		back.Hour != orig.Hour || back.Minute != orig.Minute || back.Second != orig.Second ||
		back.Microsecond != orig.Microsecond {
		t.Errorf("round-trip mismatch: orig %v, back %v", orig, back)
	}
}

// ---------------------------------------------------------------------------
// 5. timezone UTC.
// ---------------------------------------------------------------------------

// TestTimezoneUTC verifies that timezone.utc has zero offset and the
// right string representation.
func TestTimezoneUTC(t *testing.T) {
	if TimezoneUTC == nil {
		t.Fatal("TimezoneUTC is nil")
	}
	us := timedeltaToUs(TimezoneUTC.Offset)
	if us != 0 {
		t.Errorf("UTC offset: want 0 us, got %d us", us)
	}
	name := timezoneAutoName(TimezoneUTC)
	if name != "UTC" {
		t.Errorf("UTC name: want UTC, got %s", name)
	}
}

// TestTimezoneOffset verifies a non-UTC timezone with a positive offset.
func TestTimezoneOffset(t *testing.T) {
	// UTC+5:30 = 5*3600 + 30*60 = 19800 seconds.
	offsetTd, err := newTimedelta(0, 19800, 0)
	if err != nil {
		t.Fatalf("newTimedelta: %v", err)
	}
	tz, err := timezoneNew(TimezoneType, []objects.Object{offsetTd}, nil)
	if err != nil {
		t.Fatalf("timezone(): %v", err)
	}
	tzObj := tz.(*Timezone)
	name := timezoneAutoName(tzObj)
	if name != "UTC+05:30" {
		t.Errorf("name: want UTC+05:30, got %s", name)
	}
}

// ---------------------------------------------------------------------------
// 6. datetime with timezone.
// ---------------------------------------------------------------------------

// TestDatetimeWithTimezone creates a UTC datetime, converts via
// astimezone to UTC+1, and checks the hour shift.
func TestDatetimeWithTimezone(t *testing.T) {
	// 2025-01-01 12:00:00 UTC.
	utcDt, err := newDatetime(2025, 1, 1, 12, 0, 0, 0, TimezoneUTC, 0)
	if err != nil {
		t.Fatalf("newDatetime: %v", err)
	}
	// UTC+1 timezone.
	offsetTd, _ := newTimedelta(0, 3600, 0)
	tzPlus1 := &Timezone{Offset: offsetTd, Name: ""}
	tzPlus1.Init(TimezoneType)

	shifted, err := datetimeAstimezone(
		[]objects.Object{utcDt, tzPlus1}, nil)
	if err != nil {
		t.Fatalf("astimezone: %v", err)
	}
	shiftedDt := shifted.(*Datetime)
	if shiftedDt.Hour != 13 {
		t.Errorf("astimezone hour: want 13, got %d", shiftedDt.Hour)
	}
}

// ---------------------------------------------------------------------------
// 7. timedelta total_seconds via getattr.
// ---------------------------------------------------------------------------

// TestTimedeltaGetattr verifies that timedeltaGetattr exposes the three
// fields as integer attributes.
func TestTimedeltaGetattr(t *testing.T) {
	td, _ := newTimedelta(5, 3600, 500)
	for _, tc := range []struct {
		attr string
		want int64
	}{
		{"days", 5},
		{"seconds", 3600},
		{"microseconds", 500},
	} {
		v, err := timedeltaGetattr(td, objects.NewStr(tc.attr))
		if err != nil {
			t.Errorf("getattr %s: %v", tc.attr, err)
			continue
		}
		if got := intVal(t, v); got != tc.want {
			t.Errorf("getattr %s: want %d, got %d", tc.attr, tc.want, got)
		}
	}
}
