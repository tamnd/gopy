package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestOptimizeResolvesLabels verifies that Optimize rewrites jump
// opargs from label ids to instruction offsets.
func TestOptimizeResolvesLabels(t *testing.T) {
	seq := &Sequence{}
	end := seq.NewLabel()
	// LOAD_CONST 0; POP_JUMP_IF_FALSE end; LOAD_CONST 0; <end:>
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(POP_JUMP_IF_FALSE, int32(end.ID()), ast.Pos{})
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.UseLabel(end)
	seq.Addop(NOP, 0, ast.Pos{})

	info, err := Optimize(seq, []any{nil}, 0, 1)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := seq.Instrs[1].Oparg; got != 3 {
		t.Errorf("expected POP_JUMP_IF_FALSE oparg resolved to 3, got %d", got)
	}
	if info.MaxStackDepth < 1 {
		t.Errorf("expected non-zero max stack depth, got %d", info.MaxStackDepth)
	}
}

// TestStackDepthSimplePush verifies that LOAD_CONST + RETURN_VALUE
// produces depth 1.
func TestStackDepthSimplePush(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})
	d, err := calculateStackdepth(seq)
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
	d, err := calculateStackdepth(seq)
	if err != nil {
		t.Fatalf("calculateStackdepth: %v", err)
	}
	if d != 3 {
		t.Errorf("expected depth 3, got %d", d)
	}
}

// TestFromSequenceSplitsAtTerminators checks block boundaries.
func TestFromSequenceSplitsAtTerminators(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})
	b, err := FromSequence(seq)
	if err != nil {
		t.Fatalf("FromSequence: %v", err)
	}
	count := 0
	for blk := b.Head; blk != nil; blk = blk.Next {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 blocks (one per RETURN), got %d", count)
	}
}
