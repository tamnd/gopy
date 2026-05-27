// Package _datetime is the gopy port of CPython's _datetime C
// accelerator. CPython defines these types in Modules/_datetimemodule.c
// and they are exposed to Python via the `datetime` module
// (Lib/datetime.py imports from _datetime when the C extension is
// available).
//
// Five types are ported here:
//   - timedelta  - a duration of (days, seconds, microseconds)
//   - date       - a Gregorian calendar date (year, month, day)
//   - time       - a time of day with optional tzinfo
//   - timezone   - a fixed-offset tzinfo implementation
//   - datetime   - date + time combined (subclass of date)
//
// Go's time package is used for clock reads and formatting helpers.
//
// CPython: Modules/_datetimemodule.c

package _datetime

import (
	"fmt"
	"math"
	"strings"
	gotime "time"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_datetime", buildModule)
}

// buildModule materializes the _datetime module dict. Mirrors the
// PyInit__datetime / datetime_exec entry point.
//
// CPython: Modules/_datetimemodule.c:7078 datetime_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_datetime")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"timedelta", TimedeltaType},
		{"date", DateType},
		{"time", TimeType},
		{"timezone", TimezoneType},
		{"datetime", DatetimeType},
		{"MINYEAR", objects.NewInt(minyear)},
		{"MAXYEAR", objects.NewInt(maxyear)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Constants.
// ---------------------------------------------------------------------------

// CPython: Modules/_datetimemodule.c:73 MINYEAR / MAXYEAR
const (
	minyear int64 = 1
	maxyear int64 = 9999
)

// Limits used during normalization.
//
// CPython: Modules/_datetimemodule.c:88 MAX_DELTA_DAYS
const (
	maxDeltaDays int64 = 999999999
	usPerSecond  int64 = 1_000_000
	usPerMinute  int64 = 60 * usPerSecond
	usPerHour    int64 = 60 * usPerMinute
	usPerDay     int64 = 24 * usPerHour
)

// ---------------------------------------------------------------------------
// strftime format translation table.
// ---------------------------------------------------------------------------

// pythonToGoFmt converts a CPython strftime format string to the
// equivalent Go time.Format layout string. Returns ("", false) when the
// input contains no % directives at all; callers should return the
// original format string directly in that case because Go's time.Format
// would interpret bare digits (1, 2, 3…) as date/time components.
//
// CPython: Modules/_datetimemodule.c:720 format_utcoffset (and surrounding
// strftime code)
func pythonToGoFmt(pyFmt string) (string, bool) {
	if !strings.ContainsRune(pyFmt, '%') {
		return pyFmt, false
	}
	out := make([]byte, 0, len(pyFmt))
	i := 0
	for i < len(pyFmt) {
		if pyFmt[i] != '%' || i+1 >= len(pyFmt) {
			out = append(out, pyFmt[i])
			i++
			continue
		}
		i++ // consume '%'
		switch pyFmt[i] {
		case 'Y':
			out = append(out, "2006"...)
		case 'm':
			out = append(out, "01"...)
		case 'd':
			out = append(out, "02"...)
		case 'H':
			out = append(out, "15"...)
		case 'M':
			out = append(out, "04"...)
		case 'S':
			out = append(out, "05"...)
		case 'f':
			out = append(out, "000000"...)
		case 'z':
			out = append(out, "-0700"...)
		case 'Z':
			out = append(out, "MST"...)
		case 'A':
			out = append(out, "Monday"...)
		case 'a':
			out = append(out, "Mon"...)
		case 'B':
			out = append(out, "January"...)
		case 'b':
			out = append(out, "Jan"...)
		case 'p':
			out = append(out, "PM"...)
		case 'I':
			out = append(out, "03"...)
		case 'j':
			// day of year - Go does not have a direct directive;
			// use a placeholder that we handle post-format.
			out = append(out, "002"...)
		case 'X':
			out = append(out, "15:04:05"...)
		case 'x':
			out = append(out, "01/02/06"...)
		case 'c':
			out = append(out, "Mon Jan  2 15:04:05 2006"...)
		case '%':
			out = append(out, '%')
		default:
			out = append(out, '%')
			out = append(out, pyFmt[i])
		}
		i++
	}
	return string(out), true
}

// ---------------------------------------------------------------------------
// timedelta type.
// ---------------------------------------------------------------------------

// TimedeltaType is datetime.timedelta. Instances represent a duration
// normalized to (days, seconds, microseconds) with constraints
// 0 <= seconds < 86400, 0 <= microseconds < 1000000.
//
// CPython: Modules/_datetimemodule.c:2838 timedelta_type
var TimedeltaType = objects.NewType("datetime.timedelta", []*objects.Type{objects.ObjectType()})

// Timedelta backs a timedelta instance. The three fields are always
// kept in normalized form.
//
// CPython: Modules/_datetimemodule.c:2452 PyDateTime_Delta
type Timedelta struct {
	objects.Header
	Days         int64
	Seconds      int64
	Microseconds int64
}

// TimedeltaMin, TimedeltaMax, TimedeltaResolution are the class
// attributes min / max / resolution.
//
// CPython: Modules/_datetimemodule.c:2861 timedelta_min
var (
	TimedeltaMin        *Timedelta
	TimedeltaMax        *Timedelta
	TimedeltaResolution *Timedelta
)

func init() {
	// Wire all slots and class attributes after the type var is initialized
	// to break the initialization cycle.
	TimedeltaType.TpNew = timedeltaNew
	TimedeltaType.Repr = timedeltaRepr
	TimedeltaType.Str = timedeltaStr
	TimedeltaType.Hash = timedeltaHash
	TimedeltaType.RichCmp = timedeltaRichCmp
	TimedeltaType.Getattro = timedeltaGetattr
	TimedeltaType.Number = &objects.NumberMethods{
		Add:         timedeltaAdd,
		Subtract:    timedeltaSub,
		Multiply:    timedeltaMul,
		TrueDivide:  timedeltaTrueDiv,
		FloorDivide: timedeltaFloorDiv,
		Remainder:   timedeltaMod,
		Negative:    timedeltaNeg,
		Positive:    timedeltaPos,
		Absolute:    timedeltaAbs,
	}
	TimedeltaMin, _ = newTimedelta(-maxDeltaDays, 0, 0)
	TimedeltaMax, _ = newTimedelta(maxDeltaDays, 86399, 999999)
	TimedeltaResolution, _ = newTimedelta(0, 0, 1)
	objects.SetTypeDescr(TimedeltaType, "min", TimedeltaMin)
	objects.SetTypeDescr(TimedeltaType, "max", TimedeltaMax)
	objects.SetTypeDescr(TimedeltaType, "resolution", TimedeltaResolution)
	objects.SetTypeDescr(TimedeltaType, "total_seconds",
		objects.NewMethodDescr(TimedeltaType, "total_seconds", timedeltaTotalSecondsMethod))
	// Pickle hooks. __new__ exposes timedeltaNew at the Python level
	// so pickle's NEWOBJ can dispatch into it instead of falling back
	// to object.__new__.
	//
	// CPython: Objects/typeobject.c:9952 add_tp_new_wrapper
	objects.SetTypeDescr(TimedeltaType, "__new__",
		objects.NewBuiltinFunction("timedelta.__new__", timedeltaNewBuiltin))
	// __reduce__ packs (cls, (days, seconds, microseconds)) so pickle
	// recreates the timedelta via the normal constructor.
	//
	// CPython: Modules/_datetimemodule.c:3018 delta_reduce
	objects.SetTypeDescr(TimedeltaType, "__reduce__",
		objects.NewMethodDescr(TimedeltaType, "__reduce__", timedeltaReduce))
}

// timedeltaNewBuiltin is the Python-level wrapper that exposes
// timedeltaNew as timedelta.__new__.
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper
func timedeltaNewBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: timedelta.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: timedelta.__new__(X): X is not a type object")
	}
	return timedeltaNew(cls, args[1:], kwargs)
}

// timedeltaReduce is timedelta.__reduce__. Returns (cls, (days, seconds,
// microseconds)) so pickle.loads can call cls(*state).
//
// CPython: Modules/_datetimemodule.c:3018 delta_reduce
func timedeltaReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	td, ok := args[0].(*Timedelta)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'datetime.timedelta' object")
	}
	state := objects.NewTuple([]objects.Object{
		objects.NewInt(td.Days),
		objects.NewInt(td.Seconds),
		objects.NewInt(td.Microseconds),
	})
	return objects.NewTuple([]objects.Object{td.Type(), state}), nil
}

// newTimedelta normalizes (days, seconds, us) and returns a Timedelta.
//
// CPython: Modules/_datetimemodule.c:2452 normalize_d_s_us
func newTimedelta(days, seconds, us int64) (*Timedelta, error) {
	// Normalize microseconds.
	s := us / usPerSecond
	us -= s * usPerSecond
	seconds += s
	// Normalize seconds.
	d := seconds / 86400
	seconds -= d * 86400
	days += d
	// Handle negative remainder for seconds.
	if seconds < 0 {
		seconds += 86400
		days--
	}
	if us < 0 {
		us += usPerSecond
		seconds--
		if seconds < 0 {
			seconds += 86400
			days--
		}
	}
	if days < -maxDeltaDays || days > maxDeltaDays {
		return nil, fmt.Errorf("OverflowError: timedelta # of days is too large: %d", days)
	}
	td := &Timedelta{Days: days, Seconds: seconds, Microseconds: us}
	td.Init(TimedeltaType)
	return td, nil
}

// timedeltaNew is timedelta.__new__. Accepts days, seconds, microseconds,
// milliseconds, minutes, hours, weeks kwargs and normalizes them.
//
// CPython: Modules/_datetimemodule.c:2580 timedelta_new
func timedeltaNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var days, seconds, us, ms, minutes, hours, weeks int64
	if err := parseTimedeltaArgs(args, kwargs, &days, &seconds, &us, &ms, &minutes, &hours, &weeks); err != nil {
		return nil, err
	}
	// Accumulate everything into days + seconds + us.
	days += weeks * 7
	seconds += hours*3600 + minutes*60
	us += ms * 1000
	td, err := newTimedelta(days, seconds, us)
	if err != nil {
		return nil, err
	}
	td.Init(cls)
	return td, nil
}

func parseTimedeltaArgs(args []objects.Object, kwargs map[string]objects.Object,
	days, seconds, us, ms, minutes, hours, weeks *int64) error {
	positional := []struct {
		name string
		dst  *int64
	}{
		{"days", days},
		{"seconds", seconds},
		{"microseconds", us},
		{"milliseconds", ms},
		{"minutes", minutes},
		{"hours", hours},
		{"weeks", weeks},
	}
	for i, a := range args {
		if i >= len(positional) {
			return fmt.Errorf("TypeError: timedelta() takes at most 7 arguments")
		}
		n, err := asInt(a)
		if err != nil {
			return err
		}
		*positional[i].dst = n
	}
	for k, v := range kwargs {
		found := false
		for _, p := range positional {
			if p.name == k {
				n, err := asInt(v)
				if err != nil {
					return err
				}
				*p.dst = n
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("TypeError: timedelta() got an unexpected keyword argument '%s'", k)
		}
	}
	return nil
}

// asInt extracts an int64 from an Int or Float object.
func asInt(o objects.Object) (int64, error) {
	switch v := o.(type) {
	case *objects.Int:
		n, ok := v.Int64()
		if !ok {
			return 0, fmt.Errorf("OverflowError: value too large")
		}
		return n, nil
	case *objects.Float:
		f := v.Float64()
		if f != math.Trunc(f) {
			return 0, fmt.Errorf("TypeError: integer argument expected, got float")
		}
		return int64(f), nil
	}
	return 0, fmt.Errorf("TypeError: an integer is required (got type %s)", o.Type().Name)
}

// timedeltaGetattr exposes the three fields plus total_seconds.
//
// CPython: Modules/_datetimemodule.c:2700 timedelta_getattro
func timedeltaGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	td := o.(*Timedelta)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "days":
		return objects.NewInt(td.Days), nil
	case "seconds":
		return objects.NewInt(td.Seconds), nil
	case "microseconds":
		return objects.NewInt(td.Microseconds), nil
	}
	return objects.GenericGetAttr(o, name)
}

// timedeltaTotalSecondsMethod is timedelta.total_seconds().
//
// CPython: Modules/_datetimemodule.c:2721 delta_total_seconds
func timedeltaTotalSecondsMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	td := args[0].(*Timedelta)
	total := float64(td.Days)*86400.0 + float64(td.Seconds) + float64(td.Microseconds)*1e-6
	return objects.NewFloat(total), nil
}

// timedeltaToUs converts a Timedelta to total microseconds.
func timedeltaToUs(td *Timedelta) int64 {
	return td.Days*usPerDay + td.Seconds*usPerSecond + td.Microseconds
}

// usToTimedelta converts total microseconds to a normalized Timedelta.
func usToTimedelta(us int64) (*Timedelta, error) {
	return newTimedelta(0, 0, us)
}

// timedeltaRepr renders timedelta(days, seconds, microseconds).
//
// CPython: Modules/_datetimemodule.c:2639 delta_repr
func timedeltaRepr(o objects.Object) (string, error) {
	td := o.(*Timedelta)
	if td.Seconds == 0 && td.Microseconds == 0 {
		return fmt.Sprintf("datetime.timedelta(days=%d)", td.Days), nil
	}
	if td.Microseconds == 0 {
		return fmt.Sprintf("datetime.timedelta(days=%d, seconds=%d)", td.Days, td.Seconds), nil
	}
	return fmt.Sprintf("datetime.timedelta(days=%d, seconds=%d, microseconds=%d)",
		td.Days, td.Seconds, td.Microseconds), nil
}

// timedeltaStr renders the human-readable string (H:MM:SS[.ffffff]).
//
// CPython: Modules/_datetimemodule.c:2667 delta_str
func timedeltaStr(o objects.Object) (string, error) {
	td := o.(*Timedelta)
	mm := td.Seconds / 60
	ss := td.Seconds % 60
	hh := mm / 60
	mm = mm % 60
	s := fmt.Sprintf("%d:%02d:%02d", hh, mm, ss)
	if td.Days != 0 {
		plural := ""
		days := td.Days
		if days < 0 {
			days = -days
			plural = " day, "
		} else {
			plural = " days, "
		}
		if days == 1 {
			plural = " day, "
		}
		s = fmt.Sprintf("%d%s%s", td.Days, plural, s)
	}
	if td.Microseconds != 0 {
		s = fmt.Sprintf("%s.%06d", s, td.Microseconds)
	}
	return s, nil
}

// timedeltaHash hashes as a tuple of (days, seconds, microseconds).
//
// CPython: Modules/_datetimemodule.c:2750 delta_hash
func timedeltaHash(o objects.Object) (int64, error) {
	td := o.(*Timedelta)
	// Simple polynomial hash to avoid pulling in hash.Hash.
	h := int64(0x345678)
	h = h*1000003 ^ td.Days
	h = h*1000003 ^ td.Seconds
	h = h*1000003 ^ td.Microseconds
	return h, nil
}

// timedeltaRichCmp compares two timedeltas by total microseconds.
//
// CPython: Modules/_datetimemodule.c:2760 delta_richcompare
func timedeltaRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	lhs, ok := a.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	al := timedeltaToUs(lhs)
	bl := timedeltaToUs(rhs)
	var res bool
	switch op {
	case objects.CompareLT:
		res = al < bl
	case objects.CompareLE:
		res = al <= bl
	case objects.CompareEQ:
		res = al == bl
	case objects.CompareNE:
		res = al != bl
	case objects.CompareGT:
		res = al > bl
	case objects.CompareGE:
		res = al >= bl
	}
	return objects.NewBool(res), nil
}

func timedeltaAdd(a, b objects.Object) (objects.Object, error) {
	lhs, ok := a.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	return usToTimedelta(timedeltaToUs(lhs) + timedeltaToUs(rhs))
}

func timedeltaSub(a, b objects.Object) (objects.Object, error) {
	lhs, ok := a.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	return usToTimedelta(timedeltaToUs(lhs) - timedeltaToUs(rhs))
}

// timedeltaMul handles timedelta * int and int * timedelta.
//
// CPython: Modules/_datetimemodule.c:2790 delta_multiply
func timedeltaMul(a, b objects.Object) (objects.Object, error) {
	if td, ok := a.(*Timedelta); ok {
		n, err := asInt(b)
		if err == nil {
			return usToTimedelta(timedeltaToUs(td) * n)
		}
		if f, ok2 := b.(*objects.Float); ok2 {
			return usToTimedelta(int64(math.Round(float64(timedeltaToUs(td)) * f.Float64())))
		}
		return objects.NotImplemented(), nil
	}
	if td, ok := b.(*Timedelta); ok {
		n, err := asInt(a)
		if err == nil {
			return usToTimedelta(timedeltaToUs(td) * n)
		}
		if f, ok2 := a.(*objects.Float); ok2 {
			return usToTimedelta(int64(math.Round(float64(timedeltaToUs(td)) * f.Float64())))
		}
		return objects.NotImplemented(), nil
	}
	return objects.NotImplemented(), nil
}

// timedeltaTrueDiv handles timedelta / timedelta (float) and
// timedelta / int.
//
// CPython: Modules/_datetimemodule.c:2806 delta_truedivide
func timedeltaTrueDiv(a, b objects.Object) (objects.Object, error) {
	lhs, ok := a.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	lhsUs := float64(timedeltaToUs(lhs))
	if rhs, ok := b.(*Timedelta); ok {
		rhsUs := float64(timedeltaToUs(rhs))
		if rhsUs == 0 {
			return nil, fmt.Errorf("ZeroDivisionError: division by zero")
		}
		return objects.NewFloat(lhsUs / rhsUs), nil
	}
	if f, ok := b.(*objects.Float); ok {
		if f.Float64() == 0 {
			return nil, fmt.Errorf("ZeroDivisionError: division by zero")
		}
		return usToTimedelta(int64(math.Round(lhsUs / f.Float64())))
	}
	n, err := asInt(b)
	if err != nil {
		return objects.NotImplemented(), nil
	}
	if n == 0 {
		return nil, fmt.Errorf("ZeroDivisionError: division by zero")
	}
	return usToTimedelta(int64(math.Round(lhsUs / float64(n))))
}

// timedeltaFloorDiv handles timedelta // timedelta (int) and
// timedelta // int.
//
// CPython: Modules/_datetimemodule.c:2820 delta_floordivide
func timedeltaFloorDiv(a, b objects.Object) (objects.Object, error) {
	lhs, ok := a.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	lhsUs := timedeltaToUs(lhs)
	if rhs, ok := b.(*Timedelta); ok {
		rhsUs := timedeltaToUs(rhs)
		if rhsUs == 0 {
			return nil, fmt.Errorf("ZeroDivisionError: division by zero")
		}
		return objects.NewInt(floorDivInt64(lhsUs, rhsUs)), nil
	}
	n, err := asInt(b)
	if err != nil {
		return objects.NotImplemented(), nil
	}
	if n == 0 {
		return nil, fmt.Errorf("ZeroDivisionError: division by zero")
	}
	return usToTimedelta(floorDivInt64(lhsUs, n))
}

// timedeltaMod handles timedelta % timedelta and timedelta % int.
//
// CPython: Modules/_datetimemodule.c:2830 delta_remainder
func timedeltaMod(a, b objects.Object) (objects.Object, error) {
	lhs, ok := a.(*Timedelta)
	if !ok {
		return objects.NotImplemented(), nil
	}
	lhsUs := timedeltaToUs(lhs)
	if rhs, ok := b.(*Timedelta); ok {
		rhsUs := timedeltaToUs(rhs)
		if rhsUs == 0 {
			return nil, fmt.Errorf("ZeroDivisionError: division by zero")
		}
		return usToTimedelta(floorModInt64(lhsUs, rhsUs))
	}
	n, err := asInt(b)
	if err != nil {
		return objects.NotImplemented(), nil
	}
	if n == 0 {
		return nil, fmt.Errorf("ZeroDivisionError: division by zero")
	}
	return usToTimedelta(floorModInt64(lhsUs, n))
}

func timedeltaNeg(o objects.Object) (objects.Object, error) {
	td := o.(*Timedelta)
	return usToTimedelta(-timedeltaToUs(td))
}

func timedeltaPos(o objects.Object) (objects.Object, error) {
	return o, nil
}

func timedeltaAbs(o objects.Object) (objects.Object, error) {
	td := o.(*Timedelta)
	us := timedeltaToUs(td)
	if us < 0 {
		us = -us
	}
	return usToTimedelta(us)
}

// floorDivInt64 is Python-style floor division for int64 values.
func floorDivInt64(a, b int64) int64 {
	q := a / b
	// Adjust for negative remainder (Python floor division).
	if (a^b) < 0 && q*b != a {
		q--
	}
	return q
}

// floorModInt64 is Python-style modulo for int64 values.
func floorModInt64(a, b int64) int64 {
	return a - floorDivInt64(a, b)*b
}

// ---------------------------------------------------------------------------
// date type.
// ---------------------------------------------------------------------------

// DateType is datetime.date.
//
// CPython: Modules/_datetimemodule.c:3451 date_type
var DateType = objects.NewType("datetime.date", []*objects.Type{objects.ObjectType()})

// Date backs a date instance.
//
// CPython: Modules/_datetimemodule.c:2945 PyDateTime_Date
type Date struct {
	objects.Header
	Year  int64
	Month int64
	Day   int64
}

func init() {
	// Wire slots in init() to break the initialization cycle between DateType
	// and newDate (which calls d.Init(DateType)).
	DateType.TpNew = dateNew
	DateType.Repr = dateRepr
	DateType.Str = dateStr
	DateType.Hash = dateHash
	DateType.RichCmp = dateRichCmp
	DateType.Getattro = dateGetattr

	// __new__ exposes dateNew as a Python-level descriptor so
	// pickle's load_newobj can do `cls.__new__(cls, state_bytes)`
	// without falling through to object.__new__ (which would
	// allocate a bare Instance instead of a Date).
	//
	// CPython: Objects/typeobject.c:9952 add_tp_new_wrapper
	objects.SetTypeDescr(DateType, "__new__",
		objects.NewBuiltinFunction("date.__new__", dateNewBuiltin))
	// __reduce__ packs (cls, (state_bytes,)) so pickle.dumps
	// produces NEWOBJ bytes that round-trip through dateNew's
	// bytes-state path.
	//
	// CPython: Modules/_datetimemodule.c:3902 date_reduce
	objects.SetTypeDescr(DateType, "__reduce__",
		objects.NewMethodDescr(DateType, "__reduce__", dateReduce))
	objects.SetTypeDescr(DateType, "today",
		objects.NewClassMethod(objects.NewBuiltinFunction("today", dateTodayMethod)))
	objects.SetTypeDescr(DateType, "fromtimestamp",
		objects.NewClassMethod(objects.NewBuiltinFunction("fromtimestamp", dateFromtimestampMethod)))
	objects.SetTypeDescr(DateType, "fromisoformat",
		objects.NewClassMethod(objects.NewBuiltinFunction("fromisoformat", dateFromisoformatMethod)))
	objects.SetTypeDescr(DateType, "timetuple",
		objects.NewMethodDescr(DateType, "timetuple", dateTimetuple))
	objects.SetTypeDescr(DateType, "toordinal",
		objects.NewMethodDescr(DateType, "toordinal", dateToordinal))
	objects.SetTypeDescr(DateType, "weekday",
		objects.NewMethodDescr(DateType, "weekday", dateWeekday))
	objects.SetTypeDescr(DateType, "isoweekday",
		objects.NewMethodDescr(DateType, "isoweekday", dateIsoweekday))
	objects.SetTypeDescr(DateType, "isocalendar",
		objects.NewMethodDescr(DateType, "isocalendar", dateIsocalendar))
	objects.SetTypeDescr(DateType, "isoformat",
		objects.NewMethodDescr(DateType, "isoformat", dateIsoformat))
	objects.SetTypeDescr(DateType, "strftime",
		objects.NewMethodDescr(DateType, "strftime", dateStrftime))
	objects.SetTypeDescr(DateType, "__format__",
		objects.NewMethodDescr(DateType, "__format__", dateFormatMethod))
	objects.SetTypeDescr(DateType, "replace",
		objects.NewMethodDescr(DateType, "replace", dateReplace))
	objects.SetTypeDescr(DateType, "min", mustDate(minyear, 1, 1))
	objects.SetTypeDescr(DateType, "max", mustDate(maxyear, 12, 31))
	objects.SetTypeDescr(DateType, "resolution", mustTimedelta(1))
}

func mustDate(y, m, d int64) *Date {
	date, err := newDate(y, m, d)
	if err != nil {
		panic(err)
	}
	return date
}

func mustTimedelta(days int64) *Timedelta {
	td, err := newTimedelta(days, 0, 0)
	if err != nil {
		panic(err)
	}
	return td
}

// newDate validates and allocates a Date.
//
// CPython: Modules/_datetimemodule.c:2945 new_date
func newDate(year, month, day int64) (*Date, error) {
	if err := checkDate(year, month, day); err != nil {
		return nil, err
	}
	d := &Date{Year: year, Month: month, Day: day}
	d.Init(DateType)
	return d, nil
}

// checkDate validates year/month/day ranges.
//
// CPython: Modules/_datetimemodule.c:2945 check_date_args
func checkDate(year, month, day int64) error {
	if year < minyear || year > maxyear {
		return fmt.Errorf("ValueError: year %d is out of range", year)
	}
	if month < 1 || month > 12 {
		return fmt.Errorf("ValueError: month must be in 1..12")
	}
	dim := daysInMonth(year, month)
	if day < 1 || day > dim {
		return fmt.Errorf("ValueError: day is out of range for month")
	}
	return nil
}

// daysInMonth returns the number of days in the given month of the
// given year.
//
// CPython: Modules/_datetimemodule.c:300 days_in_month
func daysInMonth(year, month int64) int64 {
	if month == 2 && isLeap(year) {
		return 29
	}
	return [...]int64{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month]
}

// isLeap returns true for Gregorian leap years.
//
// CPython: Modules/_datetimemodule.c:290 is_leap
func isLeap(year int64) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// dateDataSize is the length in bytes of a pickled date state.
//
// CPython: Include/datetime.h:25 _PyDateTime_DATE_DATASIZE
const dateDataSize = 4

// monthIsSane mirrors CPython's MONTH_IS_SANE macro.
//
// CPython: Modules/_datetimemodule.c:325 MONTH_IS_SANE
func monthIsSane(m byte) bool { return uint(m)-1 < 12 }

// dateFromPickle rebuilds a date from a 4-byte state buffer
// [year_hi, year_lo, month, day].
//
// CPython: Modules/_datetimemodule.c:3193 date_from_pickle
func dateFromPickle(cls *objects.Type, state []byte) (objects.Object, error) {
	if len(state) != dateDataSize {
		return nil, fmt.Errorf("ValueError: bad date state length")
	}
	year := int64(state[0])<<8 | int64(state[1])
	month := int64(state[2])
	day := int64(state[3])
	d := &Date{Year: year, Month: month, Day: day}
	d.Init(cls)
	return d, nil
}

// dateNew is date.__new__. The pickle path passes a single 4-byte
// state (or a latin1 unicode equivalent) and routes through
// dateFromPickle; the normal constructor path takes (year, month, day).
//
// CPython: Modules/_datetimemodule.c:3206 date_new
func dateNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 1 && len(kwargs) == 0 {
		switch s := args[0].(type) {
		case *objects.Bytes:
			buf := s.Bytes()
			if len(buf) == dateDataSize && monthIsSane(buf[2]) {
				return dateFromPickle(cls, buf)
			}
		case *objects.Unicode:
			v := s.Value()
			if len(v) == dateDataSize && monthIsSane(v[2]) {
				return dateFromPickle(cls, []byte(v))
			}
		}
	}
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: date() takes exactly 3 arguments (%d given)", len(args))
	}
	year, err := asInt(args[0])
	if err != nil {
		return nil, err
	}
	month, err := asInt(args[1])
	if err != nil {
		return nil, err
	}
	day, err := asInt(args[2])
	if err != nil {
		return nil, err
	}
	d, err := newDate(year, month, day)
	if err != nil {
		return nil, err
	}
	d.Init(cls)
	return d, nil
}

// dateNewBuiltin is the Python-level wrapper that exposes dateNew as
// date.__new__. The first positional arg is the class; the rest is
// forwarded as the constructor argument list.
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper
func dateNewBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: date.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: date.__new__(X): X is not a type object")
	}
	return dateNew(cls, args[1:], kwargs)
}

// dateGetstate packs (year, month, day) into a 4-byte state buffer
// matching CPython's PyDateTime_Date data layout. The buffer is
// returned wrapped in a single-element tuple.
//
// CPython: Modules/_datetimemodule.c:3894 date_getstate
func dateGetstate(d *Date) *objects.Tuple {
	buf := []byte{
		byte((d.Year >> 8) & 0xff),
		byte(d.Year & 0xff),
		byte(d.Month),
		byte(d.Day),
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(buf)})
}

// dateReduce is date.__reduce__. Returns (cls, state) so pickle can
// recreate the date via cls.__new__(cls, state_bytes).
//
// CPython: Modules/_datetimemodule.c:3902 date_reduce
func dateReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	d, ok := args[0].(*Date)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'datetime.date' object")
	}
	return objects.NewTuple([]objects.Object{
		d.Type(),
		dateGetstate(d),
	}), nil
}

// dateGetattr exposes year, month, day attributes.
//
// CPython: Modules/_datetimemodule.c:3183 date_getattro
func dateGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	d := o.(*Date)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "year":
		return objects.NewInt(d.Year), nil
	case "month":
		return objects.NewInt(d.Month), nil
	case "day":
		return objects.NewInt(d.Day), nil
	}
	return objects.GenericGetAttr(o, name)
}

// dateRepr renders datetime.date(year, month, day).
//
// CPython: Modules/_datetimemodule.c:3173 date_repr
func dateRepr(o objects.Object) (string, error) {
	d := o.(*Date)
	return fmt.Sprintf("datetime.date(%d, %d, %d)", d.Year, d.Month, d.Day), nil
}

// dateStr renders the ISO 8601 string YYYY-MM-DD.
//
// CPython: Modules/_datetimemodule.c:3181 date_isoformat
func dateStr(o objects.Object) (string, error) {
	d := o.(*Date)
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day), nil
}

// dateHash hashes as a tuple of (year, month, day).
//
// CPython: Modules/_datetimemodule.c:3247 date_hash
func dateHash(o objects.Object) (int64, error) {
	d := o.(*Date)
	h := int64(0x345678)
	h = h*1000003 ^ d.Year
	h = h*1000003 ^ d.Month
	h = h*1000003 ^ d.Day
	return h, nil
}

// dateRichCmp compares two dates by (year, month, day).
//
// CPython: Modules/_datetimemodule.c:3255 date_richcompare
func dateRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	lhs, ok := a.(*Date)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Date)
	if !ok {
		return objects.NotImplemented(), nil
	}
	cmp := dateCompare(lhs, rhs)
	return richCmpInt(cmp, op), nil
}

// dateCompare returns -1/0/1 for a < b, a == b, a > b.
func dateCompare(a, b *Date) int {
	if a.Year != b.Year {
		if a.Year < b.Year {
			return -1
		}
		return 1
	}
	if a.Month != b.Month {
		if a.Month < b.Month {
			return -1
		}
		return 1
	}
	if a.Day != b.Day {
		if a.Day < b.Day {
			return -1
		}
		return 1
	}
	return 0
}

// richCmpInt converts -1/0/1 comparison result to a Python bool.
func richCmpInt(cmp int, op objects.CompareOp) objects.Object {
	var res bool
	switch op {
	case objects.CompareLT:
		res = cmp < 0
	case objects.CompareLE:
		res = cmp <= 0
	case objects.CompareEQ:
		res = cmp == 0
	case objects.CompareNE:
		res = cmp != 0
	case objects.CompareGT:
		res = cmp > 0
	case objects.CompareGE:
		res = cmp >= 0
	}
	return objects.NewBool(res)
}

// dateToGoTime converts a Date to a Go time.Time at midnight UTC.
func dateToGoTime(d *Date) gotime.Time {
	return gotime.Date(int(d.Year), gotime.Month(d.Month), int(d.Day), 0, 0, 0, 0, gotime.UTC)
}

// dateTodayMethod is date.today().
//
// CPython: Modules/_datetimemodule.c:3131 date_today
func dateTodayMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	now := gotime.Now()
	return newDate(int64(now.Year()), int64(now.Month()), int64(now.Day()))
}

// dateFromtimestampMethod is date.fromtimestamp(t).
//
// CPython: Modules/_datetimemodule.c:3143 date_fromtimestamp
func dateFromtimestampMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// First arg is the class (method descriptor receiver); actual arg is second.
	ts, err := asFloat64(args[len(args)-1])
	if err != nil {
		return nil, err
	}
	t := gotime.Unix(int64(ts), int64((ts-math.Trunc(ts))*1e9)).Local()
	return newDate(int64(t.Year()), int64(t.Month()), int64(t.Day()))
}

// dateFromisoformatMethod is date.fromisoformat(s).
//
// CPython: Modules/_datetimemodule.c:3160 date_fromisoformat
func dateFromisoformatMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := objects.Str(args[len(args)-1])
	if err != nil {
		return nil, err
	}
	t, err := gotime.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("ValueError: Invalid isoformat string: %q", s)
	}
	return newDate(int64(t.Year()), int64(t.Month()), int64(t.Day()))
}

// dateTimetuple is date.timetuple().
//
// CPython: Modules/_datetimemodule.c:3270 date_timetuple
func dateTimetuple(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*Date)
	gt := dateToGoTime(d)
	fields := []objects.Object{
		objects.NewInt(d.Year),
		objects.NewInt(d.Month),
		objects.NewInt(d.Day),
		objects.NewInt(0),
		objects.NewInt(0),
		objects.NewInt(0),
		objects.NewInt(int64(gt.Weekday()+6) % 7),
		objects.NewInt(int64(gt.YearDay())),
		objects.NewInt(-1),
	}
	return objects.NewTuple(fields), nil
}

// dateToordinal is date.toordinal().
//
// CPython: Modules/_datetimemodule.c:3284 date_toordinal
func dateToordinal(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*Date)
	gt := dateToGoTime(d)
	// Python ordinal: January 1, year 1 = 1.
	epoch := gotime.Date(1, 1, 1, 0, 0, 0, 0, gotime.UTC)
	ord := int64(gt.Sub(epoch).Hours()/24) + 1
	return objects.NewInt(ord), nil
}

// dateWeekday is date.weekday() (Monday=0).
//
// CPython: Modules/_datetimemodule.c:3292 date_weekday
func dateWeekday(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*Date)
	gt := dateToGoTime(d)
	wd := (int64(gt.Weekday()) + 6) % 7
	return objects.NewInt(wd), nil
}

// dateIsoweekday is date.isoweekday() (Monday=1).
//
// CPython: Modules/_datetimemodule.c:3298 date_isoweekday
func dateIsoweekday(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*Date)
	gt := dateToGoTime(d)
	wd := int64(gt.Weekday())
	if wd == 0 {
		wd = 7
	}
	return objects.NewInt(wd), nil
}

// dateIsocalendar is date.isocalendar() -> (ISO year, ISO week, ISO weekday).
//
// CPython: Modules/_datetimemodule.c:3305 date_isocalendar
func dateIsocalendar(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*Date)
	gt := dateToGoTime(d)
	isoYear, isoWeek := gt.ISOWeek()
	isoWD := int64(gt.Weekday())
	if isoWD == 0 {
		isoWD = 7
	}
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(int64(isoYear)),
		objects.NewInt(int64(isoWeek)),
		objects.NewInt(isoWD),
	})
	return tup, nil
}

// dateIsoformat is date.isoformat().
//
// CPython: Modules/_datetimemodule.c:3322 date_isoformat
func dateIsoformat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := dateStr(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// dateStrftime is date.strftime(fmt).
//
// CPython: Modules/_datetimemodule.c:3328 date_strftime
func dateStrftime(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: strftime() requires a format string")
	}
	d := args[0].(*Date)
	fmtStr, err := objects.Str(args[1])
	if err != nil {
		return nil, err
	}
	gt := dateToGoTime(d)
	goFmt, hasDirectives := pythonToGoFmt(fmtStr)
	if !hasDirectives {
		return objects.NewStr(fmtStr), nil
	}
	return objects.NewStr(gt.Format(goFmt)), nil
}

// dateFormatMethod is date.__format__(format_spec).
// Empty spec returns str(self); non-empty delegates to strftime.
//
// CPython: Modules/_datetimemodule.c:3598 date_format
func dateFormatMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __format__() takes exactly one argument")
	}
	spec, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __format__() argument 1 must be str")
	}
	if spec.Value() == "" {
		s, err := objects.Str(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	}
	return dateStrftime(args, kwargs)
}

// dateReplace is date.replace(**kw).
//
// CPython: Modules/_datetimemodule.c:3359 date_replace
func dateReplace(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*Date)
	year, month, day := d.Year, d.Month, d.Day
	if v, ok := kwargs["year"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		year = n
	}
	if v, ok := kwargs["month"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		month = n
	}
	if v, ok := kwargs["day"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		day = n
	}
	return newDate(year, month, day)
}

// asFloat64 extracts a float64 from an Int or Float object.
func asFloat64(o objects.Object) (float64, error) {
	switch v := o.(type) {
	case *objects.Float:
		return v.Float64(), nil
	case *objects.Int:
		n, ok := v.Int64()
		if !ok {
			return 0, fmt.Errorf("OverflowError: value too large")
		}
		return float64(n), nil
	}
	return 0, fmt.Errorf("TypeError: a float is required (got type %s)", o.Type().Name)
}

// ---------------------------------------------------------------------------
// timezone type.
// ---------------------------------------------------------------------------

// TimezoneType is datetime.timezone.
//
// CPython: Modules/_datetimemodule.c:7010 timezone_type
var TimezoneType = objects.NewType("datetime.timezone", []*objects.Type{objects.ObjectType()})

// Timezone is a fixed-offset tzinfo implementation.
//
// CPython: Modules/_datetimemodule.c:3690 PyDateTime_TimeZone
type Timezone struct {
	objects.Header
	// Offset is in microseconds (stored as Timedelta).
	Offset *Timedelta
	// Name is the optional name override. Empty means auto-generate.
	Name string
}

// TimezoneUTC, TimezoneMin, TimezoneMax are the class attributes.
//
// CPython: Modules/_datetimemodule.c:4040 timezone_utc
var (
	TimezoneUTC *Timezone
	TimezoneMin *Timezone
	TimezoneMax *Timezone
)

func init() {
	// Wire slots in init() to break the initialization cycle.
	TimezoneType.TpNew = timezoneNew
	TimezoneType.Repr = timezoneRepr
	TimezoneType.Str = timezoneStr
	TimezoneType.Hash = timezoneHash
	TimezoneType.RichCmp = timezoneRichCmp
	TimezoneType.Getattro = timezoneGetattr

	TimezoneUTC = mustTimezone(0, "UTC")
	// min offset: -23:59 = -(23*60+59) minutes = -(23*60+59)*60 seconds.
	TimezoneMin = mustTimezone(-(23*60+59)*60, "")
	TimezoneMax = mustTimezone((23*60+59)*60, "")
	objects.SetTypeDescr(TimezoneType, "utc", TimezoneUTC)
	objects.SetTypeDescr(TimezoneType, "min", TimezoneMin)
	objects.SetTypeDescr(TimezoneType, "max", TimezoneMax)
	objects.SetTypeDescr(TimezoneType, "utcoffset",
		objects.NewMethodDescr(TimezoneType, "utcoffset", timezoneUtcoffset))
	objects.SetTypeDescr(TimezoneType, "tzname",
		objects.NewMethodDescr(TimezoneType, "tzname", timezoneTzname))
	objects.SetTypeDescr(TimezoneType, "dst",
		objects.NewMethodDescr(TimezoneType, "dst", timezoneDst))
	objects.SetTypeDescr(TimezoneType, "fromutc",
		objects.NewMethodDescr(TimezoneType, "fromutc", timezoneFromutc))
	// Pickle hooks. CPython's tzinfo base type provides __reduce__ that
	// dispatches via __getinitargs__, yielding (cls, init_args). Since
	// gopy doesn't carry a separate tzinfo type, expose both on Timezone.
	//
	// CPython: Modules/_datetimemodule.c:4140 tzinfo_reduce
	// CPython: Modules/_datetimemodule.c:4433 timezone_getinitargs
	objects.SetTypeDescr(TimezoneType, "__new__",
		objects.NewBuiltinFunction("timezone.__new__", timezoneNewBuiltin))
	objects.SetTypeDescr(TimezoneType, "__getinitargs__",
		objects.NewMethodDescr(TimezoneType, "__getinitargs__", timezoneGetinitargs))
	objects.SetTypeDescr(TimezoneType, "__reduce__",
		objects.NewMethodDescr(TimezoneType, "__reduce__", timezoneReduce))
}

// timezoneNewBuiltin exposes timezoneNew as timezone.__new__.
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper
func timezoneNewBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: timezone.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: timezone.__new__(X): X is not a type object")
	}
	return timezoneNew(cls, args[1:], kwargs)
}

// timezoneReduce mirrors CPython's tzinfo.__reduce__, which gopy
// flattens onto Timezone since we lack a separate tzinfo base. The
// shape returned is (cls, init_args) so pickle proto 2+ replays the
// constructor on unpickle.
//
// CPython: Modules/_datetimemodule.c:4140 tzinfo_reduce
func timezoneReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	tz, ok := args[0].(*Timezone)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'datetime.timezone' object")
	}
	initArgs, err := timezoneGetinitargs([]objects.Object{tz}, nil)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{tz.Type(), initArgs}), nil
}

// timezoneGetinitargs returns the init args (offset[, name]) so
// copyreg.__reduce_ex__ can replay the constructor on unpickle.
//
// CPython: Modules/_datetimemodule.c:4433 timezone_getinitargs
func timezoneGetinitargs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getinitargs__() takes no arguments (%d given)", len(args)-1)
	}
	tz, ok := args[0].(*Timezone)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getinitargs__' requires a 'datetime.timezone' object")
	}
	if tz.Name == "" {
		return objects.NewTuple([]objects.Object{tz.Offset}), nil
	}
	return objects.NewTuple([]objects.Object{tz.Offset, objects.NewStr(tz.Name)}), nil
}

func mustTimezone(offsetSecs int64, name string) *Timezone {
	td, err := newTimedelta(0, offsetSecs, 0)
	if err != nil {
		panic(err)
	}
	tz := &Timezone{Offset: td, Name: name}
	tz.Init(TimezoneType)
	return tz
}

// timezoneNew is timezone.__new__(offset[, name]).
//
// CPython: Modules/_datetimemodule.c:3961 timezone_new
func timezoneNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: timezone() requires 1 or 2 arguments")
	}
	td, ok := args[0].(*Timedelta)
	if !ok {
		return nil, fmt.Errorf("TypeError: offset must be a timedelta")
	}
	name := ""
	if len(args) == 2 {
		n, err := objects.Str(args[1])
		if err != nil {
			return nil, err
		}
		name = n
	}
	// Validate offset is in range [-23:59, +23:59].
	us := timedeltaToUs(td)
	limit := int64((23*60+59)*60) * usPerSecond
	if us < -limit || us > limit {
		return nil, fmt.Errorf("ValueError: offset must be a timedelta strictly between -timedelta(hours=24) and timedelta(hours=24)")
	}
	tz := &Timezone{Offset: td, Name: name}
	tz.Init(cls)
	return tz, nil
}

func timezoneGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	return objects.GenericGetAttr(o, name)
}

// timezoneAutoName generates a UTC+HH:MM style name from the offset.
//
// CPython: Modules/_datetimemodule.c:3927 timezone_str
func timezoneAutoName(tz *Timezone) string {
	us := timedeltaToUs(tz.Offset)
	if us == 0 {
		return "UTC"
	}
	sign := "+"
	if us < 0 {
		sign = "-"
		us = -us
	}
	secs := us / usPerSecond
	mm := secs / 60
	hh := mm / 60
	mm = mm % 60
	ss := secs % 60
	if ss != 0 {
		return fmt.Sprintf("UTC%s%02d:%02d:%02d", sign, hh, mm, ss)
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, hh, mm)
}

func timezoneStr(o objects.Object) (string, error) {
	tz := o.(*Timezone)
	if tz.Name != "" {
		return tz.Name, nil
	}
	return timezoneAutoName(tz), nil
}

func timezoneRepr(o objects.Object) (string, error) {
	tz := o.(*Timezone)
	tdRepr, _ := timedeltaRepr(tz.Offset)
	if tz.Name != "" {
		return fmt.Sprintf("datetime.timezone(%s, %q)", tdRepr, tz.Name), nil
	}
	if timedeltaToUs(tz.Offset) == 0 {
		return "datetime.timezone.utc", nil
	}
	return fmt.Sprintf("datetime.timezone(%s)", tdRepr), nil
}

func timezoneHash(o objects.Object) (int64, error) {
	tz := o.(*Timezone)
	h, err := timedeltaHash(tz.Offset)
	if err != nil {
		return 0, err
	}
	return h*1000003 ^ int64(len(tz.Name)), nil
}

func timezoneRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	lhs, ok := a.(*Timezone)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Timezone)
	if !ok {
		return objects.NotImplemented(), nil
	}
	return timedeltaRichCmp(lhs.Offset, rhs.Offset, op)
}

// timezoneUtcoffset is timezone.utcoffset(dt) -> timedelta.
//
// CPython: Modules/_datetimemodule.c:3993 timezone_utcoffset
func timezoneUtcoffset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	tz := args[0].(*Timezone)
	return tz.Offset, nil
}

// timezoneTzname is timezone.tzname(dt) -> str.
//
// CPython: Modules/_datetimemodule.c:4003 timezone_tzname
func timezoneTzname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	tz := args[0].(*Timezone)
	return objects.NewStr(timezoneAutoName(tz)), nil
}

// timezoneDst is timezone.dst(dt) -> None.
//
// CPython: Modules/_datetimemodule.c:4016 timezone_dst
func timezoneDst(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// timezoneFromutc is timezone.fromutc(dt) -> datetime.
//
// CPython: Modules/_datetimemodule.c:4026 timezone_fromutc
func timezoneFromutc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	tz := args[0].(*Timezone)
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: fromutc() requires a datetime argument")
	}
	dt, ok := args[1].(*Datetime)
	if !ok {
		return nil, fmt.Errorf("TypeError: fromutc() argument must be a datetime instance")
	}
	// Apply the UTC offset to the datetime.
	offsetSecs := tz.Offset.Seconds + tz.Offset.Days*86400
	newDt := addSecsToDatetime(dt, offsetSecs)
	newDt.TzInfo = tz
	return newDt, nil
}

// addSecsToDatetime returns a new Datetime shifted by secs seconds.
func addSecsToDatetime(dt *Datetime, secs int64) *Datetime {
	gt := datetimeToGoTime(dt)
	gt = gt.Add(gotime.Duration(secs) * gotime.Second)
	nd := &Datetime{
		Date: Date{
			Year:  int64(gt.Year()),
			Month: int64(gt.Month()),
			Day:   int64(gt.Day()),
		},
		Hour:        int64(gt.Hour()),
		Minute:      int64(gt.Minute()),
		Second:      int64(gt.Second()),
		Microsecond: int64(gt.Nanosecond() / 1000),
		TzInfo:      dt.TzInfo,
	}
	nd.Init(DatetimeType)
	return nd
}

// ---------------------------------------------------------------------------
// time type.
// ---------------------------------------------------------------------------

// TimeType is datetime.time.
//
// CPython: Modules/_datetimemodule.c:4200 time_type
var TimeType = objects.NewType("datetime.time", []*objects.Type{objects.ObjectType()})

// Time backs a time-of-day instance.
//
// CPython: Modules/_datetimemodule.c:2900 PyDateTime_Time
type Time struct {
	objects.Header
	Hour        int64
	Minute      int64
	Second      int64
	Microsecond int64
	TzInfo      *Timezone
	Fold        int64
}

func init() {
	// Wire slots in init() to break the initialization cycle.
	TimeType.TpNew = timeNew
	TimeType.Repr = timeRepr
	TimeType.Str = timeStr
	TimeType.Hash = timeHash
	TimeType.RichCmp = timeRichCmp
	TimeType.Getattro = timeGetattr

	objects.SetTypeDescr(TimeType, "fromisoformat",
		objects.NewClassMethod(objects.NewBuiltinFunction("fromisoformat", timeFromisoformatMethod)))
	objects.SetTypeDescr(TimeType, "isoformat",
		objects.NewMethodDescr(TimeType, "isoformat", timeIsoformat))
	objects.SetTypeDescr(TimeType, "strftime",
		objects.NewMethodDescr(TimeType, "strftime", timeStrftime))
	objects.SetTypeDescr(TimeType, "replace",
		objects.NewMethodDescr(TimeType, "replace", timeReplace))
	objects.SetTypeDescr(TimeType, "utcoffset",
		objects.NewMethodDescr(TimeType, "utcoffset", timeUtcoffset))
	objects.SetTypeDescr(TimeType, "dst",
		objects.NewMethodDescr(TimeType, "dst", timeDst))
	objects.SetTypeDescr(TimeType, "tzname",
		objects.NewMethodDescr(TimeType, "tzname", timeTzname))
	objects.SetTypeDescr(TimeType, "min", mustTimeObj(0, 0, 0, 0, nil))
	objects.SetTypeDescr(TimeType, "max", mustTimeObj(23, 59, 59, 999999, nil))
	objects.SetTypeDescr(TimeType, "resolution", mustTimedelta2(0, 1))
	// Pickle hooks.
	//
	// CPython: Modules/_datetimemodule.c:5108 time_getstate
	// CPython: Modules/_datetimemodule.c:5140 time_reduce
	objects.SetTypeDescr(TimeType, "__new__",
		objects.NewBuiltinFunction("time.__new__", timeNewBuiltin))
	objects.SetTypeDescr(TimeType, "__reduce__",
		objects.NewMethodDescr(TimeType, "__reduce__", timeReduce))
	objects.SetTypeDescr(TimeType, "__reduce_ex__",
		objects.NewMethodDescr(TimeType, "__reduce_ex__", timeReduceEx))
}

// timeDataSize is the length of a pickled time state buffer.
//
// CPython: Include/datetime.h:28 _PyDateTime_TIME_DATASIZE
const timeDataSize = 6

// timeFromPickle rebuilds a time from a 6-byte state buffer plus
// optional tzinfo. When the fold-flag bit is set on byte[0] (proto>3),
// we clear it and stamp fold=1 on the result.
//
// CPython: Modules/_datetimemodule.c:4592 time_from_pickle
func timeFromPickle(cls *objects.Type, state []byte, tz *Timezone) (objects.Object, error) {
	if len(state) != timeDataSize {
		return nil, fmt.Errorf("ValueError: bad time state length")
	}
	buf := make([]byte, timeDataSize)
	copy(buf, state)
	var fold int64
	if buf[0]&0x80 != 0 {
		buf[0] -= 128
		fold = 1
	}
	hour := int64(buf[0])
	minute := int64(buf[1])
	second := int64(buf[2])
	us := int64(buf[3])<<16 | int64(buf[4])<<8 | int64(buf[5])
	t := &Time{Hour: hour, Minute: minute, Second: second, Microsecond: us, TzInfo: tz, Fold: fold}
	t.Init(cls)
	return t, nil
}

// timeGetstate packs (hour, minute, second, microsecond) into a
// 6-byte state buffer. Proto>3 sets the fold flag in bit 7 of byte[0].
//
// CPython: Modules/_datetimemodule.c:5108 time_getstate
func timeGetstate(t *Time, proto int) *objects.Tuple {
	buf := []byte{
		byte(t.Hour),
		byte(t.Minute),
		byte(t.Second),
		byte((t.Microsecond >> 16) & 0xff),
		byte((t.Microsecond >> 8) & 0xff),
		byte(t.Microsecond & 0xff),
	}
	if proto > 3 && t.Fold != 0 {
		buf[0] |= 1 << 7
	}
	items := []objects.Object{objects.NewBytes(buf)}
	if t.TzInfo != nil {
		items = append(items, t.TzInfo)
	}
	return objects.NewTuple(items)
}

// timeReduce is time.__reduce__. Uses protocol 2 conventions.
//
// CPython: Modules/_datetimemodule.c:5140 time_reduce
func timeReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	t, ok := args[0].(*Time)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'datetime.time' object")
	}
	return objects.NewTuple([]objects.Object{t.Type(), timeGetstate(t, 2)}), nil
}

// timeReduceEx is time.__reduce_ex__(protocol). For proto>3 the
// state's fold flag is encoded in bit 7 of byte[0].
//
// CPython: Modules/_datetimemodule.c:5129 time_reduce_ex
func timeReduceEx(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __reduce_ex__() takes exactly one argument (%d given)", len(args)-1)
	}
	t, ok := args[0].(*Time)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce_ex__' requires a 'datetime.time' object")
	}
	proto, err := asInt(args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{t.Type(), timeGetstate(t, int(proto))}), nil
}

// timeNewBuiltin is the Python-level wrapper that exposes timeNew as
// time.__new__.
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper
func timeNewBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: time.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: time.__new__(X): X is not a type object")
	}
	return timeNew(cls, args[1:], kwargs)
}

func mustTimeObj(h, m, s, us int64, tz *Timezone) *Time {
	t, err := newTimeObj(h, m, s, us, tz, 0)
	if err != nil {
		panic(err)
	}
	return t
}

func mustTimedelta2(secs, us int64) *Timedelta {
	td, err := newTimedelta(0, secs, us)
	if err != nil {
		panic(err)
	}
	return td
}

// newTimeObj validates and allocates a Time.
//
// CPython: Modules/_datetimemodule.c:2900 new_time
func newTimeObj(hour, minute, second, microsecond int64, tz *Timezone, fold int64) (*Time, error) {
	if err := checkTime(hour, minute, second, microsecond, fold); err != nil {
		return nil, err
	}
	t := &Time{Hour: hour, Minute: minute, Second: second, Microsecond: microsecond, TzInfo: tz, Fold: fold}
	t.Init(TimeType)
	return t, nil
}

// checkTime validates time field ranges.
//
// CPython: Modules/_datetimemodule.c:2900 check_time_args
func checkTime(hour, minute, second, microsecond, fold int64) error {
	if hour < 0 || hour > 23 {
		return fmt.Errorf("ValueError: hour must be in 0..23")
	}
	if minute < 0 || minute > 59 {
		return fmt.Errorf("ValueError: minute must be in 0..59")
	}
	if second < 0 || second > 59 {
		return fmt.Errorf("ValueError: second must be in 0..59")
	}
	if microsecond < 0 || microsecond > 999999 {
		return fmt.Errorf("ValueError: microsecond must be in 0..999999")
	}
	if fold < 0 || fold > 1 {
		return fmt.Errorf("ValueError: fold must be either 0 or 1")
	}
	return nil
}

// timeNew is time.__new__.
//
// CPython: Modules/_datetimemodule.c:4094 time_new
func timeNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if (len(args) == 1 || len(args) == 2) && len(kwargs) == 0 {
		var tz *Timezone
		if len(args) == 2 {
			if !objects.IsNone(args[1]) {
				tzv, ok := args[1].(*Timezone)
				if !ok {
					return nil, fmt.Errorf("TypeError: tzinfo must be a timezone or None")
				}
				tz = tzv
			}
		}
		switch s := args[0].(type) {
		case *objects.Bytes:
			buf := s.Bytes()
			if len(buf) == timeDataSize && (buf[0]&0x7f) < 24 {
				return timeFromPickle(cls, buf, tz)
			}
		case *objects.Unicode:
			v := s.Value()
			if len(v) == timeDataSize && (v[0]&0x7f) < 24 {
				return timeFromPickle(cls, []byte(v), tz)
			}
		}
	}
	var hour, minute, second, microsecond, fold int64
	var tz *Timezone
	if err := parseTimeArgs(args, kwargs, &hour, &minute, &second, &microsecond, &tz, &fold); err != nil {
		return nil, err
	}
	t, err := newTimeObj(hour, minute, second, microsecond, tz, fold)
	if err != nil {
		return nil, err
	}
	t.Init(cls)
	return t, nil
}

func parseTimeArgs(args []objects.Object, kwargs map[string]objects.Object,
	hour, minute, second, microsecond *int64, tz **Timezone, fold *int64) error {
	names := []string{"hour", "minute", "second", "microsecond", "tzinfo", "fold"}
	intDsts := []*int64{hour, minute, second, microsecond}
	for i, a := range args {
		if i >= len(names) {
			return fmt.Errorf("TypeError: time() takes at most 6 arguments")
		}
		if i < 4 {
			n, err := asInt(a)
			if err != nil {
				return err
			}
			*intDsts[i] = n
		} else if i == 4 {
			if !objects.IsNone(a) {
				t, ok := a.(*Timezone)
				if !ok {
					return fmt.Errorf("TypeError: tzinfo must be a timezone or None")
				}
				*tz = t
			}
		} else if i == 5 {
			n, err := asInt(a)
			if err != nil {
				return err
			}
			*fold = n
		}
	}
	for k, v := range kwargs {
		switch k {
		case "hour":
			n, err := asInt(v)
			if err != nil {
				return err
			}
			*hour = n
		case "minute":
			n, err := asInt(v)
			if err != nil {
				return err
			}
			*minute = n
		case "second":
			n, err := asInt(v)
			if err != nil {
				return err
			}
			*second = n
		case "microsecond":
			n, err := asInt(v)
			if err != nil {
				return err
			}
			*microsecond = n
		case "tzinfo":
			if !objects.IsNone(v) {
				t, ok := v.(*Timezone)
				if !ok {
					return fmt.Errorf("TypeError: tzinfo must be a timezone or None")
				}
				*tz = t
			}
		case "fold":
			n, err := asInt(v)
			if err != nil {
				return err
			}
			*fold = n
		default:
			return fmt.Errorf("TypeError: time() got an unexpected keyword argument '%s'", k)
		}
	}
	return nil
}

func timeGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	t := o.(*Time)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "hour":
		return objects.NewInt(t.Hour), nil
	case "minute":
		return objects.NewInt(t.Minute), nil
	case "second":
		return objects.NewInt(t.Second), nil
	case "microsecond":
		return objects.NewInt(t.Microsecond), nil
	case "tzinfo":
		if t.TzInfo == nil {
			return objects.None(), nil
		}
		return t.TzInfo, nil
	case "fold":
		return objects.NewInt(t.Fold), nil
	}
	return objects.GenericGetAttr(o, name)
}

// timeStr renders HH:MM:SS[.ffffff][+HH:MM].
//
// CPython: Modules/_datetimemodule.c:3876 time_isoformat
func timeStr(o objects.Object) (string, error) {
	return timeIsoformatImpl(o.(*Time), "")
}

func timeRepr(o objects.Object) (string, error) {
	t := o.(*Time)
	s := fmt.Sprintf("datetime.time(%d, %d, %d", t.Hour, t.Minute, t.Second)
	if t.Microsecond != 0 {
		s += fmt.Sprintf(", %d", t.Microsecond)
	}
	if t.TzInfo != nil {
		tzStr, _ := timezoneStr(t.TzInfo)
		s += fmt.Sprintf(", tzinfo=datetime.timezone(%s)", tzStr)
	}
	s += ")"
	return s, nil
}

func timeHash(o objects.Object) (int64, error) {
	t := o.(*Time)
	h := int64(0x345678)
	h = h*1000003 ^ t.Hour
	h = h*1000003 ^ t.Minute
	h = h*1000003 ^ t.Second
	h = h*1000003 ^ t.Microsecond
	return h, nil
}

func timeRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	lhs, ok := a.(*Time)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Time)
	if !ok {
		return objects.NotImplemented(), nil
	}
	cmp := timeCompare(lhs, rhs)
	return richCmpInt(cmp, op), nil
}

func timeCompare(a, b *Time) int {
	if a.Hour != b.Hour {
		if a.Hour < b.Hour {
			return -1
		}
		return 1
	}
	if a.Minute != b.Minute {
		if a.Minute < b.Minute {
			return -1
		}
		return 1
	}
	if a.Second != b.Second {
		if a.Second < b.Second {
			return -1
		}
		return 1
	}
	if a.Microsecond != b.Microsecond {
		if a.Microsecond < b.Microsecond {
			return -1
		}
		return 1
	}
	return 0
}

// timeFromisoformatMethod is time.fromisoformat(s).
//
// CPython: Modules/_datetimemodule.c:4050 time_fromisoformat
func timeFromisoformatMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := objects.Str(args[len(args)-1])
	if err != nil {
		return nil, err
	}
	// Try parsing various time formats: HH:MM, HH:MM:SS, HH:MM:SS.ffffff.
	formats := []string{"15:04", "15:04:05", "15:04:05.000000"}
	for _, f := range formats {
		if t, err2 := gotime.Parse(f, s); err2 == nil {
			us := int64(t.Nanosecond() / 1000)
			return newTimeObj(int64(t.Hour()), int64(t.Minute()), int64(t.Second()), us, nil, 0)
		}
	}
	return nil, fmt.Errorf("ValueError: Invalid isoformat string: %q", s)
}

// timeIsoformat is time.isoformat(timespec='auto').
//
// CPython: Modules/_datetimemodule.c:3897 time_isoformat
func timeIsoformat(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	t := args[0].(*Time)
	timespec := "auto"
	if v, ok := kwargs["timespec"]; ok {
		ts, err := objects.Str(v)
		if err != nil {
			return nil, err
		}
		timespec = ts
	}
	s, err := timeIsoformatImpl(t, timespec)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

func timeIsoformatImpl(t *Time, timespec string) (string, error) {
	s := fmt.Sprintf("%02d:%02d:%02d", t.Hour, t.Minute, t.Second)
	switch timespec {
	case "", "auto":
		if t.Microsecond != 0 {
			s += fmt.Sprintf(".%06d", t.Microsecond)
		}
	case "hours":
		s = fmt.Sprintf("%02d", t.Hour)
	case "minutes":
		s = fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
	case "seconds":
		// s is already HH:MM:SS
	case "milliseconds":
		s += fmt.Sprintf(".%03d", t.Microsecond/1000)
	case "microseconds":
		s += fmt.Sprintf(".%06d", t.Microsecond)
	default:
		return "", fmt.Errorf("ValueError: unknown timespec value")
	}
	if t.TzInfo != nil {
		offsetUs := timedeltaToUs(t.TzInfo.Offset)
		sign := "+"
		if offsetUs < 0 {
			sign = "-"
			offsetUs = -offsetUs
		}
		secs := offsetUs / usPerSecond
		mm := secs / 60
		hh := mm / 60
		mm = mm % 60
		s += fmt.Sprintf("%s%02d:%02d", sign, hh, mm)
	}
	return s, nil
}

// timeStrftime is time.strftime(fmt).
//
// CPython: Modules/_datetimemodule.c:4140 time_strftime
func timeStrftime(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: strftime() requires a format string")
	}
	t := args[0].(*Time)
	fmtStr, err := objects.Str(args[1])
	if err != nil {
		return nil, err
	}
	gt := gotime.Date(1900, 1, 1, int(t.Hour), int(t.Minute), int(t.Second),
		int(t.Microsecond)*1000, gotime.UTC)
	goFmt, hasDirectives := pythonToGoFmt(fmtStr)
	if !hasDirectives {
		return objects.NewStr(fmtStr), nil
	}
	return objects.NewStr(gt.Format(goFmt)), nil
}

// timeReplace is time.replace(**kw).
//
// CPython: Modules/_datetimemodule.c:4165 time_replace
func timeReplace(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	t := args[0].(*Time)
	hour, minute, second, microsecond, fold := t.Hour, t.Minute, t.Second, t.Microsecond, t.Fold
	tz := t.TzInfo
	if v, ok := kwargs["hour"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		hour = n
	}
	if v, ok := kwargs["minute"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		minute = n
	}
	if v, ok := kwargs["second"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		second = n
	}
	if v, ok := kwargs["microsecond"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		microsecond = n
	}
	if v, ok := kwargs["tzinfo"]; ok {
		if objects.IsNone(v) {
			tz = nil
		} else {
			tzVal, ok := v.(*Timezone)
			if !ok {
				return nil, fmt.Errorf("TypeError: tzinfo must be a timezone or None")
			}
			tz = tzVal
		}
	}
	if v, ok := kwargs["fold"]; ok {
		n, err := asInt(v)
		if err != nil {
			return nil, err
		}
		fold = n
	}
	return newTimeObj(hour, minute, second, microsecond, tz, fold)
}

// timeUtcoffset is time.utcoffset().
//
// CPython: Modules/_datetimemodule.c:4180 time_utcoffset
func timeUtcoffset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t := args[0].(*Time)
	if t.TzInfo == nil {
		return objects.None(), nil
	}
	return t.TzInfo.Offset, nil
}

// timeDst is time.dst().
//
// CPython: Modules/_datetimemodule.c:4190 time_dst
func timeDst(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t := args[0].(*Time)
	if t.TzInfo == nil {
		return objects.None(), nil
	}
	return objects.None(), nil
}

// timeTzname is time.tzname().
//
// CPython: Modules/_datetimemodule.c:4195 time_tzname
func timeTzname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t := args[0].(*Time)
	if t.TzInfo == nil {
		return objects.None(), nil
	}
	return objects.NewStr(timezoneAutoName(t.TzInfo)), nil
}

// ---------------------------------------------------------------------------
// datetime type (subclass of date).
// ---------------------------------------------------------------------------

// DatetimeType is datetime.datetime, a subtype of date.
//
// CPython: Modules/_datetimemodule.c:6948 datetime_type
var DatetimeType = objects.NewType("datetime.datetime", []*objects.Type{DateType})

// Datetime backs a datetime instance.
//
// CPython: Modules/_datetimemodule.c:2960 PyDateTime_DateTime
type Datetime struct {
	Date
	Hour        int64
	Minute      int64
	Second      int64
	Microsecond int64
	TzInfo      *Timezone
	Fold        int64
}

func init() {
	// Wire slots in init() to break the initialization cycle.
	DatetimeType.TpNew = datetimeNew
	DatetimeType.Repr = datetimeRepr
	DatetimeType.Str = datetimeStr
	DatetimeType.Hash = datetimeHash
	DatetimeType.RichCmp = datetimeRichCmp
	DatetimeType.Getattro = datetimeGetattr

	objects.SetTypeDescr(DatetimeType, "now",
		objects.NewClassMethod(objects.NewBuiltinFunction("now", datetimeNowMethod)))
	objects.SetTypeDescr(DatetimeType, "utcnow",
		objects.NewClassMethod(objects.NewBuiltinFunction("utcnow", datetimeUtcnowMethod)))
	objects.SetTypeDescr(DatetimeType, "fromtimestamp",
		objects.NewClassMethod(objects.NewBuiltinFunction("fromtimestamp", datetimeFromtimestampMethod)))
	objects.SetTypeDescr(DatetimeType, "utcfromtimestamp",
		objects.NewClassMethod(objects.NewBuiltinFunction("utcfromtimestamp", datetimeUtcfromtimestampMethod)))
	objects.SetTypeDescr(DatetimeType, "combine",
		objects.NewClassMethod(objects.NewBuiltinFunction("combine", datetimeCombine)))
	objects.SetTypeDescr(DatetimeType, "fromisoformat",
		objects.NewClassMethod(objects.NewBuiltinFunction("fromisoformat", datetimeFromisoformatMethod)))
	objects.SetTypeDescr(DatetimeType, "timetuple",
		objects.NewMethodDescr(DatetimeType, "timetuple", datetimeTimetuple))
	objects.SetTypeDescr(DatetimeType, "utctimetuple",
		objects.NewMethodDescr(DatetimeType, "utctimetuple", datetimeUtctimetuple))
	objects.SetTypeDescr(DatetimeType, "date",
		objects.NewMethodDescr(DatetimeType, "date", datetimeDateMethod))
	objects.SetTypeDescr(DatetimeType, "time",
		objects.NewMethodDescr(DatetimeType, "time", datetimeTimeMethod))
	objects.SetTypeDescr(DatetimeType, "timetz",
		objects.NewMethodDescr(DatetimeType, "timetz", datetimeTimetz))
	objects.SetTypeDescr(DatetimeType, "replace",
		objects.NewMethodDescr(DatetimeType, "replace", datetimeReplace))
	objects.SetTypeDescr(DatetimeType, "astimezone",
		objects.NewMethodDescr(DatetimeType, "astimezone", datetimeAstimezone))
	objects.SetTypeDescr(DatetimeType, "utcoffset",
		objects.NewMethodDescr(DatetimeType, "utcoffset", datetimeUtcoffset))
	objects.SetTypeDescr(DatetimeType, "dst",
		objects.NewMethodDescr(DatetimeType, "dst", datetimeDst))
	objects.SetTypeDescr(DatetimeType, "tzname",
		objects.NewMethodDescr(DatetimeType, "tzname", datetimeTzname))
	objects.SetTypeDescr(DatetimeType, "timestamp",
		objects.NewMethodDescr(DatetimeType, "timestamp", datetimeTimestamp))
	objects.SetTypeDescr(DatetimeType, "weekday",
		objects.NewMethodDescr(DatetimeType, "weekday", datetimeWeekday))
	objects.SetTypeDescr(DatetimeType, "isoweekday",
		objects.NewMethodDescr(DatetimeType, "isoweekday", datetimeIsoweekday))
	objects.SetTypeDescr(DatetimeType, "isocalendar",
		objects.NewMethodDescr(DatetimeType, "isocalendar", datetimeIsocalendar))
	objects.SetTypeDescr(DatetimeType, "isoformat",
		objects.NewMethodDescr(DatetimeType, "isoformat", datetimeIsoformat))
	objects.SetTypeDescr(DatetimeType, "strftime",
		objects.NewMethodDescr(DatetimeType, "strftime", datetimeStrftime))
	objects.SetTypeDescr(DatetimeType, "__format__",
		objects.NewMethodDescr(DatetimeType, "__format__", datetimeFormatMethod))
	objects.SetTypeDescr(DatetimeType, "min",
		mustDatetime(minyear, 1, 1, 0, 0, 0, 0, nil))
	objects.SetTypeDescr(DatetimeType, "max",
		mustDatetime(maxyear, 12, 31, 23, 59, 59, 999999, nil))
	objects.SetTypeDescr(DatetimeType, "resolution", mustTimedelta2(0, 1))
	// Pickle hooks.
	//
	// CPython: Modules/_datetimemodule.c:6976 datetime_getstate
	// CPython: Modules/_datetimemodule.c:7009 datetime_reduce
	objects.SetTypeDescr(DatetimeType, "__new__",
		objects.NewBuiltinFunction("datetime.__new__", datetimeNewBuiltin))
	objects.SetTypeDescr(DatetimeType, "__reduce__",
		objects.NewMethodDescr(DatetimeType, "__reduce__", datetimeReduce))
	objects.SetTypeDescr(DatetimeType, "__reduce_ex__",
		objects.NewMethodDescr(DatetimeType, "__reduce_ex__", datetimeReduceEx))
}

// datetimeDataSize is the length of a pickled datetime state buffer.
//
// CPython: Include/datetime.h:31 _PyDateTime_DATETIME_DATASIZE
const datetimeDataSize = 10

// datetimeFromPickle rebuilds a datetime from a 10-byte state buffer
// plus optional tzinfo. CPython encodes the fold flag in bit 7 of
// byte[2] (the month byte); we clear it and stamp fold=1 on the
// result.
//
// CPython: Modules/_datetimemodule.c:5314 datetime_from_pickle
func datetimeFromPickle(cls *objects.Type, state []byte, tz *Timezone) (objects.Object, error) {
	if len(state) != datetimeDataSize {
		return nil, fmt.Errorf("ValueError: bad datetime state length")
	}
	buf := make([]byte, datetimeDataSize)
	copy(buf, state)
	var fold int64
	if buf[2]&0x80 != 0 {
		buf[2] -= 128
		fold = 1
	}
	year := int64(buf[0])<<8 | int64(buf[1])
	month := int64(buf[2])
	day := int64(buf[3])
	hour := int64(buf[4])
	minute := int64(buf[5])
	second := int64(buf[6])
	us := int64(buf[7])<<16 | int64(buf[8])<<8 | int64(buf[9])
	dt := &Datetime{
		Date:        Date{Year: year, Month: month, Day: day},
		Hour:        hour,
		Minute:      minute,
		Second:      second,
		Microsecond: us,
		TzInfo:      tz,
		Fold:        fold,
	}
	dt.Init(cls)
	return dt, nil
}

// datetimeGetstate packs (year_hi, year_lo, month, day, hour, minute,
// second, us_hi, us_mid, us_lo) into a 10-byte buffer. Proto>3 sets
// the fold flag in bit 7 of byte[2].
//
// CPython: Modules/_datetimemodule.c:6976 datetime_getstate
func datetimeGetstate(dt *Datetime, proto int) *objects.Tuple {
	buf := []byte{
		byte((dt.Year >> 8) & 0xff),
		byte(dt.Year & 0xff),
		byte(dt.Month),
		byte(dt.Day),
		byte(dt.Hour),
		byte(dt.Minute),
		byte(dt.Second),
		byte((dt.Microsecond >> 16) & 0xff),
		byte((dt.Microsecond >> 8) & 0xff),
		byte(dt.Microsecond & 0xff),
	}
	if proto > 3 && dt.Fold != 0 {
		buf[2] |= 1 << 7
	}
	items := []objects.Object{objects.NewBytes(buf)}
	if dt.TzInfo != nil {
		items = append(items, dt.TzInfo)
	}
	return objects.NewTuple(items)
}

// datetimeReduce is datetime.__reduce__. Uses protocol 2 conventions.
//
// CPython: Modules/_datetimemodule.c:7009 datetime_reduce
func datetimeReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	dt, ok := args[0].(*Datetime)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'datetime.datetime' object")
	}
	return objects.NewTuple([]objects.Object{dt.Type(), datetimeGetstate(dt, 2)}), nil
}

// datetimeReduceEx is datetime.__reduce_ex__(protocol).
//
// CPython: Modules/_datetimemodule.c:6997 datetime_reduce_ex
func datetimeReduceEx(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __reduce_ex__() takes exactly one argument (%d given)", len(args)-1)
	}
	dt, ok := args[0].(*Datetime)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce_ex__' requires a 'datetime.datetime' object")
	}
	proto, err := asInt(args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{dt.Type(), datetimeGetstate(dt, int(proto))}), nil
}

// datetimeNewBuiltin is the Python-level wrapper that exposes
// datetimeNew as datetime.__new__.
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper
func datetimeNewBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: datetime.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: datetime.__new__(X): X is not a type object")
	}
	return datetimeNew(cls, args[1:], kwargs)
}

func mustDatetime(y, mo, d, h, mi, s, us int64, tz *Timezone) *Datetime {
	dt, err := newDatetime(y, mo, d, h, mi, s, us, tz, 0)
	if err != nil {
		panic(err)
	}
	return dt
}

// newDatetime validates and allocates a Datetime.
//
// CPython: Modules/_datetimemodule.c:2960 new_datetime
func newDatetime(year, month, day, hour, minute, second, microsecond int64, tz *Timezone, fold int64) (*Datetime, error) {
	if err := checkDate(year, month, day); err != nil {
		return nil, err
	}
	if err := checkTime(hour, minute, second, microsecond, fold); err != nil {
		return nil, err
	}
	dt := &Datetime{
		Date:        Date{Year: year, Month: month, Day: day},
		Hour:        hour,
		Minute:      minute,
		Second:      second,
		Microsecond: microsecond,
		TzInfo:      tz,
		Fold:        fold,
	}
	dt.Init(DatetimeType)
	return dt, nil
}

// datetimeNew is datetime.__new__.
//
// CPython: Modules/_datetimemodule.c:5550 datetime_new
func datetimeNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if (len(args) == 1 || len(args) == 2) && len(kwargs) == 0 {
		var tz *Timezone
		if len(args) == 2 {
			if !objects.IsNone(args[1]) {
				tzv, ok := args[1].(*Timezone)
				if !ok {
					return nil, fmt.Errorf("TypeError: tzinfo must be a timezone or None")
				}
				tz = tzv
			}
		}
		switch s := args[0].(type) {
		case *objects.Bytes:
			buf := s.Bytes()
			if len(buf) == datetimeDataSize && monthIsSane(buf[2]&0x7f) {
				return datetimeFromPickle(cls, buf, tz)
			}
		case *objects.Unicode:
			v := s.Value()
			if len(v) == datetimeDataSize && monthIsSane(v[2]&0x7f) {
				return datetimeFromPickle(cls, []byte(v), tz)
			}
		}
	}
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: datetime() requires at least 3 arguments (year, month, day)")
	}
	year, err := asInt(args[0])
	if err != nil {
		return nil, err
	}
	month, err := asInt(args[1])
	if err != nil {
		return nil, err
	}
	day, err := asInt(args[2])
	if err != nil {
		return nil, err
	}
	var hour, minute, second, microsecond, fold int64
	var tz *Timezone
	if len(args) > 3 {
		if hour, err = asInt(args[3]); err != nil {
			return nil, err
		}
	}
	if len(args) > 4 {
		if minute, err = asInt(args[4]); err != nil {
			return nil, err
		}
	}
	if len(args) > 5 {
		if second, err = asInt(args[5]); err != nil {
			return nil, err
		}
	}
	if len(args) > 6 {
		if microsecond, err = asInt(args[6]); err != nil {
			return nil, err
		}
	}
	if len(args) > 7 {
		if !objects.IsNone(args[7]) {
			tzVal, ok := args[7].(*Timezone)
			if !ok {
				return nil, fmt.Errorf("TypeError: tzinfo must be a timezone or None")
			}
			tz = tzVal
		}
	}
	// Process kwargs.
	for k, v := range kwargs {
		switch k {
		case "year":
			year, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "month":
			month, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "day":
			day, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "hour":
			hour, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "minute":
			minute, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "second":
			second, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "microsecond":
			microsecond, err = asInt(v)
			if err != nil {
				return nil, err
			}
		case "tzinfo":
			if objects.IsNone(v) {
				tz = nil
			} else {
				tzVal, ok := v.(*Timezone)
				if !ok {
					return nil, fmt.Errorf("TypeError: tzinfo must be a timezone or None")
				}
				tz = tzVal
			}
		case "fold":
			fold, err = asInt(v)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("TypeError: datetime() got an unexpected keyword argument '%s'", k)
		}
	}
	dt, err := newDatetime(year, month, day, hour, minute, second, microsecond, tz, fold)
	if err != nil {
		return nil, err
	}
	dt.Init(cls)
	return dt, nil
}

func datetimeGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	dt := o.(*Datetime)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "year":
		return objects.NewInt(dt.Year), nil
	case "month":
		return objects.NewInt(dt.Month), nil
	case "day":
		return objects.NewInt(dt.Day), nil
	case "hour":
		return objects.NewInt(dt.Hour), nil
	case "minute":
		return objects.NewInt(dt.Minute), nil
	case "second":
		return objects.NewInt(dt.Second), nil
	case "microsecond":
		return objects.NewInt(dt.Microsecond), nil
	case "tzinfo":
		if dt.TzInfo == nil {
			return objects.None(), nil
		}
		return dt.TzInfo, nil
	case "fold":
		return objects.NewInt(dt.Fold), nil
	}
	return objects.GenericGetAttr(o, name)
}

// datetimeToGoTime converts a Datetime to a Go time.Time.
func datetimeToGoTime(dt *Datetime) gotime.Time {
	loc := gotime.UTC
	if dt.TzInfo != nil {
		offsetSecs := timedeltaToUs(dt.TzInfo.Offset) / usPerSecond
		loc = gotime.FixedZone(timezoneAutoName(dt.TzInfo), int(offsetSecs))
	}
	return gotime.Date(int(dt.Year), gotime.Month(dt.Month), int(dt.Day),
		int(dt.Hour), int(dt.Minute), int(dt.Second),
		int(dt.Microsecond)*1000, loc)
}

// datetimeFromGoTime builds a Datetime from a Go time.Time.
func datetimeFromGoTime(t gotime.Time, tz *Timezone) (*Datetime, error) {
	return newDatetime(
		int64(t.Year()), int64(t.Month()), int64(t.Day()),
		int64(t.Hour()), int64(t.Minute()), int64(t.Second()),
		int64(t.Nanosecond()/1000), tz, 0)
}

func datetimeRepr(o objects.Object) (string, error) {
	dt := o.(*Datetime)
	s := fmt.Sprintf("datetime.datetime(%d, %d, %d, %d, %d, %d",
		dt.Year, dt.Month, dt.Day, dt.Hour, dt.Minute, dt.Second)
	if dt.Microsecond != 0 {
		s += fmt.Sprintf(", %d", dt.Microsecond)
	}
	if dt.TzInfo != nil {
		tzName := timezoneAutoName(dt.TzInfo)
		s += fmt.Sprintf(", tzinfo=datetime.timezone(%s)", tzName)
	}
	s += ")"
	return s, nil
}

// datetimeStr renders the ISO 8601 string.
//
// CPython: Modules/_datetimemodule.c:5725 datetime_isoformat
func datetimeStr(o objects.Object) (string, error) {
	return datetimeIsoformatImpl(o.(*Datetime), "T", "auto")
}

func datetimeHash(o objects.Object) (int64, error) {
	dt := o.(*Datetime)
	h := int64(0x345678)
	h = h*1000003 ^ dt.Year
	h = h*1000003 ^ dt.Month
	h = h*1000003 ^ dt.Day
	h = h*1000003 ^ dt.Hour
	h = h*1000003 ^ dt.Minute
	h = h*1000003 ^ dt.Second
	h = h*1000003 ^ dt.Microsecond
	return h, nil
}

func datetimeRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	lhs, ok := a.(*Datetime)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*Datetime)
	if !ok {
		// Fall back to comparing as dates if rhs is a plain date.
		if _, ok2 := b.(*Date); ok2 {
			return objects.NotImplemented(), nil
		}
		return objects.NotImplemented(), nil
	}
	cmp := datetimeCompare(lhs, rhs)
	return richCmpInt(cmp, op), nil
}

func datetimeCompare(a, b *Datetime) int {
	c := dateCompare(&a.Date, &b.Date)
	if c != 0 {
		return c
	}
	return timeCompare(
		&Time{Hour: a.Hour, Minute: a.Minute, Second: a.Second, Microsecond: a.Microsecond},
		&Time{Hour: b.Hour, Minute: b.Minute, Second: b.Second, Microsecond: b.Microsecond},
	)
}

// datetimeNowMethod is datetime.now([tz]).
//
// CPython: Modules/_datetimemodule.c:5607 datetime_now
func datetimeNowMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var tz *Timezone
	// args[0] is the class (method descriptor receiver).
	if len(args) > 1 && !objects.IsNone(args[1]) {
		tzVal, ok := args[1].(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: tz must be a timezone or None")
		}
		tz = tzVal
	}
	if v, ok := kwargs["tz"]; ok && !objects.IsNone(v) {
		tzVal, ok := v.(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: tz must be a timezone or None")
		}
		tz = tzVal
	}
	var t gotime.Time
	if tz != nil {
		offsetSecs := timedeltaToUs(tz.Offset) / usPerSecond
		loc := gotime.FixedZone(timezoneAutoName(tz), int(offsetSecs))
		t = gotime.Now().In(loc)
	} else {
		t = gotime.Now().Local()
	}
	return datetimeFromGoTime(t, tz)
}

// datetimeUtcnowMethod is datetime.utcnow().
//
// CPython: Modules/_datetimemodule.c:5628 datetime_utcnow
func datetimeUtcnowMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t := gotime.Now().UTC()
	return datetimeFromGoTime(t, nil)
}

// datetimeFromtimestampMethod is datetime.fromtimestamp(t[, tz]).
//
// CPython: Modules/_datetimemodule.c:5558 datetime_fromtimestamp
func datetimeFromtimestampMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// args[0] is class, args[1] is timestamp, args[2] optionally is tz.
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: fromtimestamp() requires a timestamp argument")
	}
	ts, err := asFloat64(args[1])
	if err != nil {
		return nil, err
	}
	sec := int64(ts)
	ns := int64((ts - float64(sec)) * 1e9)
	var tz *Timezone
	if len(args) > 2 && !objects.IsNone(args[2]) {
		tzVal, ok := args[2].(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: tz must be a timezone or None")
		}
		tz = tzVal
	}
	if v, ok := kwargs["tz"]; ok && !objects.IsNone(v) {
		tzVal, ok := v.(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: tz must be a timezone or None")
		}
		tz = tzVal
	}
	var t gotime.Time
	if tz != nil {
		offsetSecs := timedeltaToUs(tz.Offset) / usPerSecond
		loc := gotime.FixedZone(timezoneAutoName(tz), int(offsetSecs))
		t = gotime.Unix(sec, ns).In(loc)
	} else {
		t = gotime.Unix(sec, ns).Local()
	}
	return datetimeFromGoTime(t, tz)
}

// datetimeUtcfromtimestampMethod is datetime.utcfromtimestamp(t).
//
// CPython: Modules/_datetimemodule.c:5580 datetime_utcfromtimestamp
func datetimeUtcfromtimestampMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: utcfromtimestamp() requires a timestamp argument")
	}
	ts, err := asFloat64(args[1])
	if err != nil {
		return nil, err
	}
	sec := int64(ts)
	ns := int64((ts - float64(sec)) * 1e9)
	t := gotime.Unix(sec, ns).UTC()
	return datetimeFromGoTime(t, nil)
}

// datetimeCombine is datetime.combine(d, t[, tzinfo]).
//
// CPython: Modules/_datetimemodule.c:5596 datetime_combine
func datetimeCombine(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// args[0] is class.
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: combine() requires date and time arguments")
	}
	d, ok := args[1].(*Date)
	if !ok {
		return nil, fmt.Errorf("TypeError: first argument must be a date")
	}
	t, ok := args[2].(*Time)
	if !ok {
		return nil, fmt.Errorf("TypeError: second argument must be a time")
	}
	tz := t.TzInfo
	if len(args) > 3 && !objects.IsNone(args[3]) {
		tzVal, ok := args[3].(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: third argument must be a timezone or None")
		}
		tz = tzVal
	}
	return newDatetime(d.Year, d.Month, d.Day, t.Hour, t.Minute, t.Second, t.Microsecond, tz, 0)
}

// datetimeFromisoformatMethod is datetime.fromisoformat(s).
//
// CPython: Modules/_datetimemodule.c:5636 datetime_fromisoformat
func datetimeFromisoformatMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := objects.Str(args[len(args)-1])
	if err != nil {
		return nil, err
	}
	// Try formats with T or space separator.
	formats := []string{
		"2006-01-02T15:04:05.000000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05.000000",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err2 := gotime.Parse(f, s); err2 == nil {
			us := int64(t.Nanosecond() / 1000)
			return newDatetime(
				int64(t.Year()), int64(t.Month()), int64(t.Day()),
				int64(t.Hour()), int64(t.Minute()), int64(t.Second()),
				us, nil, 0)
		}
	}
	return nil, fmt.Errorf("ValueError: Invalid isoformat string: %q", s)
}

// datetimeTimetuple is datetime.timetuple().
//
// CPython: Modules/_datetimemodule.c:5720 datetime_timetuple
func datetimeTimetuple(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	gt := datetimeToGoTime(dt)
	fields := []objects.Object{
		objects.NewInt(dt.Year),
		objects.NewInt(dt.Month),
		objects.NewInt(dt.Day),
		objects.NewInt(dt.Hour),
		objects.NewInt(dt.Minute),
		objects.NewInt(dt.Second),
		objects.NewInt((int64(gt.Weekday()) + 6) % 7),
		objects.NewInt(int64(gt.YearDay())),
		objects.NewInt(-1),
	}
	return objects.NewTuple(fields), nil
}

// datetimeUtctimetuple is datetime.utctimetuple().
//
// CPython: Modules/_datetimemodule.c:5732 datetime_utctimetuple
func datetimeUtctimetuple(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	gt := datetimeToGoTime(dt).UTC()
	fields := []objects.Object{
		objects.NewInt(int64(gt.Year())),
		objects.NewInt(int64(gt.Month())),
		objects.NewInt(int64(gt.Day())),
		objects.NewInt(int64(gt.Hour())),
		objects.NewInt(int64(gt.Minute())),
		objects.NewInt(int64(gt.Second())),
		objects.NewInt((int64(gt.Weekday()) + 6) % 7),
		objects.NewInt(int64(gt.YearDay())),
		objects.NewInt(0),
	}
	return objects.NewTuple(fields), nil
}

// datetimeDateMethod is datetime.date().
//
// CPython: Modules/_datetimemodule.c:5758 datetime_getdate
func datetimeDateMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	return newDate(dt.Year, dt.Month, dt.Day)
}

// datetimeTimeMethod is datetime.time() (strips tzinfo).
//
// CPython: Modules/_datetimemodule.c:5762 datetime_gettime
func datetimeTimeMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	return newTimeObj(dt.Hour, dt.Minute, dt.Second, dt.Microsecond, nil, 0)
}

// datetimeTimetz is datetime.timetz() (preserves tzinfo).
//
// CPython: Modules/_datetimemodule.c:5768 datetime_gettimetz
func datetimeTimetz(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	return newTimeObj(dt.Hour, dt.Minute, dt.Second, dt.Microsecond, dt.TzInfo, dt.Fold)
}

// datetimeReplace is datetime.replace(**kw).
//
// CPython: Modules/_datetimemodule.c:5800 datetime_replace
func datetimeReplace(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	year, month, day := dt.Year, dt.Month, dt.Day
	hour, minute, second, microsecond, fold := dt.Hour, dt.Minute, dt.Second, dt.Microsecond, dt.Fold
	tz := dt.TzInfo
	var err error
	if v, ok := kwargs["year"]; ok {
		year, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["month"]; ok {
		month, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["day"]; ok {
		day, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["hour"]; ok {
		hour, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["minute"]; ok {
		minute, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["second"]; ok {
		second, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["microsecond"]; ok {
		microsecond, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["tzinfo"]; ok {
		if objects.IsNone(v) {
			tz = nil
		} else {
			tzVal, ok := v.(*Timezone)
			if !ok {
				return nil, fmt.Errorf("TypeError: tzinfo must be a timezone or None")
			}
			tz = tzVal
		}
	}
	if v, ok := kwargs["fold"]; ok {
		fold, err = asInt(v)
		if err != nil {
			return nil, err
		}
	}
	return newDatetime(year, month, day, hour, minute, second, microsecond, tz, fold)
}

// datetimeAstimezone is datetime.astimezone([tz]).
//
// CPython: Modules/_datetimemodule.c:5817 datetime_astimezone
func datetimeAstimezone(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	var tz *Timezone
	if len(args) > 1 && !objects.IsNone(args[1]) {
		tzVal, ok := args[1].(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: tz must be a timezone or None")
		}
		tz = tzVal
	}
	if v, ok := kwargs["tz"]; ok && !objects.IsNone(v) {
		tzVal, ok := v.(*Timezone)
		if !ok {
			return nil, fmt.Errorf("TypeError: tz must be a timezone or None")
		}
		tz = tzVal
	}
	gt := datetimeToGoTime(dt)
	if tz != nil {
		offsetSecs := timedeltaToUs(tz.Offset) / usPerSecond
		loc := gotime.FixedZone(timezoneAutoName(tz), int(offsetSecs))
		gt = gt.In(loc)
	} else {
		gt = gt.Local()
	}
	return datetimeFromGoTime(gt, tz)
}

// datetimeUtcoffset is datetime.utcoffset().
//
// CPython: Modules/_datetimemodule.c:5860 datetime_utcoffset
func datetimeUtcoffset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	if dt.TzInfo == nil {
		return objects.None(), nil
	}
	return dt.TzInfo.Offset, nil
}

// datetimeDst is datetime.dst().
//
// CPython: Modules/_datetimemodule.c:5869 datetime_dst
func datetimeDst(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	if dt.TzInfo == nil {
		return objects.None(), nil
	}
	return objects.None(), nil
}

// datetimeTzname is datetime.tzname().
//
// CPython: Modules/_datetimemodule.c:5878 datetime_tzname
func datetimeTzname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	if dt.TzInfo == nil {
		return objects.None(), nil
	}
	return objects.NewStr(timezoneAutoName(dt.TzInfo)), nil
}

// datetimeTimestamp is datetime.timestamp().
//
// CPython: Modules/_datetimemodule.c:5889 datetime_timestamp
func datetimeTimestamp(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	gt := datetimeToGoTime(dt)
	ts := float64(gt.Unix()) + float64(dt.Microsecond)*1e-6
	return objects.NewFloat(ts), nil
}

// datetimeWeekday is datetime.weekday() (Monday=0).
//
// CPython: Modules/_datetimemodule.c:5910 datetime_weekday
func datetimeWeekday(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return dateWeekday([]objects.Object{&args[0].(*Datetime).Date}, nil)
}

// datetimeIsoweekday is datetime.isoweekday() (Monday=1).
//
// CPython: Modules/_datetimemodule.c:5916 datetime_isoweekday
func datetimeIsoweekday(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return dateIsoweekday([]objects.Object{&args[0].(*Datetime).Date}, nil)
}

// datetimeIsocalendar is datetime.isocalendar().
//
// CPython: Modules/_datetimemodule.c:5922 datetime_isocalendar
func datetimeIsocalendar(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return dateIsocalendar([]objects.Object{&args[0].(*Datetime).Date}, nil)
}

// datetimeIsoformat is datetime.isoformat([sep, timespec]).
//
// CPython: Modules/_datetimemodule.c:5928 datetime_isoformat
func datetimeIsoformat(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	dt := args[0].(*Datetime)
	sep := "T"
	timespec := "auto"
	if len(args) > 1 {
		s, err := objects.Str(args[1])
		if err != nil {
			return nil, err
		}
		sep = s
	}
	if v, ok := kwargs["sep"]; ok {
		s, err := objects.Str(v)
		if err != nil {
			return nil, err
		}
		sep = s
	}
	if v, ok := kwargs["timespec"]; ok {
		ts, err := objects.Str(v)
		if err != nil {
			return nil, err
		}
		timespec = ts
	}
	s, err := datetimeIsoformatImpl(dt, sep, timespec)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

func datetimeIsoformatImpl(dt *Datetime, sep, timespec string) (string, error) {
	datePart := fmt.Sprintf("%04d-%02d-%02d", dt.Year, dt.Month, dt.Day)
	timePart, err := timeIsoformatImpl(
		&Time{
			Hour:        dt.Hour,
			Minute:      dt.Minute,
			Second:      dt.Second,
			Microsecond: dt.Microsecond,
			TzInfo:      dt.TzInfo,
		}, timespec)
	if err != nil {
		return "", err
	}
	return datePart + sep + timePart, nil
}

// datetimeStrftime is datetime.strftime(fmt).
//
// CPython: Modules/_datetimemodule.c:5968 datetime_strftime
func datetimeStrftime(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: strftime() requires a format string")
	}
	dt := args[0].(*Datetime)
	fmtStr, err := objects.Str(args[1])
	if err != nil {
		return nil, err
	}
	gt := datetimeToGoTime(dt)
	goFmt, hasDirectives := pythonToGoFmt(fmtStr)
	if !hasDirectives {
		return objects.NewStr(fmtStr), nil
	}
	return objects.NewStr(gt.Format(goFmt)), nil
}

// datetimeFormatMethod is datetime.__format__(format_spec).
// Empty spec returns str(self); non-empty delegates to strftime.
// Strings with no % directives are returned unchanged (Python strftime
// passes non-%-chars through literally).
//
// CPython: Modules/_datetimemodule.c:3598 date_format (shared by datetime)
func datetimeFormatMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __format__() takes exactly one argument")
	}
	spec, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __format__() argument 1 must be str")
	}
	v := spec.Value()
	if v == "" {
		s, err := objects.Str(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	}
	return datetimeStrftime(args, kwargs)
}
