package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// makeLineCode builds a Code blob whose linetable maps each codeunit
// to a fresh line, so every opcode begins a new line and qualifies
// for INSTRUMENTED_LINE.
func makeLineCode(code []byte, consts []any) *objects.Code {
	co := &objects.Code{
		Code:        append([]byte{}, code...),
		Consts:      consts,
		Stacksize:   4,
		Firstlineno: 1,
	}
	const start = 1 << 7
	const noCols = 13 << 3
	var tab []byte
	for i := 0; i < len(code)/2; i++ {
		// First byte: start | noCols | length=0 (one codeunit).
		// Second byte: signed varint for delta=1: (1<<1)|0 = 2.
		tab = append(tab, byte(start|noCols), 2)
	}
	co.Linetable = tab
	return co
}

// TestDispatchFiresLineEvent runs a two-instruction program with the
// debugger subscribed to LINE and verifies the callback fires for the
// first line and the program still produces its return value.
func TestDispatchFiresLineEvent(t *testing.T) {
	co := makeLineCode([]byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	}, []any{int64(7)})

	ts := state.NewThread()
	interp := ts.Interp().Monitors
	if err := interp.UseToolID(monitor.ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	var fired int
	cb := objects.NewBuiltinFunction("rec", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		fired++
		return objects.None(), nil
	})
	if _, err := interp.RegisterCallback(monitor.ToolDebugger, monitor.EventLine, cb); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	if err := interp.SetEvents(monitor.ToolDebugger, monitor.EventSet(0).With(monitor.EventLine)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := monitor.Instrument(co, interp); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if got := compile.Opcode(co.Code[0]); got != compile.INSTRUMENTED_LINE {
		t.Fatalf("first codeunit not rewritten, got %v", got)
	}

	out, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if i, ok := out.(*objects.Int); !ok {
		t.Fatalf("result type = %T, want *Int", out)
	} else if v, ok2 := i.Int64(); !ok2 || v != 7 {
		t.Fatalf("result = %v, want 7", out)
	}
	if fired < 1 {
		t.Fatalf("line callback fired %d times, want >= 1", fired)
	}
}
