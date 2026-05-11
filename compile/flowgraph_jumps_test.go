package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestThreadJumpsChain rewrites JUMP A; ... ; JUMP B so the first
// jump lands directly on B's eventual target.
func TestThreadJumpsChain(t *testing.T) {
	// 0: JUMP_FORWARD -> 2
	// 1: NOP
	// 2: JUMP_FORWARD -> 4
	// 3: NOP
	// 4: RETURN_VALUE
	seq := &Sequence{Instrs: []Instr{
		{Op: JUMP_FORWARD, Oparg: 2},
		{Op: NOP},
		{Op: JUMP_FORWARD, Oparg: 4},
		{Op: NOP},
		{Op: RETURN_VALUE},
	}}
	if n := threadJumps(seq); n != 1 {
		t.Fatalf("threadJumps rewrote %d, want 1", n)
	}
	if got := seq.Instrs[0].Oparg; got != 4 {
		t.Errorf("instr 0 oparg = %d, want 4 (chained past trampoline)", got)
	}
}

// TestPropagateConditionalJump retargets POP_JUMP_IF_FALSE A when A is
// itself an unconditional jump, skipping the trampoline.
func TestPropagateConditionalJump(t *testing.T) {
	// 0: LOAD_CONST 0
	// 1: POP_JUMP_IF_FALSE -> 3
	// 2: RETURN_VALUE
	// 3: JUMP_FORWARD -> 5
	// 4: NOP
	// 5: RETURN_VALUE
	seq := &Sequence{Instrs: []Instr{
		{Op: LOAD_CONST, Oparg: 0},
		{Op: POP_JUMP_IF_FALSE, Oparg: 3},
		{Op: RETURN_VALUE},
		{Op: JUMP_FORWARD, Oparg: 5},
		{Op: NOP},
		{Op: RETURN_VALUE},
	}}
	if n := propagateConditionalJumps(seq); n != 1 {
		t.Fatalf("propagateConditionalJumps rewrote %d, want 1", n)
	}
	if got := seq.Instrs[1].Oparg; got != 5 {
		t.Errorf("POP_JUMP_IF_FALSE oparg = %d, want 5", got)
	}
}

// TestPropagateConditionalJumpSkipsBackward checks that the pass does
// not retarget a POP_JUMP_IF_X through a JUMP_BACKWARD whose final
// landing site is behind the conditional. The opcode is forward-only
// once the assembler resolves offsets; threading it backwards encodes
// a positive delta past end-of-code. Reproducer pattern: an `if` that
// is the last statement of a `for` body parks the if's end-label on
// top of the loop back-edge, and without the guard the conditional
// gets pulled all the way onto FOR_ITER.
func TestPropagateConditionalJumpSkipsBackward(t *testing.T) {
	// 0: FOR_ITER -> 5            (loop entry, header)
	// 1: LOAD_CONST 0
	// 2: POP_JUMP_IF_FALSE -> 3   (if cond ...)
	// 3: JUMP_BACKWARD -> 0       (back to FOR_ITER, also the if-end)
	// 4: RETURN_VALUE             (cleanup)
	// 5: RETURN_VALUE
	seq := &Sequence{Instrs: []Instr{
		{Op: FOR_ITER, Oparg: 5},
		{Op: LOAD_CONST, Oparg: 0},
		{Op: POP_JUMP_IF_FALSE, Oparg: 3},
		{Op: JUMP_BACKWARD, Oparg: 0},
		{Op: RETURN_VALUE},
		{Op: RETURN_VALUE},
	}}
	if n := propagateConditionalJumps(seq); n != 0 {
		t.Fatalf("propagateConditionalJumps rewrote %d, want 0 (backward target)", n)
	}
	if got := seq.Instrs[2].Oparg; got != 3 {
		t.Errorf("POP_JUMP_IF_FALSE oparg = %d, want 3 (left untouched)", got)
	}
}

// TestThreadJumpsCycleSafe is a degenerate self-loop that must not
// hang.
func TestThreadJumpsCycleSafe(t *testing.T) {
	// 0: JUMP_FORWARD -> 0
	seq := &Sequence{Instrs: []Instr{
		{Op: JUMP_FORWARD, Oparg: 0},
	}}
	threadJumps(seq) // must terminate
}

// TestRemoveUnreachableBlocks NOPs out instructions no path can reach.
func TestRemoveUnreachableBlocks(t *testing.T) {
	// 0: LOAD_CONST 0
	// 1: RETURN_VALUE
	// 2: LOAD_CONST 0   (unreachable)
	// 3: RETURN_VALUE   (unreachable)
	seq := &Sequence{Instrs: []Instr{
		{Op: LOAD_CONST, Oparg: 0},
		{Op: RETURN_VALUE},
		{Op: LOAD_CONST, Oparg: 0},
		{Op: RETURN_VALUE},
	}}
	if n := removeUnreachableBlocks(seq); n != 2 {
		t.Fatalf("dropped %d, want 2", n)
	}
	if seq.Instrs[2].Op != NOP || seq.Instrs[3].Op != NOP {
		t.Errorf("trailing dead instrs not NOPed: %+v", seq.Instrs[2:])
	}
}

// TestRemoveUnreachableKeepsHandlerTarget pins handler labels as roots.
func TestRemoveUnreachableKeepsHandlerTarget(t *testing.T) {
	// 0: LOAD_CONST 0      (handler -> 3)
	// 1: RETURN_VALUE
	// 2: LOAD_CONST 0      (unreachable by linear walk)
	// 3: RETURN_VALUE      (only reached via handler edge)
	seq := &Sequence{Instrs: []Instr{
		{Op: LOAD_CONST, Oparg: 0, Handler: ExceptHandlerInfo{Label: 3}},
		{Op: RETURN_VALUE, Handler: ExceptHandlerInfo{Label: -1}},
		{Op: LOAD_CONST, Oparg: 0, Handler: ExceptHandlerInfo{Label: -1}},
		{Op: RETURN_VALUE, Handler: ExceptHandlerInfo{Label: -1}},
	}}
	removeUnreachableBlocks(seq)
	if seq.Instrs[3].Op != RETURN_VALUE {
		t.Errorf("handler target was clobbered: %v", seq.Instrs[3].Op)
	}
	if seq.Instrs[2].Op != NOP {
		t.Errorf("instr 2 should be unreachable: %v", seq.Instrs[2].Op)
	}
}

// TestOptimizeIntegratesPasses drives the full Optimize pipeline with
// a chain that exercises threading and unreachable removal together.
func TestOptimizeIntegratesPasses(t *testing.T) {
	seq := &Sequence{}
	end := seq.NewLabel()
	mid := seq.NewLabel()
	// LOAD_CONST 0; JUMP mid; <unreachable LOAD_CONST>; <mid:> JUMP end;
	// <end:> RETURN_VALUE
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(JUMP_FORWARD, int32(mid.ID()), ast.Pos{})
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.UseLabel(mid)
	seq.Addop(JUMP_FORWARD, int32(end.ID()), ast.Pos{})
	seq.UseLabel(end)
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})

	consts := []any{nil}
	if _, err := Optimize(seq, &consts, 0, 1); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	// First JUMP should now point straight at the RETURN, skipping the
	// trampoline JUMP at mid. Opargs are relative deltas after Optimize:
	// target = (i + 1) + oparg.
	for i, ins := range seq.Instrs {
		if ins.Op == JUMP_FORWARD {
			target := i + 1 + int(ins.Oparg)
			if target < 0 || target >= len(seq.Instrs) || seq.Instrs[target].Op != RETURN_VALUE {
				op := NOP
				if target >= 0 && target < len(seq.Instrs) {
					op = seq.Instrs[target].Op
				}
				t.Errorf("instr %d JUMP did not thread to RETURN: target op = %v",
					i, op)
			}
			break
		}
	}
}
