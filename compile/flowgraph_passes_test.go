package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestFoldBinaryIntConstAdd: a 1+2 triple folds to a single LOAD_CONST
// pointing at 3 in the const pool.
func TestFoldBinaryIntConstAdd(t *testing.T) {
	consts := []any{int64(1), int64(2)}
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(LOAD_CONST, 1, ast.Pos{Lineno: 1})
	seq.Addop(BINARY_OP, nbAdd, ast.Pos{Lineno: 1})
	folded := foldBinaryIntConst(seq, &consts)
	if folded != 1 {
		t.Errorf("folded = %d, want 1", folded)
	}
	if seq.Instrs[0].Op != NOP || seq.Instrs[1].Op != NOP {
		t.Errorf("operand loads not nopped: %+v", seq.Instrs)
	}
	if seq.Instrs[2].Op != LOAD_CONST {
		t.Errorf("BINARY_OP not rewritten to LOAD_CONST: %+v", seq.Instrs[2])
	}
	got := consts[seq.Instrs[2].Oparg]
	if got != int64(3) {
		t.Errorf("folded const = %v, want int64(3)", got)
	}
}

// TestFoldBinaryIntConstNonInt: a triple with a non-int operand is
// left alone.
func TestFoldBinaryIntConstNonInt(t *testing.T) {
	consts := []any{int64(1), "two"}
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(LOAD_CONST, 1, ast.Pos{Lineno: 1})
	seq.Addop(BINARY_OP, nbAdd, ast.Pos{Lineno: 1})
	if folded := foldBinaryIntConst(seq, &consts); folded != 0 {
		t.Errorf("folded = %d, want 0", folded)
	}
}

// TestEliminateDeadCodeAfterTerminator: NOP after RETURN_VALUE gets
// killed when no label points at it.
func TestEliminateDeadCodeAfterTerminator(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})
	seq.Addop(POP_TOP, 0, ast.Pos{Lineno: 1}) // unreachable
	seq.Addop(POP_TOP, 0, ast.Pos{Lineno: 1}) // unreachable
	dropped := eliminateDeadCodeAfterTerminator(seq)
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	if seq.Instrs[2].Op != NOP || seq.Instrs[3].Op != NOP {
		t.Errorf("dead instructions not nopped: %+v", seq.Instrs)
	}
}

// TestEliminateDeadCodePreservesLabelTargets: an instruction that is
// a label target stays live even after a terminator.
func TestEliminateDeadCodePreservesLabelTargets(t *testing.T) {
	seq := &Sequence{}
	lbl := seq.NewLabel()
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})
	seq.UseLabel(lbl)
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1}) // pinned by lbl
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})
	dropped := eliminateDeadCodeAfterTerminator(seq)
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d (%+v)", dropped, seq.Instrs)
	}
}

// TestRemoveRedundantNopsCompacts: NOP instructions vanish after
// compaction; jump opargs are reindexed.
func TestRemoveRedundantNopsCompacts(t *testing.T) {
	// Build sequence with a jump over a NOP block.
	seq := &Sequence{}
	end := seq.NewLabel()
	seq.Addop(JUMP_FORWARD, int32(end.ID()), ast.Pos{Lineno: 1})
	seq.Addop(NOP, 0, ast.Pos{Lineno: 1})
	seq.Addop(NOP, 0, ast.Pos{Lineno: 1})
	seq.UseLabel(end)
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})

	seq.ApplyLabelMap(HasTarget)
	dropped := removeRedundantNops(seq)
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	if len(seq.Instrs) != 2 {
		t.Fatalf("len after compact = %d, want 2 (%+v)", len(seq.Instrs), seq.Instrs)
	}
	if seq.Instrs[0].Op != JUMP_FORWARD || seq.Instrs[0].Oparg != 1 {
		t.Errorf("jump oparg not reindexed: %+v", seq.Instrs[0])
	}
	if seq.Instrs[1].Op != RETURN_VALUE {
		t.Errorf("RETURN_VALUE missing: %+v", seq.Instrs)
	}
}

// TestRemoveRedundantNopsKeepsPinned: a NOP that is itself a jump
// target stays put.
func TestRemoveRedundantNopsKeepsPinned(t *testing.T) {
	seq := &Sequence{}
	tgt := seq.NewLabel()
	seq.Addop(JUMP_FORWARD, int32(tgt.ID()), ast.Pos{Lineno: 1})
	seq.UseLabel(tgt)
	seq.Addop(NOP, 0, ast.Pos{Lineno: 1}) // pinned by tgt
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})

	seq.ApplyLabelMap(HasTarget)
	dropped := removeRedundantNops(seq)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (NOP was a jump target)", dropped)
	}
}

// TestRemoveRedundantNopsRewritesPseudoJump: the pseudo opcode JUMP has
// no HasTarget metadata row, but it still carries a label oparg until
// resolveUnconditionalJumps lowers it. NOP compaction must reindex the
// pseudo form too, otherwise the oparg points at a stale slot.
func TestRemoveRedundantNopsRewritesPseudoJump(t *testing.T) {
	seq := &Sequence{}
	end := seq.NewLabel()
	seq.Addop(JUMP, int32(end.ID()), ast.Pos{Lineno: 1})
	seq.Addop(NOP, 0, ast.Pos{Lineno: 1})
	seq.UseLabel(end)
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})

	seq.ApplyLabelMap(func(op Opcode) bool {
		return HasTarget(op) || op == JUMP || op == JUMP_NO_INTERRUPT
	})
	removeRedundantNops(seq)
	if len(seq.Instrs) != 2 {
		t.Fatalf("len after compact = %d, want 2", len(seq.Instrs))
	}
	if seq.Instrs[0].Op != JUMP || seq.Instrs[0].Oparg != 1 {
		t.Errorf("pseudo JUMP oparg not reindexed: %+v", seq.Instrs[0])
	}
}

// TestOptimizeRunsPanel: the public Optimize entry runs the panel
// end-to-end and produces a sane stack depth on a trivial sequence.
func TestOptimizeRunsPanel(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(RESUME, 0, ast.Pos{Lineno: 1})
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})
	seq.Addop(NOP, 0, ast.Pos{Lineno: 1}) // dead
	consts := []any{nil}
	info, err := Optimize(seq, &consts, 0, 0)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if info.MaxStackDepth < 1 {
		t.Errorf("MaxStackDepth = %d, want >= 1", info.MaxStackDepth)
	}
	// Tail NOP should be gone after the panel.
	for _, ins := range seq.Instrs {
		if ins.Op == NOP {
			t.Errorf("residual NOP after Optimize: %+v", seq.Instrs)
			break
		}
	}
}
