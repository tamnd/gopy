package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestCfgCalculateStackdepthLinear(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(LOAD_CONST, 1, loc)
	g.addOp(BINARY_OP, nbAdd, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	depth, err := cfgCalculateStackdepth(g)
	if err != nil {
		t.Fatalf("calculateStackdepth: %v", err)
	}
	if depth != 2 {
		t.Fatalf("max stackdepth = %d, want 2", depth)
	}
}

func TestCfgCalculateStackdepthBranch(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	tgt := g.newBlock()
	g.addOp(LOAD_CONST, 0, loc)
	g.CurBlock.addJump(JUMP, tgt, loc)
	g.useNextBlock(tgt)
	g.addOp(RETURN_VALUE, 0, loc)
	depth, err := cfgCalculateStackdepth(g)
	if err != nil {
		t.Fatalf("calculateStackdepth: %v", err)
	}
	if depth != 1 {
		t.Fatalf("max stackdepth = %d, want 1", depth)
	}
}

func TestCfgCalculateStackdepthUnderflowReturnsError(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(POP_TOP, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	if _, err := cfgCalculateStackdepth(g); err == nil {
		t.Fatal("expected underflow error")
	}
}

func TestOptimizeLoadFastDowngradesPlainReturn(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(LOAD_FAST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	if err := optimizeLoadFast(g); err != nil {
		t.Fatalf("optimizeLoadFast: %v", err)
	}
	if got := g.EntryBlock.Instr[0].Op; got != LOAD_FAST_BORROW {
		t.Fatalf("instr[0].Op = %s, want LOAD_FAST_BORROW", got.Name())
	}
}

func TestOptimizeLoadFastKeepsWhenStoredAsLocal(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(LOAD_FAST, 0, loc)
	g.addOp(STORE_FAST, 1, loc)
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	if err := optimizeLoadFast(g); err != nil {
		t.Fatalf("optimizeLoadFast: %v", err)
	}
	if got := g.EntryBlock.Instr[0].Op; got != LOAD_FAST {
		t.Fatalf("instr[0].Op = %s, want LOAD_FAST (storedAsLocal blocks borrow)", got.Name())
	}
}

func TestRefStackPushPopSwap(t *testing.T) {
	s := &refStack{}
	s.push(ref{Instr: 1, Local: 10})
	s.push(ref{Instr: 2, Local: 20})
	s.push(ref{Instr: 3, Local: 30})
	if s.size() != 3 {
		t.Fatalf("size = %d, want 3", s.size())
	}
	s.swapTop(3)
	if s.at(0).Instr != 3 || s.at(2).Instr != 1 {
		t.Fatalf("swap_top(3) wrong: %+v", s.Refs)
	}
	r := s.pop()
	if r.Instr != 1 {
		t.Fatalf("pop = %+v, want instr=1", r)
	}
	s.clear()
	if s.size() != 0 {
		t.Fatalf("clear left size=%d", s.size())
	}
}

func TestKillLocalMarksMatchingRefs(t *testing.T) {
	flags := make([]uint8, 4)
	s := &refStack{}
	s.push(ref{Instr: 0, Local: 5})
	s.push(ref{Instr: 1, Local: 6})
	s.push(ref{Instr: 2, Local: 5})
	killLocal(flags, s, 5)
	if flags[0] != supportKilled || flags[2] != supportKilled {
		t.Fatalf("flags = %v, want [SK, 0, SK, 0]", flags)
	}
	if flags[1] != 0 {
		t.Fatalf("flag[1] = %d, want 0", flags[1])
	}
}

func TestStoreLocalSetsStoredAndKills(t *testing.T) {
	flags := make([]uint8, 4)
	s := &refStack{}
	s.push(ref{Instr: 0, Local: 7})
	storeLocal(flags, s, 7, ref{Instr: 1, Local: notLocal})
	if flags[0] != supportKilled {
		t.Fatalf("flags[0] = %d, want supportKilled", flags[0])
	}
	if flags[1] != storedAsLocal {
		t.Fatalf("flags[1] = %d, want storedAsLocal", flags[1])
	}
}

func TestMaybePushExtendsMaskOnce(t *testing.T) {
	b := &basicblock{}
	stack := []*basicblock{}
	stack = maybePush(b, 0b101, stack)
	if len(stack) != 1 || b.UnsafeLocalsMask != 0b101 || !b.Visited {
		t.Fatalf("first push wrong: stack=%d mask=%b visited=%v", len(stack), b.UnsafeLocalsMask, b.Visited)
	}
	stack = maybePush(b, 0b001, stack)
	if len(stack) != 1 {
		t.Fatalf("subset push grew stack to %d", len(stack))
	}
	_ = maybePush(b, 0b010, stack)
	if b.UnsafeLocalsMask != 0b111 {
		t.Fatalf("mask after union = %b, want 111", b.UnsafeLocalsMask)
	}
}

func TestScanBlockForLocalsUpgradesLoadFastToCheck(t *testing.T) {
	var loc ast.Pos
	b := &basicblock{UnsafeLocalsMask: 0b1}
	b.addOp(LOAD_FAST, 0, loc)
	stack := scanBlockForLocals(b, nil)
	if b.Instr[0].Op != LOAD_FAST_CHECK {
		t.Fatalf("LOAD_FAST not upgraded, op=%s", b.Instr[0].Op.Name())
	}
	_ = stack
}

func TestAddChecksForLoadsFlipsEntryUninitLoad(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(LOAD_FAST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	if err := addChecksForLoadsOfUninitializedVariables(g.EntryBlock, 2, 0); err != nil {
		t.Fatalf("addChecks: %v", err)
	}
	if g.EntryBlock.Instr[0].Op != LOAD_FAST_CHECK {
		t.Fatalf("LOAD_FAST not flipped to LOAD_FAST_CHECK")
	}
}

func TestAddChecksForLoadsLeavesParamLoad(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(LOAD_FAST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	// nparams = 1 means local 0 is a parameter and starts initialized.
	if err := addChecksForLoadsOfUninitializedVariables(g.EntryBlock, 1, 1); err != nil {
		t.Fatalf("addChecks: %v", err)
	}
	if g.EntryBlock.Instr[0].Op != LOAD_FAST {
		t.Fatalf("param LOAD_FAST should not become LOAD_FAST_CHECK")
	}
}

func TestFastScanManyLocalsRequiresOver64(t *testing.T) {
	if err := fastScanManyLocals(nil, 64); err == nil {
		t.Fatal("expected error for nlocals<=64")
	}
}

func TestFastScanManyLocalsFlipsCrossBlockLoad(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	// Block 1: STORE_FAST local 70.
	g.addOp(STORE_FAST, 70, loc)
	tgt := g.newBlock()
	g.useNextBlock(tgt)
	// Block 2: LOAD_FAST 70 (different block, so flips to CHECK).
	g.addOp(LOAD_FAST, 70, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	if err := fastScanManyLocals(g.EntryBlock, 80); err != nil {
		t.Fatalf("fastScanManyLocals: %v", err)
	}
	if tgt.Instr[0].Op != LOAD_FAST_CHECK {
		t.Fatalf("expected cross-block LOAD_FAST -> LOAD_FAST_CHECK, got %s", tgt.Instr[0].Op.Name())
	}
}
