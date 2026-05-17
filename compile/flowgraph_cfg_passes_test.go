package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestCfgRemoveRedundantNopsDropsUnlineNops(t *testing.T) {
	g := newCfgBuilder()
	g.addOp(NOP, 0, ast.Pos{Lineno: -1})
	g.addOp(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	g.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: 1})

	if got := cfgRemoveRedundantNops(g); got != 1 {
		t.Errorf("removed = %d, want 1", got)
	}
	if len(g.EntryBlock.Instr) != 2 {
		t.Errorf("after compact: %d instrs, want 2", len(g.EntryBlock.Instr))
	}
}

func TestCfgRemoveRedundantNopsKeepsLoneLineMarker(t *testing.T) {
	// A NOP whose lineno differs from both neighbors must survive.
	g := newCfgBuilder()
	g.addOp(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	g.addOp(NOP, 0, ast.Pos{Lineno: 5})
	g.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: 9})

	if got := cfgRemoveRedundantNops(g); got != 0 {
		t.Errorf("removed = %d, want 0", got)
	}
}

func TestCfgRemoveRedundantNopsDonatesLocationForward(t *testing.T) {
	// NOP on line 4 followed by an unlocated LOAD_CONST: the NOP is
	// dropped and the LOAD_CONST inherits the location.
	g := newCfgBuilder()
	g.addOp(NOP, 0, ast.Pos{Lineno: 4})
	g.addOp(LOAD_CONST, 0, ast.Pos{Lineno: -1})
	g.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: 4})

	cfgRemoveRedundantNops(g)
	if g.EntryBlock.Instr[0].Op != LOAD_CONST {
		t.Fatalf("instr[0] = %v, want LOAD_CONST", g.EntryBlock.Instr[0].Op)
	}
	if g.EntryBlock.Instr[0].Loc.Lineno != 4 {
		t.Errorf("LOAD_CONST lineno = %d, want 4 (inherited from NOP)", g.EntryBlock.Instr[0].Loc.Lineno)
	}
}

func TestCfgNormalizeJumpsForwardAddsNotTaken(t *testing.T) {
	// b1: POP_JUMP_IF_FALSE -> b2 (forward).
	// Expect NOT_TAKEN appended after the conditional in b1.
	g := newCfgBuilder()
	b2 := g.newBlock()
	loc := ast.Pos{Lineno: 1}
	g.CurBlock.Instr = append(g.CurBlock.Instr, cfgInstr{
		Op: POP_JUMP_IF_FALSE, Target: b2, Loc: loc,
	})
	g.useNextBlock(b2)
	b2.addOp(RETURN_VALUE, 0, loc)

	cfgNormalizeJumps(g)

	if got := len(g.EntryBlock.Instr); got != 2 {
		t.Fatalf("entry block instrs = %d, want 2 (cond + NOT_TAKEN)", got)
	}
	if g.EntryBlock.Instr[1].Op != NOT_TAKEN {
		t.Errorf("instr[1] = %v, want NOT_TAKEN", g.EntryBlock.Instr[1].Op)
	}
}

func TestCfgNormalizeJumpsBackwardReversesAndInsertsTrampoline(t *testing.T) {
	// entry (loop head) <- POP_JUMP_IF_TRUE (backward).
	// Expected layout: entry, ..., body, trampoline, after.
	g := newCfgBuilder()
	entry := g.EntryBlock
	loc := ast.Pos{Lineno: 1}
	entry.addOp(LOAD_CONST, 0, loc)

	body := g.newBlock()
	g.useNextBlock(body)
	body.Instr = append(body.Instr, cfgInstr{
		Op: POP_JUMP_IF_TRUE, Target: entry, Loc: loc,
	})

	after := g.newBlock()
	g.useNextBlock(after)
	after.addOp(RETURN_VALUE, 0, loc)

	cfgNormalizeJumps(g)

	last := body.lastInstr()
	if last.Op != POP_JUMP_IF_FALSE {
		t.Errorf("body last op = %v, want POP_JUMP_IF_FALSE (reversed)", last.Op)
	}
	if last.Target == entry {
		t.Error("reversed jump should retarget away from the loop head")
	}
	if body.Next == after {
		t.Fatal("trampoline block should have been inserted between body and after")
	}
	tramp := body.Next
	if tramp.Next != after {
		t.Error("trampoline should chain to the original next")
	}
	if len(tramp.Instr) != 2 || tramp.Instr[0].Op != NOT_TAKEN || tramp.Instr[1].Op != JUMP {
		t.Errorf("trampoline body = %+v, want [NOT_TAKEN, JUMP]", tramp.Instr)
	}
	if tramp.Instr[1].Target != entry {
		t.Error("trampoline JUMP should target the loop head")
	}
}

func TestCfgRemoveRedundantNopsAcrossBlockBoundary(t *testing.T) {
	// Trailing NOP whose line matches the next block's first real
	// instruction is removable.
	g := newCfgBuilder()
	g.addOp(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	g.addOp(NOP, 0, ast.Pos{Lineno: 2})
	b2 := g.newBlock()
	g.useNextBlock(b2)
	g.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: 2})

	if got := cfgRemoveRedundantNops(g); got != 1 {
		t.Errorf("removed = %d, want 1", got)
	}
	if len(g.EntryBlock.Instr) != 1 {
		t.Errorf("entry instrs = %d, want 1", len(g.EntryBlock.Instr))
	}
}
