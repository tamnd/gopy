package vm

import (
	"testing"

	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func callBuiltin(t *testing.T, fn objects.Object, args ...objects.Object) objects.Object {
	t.Helper()
	tp := fn.Type()
	if tp.Call == nil {
		t.Fatalf("%T is not callable", fn)
	}
	out, err := tp.Call(fn, args, nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	return out
}

func TestSysSetTraceInstallsBridge(t *testing.T) {
	ts := state.NewThread()
	prev, gid := setActiveThread(ts)
	defer restoreActiveThread(prev, gid)

	cb := objects.NewBuiltinFunction("trace_cb", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	settrace := SysSetTrace()
	out := callBuiltin(t, settrace, cb)
	if out != objects.None() {
		t.Errorf("settrace returned %v, want None", out)
	}
	if got := ts.Interp().Monitors.SysTracingThreads; got != 1 {
		t.Errorf("SysTracingThreads = %d, want 1", got)
	}
	mask := ts.Interp().Monitors.GetEvents(monitor.ToolSysTrace)
	if mask == 0 {
		t.Errorf("trace event mask = 0, want non-zero after settrace")
	}

	gettrace := SysGetTrace()
	got := callBuiltin(t, gettrace)
	if got != cb {
		t.Errorf("gettrace returned %v, want the installed callable", got)
	}

	out = callBuiltin(t, settrace, objects.None())
	if out != objects.None() {
		t.Errorf("settrace(None) returned %v, want None", out)
	}
	if got := ts.Interp().Monitors.SysTracingThreads; got != 0 {
		t.Errorf("SysTracingThreads = %d after clear, want 0", got)
	}
	if got := ts.Interp().Monitors.GetEvents(monitor.ToolSysTrace); got != 0 {
		t.Errorf("trace event mask = %#x after clear, want 0", got)
	}
}

func TestSysSetProfileInstallsBridge(t *testing.T) {
	ts := state.NewThread()
	prev, gid := setActiveThread(ts)
	defer restoreActiveThread(prev, gid)

	cb := objects.NewBuiltinFunction("profile_cb", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	setprofile := SysSetProfile()
	if out := callBuiltin(t, setprofile, cb); out != objects.None() {
		t.Errorf("setprofile returned %v, want None", out)
	}
	if got := ts.Interp().Monitors.SysProfilingThreads; got != 1 {
		t.Errorf("SysProfilingThreads = %d, want 1", got)
	}

	getprofile := SysGetProfile()
	if got := callBuiltin(t, getprofile); got != cb {
		t.Errorf("getprofile = %v, want the installed callable", got)
	}
}

func TestSysCallTracing(t *testing.T) {
	ts := state.NewThread()
	prev, gid := setActiveThread(ts)
	defer restoreActiveThread(prev, gid)

	// call_tracing must run func(*args) with tracing zeroed during the
	// call and the prior depth restored once it returns.
	ts.Tracing = 5
	var sawTracing int
	fn := objects.NewBuiltinFunction("adder", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		sawTracing = ts.Tracing
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a + b), nil
	})
	out := callBuiltin(t, SysCallTracing(), fn, objects.NewTuple([]objects.Object{objects.NewInt(3), objects.NewInt(4)}))
	if v, _ := out.(*objects.Int).Int64(); v != 7 {
		t.Errorf("call_tracing result = %v, want 7", v)
	}
	if sawTracing != 0 {
		t.Errorf("tracing during call = %d, want 0", sawTracing)
	}
	if ts.Tracing != 5 {
		t.Errorf("tracing after call = %d, want 5 (restored)", ts.Tracing)
	}

	// Wrong arg count and non-tuple second argument both raise TypeError.
	ct := SysCallTracing()
	if _, err := ct.Type().Call(ct, []objects.Object{fn}, nil); err == nil {
		t.Errorf("call_tracing with one arg should error")
	}
	if _, err := ct.Type().Call(ct, []objects.Object{fn, objects.NewList(nil)}, nil); err == nil {
		t.Errorf("call_tracing with non-tuple args should error")
	}
}

func TestRegisterSysTraceBuiltins(t *testing.T) {
	d := objects.NewDict()
	if err := RegisterSysTraceBuiltins(d); err != nil {
		t.Fatalf("RegisterSysTraceBuiltins: %v", err)
	}
	for _, name := range []string{"settrace", "setprofile", "gettrace", "getprofile", "call_tracing"} {
		v, err := d.GetItem(objects.NewStr(name))
		if err != nil {
			t.Errorf("missing builtin %q: %v", name, err)
			continue
		}
		if v.Type().Call == nil {
			t.Errorf("%q is not callable", name)
		}
	}
}
