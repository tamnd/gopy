package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestDispatchFiresInstrumentedReturn drives a small program that
// returns a constant, instruments the RETURN_VALUE opcode, and checks
// that the registered callback fires before the value escapes Eval.
func TestDispatchFiresInstrumentedReturn(t *testing.T) {
	bc := []byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	}
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(42)},
		Stacksize: 4,
	}

	ts := state.NewThread()
	interp := ts.Interp().Monitors
	if err := interp.UseToolID(monitor.ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}

	var called int
	cb := objects.NewBuiltinFunction("recorder", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		called++
		if len(args) != 3 {
			t.Errorf("callback got %d args, want 3", len(args))
		}
		return objects.None(), nil
	})
	if _, err := interp.RegisterCallback(monitor.ToolDebugger, monitor.EventPyReturn, cb); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	if err := interp.SetEvents(monitor.ToolDebugger, monitor.EventSet(0).With(monitor.EventPyReturn)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := monitor.Instrument(co, interp); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if got := compile.Opcode(co.Code[2]); got != compile.INSTRUMENTED_RETURN_VALUE {
		t.Fatalf("setup: RETURN_VALUE not rewritten, got %v", got)
	}

	out, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if i, ok := out.(*objects.Int); !ok {
		t.Fatalf("result type = %T, want *Int", out)
	} else if v, ok2 := i.Int64(); !ok2 || v != 42 {
		t.Fatalf("result = %v, want 42", out)
	}
	if called != 1 {
		t.Fatalf("callback invoked %d times, want 1", called)
	}
}

// TestDispatchSkipsFireWhenNoToolSubscribed verifies that an
// instrumented opcode without active tools at this offset does not
// invoke the fan-out and does not panic.
func TestDispatchSkipsFireWhenNoToolSubscribed(t *testing.T) {
	bc := []byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.INSTRUMENTED_RETURN_VALUE), 0,
	}
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(1)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	out, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if i, ok := out.(*objects.Int); !ok {
		t.Fatalf("result type = %T, want *Int", out)
	} else if v, ok2 := i.Int64(); !ok2 || v != 1 {
		t.Fatalf("result = %v, want 1", out)
	}
}

// TestDispatchDisablingCallbackClearsBit checks that returning the
// Disable sentinel from a callback clears its bit in the per-codeunit
// tools mask so the next dispatch does not call it again.
func TestDispatchDisablingCallbackClearsBit(t *testing.T) {
	bc := []byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	}
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(0)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	interp := ts.Interp().Monitors
	if err := interp.UseToolID(monitor.ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	cb := objects.NewBuiltinFunction("disable", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return monitor.Disable, nil
	})
	if _, err := interp.RegisterCallback(monitor.ToolDebugger, monitor.EventPyReturn, cb); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	if err := interp.SetEvents(monitor.ToolDebugger, monitor.EventSet(0).With(monitor.EventPyReturn)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := monitor.Instrument(co, interp); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if _, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict()); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	data := monitor.CoMonitoring(co)
	if data == nil {
		t.Fatalf("CoMonitoringData missing after run")
	}
	if got := data.Tools[1]; got != 0 {
		t.Fatalf("Tools[1] = %#b, want 0 after Disable return", got)
	}
}
