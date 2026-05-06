package sys

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestBindRegistersHelpers pins the v0.7 sys-D surface: every entry
// from the spec list (exit, setrecursionlimit, getrecursionlimit,
// getrefcount, intern) lands in the dict as a builtin function.
func TestBindRegistersHelpers(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()
	if berr := Bind(d, ts); berr != nil {
		t.Fatalf("Bind: %v", berr)
	}
	want := []string{"exit", "setrecursionlimit", "getrecursionlimit", "getrefcount", "intern"}
	for _, name := range want {
		v, gerr := d.GetItem(objects.NewStr(name))
		if gerr != nil {
			t.Errorf("GetItem(%q): %v", name, gerr)
			continue
		}
		if _, ok := v.(*objects.BuiltinFunction); !ok {
			t.Errorf("%q is %T, want *BuiltinFunction", name, v)
		}
	}
}

// TestExitInstallsSystemExit pins sys.exit(2): SystemExit on the
// thread, with the int argument round-tripped into args[0]. The Go
// error is a sentinel; the actual exit code comes off the thread.
func TestExitInstallsSystemExit(t *testing.T) {
	ts := state.NewThread()
	fn := makeExit(ts)
	_, err := fn([]objects.Object{objects.NewInt(2)}, nil)
	if err == nil {
		t.Fatal("exit returned nil error, want sentinel")
	}
	exc := errors.Occurred(ts)
	if exc == nil {
		t.Fatal("exit did not install an exception on the thread")
	}
	if exc.ExcType != errors.PyExc_SystemExit {
		t.Errorf("exception type = %s, want SystemExit", exc.TypeName())
	}
	if exc.Args.Len() != 1 {
		t.Fatalf("args.len = %d, want 1", exc.Args.Len())
	}
	got, _ := exc.Args.Item(0).(*objects.Int).Int64()
	if got != 2 {
		t.Errorf("args[0] = %d, want 2", got)
	}
}

// TestExitDefaultsToNone pins the no-argument form: sys.exit() is
// SystemExit(None) per CPython.
func TestExitDefaultsToNone(t *testing.T) {
	ts := state.NewThread()
	fn := makeExit(ts)
	_, _ = fn(nil, nil)
	exc := errors.Occurred(ts)
	if exc == nil {
		t.Fatal("exit did not install an exception")
	}
	if exc.Args.Item(0) != objects.None() {
		t.Errorf("args[0] = %v, want None", exc.Args.Item(0))
	}
}

// TestSetRecursionLimitRoundTrip pins the basic getter/setter cycle.
// The package-level atomic resets between cases to avoid cross-test
// contamination.
func TestSetRecursionLimitRoundTrip(t *testing.T) {
	defer recursionLimit.Store(DefaultRecursionLimit)
	ts := state.NewThread()
	set := makeSetRecursionLimit(ts)
	if _, err := set([]objects.Object{objects.NewInt(2500)}, nil); err != nil {
		t.Fatalf("setrecursionlimit: %v", err)
	}
	got, _ := getRecursionLimit(nil, nil)
	v, _ := got.(*objects.Int).Int64()
	if v != 2500 {
		t.Errorf("getrecursionlimit() = %d, want 2500", v)
	}
}

// TestSetRecursionLimitRejectsZero pins the ValueError branch:
// limit < 1 raises ValueError on the thread.
func TestSetRecursionLimitRejectsZero(t *testing.T) {
	defer recursionLimit.Store(DefaultRecursionLimit)
	ts := state.NewThread()
	set := makeSetRecursionLimit(ts)
	_, err := set([]objects.Object{objects.NewInt(0)}, nil)
	if err == nil {
		t.Fatal("setrecursionlimit(0) returned nil error")
	}
	exc := errors.Occurred(ts)
	if exc == nil || exc.ExcType != errors.PyExc_ValueError {
		t.Errorf("expected ValueError on thread, got %v", exc)
	}
}

// TestGetRefcountReadsHeader pins sys.getrefcount: read the header
// counter directly. The freshly-allocated str starts at 1.
func TestGetRefcountReadsHeader(t *testing.T) {
	s := objects.NewStr("hello")
	v, err := getRefcount([]objects.Object{s}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 1 {
		t.Errorf("getrefcount = %d, want 1", got)
	}
}

// TestInternRoundTripsStr pins the str path: intern returns the same
// object it was passed (gopy has no real intern table yet, but the
// round-trip semantics match).
func TestInternRoundTripsStr(t *testing.T) {
	ts := state.NewThread()
	fn := makeIntern(ts)
	in := objects.NewStr("hello")
	out, err := fn([]objects.Object{in}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(out)
	if got != "hello" {
		t.Errorf("intern returned %q, want %q", got, "hello")
	}
}

// TestInternRejectsNonStr pins the TypeError branch: intern of an
// int raises TypeError on the thread.
func TestInternRejectsNonStr(t *testing.T) {
	ts := state.NewThread()
	fn := makeIntern(ts)
	_, err := fn([]objects.Object{objects.NewInt(7)}, nil)
	if err == nil {
		t.Fatal("intern(int) returned nil error")
	}
	exc := errors.Occurred(ts)
	if exc == nil || exc.ExcType != errors.PyExc_TypeError {
		t.Errorf("expected TypeError on thread, got %v", exc)
	}
	if !strings.Contains(exc.Message(), "intern") {
		t.Errorf("message %q must mention intern", exc.Message())
	}
}
