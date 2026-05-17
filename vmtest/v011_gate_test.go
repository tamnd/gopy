// End-to-end gate for v0.11 features: PEP 659 specializer + PEP 669
// monitor + the legacy sys.settrace / sys.setprofile bridge. Each
// subtest drives the public entry points the way a user would: build
// a Code, register against monitor.InterpState or call vm.SetTrace,
// run vm.EvalCode, and assert the callback / specialization side-
// effect actually happened.

package vmtest

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// emitWithCache appends one (op, arg) plus enough zero codeunits to
// match op's CacheCount. Mirrors what the assembler will emit once
// 1687 lands the typed Code path.
func emitWithCache(buf []byte, op compile.Opcode, arg byte) []byte {
	buf = append(buf, byte(op), arg)
	for i := 0; i < specialize.CacheCount(op); i++ {
		buf = append(buf, 0, 0)
	}
	return buf
}

// TestGateSpecializerRewritesToBool drives a TO_BOOL through the
// adaptive path with a forced-zero counter. The dispatch loop has to
// land on the specialized variant and the TO_BOOL_INT fast-path arm
// produces True for the int operand without deopting.
func TestGateSpecializerRewritesToBool(t *testing.T) {
	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.TO_BOOL, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)

	code := make([]byte, len(bc))
	copy(code, bc)
	specialize.Quicken(code, true)
	co := &objects.Code{
		Code:      code,
		Consts:    []any{int64(7)},
		Quickened: true,
		Stacksize: 8,
	}

	idx := 1
	cnt := specialize.LoadCounter(co.Code, idx)
	specialize.StoreCounter(co.Code, idx, specialize.MakeBackoffCounter(0, cnt.ValueAndBackoff&15))

	ts := state.NewThread()
	out, err := vm.EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if out != objects.True() {
		t.Fatalf("result = %v, want True", out)
	}
	if got := compile.Opcode(co.Code[2*idx]); got != compile.TO_BOOL_INT {
		t.Fatalf("TO_BOOL slot = %s, want TO_BOOL_INT after specialization", got.Name())
	}
}

// TestGateMonitorPyReturnFires verifies the PEP 669 fan-out runs end
// to end: register a callback against the debugger tool, instrument
// the code, run the program, and check the callback received the
// per-event arg trio.
func TestGateMonitorPyReturnFires(t *testing.T) {
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.RETURN_VALUE), 0,
		},
		Consts:    []any{int64(11)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	interp := ts.Interp().Monitors
	if err := interp.UseToolID(monitor.ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	calls := 0
	cb := objects.NewBuiltinFunction("rec", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		if got := len(args); got != 3 {
			t.Errorf("PY_RETURN got %d args, want 3", got)
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
	if _, err := vm.EvalCode(ts, co, objects.NewDict(), objects.NewDict()); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if calls != 1 {
		t.Fatalf("PY_RETURN callback fired %d times, want 1", calls)
	}
}

// TestGateLegacyTraceFires drives the sys.settrace bridge end to end:
// install a Go-level legacy tracefunc, run a small program, and
// confirm the bridge fires PyTrace_RETURN with the right value.
func TestGateLegacyTraceFires(t *testing.T) {
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.RETURN_VALUE), 0,
		},
		Consts:    []any{int64(13)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	var sawReturn bool
	var retArg objects.Object
	fn := func(_ objects.Object, _ *frame.Frame, what int, arg objects.Object) error {
		if what == vm.PyTraceReturn {
			sawReturn = true
			retArg = arg
		}
		return nil
	}
	if _, err := vm.SetTrace(ts, fn, objects.NewStr("ctx")); err != nil {
		t.Fatalf("SetTrace: %v", err)
	}
	if err := monitor.Instrument(co, ts.Interp().Monitors); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	out, err := vm.EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if i, ok := out.(*objects.Int); !ok {
		t.Fatalf("EvalCode result = %T, want *Int", out)
	} else if v, _ := i.Int64(); v != 13 {
		t.Fatalf("EvalCode result = %d, want 13", v)
	}
	if !sawReturn {
		t.Fatalf("PyTraceReturn never fired")
	}
	if i, ok := retArg.(*objects.Int); !ok {
		t.Fatalf("PyTraceReturn arg = %T, want *Int", retArg)
	} else if v, _ := i.Int64(); v != 13 {
		t.Fatalf("PyTraceReturn arg = %d, want 13", v)
	}
}

// TestGateSysMonitoringRoundTrip exercises the sys.monitoring builtin
// surface: claim a tool slot through use_tool_id, register a Python
// callback through register_callback, set events, and confirm the
// internal state reflects the request. This is the user-visible API
// and shares the wiring with the lower-level fire path the previous
// test exercises.
func TestGateSysMonitoringRoundTrip(t *testing.T) {
	ts := state.NewThread()
	mod := monitor.BuildSysMonitoring(ts.Interp().Monitors)

	useID, _ := mod.Dict().GetItem(objects.NewStr("use_tool_id"))
	if _, err := useID.Type().Call(useID, []objects.Object{objects.NewInt(int64(monitor.ToolCoverage)), objects.NewStr("cov")}, nil); err != nil {
		t.Fatalf("use_tool_id: %v", err)
	}
	if got := ts.Interp().Monitors.ToolName(monitor.ToolCoverage); got == nil {
		t.Fatalf("ToolName(Coverage) is nil after use_tool_id")
	}

	cb := objects.NewBuiltinFunction("noop", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	regCb, _ := mod.Dict().GetItem(objects.NewStr("register_callback"))
	prev, err := regCb.Type().Call(regCb, []objects.Object{
		objects.NewInt(int64(monitor.ToolCoverage)),
		objects.NewInt(1 << monitor.EventPyReturn),
		cb,
	}, nil)
	if err != nil {
		t.Fatalf("register_callback: %v", err)
	}
	if prev != objects.None() {
		t.Errorf("first register_callback prev = %v, want None", prev)
	}
	if got := ts.Interp().Monitors.Callback(monitor.ToolCoverage, monitor.EventPyReturn); got == nil {
		t.Fatalf("Callback not stored on InterpState")
	}
}
