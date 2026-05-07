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
	prev := setActiveThread(ts)
	defer restoreActiveThread(prev)

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
	prev := setActiveThread(ts)
	defer restoreActiveThread(prev)

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

func TestRegisterSysTraceBuiltins(t *testing.T) {
	d := objects.NewDict()
	if err := RegisterSysTraceBuiltins(d); err != nil {
		t.Fatalf("RegisterSysTraceBuiltins: %v", err)
	}
	for _, name := range []string{"settrace", "setprofile", "gettrace", "getprofile"} {
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
