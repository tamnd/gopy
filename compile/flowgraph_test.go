package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestOptimizeResolvesLabels verifies that Optimize runs without error
// on a sequence with a conditional jump and label.
func TestOptimizeResolvesLabels(t *testing.T) {
	seq := &Sequence{}
	end := seq.NewLabel()
	// RESUME; LOAD_CONST 42 (truthy); POP_JUMP_IF_FALSE end; LOAD_CONST 0; <end:> RETURN_VALUE
	seq.Addop(RESUME, 0, ast.Pos{Lineno: 1})
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(POP_JUMP_IF_FALSE, int32(end.ID()), ast.Pos{Lineno: 1})
	seq.Addop(LOAD_CONST, 1, ast.Pos{Lineno: 1})
	seq.UseLabel(end)
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})

	consts := []any{int64(42), int64(99)}
	info, err := Optimize(seq, &consts, 0, 1)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if info.MaxStackDepth < 1 {
		t.Errorf("expected non-zero max stack depth, got %d", info.MaxStackDepth)
	}
	// The pipeline must have resolved all labels: no instruction should
	// carry a raw label id as its oparg for jump opcodes. Every surviving
	// jump must have a relative byte offset as oparg (not a large label id).
	for i, ins := range seq.Instrs {
		if HasTarget(ins.Op) && int(ins.Oparg) > len(seq.Instrs) {
			t.Errorf("instr[%d] %v has suspiciously large oparg %d (unresolved label?)", i, ins.Op, ins.Oparg)
		}
	}
}

// TestStackDepthSimplePush verifies that LOAD_CONST + RETURN_VALUE
// produces depth 1.
func TestStackDepthSimplePush(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})
	d, _, err := calculateStackdepth(seq)
	if err != nil {
		t.Fatalf("calculateStackdepth: %v", err)
	}
	if d != 1 {
		t.Errorf("expected depth 1, got %d", d)
	}
}

// TestStackDepthBuildTuple checks BUILD_TUPLE n consumes n and pushes 1.
func TestStackDepthBuildTuple(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(BUILD_TUPLE, 3, ast.Pos{})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})
	d, _, err := calculateStackdepth(seq)
	if err != nil {
		t.Fatalf("calculateStackdepth: %v", err)
	}
	if d != 3 {
		t.Errorf("expected depth 3, got %d", d)
	}
}
