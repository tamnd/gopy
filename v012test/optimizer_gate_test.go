// Package v012test is the v0.12 Tier-2 optimizer gate. The eval loop
// calls the warmup callback at every JUMP_BACKWARD; once the
// projector returns a usable trace, the install site flips to
// ENTER_EXECUTOR and subsequent passes route through the executor.
// gopy stays interpreter-only for v0.12 (the uop dispatcher lands
// with #431), so ENTER_EXECUTOR dispatch deopts to the Tier-1
// instruction stashed at install time. The gate pins all three
// halves: projection install, side-table wiring, and the deopt arm.
//
// Spec 1697 also asks for invalidation and side-exit coverage; those
// are gated by #444 (analysis) and #445 (watcher wiring) and land in
// follow-up gates as those tasks complete.
package v012test

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/optimizer"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// loopBytecode is a tiny loop fixture: LOAD_CONST + POP_TOP +
// JUMP_BACKWARD with the inline cache slot CPython expects. Same
// shape as the projection's TestTranslateBytecodeToTrace fixture so
// the trace closes with _JUMP_TO_TOP.
func loopBytecode() *objects.Code {
	return &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.POP_TOP), 0,
			byte(compile.JUMP_BACKWARD), 4,
			byte(compile.CACHE), 0,
		},
		Stacksize: 4,
		Consts:    []any{nil},
	}
}

// TestOptimizerGateInstall pins the install side of the warmup
// callback. Optimize is what the eval loop calls from JUMP_BACKWARD;
// on success it patches the install site to ENTER_EXECUTOR and
// stamps the executor into Code.Executors. This is the same code
// path tier2.go's tryWarmupTier2 drives, so a passing gate here means
// the JUMP_BACKWARD wiring has a working install target waiting for
// it at runtime.
func TestOptimizerGateInstall(t *testing.T) {
	co := loopBytecode()
	interp := &state.Interpreter{JIT: true}

	exec, status := optimizer.Optimize(interp, nil, co, 0, 0, 0)
	if status != 1 {
		t.Fatalf("Optimize status = %d, want 1", status)
	}
	if exec == nil {
		t.Fatal("Optimize returned nil executor on success")
	}

	if op := compile.Opcode(co.Code[0]); op != compile.ENTER_EXECUTOR {
		t.Errorf("install site Code[0] = %s, want ENTER_EXECUTOR", op.Name())
	}
	arr, ok := co.Executors.(*optimizer.ExecutorArray)
	if !ok || arr == nil {
		t.Fatalf("Code.Executors not populated: got %T", co.Executors)
	}
	slot := int(co.Code[1])
	if slot < 0 || slot >= len(arr.Entries) || arr.Entries[slot] != exec {
		t.Errorf("executor not in slot %d (entries=%d)", slot, len(arr.Entries))
	}

	// VMData stashes the original Tier-1 instruction. The deopt arm
	// reads it back, so the runtime must see exactly the LOAD_CONST
	// the warmup site was patched out of.
	if exec.VMData.Opcode != uint8(compile.LOAD_CONST) {
		t.Errorf("exec.VMData.Opcode = %d, want LOAD_CONST", exec.VMData.Opcode)
	}
	if exec.VMData.Index != 0 {
		t.Errorf("exec.VMData.Index = %d, want 0", exec.VMData.Index)
	}
	if exec.VMData.Code != co {
		t.Error("exec.VMData.Code does not point back at the source code")
	}

	// Trace prelude is fixed: _START_EXECUTOR + _MAKE_WARM. The loop
	// edge folds back via _JUMP_TO_TOP somewhere in the body.
	if len(exec.Trace) < 3 {
		t.Fatalf("trace too short: len=%d", len(exec.Trace))
	}
	if name := optimizer.UopNames[exec.Trace[0].Opcode()]; name != "_START_EXECUTOR" {
		t.Errorf("trace[0] = %q, want _START_EXECUTOR", name)
	}
	if name := optimizer.UopNames[exec.Trace[1].Opcode()]; name != "_MAKE_WARM" {
		t.Errorf("trace[1] = %q, want _MAKE_WARM", name)
	}
	sawJumpToTop := false
	for i := 0; i < len(exec.Trace); i++ {
		if optimizer.UopNames[exec.Trace[i].Opcode()] == "_JUMP_TO_TOP" {
			sawJumpToTop = true
			break
		}
	}
	if !sawJumpToTop {
		t.Errorf("trace missing _JUMP_TO_TOP loop closure")
	}
}

// TestOptimizerGateEnterExecutorDispatch pins the deopt arm. With no
// uop interpreter wired, ENTER_EXECUTOR's job is to read back the
// stashed Tier-1 op and dispatch it through trySimple. Stash a
// RETURN_VALUE so the deopt terminates the eval loop and surfaces
// the value the trace would have returned. A clean None back from
// EvalCode proves the path went through enterExecutor (not through
// any other hand-rolled arm or notImplemented).
func TestOptimizerGateEnterExecutorDispatch(t *testing.T) {
	ts := state.NewThread()

	// Bytecode: LOAD_CONST None pushes the value RETURN_VALUE will
	// pop back; ENTER_EXECUTOR fires next and the deopt path
	// completes the return.
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.ENTER_EXECUTOR), 0,
		},
		Stacksize: 4,
		Consts:    []any{nil},
	}

	exec := optimizer.AllocateExecutor(0, 0)
	exec.VMData.Opcode = uint8(compile.RETURN_VALUE)
	exec.VMData.Oparg = 0
	exec.VMData.Code = co
	exec.VMData.Index = 1
	co.Executors = &optimizer.ExecutorArray{
		Capacity: 1,
		Size:     1,
		Entries:  []*optimizer.Executor{exec},
	}

	v, err := vm.EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode err: %v", err)
	}
	if v != objects.None() {
		t.Errorf("EvalCode return = %v, want None", v)
	}
}
