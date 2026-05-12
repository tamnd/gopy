// Tests for the time builtin port. Each test exercises a single
// entry point and pins the user-visible behavior CPython documents.

package _time

import (
	"strings"
	"testing"
	gotime "time"

	"github.com/tamnd/gopy/objects"
)

// callFn is a tiny driver: feed positional args, get the result.
func callFn(t *testing.T, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error), args ...objects.Object) objects.Object {
	t.Helper()
	out, err := fn(args, nil)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	return out
}

func TestTimeAndTimeNs(t *testing.T) {
	a := callFn(t, timeTime).(*objects.Float)
	b := callFn(t, timeTime).(*objects.Float)
	if b.Float64() < a.Float64() {
		t.Fatalf("time() went backward: %v then %v", a.Float64(), b.Float64())
	}
	an := callFn(t, timeTimeNs).(*objects.Int)
	bn := callFn(t, timeTimeNs).(*objects.Int)
	av, _ := an.Int64()
	bv, _ := bn.Int64()
	if bv < av {
		t.Fatalf("time_ns() went backward: %d then %d", av, bv)
	}
}

func TestMonotonicIncreasing(t *testing.T) {
	a := callFn(t, monotonic).(*objects.Float).Float64()
	gotime.Sleep(2 * gotime.Millisecond)
	b := callFn(t, monotonic).(*objects.Float).Float64()
	if b <= a {
		t.Fatalf("monotonic did not advance: %v -> %v", a, b)
	}
}

func TestSleepZero(t *testing.T) {
	out := callFn(t, sleep, objects.NewFloat(0))
	if !objects.IsNone(out) {
		t.Fatalf("sleep(0) returned %T, want None", out)
	}
}

func TestSleepSmallMeasured(t *testing.T) {
	start := callFn(t, monotonic).(*objects.Float).Float64()
	_ = callFn(t, sleep, objects.NewFloat(0.01))
	end := callFn(t, monotonic).(*objects.Float).Float64()
	if end-start < 0.005 {
		t.Fatalf("sleep(0.01) elapsed only %v seconds", end-start)
	}
}

func TestSleepNegativeRaises(t *testing.T) {
	_, err := sleep([]objects.Object{objects.NewFloat(-1)}, nil)
	if err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("expected ValueError, got %v", err)
	}
}

func TestGmtimeEpochIs1970(t *testing.T) {
	st := callFn(t, gmtime, objects.NewInt(0)).(*objects.StructSeq)
	items := st.Items()
	if y, _ := items[0].(*objects.Int).Int64(); y != 1970 {
		t.Fatalf("gmtime(0).tm_year = %d, want 1970", y)
	}
	if m, _ := items[1].(*objects.Int).Int64(); m != 1 {
		t.Fatalf("gmtime(0).tm_mon = %d, want 1", m)
	}
	if d, _ := items[2].(*objects.Int).Int64(); d != 1 {
		t.Fatalf("gmtime(0).tm_mday = %d, want 1", d)
	}
}

func TestLocaltimeReasonable(t *testing.T) {
	st := callFn(t, localtime, objects.NewInt(0)).(*objects.StructSeq)
	yr, _ := st.Items()[0].(*objects.Int).Int64()
	if yr < 1969 || yr > 1970 {
		t.Fatalf("localtime(0).tm_year = %d, want 1969 or 1970", yr)
	}
}

func TestStructTimeFieldAccess(t *testing.T) {
	st := callFn(t, gmtime, objects.NewInt(0))
	v, err := st.Type().Getattro(st, objects.NewStr("tm_year"))
	if err != nil {
		t.Fatalf("getattr tm_year: %v", err)
	}
	if y, _ := v.(*objects.Int).Int64(); y != 1970 {
		t.Fatalf("tm_year via attr = %d", y)
	}
}

// TestStrftimeYMD pins the exact format produced for the epoch.
func TestStrftimeYMD(t *testing.T) {
	st := callFn(t, gmtime, objects.NewInt(0))
	out, err := strftime([]objects.Object{objects.NewStr("%Y-%m-%d"), st}, nil)
	if err != nil {
		t.Fatalf("strftime: %v", err)
	}
	s, _ := objects.Str(out)
	if s != "1970-01-01" {
		t.Fatalf("strftime(epoch, %%Y-%%m-%%d) = %q, want 1970-01-01", s)
	}
}

func TestStrptimeParsesStrftimeOutput(t *testing.T) {
	st1 := callFn(t, gmtime, objects.NewInt(0))
	s, _ := strftime([]objects.Object{objects.NewStr("%Y-%m-%d %H:%M:%S"), st1}, nil)
	st2, err := strptime([]objects.Object{s, objects.NewStr("%Y-%m-%d %H:%M:%S")}, nil)
	if err != nil {
		t.Fatalf("strptime: %v", err)
	}
	items := st2.(*objects.StructSeq).Items()
	if y, _ := items[0].(*objects.Int).Int64(); y != 1970 {
		t.Fatalf("strptime year = %d", y)
	}
}

func TestAsctimeFormat(t *testing.T) {
	st := callFn(t, gmtime, objects.NewInt(0))
	out, err := asctime([]objects.Object{st}, nil)
	if err != nil {
		t.Fatalf("asctime: %v", err)
	}
	s, _ := objects.Str(out)
	// "Thu Jan  1 00:00:00 1970"
	if !strings.HasPrefix(s, "Thu Jan  1 00:00:00 1970") {
		t.Fatalf("asctime epoch = %q", s)
	}
}

func TestCtimeFormat(t *testing.T) {
	out, err := ctime([]objects.Object{objects.NewInt(0)}, nil)
	if err != nil {
		t.Fatalf("ctime: %v", err)
	}
	s, _ := objects.Str(out)
	if len(s) < 20 || !strings.Contains(s, "1970") && !strings.Contains(s, "1969") {
		t.Fatalf("ctime(0) = %q", s)
	}
}

func TestGetClockInfoMonotonic(t *testing.T) {
	out, err := getClockInfo([]objects.Object{objects.NewStr("monotonic")}, nil)
	if err != nil {
		t.Fatalf("get_clock_info: %v", err)
	}
	ns := out.(*objects.Namespace)
	v, err := ns.Dict().GetItem(objects.NewStr("monotonic"))
	if err != nil {
		t.Fatalf("missing monotonic key: %v", err)
	}
	b, _ := objects.IsTruthy(v)
	if !b {
		t.Fatalf("get_clock_info('monotonic').monotonic should be True")
	}
}

func TestMktimeRoundTrip(t *testing.T) {
	st := callFn(t, localtime, objects.NewInt(1700000000))
	out, err := mktime([]objects.Object{st}, nil)
	if err != nil {
		t.Fatalf("mktime: %v", err)
	}
	v := out.(*objects.Float).Float64()
	if v < 1699999000 || v > 1700001000 {
		t.Fatalf("mktime round trip = %v, want ~1700000000", v)
	}
}

func TestModuleBuilds(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	for _, name := range []string{
		"time", "time_ns", "monotonic", "monotonic_ns",
		"perf_counter", "perf_counter_ns", "process_time", "process_time_ns",
		"thread_time", "thread_time_ns", "sleep",
		"gmtime", "localtime", "asctime", "ctime", "mktime",
		"strftime", "strptime", "get_clock_info", "tzset",
		"clock_gettime", "clock_gettime_ns", "clock_settime",
		"clock_settime_ns", "clock_getres",
		"struct_time", "_STRUCT_TM_ITEMS",
		"timezone", "altzone", "daylight", "tzname",
	} {
		if _, err := m.Dict().GetItem(objects.NewStr(name)); err != nil {
			t.Errorf("module missing %q: %v", name, err)
		}
	}
}
