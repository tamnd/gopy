package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestSetProfileInstallsHandlers verifies SetProfile flips the tool
// 6 event mask through monitor.SetEvents and bumps the profiling
// counter, and clearing the func reverses both effects.
func TestSetProfileInstallsHandlers(t *testing.T) {
	ts := state.NewThread()
	interp := ts.Interp().Monitors

	called := 0
	fn := func(obj objects.Object, f *frame.Frame, kind int, arg objects.Object) error {
		called++
		return nil
	}

	if _, err := SetProfile(ts, fn, objects.NewStr("ctx")); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if got := interp.SysProfilingThreads; got != 1 {
		t.Errorf("SysProfilingThreads = %d, want 1", got)
	}
	if got := interp.GetEvents(monitor.ToolSysProfile); got == 0 {
		t.Errorf("event mask = 0, want non-zero after SetProfile")
	}

	if _, err := SetProfile(ts, nil, nil); err != nil {
		t.Fatalf("SetProfile(nil): %v", err)
	}
	if got := interp.SysProfilingThreads; got != 0 {
		t.Errorf("SysProfilingThreads = %d, want 0 after clear", got)
	}
	if got := interp.GetEvents(monitor.ToolSysProfile); got != 0 {
		t.Errorf("event mask = %#x, want 0 after clear", got)
	}
	if called != 0 {
		t.Errorf("trace func called %d times, want 0 (no events fired)", called)
	}
}

// TestSetTraceInstallsHandlers exercises the trace-side parallel.
func TestSetTraceInstallsHandlers(t *testing.T) {
	ts := state.NewThread()
	interp := ts.Interp().Monitors

	fn := func(obj objects.Object, f *frame.Frame, kind int, arg objects.Object) error {
		return nil
	}
	old, err := SetTrace(ts, fn, objects.NewStr("ctx"))
	if err != nil {
		t.Fatalf("SetTrace: %v", err)
	}
	if old != nil {
		t.Errorf("first SetTrace returned old = %v, want nil", old)
	}
	if got := interp.SysTracingThreads; got != 1 {
		t.Errorf("SysTracingThreads = %d, want 1", got)
	}
	mask := interp.GetEvents(monitor.ToolSysTrace)
	want := traceEventMask()
	if mask != want {
		t.Errorf("trace mask = %#x, want %#x", mask, want)
	}
}

// TestLegacyProfileFiresOnReturn drives a one-instruction RETURN
// program with a profile func installed and checks the legacy
// callback receives PyTrace_RETURN with the return value.
func TestLegacyProfileFiresOnReturn(t *testing.T) {
	bc := []byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	}
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(7)},
		Stacksize: 4,
	}
	ts := state.NewThread()

	type seenEvent struct {
		kind int
		arg  objects.Object
	}
	var seen []seenEvent
	fn := func(obj objects.Object, f *frame.Frame, kind int, arg objects.Object) error {
		seen = append(seen, seenEvent{kind, arg})
		return nil
	}
	if _, err := SetProfile(ts, fn, objects.NewStr("ctx")); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := monitor.Instrument(co, ts.Interp().Monitors); err != nil {
		t.Fatalf("Instrument: %v", err)
	}

	out, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if i, ok := out.(*objects.Int); !ok {
		t.Fatalf("result = %T, want *Int", out)
	} else if v, _ := i.Int64(); v != 7 {
		t.Fatalf("result = %d, want 7", v)
	}
	gotReturn := false
	for _, e := range seen {
		if e.kind == PyTraceReturn {
			gotReturn = true
			if i, ok := e.arg.(*objects.Int); !ok {
				t.Errorf("PyTraceReturn arg = %T, want *Int", e.arg)
			} else if v, _ := i.Int64(); v != 7 {
				t.Errorf("PyTraceReturn arg = %d, want 7", v)
			}
		}
	}
	if !gotReturn {
		t.Errorf("no PyTraceReturn fired; got %d events", len(seen))
	}
}

// TestSetTraceJumpDisablesForwardJump checks the dispatcher returns
// the Disable sentinel for forward jumps so the line handler can take
// over via INSTRUMENTED_LINE.
func TestSetTraceJumpDisablesForwardJump(t *testing.T) {
	co := &objects.Code{Firstlineno: 1}
	ts := state.NewThread()
	if _, err := SetTrace(ts, func(objects.Object, *frame.Frame, int, objects.Object) error { return nil }, nil); err != nil {
		t.Fatalf("SetTrace: %v", err)
	}
	prev := setActiveThread(ts)
	defer restoreActiveThread(prev)
	args := []objects.Object{co, objects.NewInt(0), objects.NewInt(8)}
	out, err := sysTraceJumpFunc(args, nil)
	if err != nil {
		t.Fatalf("sysTraceJumpFunc: %v", err)
	}
	if !monitor.IsDisable(out) {
		t.Errorf("forward jump did not return Disable, got %v", out)
	}
}
