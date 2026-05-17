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

func TestCfgRemoveRedundantJumpsDropsFallthroughJump(t *testing.T) {
	// entry: JUMP -> b2; b2: RETURN_VALUE. Fallthrough also lands on b2,
	// so the JUMP is redundant and becomes NOP.
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	b2 := g.newBlock()
	g.CurBlock.addJump(JUMP, b2, loc)
	g.useNextBlock(b2)
	b2.addOp(RETURN_VALUE, 0, loc)

	if got := cfgRemoveRedundantJumps(g); got != 1 {
		t.Errorf("changes = %d, want 1", got)
	}
	if g.EntryBlock.Instr[0].Op != NOP {
		t.Errorf("entry[0] = %v, want NOP", g.EntryBlock.Instr[0].Op)
	}
}

func TestCfgRemoveRedundantJumpsKeepsRealJump(t *testing.T) {
	// entry: JUMP -> b3 (skips b2). JUMP must survive.
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	b2 := g.newBlock()
	b3 := g.newBlock()
	g.CurBlock.addJump(JUMP, b3, loc)
	g.useNextBlock(b2)
	b2.addOp(NOP, 0, loc)
	g.useNextBlock(b3)
	b3.addOp(RETURN_VALUE, 0, loc)

	if got := cfgRemoveRedundantJumps(g); got != 0 {
		t.Errorf("changes = %d, want 0", got)
	}
	if g.EntryBlock.Instr[0].Op != JUMP {
		t.Errorf("entry[0] = %v, want JUMP (preserved)", g.EntryBlock.Instr[0].Op)
	}
}

func TestCfgRemoveUnreachableEmptiesOrphanBlock(t *testing.T) {
	// entry: RETURN_VALUE  (terminator, no fallthrough)
	// orphan: LOAD_CONST   (unreferenced -> emptied)
	// reachable_via_jump: NOP (target of entry-side jump? no — we only
	// have RETURN_VALUE; orphan is genuinely unreachable)
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(RETURN_VALUE, 0, loc)
	orphan := g.newBlock()
	g.useNextBlock(orphan)
	orphan.addOp(LOAD_CONST, 0, loc)

	cfgRemoveUnreachable(g)
	if orphan.Predecessors != 0 {
		t.Errorf("orphan predecessors = %d, want 0", orphan.Predecessors)
	}
	if len(orphan.Instr) != 0 {
		t.Errorf("orphan should be emptied, has %d instrs", len(orphan.Instr))
	}
	if g.EntryBlock.Predecessors != 1 {
		t.Errorf("entry predecessors = %d, want 1", g.EntryBlock.Predecessors)
	}
}

func TestCfgRemoveUnreachableCountsJumpAndFallthrough(t *testing.T) {
	// entry: JUMP -> b2 (no fallthrough since JUMP is unconditional).
	// b2: NOP -> falls through to b3.
	// b3: RETURN_VALUE.
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	b2 := g.newBlock()
	b3 := g.newBlock()
	g.CurBlock.addJump(JUMP, b2, loc)
	g.useNextBlock(b2)
	b2.addOp(NOP, 0, loc)
	g.useNextBlock(b3)
	b3.addOp(RETURN_VALUE, 0, loc)

	cfgRemoveUnreachable(g)
	if b2.Predecessors != 1 || b3.Predecessors != 1 {
		t.Errorf("predecessors b2=%d b3=%d, want 1/1", b2.Predecessors, b3.Predecessors)
	}
}

func TestCfgPropagateLineNumbersFillsHolesInBlock(t *testing.T) {
	// First instr on line 5, second unlocated -> inherits 5.
	g := newCfgBuilder()
	g.addOp(LOAD_CONST, 0, ast.Pos{Lineno: 5})
	g.addOp(NOP, 0, ast.Pos{Lineno: -1})
	g.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: 6})

	cfgPropagateLineNumbers(g)
	if got := g.EntryBlock.Instr[1].Loc.Lineno; got != 5 {
		t.Errorf("middle NOP lineno = %d, want 5 (inherited)", got)
	}
	if got := g.EntryBlock.Instr[2].Loc.Lineno; got != 6 {
		t.Errorf("RETURN_VALUE lineno = %d, want 6 (unchanged)", got)
	}
}

func TestCfgPropagateLineNumbersSeedsSinglePredSuccessor(t *testing.T) {
	g := newCfgBuilder()
	g.addOp(LOAD_CONST, 0, ast.Pos{Lineno: 9})
	b2 := g.newBlock()
	b2.Predecessors = 1
	g.useNextBlock(b2)
	b2.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: -1})

	cfgPropagateLineNumbers(g)
	if got := b2.Instr[0].Loc.Lineno; got != 9 {
		t.Errorf("successor head lineno = %d, want 9 (seeded)", got)
	}
}

func TestCfgDuplicateExitsWithoutLinenoSplitsSharedExit(t *testing.T) {
	// Two predecessors jump into a shared RETURN_VALUE with no lineno.
	// duplicate_exits_without_lineno must clone it for one of them.
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 4}
	b2 := g.newBlock()
	exit := g.newBlock()
	exit.addOp(RETURN_VALUE, 0, ast.Pos{Lineno: -1})
	exit.Predecessors = 2
	g.CurBlock.addJump(JUMP, exit, loc)
	b2.addJump(JUMP, exit, loc)
	g.useNextBlock(b2)
	g.useNextBlock(exit)

	cfgDuplicateExitsWithoutLineno(g)
	// One of the jumps should now point to a freshly-cloned exit.
	t1 := g.EntryBlock.Instr[0].Target
	t2 := b2.Instr[0].Target
	if t1 == t2 {
		t.Fatal("expected one jump retargeted to a clone")
	}
	if exit.Predecessors != 1 {
		t.Errorf("original exit predecessors = %d, want 1", exit.Predecessors)
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
