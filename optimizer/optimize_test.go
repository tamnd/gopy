package optimizer

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func TestOptimize_InstallsExecutorOnLoop(t *testing.T) {
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.POP_TOP), 0,
			byte(compile.JUMP_BACKWARD), 4,
			0, 0,
		},
	}
	interp := &state.Interpreter{}
	exec, status := Optimize(interp, nil, co, 0, 0, 0)
	if status != 1 {
		t.Fatalf("Optimize status = %d, want 1", status)
	}
	if exec == nil {
		t.Fatalf("Optimize returned nil executor")
	}
	if compile.Opcode(co.Code[0]) != compile.ENTER_EXECUTOR {
		t.Errorf("install site code[0] = %d, want ENTER_EXECUTOR", co.Code[0])
	}
	arr := codeExecutors(co)
	if arr == nil || arr.Entries[co.Code[1]] != exec {
		t.Errorf("executor not installed in side table")
	}
}

func TestOptimize_BailsOnEntryReturn(t *testing.T) {
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.RETURN_VALUE), 0,
		},
	}
	interp := &state.Interpreter{}
	exec, status := Optimize(interp, nil, co, 0, 0, 0)
	if status != 0 {
		t.Errorf("Optimize status = %d, want 0", status)
	}
	if exec != nil {
		t.Errorf("expected nil executor on bail, got %v", exec)
	}
	if compile.Opcode(co.Code[0]) == compile.ENTER_EXECUTOR {
		t.Errorf("install site should be untouched on bail")
	}
}
