// Package _time is the gopy port of CPython's time built-in module.
// The C source lives in Modules/timemodule.c; this package mirrors its
// public surface (time, time_ns, monotonic, sleep, gmtime, localtime,
// asctime, ctime, mktime, strftime, strptime, get_clock_info, tzset,
// clock_gettime, clock_settime, clock_getres, perf_counter,
// process_time, thread_time and their _ns variants, plus the
// struct_time named tuple).
//
// The Go directory is named _time only to avoid colliding with Go's
// stdlib time package; the Python import name registered through
// imp.AppendInittab is the bare "time", matching CPython.
//
// Underlying clocks are sourced from the Go time package: time.Now()
// supplies wall plus monotonic readings, runtime startup time anchors
// the perf/process/thread counters.
//
// CPython: Modules/timemodule.c

package _time

import (
	"fmt"
	"math"
	"os"
	"strings"
	gotime "time"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("time", buildModule)
}

// monoStart anchors monotonic / perf_counter / process_time so they
// return seconds elapsed since process start, matching CPython's
// PyTime_Monotonic semantics (process-relative monotonic clock).
//
// CPython: Python/pytime.c PyTime_Monotonic
var monoStart = gotime.Now()

// StructTimeType is the time.struct_time named-tuple type. Eleven
// fields: nine documented integers plus tm_zone (str) and tm_gmtoff
// (int). Subclassing tuple means isinstance(t, tuple) holds.
//
// CPython: Modules/timemodule.c:420 struct_time_type_fields
// StructTimeType mirrors CPython's struct_time: nine fields are part of
// the tuple sequence (tm_year .. tm_isdst), while tm_zone and tm_gmtoff
// are hidden, reachable only by attribute name.
//
// CPython: Modules/timemodule.c:1041 struct_time_type_desc
var StructTimeType = objects.NewStructSeqTypeDesc(objects.StructSeqDesc{
	Name:        "time.struct_time",
	NInSequence: 9,
	Fields: []objects.StructSeqField{
		{Name: "tm_year", Doc: "year, for example, 1993"},
		{Name: "tm_mon", Doc: "month of year, range [1, 12]"},
		{Name: "tm_mday", Doc: "day of month, range [1, 31]"},
		{Name: "tm_hour", Doc: "hours, range [0, 23]"},
		{Name: "tm_min", Doc: "minutes, range [0, 59]"},
		{Name: "tm_sec", Doc: "seconds, range [0, 61])"},
		{Name: "tm_wday", Doc: "day of week, range [0, 6], Monday is 0"},
		{Name: "tm_yday", Doc: "day of year, range [1, 366]"},
		{Name: "tm_isdst", Doc: "1 if summer time is in effect, 0 if not, and -1 if unknown"},
		{Name: "tm_zone", Doc: "abbreviation of timezone name"},
		{Name: "tm_gmtoff", Doc: "offset from UTC in seconds"},
	},
})

// buildModule is the time module's init entry: it mirrors
// PyInit_time / time_exec, populating the module dict with every
// public name and constant.
//
// CPython: Modules/timemodule.c:2206 PyInit_time
// CPython: Modules/timemodule.c:1977 time_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("time")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		{"time", objects.NewBuiltinFunction("time", timeTime)},
		{"time_ns", objects.NewBuiltinFunction("time_ns", timeTimeNs)},
		{"clock_gettime", objects.NewBuiltinFunction("clock_gettime", clockGettime)},
		{"clock_gettime_ns", objects.NewBuiltinFunction("clock_gettime_ns", clockGettimeNs)},
		{"clock_settime", objects.NewBuiltinFunction("clock_settime", clockSettime)},
		{"clock_settime_ns", objects.NewBuiltinFunction("clock_settime_ns", clockSettimeNs)},
		{"clock_getres", objects.NewBuiltinFunction("clock_getres", clockGetres)},
		{"monotonic", objects.NewBuiltinFunction("monotonic", monotonic)},
		{"monotonic_ns", objects.NewBuiltinFunction("monotonic_ns", monotonicNs)},
		{"perf_counter", objects.NewBuiltinFunction("perf_counter", perfCounter)},
		{"perf_counter_ns", objects.NewBuiltinFunction("perf_counter_ns", perfCounterNs)},
		{"process_time", objects.NewBuiltinFunction("process_time", processTime)},
		{"process_time_ns", objects.NewBuiltinFunction("process_time_ns", processTimeNs)},
		{"thread_time", objects.NewBuiltinFunction("thread_time", threadTime)},
		{"thread_time_ns", objects.NewBuiltinFunction("thread_time_ns", threadTimeNs)},
		{"sleep", objects.NewBuiltinFunction("sleep", sleep)},
		{"gmtime", objects.NewBuiltinFunction("gmtime", gmtime)},
		{"localtime", objects.NewBuiltinFunction("localtime", localtime)},
		{"asctime", objects.NewBuiltinFunction("asctime", asctime)},
		{"ctime", objects.NewBuiltinFunction("ctime", ctime)},
		{"mktime", objects.NewBuiltinFunction("mktime", mktime)},
		{"strftime", objects.NewBuiltinFunction("strftime", strftime)},
		{"strptime", objects.NewBuiltinFunction("strptime", strptime)},
		{"tzset", objects.NewBuiltinFunction("tzset", tzset)},
		{"get_clock_info", objects.NewBuiltinFunction("get_clock_info", getClockInfo)},
		{"struct_time", StructTimeType},
		{"_STRUCT_TM_ITEMS", objects.NewInt(11)},
		// POSIX clk_id constants. Values match Linux but are only used
		// as opaque ids by the gopy clock_* shims.
		// CPython: Modules/timemodule.c:2029 CLOCK_*
		{"CLOCK_REALTIME", objects.NewInt(0)},
		{"CLOCK_MONOTONIC", objects.NewInt(1)},
		{"CLOCK_PROCESS_CPUTIME_ID", objects.NewInt(2)},
		{"CLOCK_THREAD_CPUTIME_ID", objects.NewInt(3)},
		{"CLOCK_MONOTONIC_RAW", objects.NewInt(4)},
		{"CLOCK_BOOTTIME", objects.NewInt(7)},
		{"CLOCK_TAI", objects.NewInt(11)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	if err := initTimezone(d); err != nil {
		return nil, err
	}
	return m, nil
}

// initTimezone populates timezone / altzone / daylight / tzname from
// the host runtime. Mirrors init_timezone in CPython.
//
// CPython: Modules/timemodule.c:1781 init_timezone
func initTimezone(d *objects.Dict) error {
	zoneName, offset := gotime.Now().Zone()
	// CPython's `timezone` is the offset of the standard (non-DST)
	// zone west of UTC, in seconds. Use the current zone as a proxy:
	// gopy does not track jan/jul probing the way CPython does.
	tzSecs := -int64(offset)

	// Probe January and July to detect DST.
	now := gotime.Now()
	yr := now.Year()
	janZone, janOff := gotime.Date(yr, 1, 1, 12, 0, 0, 0, gotime.Local).Zone()
	julyZone, julyOff := gotime.Date(yr, 7, 1, 12, 0, 0, 0, gotime.Local).Zone()
	daylight := 0
	if janOff != julyOff {
		daylight = 1
	}
	stdName, dstName := janZone, julyZone
	stdOff := -int64(janOff)
	altOff := -int64(julyOff)
	if julyOff < janOff {
		// Southern hemisphere: July is the std offset.
		stdName, dstName = julyZone, janZone
		stdOff, altOff = -int64(julyOff), -int64(janOff)
	}
	_ = tzSecs
	_ = zoneName

	pairs := []struct {
		k string
		v objects.Object
	}{
		{"timezone", objects.NewInt(stdOff)},
		{"altzone", objects.NewInt(altOff)},
		{"daylight", objects.NewInt(int64(daylight))},
		{"tzname", objects.NewTuple([]objects.Object{
			objects.NewStr(stdName),
			objects.NewStr(dstName),
		})},
	}
	for _, p := range pairs {
		if err := d.SetItem(objects.NewStr(p.k), p.v); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// time / time_ns / monotonic / perf_counter / process_time / thread_time
// ---------------------------------------------------------------------------

// timeTime returns the current wall-clock time in seconds since the
// epoch. Mirrors time_time.
//
// CPython: Modules/timemodule.c:108 time_time
func timeTime(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewFloat(float64(gotime.Now().UnixNano()) / 1e9), nil
}

// timeTimeNs returns the current wall-clock time in nanoseconds.
// Mirrors time_time_ns.
//
// CPython: Modules/timemodule.c:125 time_time_ns
func timeTimeNs(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(gotime.Now().UnixNano()), nil
}

// monotonic returns seconds since process start. Mirrors time_monotonic.
//
// CPython: Modules/timemodule.c:1198 time_monotonic
func monotonic(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewFloat(gotime.Since(monoStart).Seconds()), nil
}

// monotonicNs returns nanoseconds since process start.
//
// CPython: Modules/timemodule.c:1213 time_monotonic_ns
func monotonicNs(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(gotime.Since(monoStart).Nanoseconds()), nil
}

// perfCounter is identical to monotonic in gopy: Go's time.Now()
// already exposes the highest-resolution monotonic clock available.
//
// CPython: Modules/timemodule.c:1229 time_perf_counter
func perfCounter(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return monotonic(args, kwargs)
}

// perfCounterNs mirrors perfCounter but returns an int.
//
// CPython: Modules/timemodule.c:1245 time_perf_counter_ns
func perfCounterNs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return monotonicNs(args, kwargs)
}

// processTime returns CPU seconds consumed by the process so far.
// gopy lacks getrusage bindings; fall back to monotonic so callers
// still see a strictly increasing value.
//
// CPython: Modules/timemodule.c:1417 time_process_time
func processTime(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return monotonic(args, kwargs)
}

// processTimeNs mirrors processTime.
//
// CPython: Modules/timemodule.c:1433 time_process_time_ns
func processTimeNs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return monotonicNs(args, kwargs)
}

// threadTime returns thread CPU time. gopy has no per-thread CPU
// counter binding, so reuse monotonic.
//
// CPython: Modules/timemodule.c:1592 time_thread_time
func threadTime(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return monotonic(args, kwargs)
}

// threadTimeNs mirrors threadTime.
//
// CPython: Modules/timemodule.c:1607 time_thread_time_ns
func threadTimeNs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return monotonicNs(args, kwargs)
}

// ---------------------------------------------------------------------------
// clock_* family.
// ---------------------------------------------------------------------------

// clockGettime returns a float reading for the given clk_id. gopy
// maps every supported clk_id onto either the wall or monotonic clock.
//
// CPython: Modules/timemodule.c:228 time_clock_gettime_impl
func clockGettime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := singleIntArg(args, "clock_gettime")
	if err != nil {
		return nil, err
	}
	switch id {
	case 0: // CLOCK_REALTIME
		return objects.NewFloat(float64(gotime.Now().UnixNano()) / 1e9), nil
	default:
		return objects.NewFloat(gotime.Since(monoStart).Seconds()), nil
	}
}

// clockGettimeNs mirrors clockGettime but in nanoseconds.
//
// CPython: Modules/timemodule.c:250 time_clock_gettime_ns_impl
func clockGettimeNs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := singleIntArg(args, "clock_gettime_ns")
	if err != nil {
		return nil, err
	}
	switch id {
	case 0:
		return objects.NewInt(gotime.Now().UnixNano()), nil
	default:
		return objects.NewInt(gotime.Since(monoStart).Nanoseconds()), nil
	}
}

// clockSettime is a no-op in gopy: setting system clocks requires
// privileged syscalls the runtime does not expose. Raises PermissionError
// to surface the limitation rather than silently lying about success.
//
// CPython: Modules/timemodule.c:270 time_clock_settime
func clockSettime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: clock_settime() takes exactly 2 arguments (%d given)", len(args))
	}
	return nil, fmt.Errorf("PermissionError: clock_settime is not supported")
}

// clockSettimeNs mirrors clockSettime.
//
// CPython: Modules/timemodule.c:301 time_clock_settime_ns
func clockSettimeNs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: clock_settime_ns() takes exactly 2 arguments (%d given)", len(args))
	}
	return nil, fmt.Errorf("PermissionError: clock_settime_ns is not supported")
}

// clockGetres returns the resolution of the given clk_id. Go's
// time.Now() is nanosecond-resolution on every supported platform.
//
// CPython: Modules/timemodule.c:336 time_clock_getres
func clockGetres(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if _, err := singleIntArg(args, "clock_getres"); err != nil {
		return nil, err
	}
	return objects.NewFloat(1e-9), nil
}

// ---------------------------------------------------------------------------
// sleep.
// ---------------------------------------------------------------------------

// sleep pauses execution for the given number of seconds. Accepts an
// int or float; negative values raise ValueError.
//
// CPython: Modules/timemodule.c:394 time_sleep
func sleep(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: sleep() takes exactly one argument (%d given)", len(args))
	}
	secs, err := toFloat(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: an integer or float is required")
	}
	if math.IsNaN(secs) {
		return nil, fmt.Errorf("ValueError: sleep length must be non-negative")
	}
	if secs < 0 {
		return nil, fmt.Errorf("ValueError: sleep length must be non-negative")
	}
	if secs > 0 {
		gotime.Sleep(gotime.Duration(secs * float64(gotime.Second)))
	}
	return objects.None(), nil
}

// ---------------------------------------------------------------------------
// gmtime / localtime / asctime / ctime / mktime.
// ---------------------------------------------------------------------------

// gmtime converts seconds since the epoch into a struct_time in UTC.
// With no argument the current time is used.
//
// CPython: Modules/timemodule.c:527 time_gmtime
func gmtime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t, err := parseTimeTArg(args, "gmtime")
	if err != nil {
		return nil, err
	}
	return tmToStruct(t.UTC(), "UTC", 0), nil
}

// localtime converts seconds since the epoch into a struct_time in
// the local timezone.
//
// CPython: Modules/timemodule.c:571 time_localtime
func localtime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t, err := parseTimeTArg(args, "localtime")
	if err != nil {
		return nil, err
	}
	t = t.Local()
	zone, off := t.Zone()
	return tmToStruct(t, zone, int64(off)), nil
}

// asctime formats a struct_time / tuple as a 24-character string of
// the form "Sat Jun  6 16:26:11 1998".
//
// CPython: Modules/timemodule.c:1025 time_asctime
func asctime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	tm, err := parseTimeTupleArg(args, "asctime")
	if err != nil {
		return nil, err
	}
	return objects.NewStr(formatAsctime(&tm)), nil
}

// ctime converts a numeric epoch value to a string in the same format
// as asctime(localtime(seconds)).
//
// CPython: Modules/timemodule.c:1056 time_ctime
func ctime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t, err := parseTimeTArg(args, "ctime")
	if err != nil {
		return nil, err
	}
	t = t.Local()
	zone, off := t.Zone()
	st := mustReadStructTime(tmToStruct(t, zone, int64(off)))
	return objects.NewStr(formatAsctime(&st)), nil
}

// mktime is the inverse of localtime: a struct_time / tuple in local
// time is mapped back to a Unix timestamp.
//
// CPython: Modules/timemodule.c:1076 time_mktime
func mktime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: mktime() takes exactly one argument (%d given)", len(args))
	}
	st, err := readTimeTuple(args[0])
	if err != nil {
		return nil, err
	}
	t := gotime.Date(int(st.year), gotime.Month(st.mon), int(st.mday),
		int(st.hour), int(st.min), int(st.sec), 0, gotime.Local)
	return objects.NewFloat(float64(t.Unix())), nil
}

// ---------------------------------------------------------------------------
// strftime / strptime.
// ---------------------------------------------------------------------------

// strftime formats a struct_time / tuple by the given format string,
// honoring the standard 1989 C %-directives.
//
// CPython: Modules/timemodule.c:860 time_strftime
func strftime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: strftime() takes 1 or 2 arguments (%d given)", len(args))
	}
	format, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: strftime() argument 1 must be str")
	}
	var st structTime
	if len(args) == 2 {
		st, err = readTimeTuple(args[1])
		if err != nil {
			return nil, err
		}
	} else {
		now := gotime.Now().Local()
		zone, off := now.Zone()
		v := tmToStruct(now, zone, int64(off))
		st = mustReadStructTime(v)
	}
	return objects.NewStr(applyStrftime(format, &st)), nil
}

// strptime parses a string by the given format and returns a
// struct_time. CPython forwards to Lib/_strptime.py; gopy supports
// the most common conversion specifiers directly.
//
// CPython: Modules/timemodule.c:980 time_strptime
func strptime(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: strptime() takes exactly 2 arguments (%d given)", len(args))
	}
	s, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: strptime() argument 1 must be str")
	}
	format, err := objects.Str(args[1])
	if err != nil {
		return nil, fmt.Errorf("TypeError: strptime() argument 2 must be str")
	}
	st, perr := applyStrptime(s, format)
	if perr != nil {
		return nil, fmt.Errorf("ValueError: %s", perr.Error())
	}
	t := gotime.Date(int(st.year), gotime.Month(st.mon), int(st.mday),
		int(st.hour), int(st.min), int(st.sec), 0, gotime.UTC)
	return tmToStruct(t, "UTC", 0), nil
}

// ---------------------------------------------------------------------------
// tzset / get_clock_info.
// ---------------------------------------------------------------------------

// tzset reinitializes the local-timezone-derived module attributes
// after $TZ changes. Go reads $TZ on time.LoadLocation, but the
// process-wide time.Local cache is only refreshed lazily. We force a
// re-read.
//
// CPython: Modules/timemodule.c:1158 time_tzset
func tzset(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := gotime.LoadLocation(tz); err == nil {
			gotime.Local = loc
		}
	}
	return objects.None(), nil
}

// getClockInfo returns a types.SimpleNamespace describing the named
// clock: implementation, monotonic, adjustable, resolution.
//
// CPython: Modules/timemodule.c:1630 time_get_clock_info
func getClockInfo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: get_clock_info() takes exactly one argument (%d given)", len(args))
	}
	name, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: get_clock_info() argument must be str")
	}
	var impl string
	var mono, adj bool
	res := 1e-9
	switch name {
	case "time":
		impl, mono, adj = "time.Now()", false, true
	case "monotonic", "perf_counter":
		impl, mono, adj = "time.Now() monotonic", true, false
	case "process_time", "thread_time":
		impl, mono, adj = "time.Now() monotonic", true, false
	default:
		return nil, fmt.Errorf("ValueError: unknown clock")
	}
	ns := objects.NewNamespace()
	d := ns.Dict()
	pairs := []struct {
		k string
		v objects.Object
	}{
		{"implementation", objects.NewStr(impl)},
		{"monotonic", objects.NewBool(mono)},
		{"adjustable", objects.NewBool(adj)},
		{"resolution", objects.NewFloat(res)},
	}
	for _, p := range pairs {
		if err := d.SetItem(objects.NewStr(p.k), p.v); err != nil {
			return nil, err
		}
	}
	return ns, nil
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// structTime is the Go-side mirror of struct tm. tmToStruct produces
// a Python-visible StructSeq from one of these.
type structTime struct {
	year, mon, mday, hour, min, sec int64
	wday, yday, isdst               int64
	zone                            string
	gmtoff                          int64
}

// tmToStruct builds a time.struct_time StructSeq from a Go time.Time
// plus its zone abbreviation and offset (seconds east of UTC).
//
// CPython: Modules/timemodule.c:456 tmtotuple
func tmToStruct(t gotime.Time, zone string, gmtoff int64) objects.Object {
	// CPython's tm_wday is Monday==0; Go's Weekday is Sunday==0.
	wday := int(t.Weekday()) - 1
	if wday < 0 {
		wday = 6
	}
	isdst := 0
	// gopy lacks a portable IsDST hook; rely on a probe that compares
	// this offset to January's standard offset.
	if _, off := t.Zone(); true {
		yr := t.Year()
		_, janOff := gotime.Date(yr, 1, 1, 12, 0, 0, 0, t.Location()).Zone()
		if off != janOff {
			isdst = 1
		}
	}
	values := []objects.Object{
		objects.NewInt(int64(t.Year())),
		objects.NewInt(int64(t.Month())),
		objects.NewInt(int64(t.Day())),
		objects.NewInt(int64(t.Hour())),
		objects.NewInt(int64(t.Minute())),
		objects.NewInt(int64(t.Second())),
		objects.NewInt(int64(wday)),
		objects.NewInt(int64(t.YearDay())),
		objects.NewInt(int64(isdst)),
		objects.NewStr(zone),
		objects.NewInt(gmtoff),
	}
	return objects.NewStructSeq(StructTimeType, values)
}

// parseTimeTArg consumes the optional first positional arg shared by
// gmtime / localtime / ctime: a number of seconds since the epoch, or
// the current time when omitted / None.
//
// CPython: Modules/timemodule.c:508 parse_time_t_args
func parseTimeTArg(args []objects.Object, name string) (gotime.Time, error) {
	if len(args) > 1 {
		return gotime.Time{}, fmt.Errorf("TypeError: %s() takes at most 1 argument (%d given)", name, len(args))
	}
	if len(args) == 0 || objects.IsNone(args[0]) {
		return gotime.Now(), nil
	}
	secs, err := toFloat(args[0])
	if err != nil {
		return gotime.Time{}, fmt.Errorf("TypeError: %s() argument must be a number", name)
	}
	whole := int64(secs)
	frac := secs - float64(whole)
	return gotime.Unix(whole, int64(frac*1e9)), nil
}

// parseTimeTupleArg consumes the optional first positional arg shared
// by asctime / strftime: a 9-tuple or struct_time, or the current
// localtime when omitted.
//
// CPython: Modules/timemodule.c:1025 time_asctime (no-tuple branch)
func parseTimeTupleArg(args []objects.Object, name string) (structTime, error) {
	if len(args) > 1 {
		return structTime{}, fmt.Errorf("TypeError: %s() takes at most 1 argument (%d given)", name, len(args))
	}
	if len(args) == 0 {
		now := gotime.Now().Local()
		zone, off := now.Zone()
		return mustReadStructTime(tmToStruct(now, zone, int64(off))), nil
	}
	return readTimeTuple(args[0])
}

// readTimeTuple normalizes a struct_time or a 9+ element tuple into
// the internal structTime shape. Mirrors gettmarg.
//
// CPython: Modules/timemodule.c:610 gettmarg
func readTimeTuple(o objects.Object) (structTime, error) {
	if ss, ok := o.(*objects.StructSeq); ok {
		items := ss.Items()
		if len(items) < 9 {
			return structTime{}, fmt.Errorf("TypeError: struct_time must have at least 9 fields")
		}
		return extractFields(items)
	}
	if tup, ok := o.(*objects.Tuple); ok {
		n := tup.Len()
		if n < 9 {
			return structTime{}, fmt.Errorf("TypeError: argument must be 9-item sequence, not %d-item", n)
		}
		items := make([]objects.Object, n)
		for i := 0; i < n; i++ {
			items[i] = tup.Item(i)
		}
		return extractFields(items)
	}
	return structTime{}, fmt.Errorf("TypeError: Tuple or struct_time argument required")
}

func extractFields(items []objects.Object) (structTime, error) {
	var st structTime
	dst := []*int64{&st.year, &st.mon, &st.mday, &st.hour, &st.min, &st.sec, &st.wday, &st.yday, &st.isdst}
	for i, p := range dst {
		v, err := toInt(items[i])
		if err != nil {
			return st, fmt.Errorf("TypeError: an integer is required (got type %s)", items[i].Type().Name)
		}
		*p = v
	}
	st.zone = ""
	if len(items) > 9 {
		if s, err := objects.Str(items[9]); err == nil {
			st.zone = s
		}
	}
	if len(items) > 10 {
		if v, err := toInt(items[10]); err == nil {
			st.gmtoff = v
		}
	}
	return st, nil
}

// mustReadStructTime extracts a structTime from a freshly-built
// StructSeq. Panics on layout drift, which would be a bug in this
// package only.
func mustReadStructTime(o objects.Object) structTime {
	st, err := readTimeTuple(o)
	if err != nil {
		panic(err)
	}
	return st
}

// Short and long weekday / month name tables, indexed by the
// CPython-style wday (Monday==0) and 0-based month.
var (
	wdayShort = [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	wdayLong  = [7]string{
		"Monday", "Tuesday", "Wednesday", "Thursday",
		"Friday", "Saturday", "Sunday",
	}
	monShort = [12]string{
		"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
	}
	monLong = [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
)

// clampWday clamps wday into [0, 6].
func clampWday(w int64) int {
	if w < 0 || w > 6 {
		return 0
	}
	return int(w)
}

// clampMonth clamps month-1 into [0, 11].
func clampMonth(m int64) int {
	mo := int(m) - 1
	if mo < 0 || mo > 11 {
		return 0
	}
	return mo
}

// formatAsctime renders a structTime in the canonical asctime form:
// "Sat Jun  6 16:26:11 1998". CPython spells the day with %3d (right-
// aligned in a 3-char field).
//
// CPython: Modules/timemodule.c:1004 _asctime
func formatAsctime(st *structTime) string {
	wd := clampWday(st.wday)
	mo := clampMonth(st.mon)
	return fmt.Sprintf("%s %s%3d %02d:%02d:%02d %d",
		wdayShort[wd], monShort[mo], st.mday, st.hour, st.min, st.sec, st.year)
}

// strftimeNumeric handles the digit-output directives.
func strftimeNumeric(b *strings.Builder, spec byte, st *structTime) bool {
	switch spec {
	case 'Y':
		fmt.Fprintf(b, "%04d", st.year)
	case 'y':
		fmt.Fprintf(b, "%02d", st.year%100)
	case 'm':
		fmt.Fprintf(b, "%02d", st.mon)
	case 'd':
		fmt.Fprintf(b, "%02d", st.mday)
	case 'H':
		fmt.Fprintf(b, "%02d", st.hour)
	case 'I':
		h := st.hour % 12
		if h == 0 {
			h = 12
		}
		fmt.Fprintf(b, "%02d", h)
	case 'M':
		fmt.Fprintf(b, "%02d", st.min)
	case 'S':
		fmt.Fprintf(b, "%02d", st.sec)
	case 'j':
		fmt.Fprintf(b, "%03d", st.yday)
	default:
		return false
	}
	return true
}

// strftimeName handles weekday / month name and AM/PM directives.
func strftimeName(b *strings.Builder, spec byte, st *structTime) bool {
	wd := clampWday(st.wday)
	mo := clampMonth(st.mon)
	switch spec {
	case 'p':
		if st.hour < 12 {
			b.WriteString("AM")
		} else {
			b.WriteString("PM")
		}
	case 'a':
		b.WriteString(wdayShort[wd])
	case 'A':
		b.WriteString(wdayLong[wd])
	case 'b', 'h':
		b.WriteString(monShort[mo])
	case 'B':
		b.WriteString(monLong[mo])
	case 'w':
		fmt.Fprintf(b, "%d", (wd+1)%7)
	case 'u':
		fmt.Fprintf(b, "%d", wd+1)
	default:
		return false
	}
	return true
}

// strftimeSpec expands one %-directive into b. Pulled out of the main
// loop so each directive stays a one-liner and the dispatcher stays
// under the cyclomatic-complexity budget.
//
// CPython: Modules/timemodule.c:777 time_strftime1
func strftimeSpec(b *strings.Builder, spec byte, st *structTime) {
	if strftimeNumeric(b, spec, st) {
		return
	}
	if strftimeName(b, spec, st) {
		return
	}
	switch spec {
	case 'Z':
		b.WriteString(st.zone)
	case 'z':
		strftimeOffset(b, st.gmtoff)
	case 'c':
		b.WriteString(formatAsctime(st))
	case 'x':
		fmt.Fprintf(b, "%02d/%02d/%02d", st.mon, st.mday, st.year%100)
	case 'X':
		fmt.Fprintf(b, "%02d:%02d:%02d", st.hour, st.min, st.sec)
	case 'n':
		b.WriteByte('\n')
	case 't':
		b.WriteByte('\t')
	case '%':
		b.WriteByte('%')
	default:
		b.WriteByte('%')
		b.WriteByte(spec)
	}
}

// strftimeOffset writes a %z offset like "+0530" or "-0800".
func strftimeOffset(b *strings.Builder, off int64) {
	sign := byte('+')
	if off < 0 {
		sign = '-'
		off = -off
	}
	fmt.Fprintf(b, "%c%02d%02d", sign, off/3600, (off%3600)/60)
}

// applyStrftime is a hand-rolled, locale-free %-directive expander
// implementing the subset CPython documents as commonly supported.
//
// CPython: Modules/timemodule.c:777 time_strftime1
func applyStrftime(format string, st *structTime) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			b.WriteByte(c)
			continue
		}
		i++
		strftimeSpec(&b, format[i], st)
	}
	return b.String()
}

// monLookup maps lowercase short and long month names to their
// 1-based ordinal. CPython's _strptime builds the same table from
// LC_TIME locale data; gopy hard-codes the C locale to stay
// reproducible across hosts.
var monLookup = map[string]int64{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	"january": 1, "february": 2, "march": 3, "april": 4, "june": 6,
	"july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
}

// wdayLookup maps lowercase short and long weekday names to a
// Monday==0 ordinal, matching structTime.wday.
var wdayLookup = map[string]int64{
	"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6,
	"monday": 0, "tuesday": 1, "wednesday": 2, "thursday": 3,
	"friday": 4, "saturday": 5, "sunday": 6,
}

// strptimeDigitSpec is a numeric directive: read up to maxN digits
// from s[si:] and stash the result into the structTime field at fp.
// year/y get special handling via a post-processor.
type strptimeDigitSpec struct {
	maxN int
	fp   func(st *structTime, v int64)
}

// strptimeDigitSpecs maps the digit-style directives to their
// extractor. Pulled out so the main loop dispatches in O(1) instead
// of carrying a giant switch.
var strptimeDigitSpecs = map[byte]strptimeDigitSpec{
	'Y': {4, func(st *structTime, v int64) { st.year = v }},
	'y': {2, func(st *structTime, v int64) {
		if v < 69 {
			st.year = v + 2000
		} else {
			st.year = v + 1900
		}
	}},
	'm': {2, func(st *structTime, v int64) { st.mon = v }},
	'd': {2, func(st *structTime, v int64) { st.mday = v }},
	'H': {2, func(st *structTime, v int64) { st.hour = v }},
	'M': {2, func(st *structTime, v int64) { st.min = v }},
	'S': {2, func(st *structTime, v int64) { st.sec = v }},
	'j': {3, func(st *structTime, v int64) { st.yday = v }},
}

// strptimeOneSpec consumes one %-directive at format[fi-1:fi+1] from
// s[si:]. Returns the new si and any parse error.
func strptimeOneSpec(s string, si int, spec byte, st *structTime) (int, error) {
	if spec == '%' {
		if si >= len(s) || s[si] != '%' {
			return si, fmt.Errorf("expected %% literal")
		}
		return si + 1, nil
	}
	if d, ok := strptimeDigitSpecs[spec]; ok {
		v, ns, err := readDigits(s, si, d.maxN)
		if err != nil {
			return si, err
		}
		d.fp(st, v)
		return ns, nil
	}
	switch spec {
	case 'b', 'h', 'B':
		word, ns := readWord(s, si)
		v, ok := monLookup[strings.ToLower(word)]
		if !ok {
			return si, fmt.Errorf("unrecognized month name %q", word)
		}
		st.mon = v
		return ns, nil
	case 'a', 'A':
		word, ns := readWord(s, si)
		v, ok := wdayLookup[strings.ToLower(word)]
		if !ok {
			return si, fmt.Errorf("unrecognized weekday name %q", word)
		}
		st.wday = v
		return ns, nil
	}
	return si, fmt.Errorf("unsupported format directive %%%c", spec)
}

// strptimeLiteral handles a non-% byte in the format: whitespace
// consumes any run, anything else must match exactly.
func strptimeLiteral(s string, si int, c byte, format string) (int, error) {
	if si >= len(s) {
		return si, fmt.Errorf("unconverted data remains in format")
	}
	if c == ' ' {
		for si < len(s) && s[si] == ' ' {
			si++
		}
		return si, nil
	}
	if s[si] != c {
		return si, fmt.Errorf("time data %q does not match format %q", s, format)
	}
	return si + 1, nil
}

// applyStrptime is the matching parser for applyStrftime. It walks
// the format string and the input in lockstep, decoding each spec.
//
// CPython: Modules/timemodule.c:980 time_strptime (delegates to _strptime)
func applyStrptime(s, format string) (structTime, error) {
	st := structTime{year: 1900, mon: 1, mday: 1}
	si, fi := 0, 0
	for fi < len(format) {
		c := format[fi]
		if c != '%' || fi+1 >= len(format) {
			ns, err := strptimeLiteral(s, si, c, format)
			if err != nil {
				return st, err
			}
			si = ns
			fi++
			continue
		}
		spec := format[fi+1]
		fi += 2
		ns, err := strptimeOneSpec(s, si, spec, &st)
		if err != nil {
			return st, err
		}
		si = ns
	}
	if si != len(s) {
		return st, fmt.Errorf("unconverted data remains: %q", s[si:])
	}
	// Backfill wday / yday from the calendar date when not explicitly
	// supplied. CPython's _strptime does the same.
	t := gotime.Date(int(st.year), gotime.Month(st.mon), int(st.mday),
		int(st.hour), int(st.min), int(st.sec), 0, gotime.UTC)
	wd := int(t.Weekday()) - 1
	if wd < 0 {
		wd = 6
	}
	st.wday = int64(wd)
	st.yday = int64(t.YearDay())
	return st, nil
}

func readDigits(s string, start, maxN int) (int64, int, error) {
	end := start
	for end < len(s) && end-start < maxN && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == start {
		return 0, start, fmt.Errorf("expected digits at position %d", start)
	}
	var v int64
	for i := start; i < end; i++ {
		v = v*10 + int64(s[i]-'0')
	}
	return v, end, nil
}

func readWord(s string, start int) (string, int) {
	end := start
	for end < len(s) && ((s[end] >= 'a' && s[end] <= 'z') || (s[end] >= 'A' && s[end] <= 'Z')) {
		end++
	}
	return s[start:end], end
}

// singleIntArg validates that args has exactly one integer and
// returns its int64 value.
func singleIntArg(args []objects.Object, name string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("TypeError: %s() takes exactly one argument (%d given)", name, len(args))
	}
	return toInt(args[0])
}

// toInt coerces an Object to int64, accepting int or bool.
func toInt(o objects.Object) (int64, error) {
	if i, ok := o.(*objects.Int); ok {
		v, ok2 := i.Int64()
		if !ok2 {
			return 0, fmt.Errorf("OverflowError: int too large to convert")
		}
		return v, nil
	}
	if o == objects.NewBool(true) {
		return 1, nil
	}
	if o == objects.NewBool(false) {
		return 0, nil
	}
	return 0, fmt.Errorf("TypeError: an integer is required")
}

// toFloat coerces an Object to float64, accepting int or float.
func toFloat(o objects.Object) (float64, error) {
	if f, ok := o.(*objects.Float); ok {
		return f.Float64(), nil
	}
	if i, ok := o.(*objects.Int); ok {
		v, ok2 := i.Int64()
		if !ok2 {
			return 0, fmt.Errorf("OverflowError: int too large for float")
		}
		return float64(v), nil
	}
	return 0, fmt.Errorf("TypeError: a number is required")
}
