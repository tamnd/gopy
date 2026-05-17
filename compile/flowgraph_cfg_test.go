package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestNewCfgBuilderHasEntryBlock(t *testing.T) {
	g := newCfgBuilder()
	if g.EntryBlock == nil {
		t.Fatal("entry block nil")
	}
	if g.CurBlock != g.EntryBlock {
		t.Fatal("curblock != entryblock at init")
	}
	if g.BlockList != g.EntryBlock {
		t.Fatal("block list head should be entry block")
	}
}

func TestCfgBuilderAddOpAppendsToCurrentBlock(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	if got := len(g.EntryBlock.Instr); got != 2 {
		t.Fatalf("entry block instr count = %d, want 2", got)
	}
	if g.EntryBlock.Instr[0].Op != LOAD_CONST {
		t.Errorf("instr[0] = %v, want LOAD_CONST", g.EntryBlock.Instr[0].Op)
	}
}

func TestUseLabelStartsNewBlockWhenCurrentNonempty(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(LOAD_CONST, 0, loc)
	lbl := JumpTargetLabel{id: 1}
	g.useLabel(lbl)
	g.addOp(RETURN_VALUE, 0, loc)
	if g.CurBlock == g.EntryBlock {
		t.Fatal("expected fresh block after useLabel on populated entry")
	}
	if g.CurBlock.Label.id != 1 {
		t.Errorf("curblock label = %d, want 1", g.CurBlock.Label.id)
	}
	if g.EntryBlock.Next != g.CurBlock {
		t.Error("entry.Next should chain to current block")
	}
}

func TestUseLabelOnEmptyBlockJustLabelsIt(t *testing.T) {
	g := newCfgBuilder()
	lbl := JumpTargetLabel{id: 7}
	g.useLabel(lbl)
	if g.CurBlock != g.EntryBlock {
		t.Fatal("expected no new block when current is empty and unlabeled")
	}
	if g.EntryBlock.Label.id != 7 {
		t.Errorf("entry label = %d, want 7", g.EntryBlock.Label.id)
	}
}

func TestAddJumpRecordsTargetPointer(t *testing.T) {
	g := newCfgBuilder()
	target := g.newBlock()
	target.Label = JumpTargetLabel{id: 3}
	loc := ast.Pos{Lineno: 1}
	if !g.CurBlock.addJump(JUMP, target, loc) {
		t.Fatal("addJump returned false on empty block")
	}
	last := g.CurBlock.lastInstr()
	if last == nil || last.Op != JUMP {
		t.Fatalf("last instr = %v, want JUMP", last)
	}
	if last.Target != target {
		t.Error("jump target pointer not recorded")
	}
	if last.Oparg != 3 {
		t.Errorf("oparg = %d, want target label id 3", last.Oparg)
	}
}

func TestAddJumpRefusesDoubleJump(t *testing.T) {
	g := newCfgBuilder()
	target := g.newBlock()
	target.Label = JumpTargetLabel{id: 1}
	loc := ast.Pos{Lineno: 1}
	if !g.CurBlock.addJump(JUMP, target, loc) {
		t.Fatal("first addJump failed")
	}
	if g.CurBlock.addJump(JUMP, target, loc) {
		t.Error("second addJump on a jump-terminated block must fail")
	}
}

func TestNofallthroughRecognizesScopeExitAndJump(t *testing.T) {
	loc := ast.Pos{Lineno: 1}
	b := &basicblock{}
	if b.nofallthrough() {
		t.Error("empty block should fall through")
	}
	b.addOp(LOAD_CONST, 0, loc)
	if b.nofallthrough() {
		t.Error("plain LOAD_CONST should fall through")
	}
	b.addOp(RETURN_VALUE, 0, loc)
	if !b.nofallthrough() {
		t.Error("RETURN_VALUE block should not fall through")
	}
}

func TestBlocksWalkFollowsBNext(t *testing.T) {
	g := newCfgBuilder()
	b2 := g.newBlock()
	b3 := g.newBlock()
	g.useNextBlock(b2)
	g.useNextBlock(b3)
	got := g.blocks()
	if len(got) != 3 {
		t.Fatalf("blocks() len = %d, want 3", len(got))
	}
	if got[0] != g.EntryBlock || got[1] != b2 || got[2] != b3 {
		t.Errorf("walk order = %v", got)
	}
}

func TestAppendInstructionsCopiesFromSource(t *testing.T) {
	loc := ast.Pos{Lineno: 1}
	src := &basicblock{}
	src.addOp(LOAD_CONST, 5, loc)
	src.addOp(NOP, 0, loc)
	dst := &basicblock{}
	dst.addOp(RETURN_VALUE, 0, loc)
	dst.appendInstructions(src)
	if len(dst.Instr) != 3 {
		t.Fatalf("dst instr len = %d, want 3", len(dst.Instr))
	}
	if dst.Instr[1].Op != LOAD_CONST || dst.Instr[1].Oparg != 5 {
		t.Errorf("appended instr[1] = %+v", dst.Instr[1])
	}
}

func TestInsertInstructionShiftsTail(t *testing.T) {
	loc := ast.Pos{Lineno: 1}
	b := &basicblock{}
	b.addOp(LOAD_CONST, 1, loc)
	b.addOp(RETURN_VALUE, 0, loc)
	b.insertInstruction(1, cfgInstr{Op: NOP})
	if len(b.Instr) != 3 || b.Instr[1].Op != NOP || b.Instr[2].Op != RETURN_VALUE {
		t.Errorf("post-insert ops = %v / %v / %v", b.Instr[0].Op, b.Instr[1].Op, b.Instr[2].Op)
	}
}

func TestExitsScopeMatchesScopeExitOnly(t *testing.T) {
	loc := ast.Pos{Lineno: 1}
	b := &basicblock{}
	b.addOp(JUMP, 0, loc)
	if b.exitsScope() {
		t.Error("JUMP is nofallthrough but not a scope exit")
	}
	b.Instr = nil
	b.addOp(RETURN_VALUE, 0, loc)
	if !b.exitsScope() {
		t.Error("RETURN_VALUE should be recognized as a scope exit")
	}
}

func TestCopyBasicblockDuplicatesNofallthroughBlock(t *testing.T) {
	loc := ast.Pos{Lineno: 1}
	g := newCfgBuilder()
	src := g.newBlock()
	src.addOp(LOAD_CONST, 9, loc)
	src.addOp(RETURN_VALUE, 0, loc)
	dup := g.copyBasicblock(src)
	if dup == src {
		t.Fatal("copy returned the same block")
	}
	if len(dup.Instr) != 2 || dup.Instr[0].Oparg != 9 {
		t.Errorf("copied instr = %v", dup.Instr)
	}
}

func TestNextNonemptyBlockSkipsEmpties(t *testing.T) {
	loc := ast.Pos{Lineno: 1}
	a := &basicblock{}
	b := &basicblock{}
	c := &basicblock{}
	c.addOp(NOP, 0, loc)
	a.Next = b
	b.Next = c
	if nextNonemptyBlock(a) != c {
		t.Error("should skip a and b to land on c")
	}
}
