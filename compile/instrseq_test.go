package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

const (
	opNop       Opcode = 9
	opLoadConst Opcode = 83
	opJumpAbs   Opcode = 113
	opJumpFwd   Opcode = 110
)

// fakeHasTarget treats opJumpAbs and opJumpFwd as jump opcodes.
func fakeHasTarget(op Opcode) bool {
	return op == opJumpAbs || op == opJumpFwd
}

func TestNewLabelIDs(t *testing.T) {
	var s Sequence
	a := s.NewLabel()
	b := s.NewLabel()
	if a.ID() != 1 || b.ID() != 2 {
		t.Fatalf("labels = %d, %d, want 1, 2", a.ID(), b.ID())
	}
}

func TestAddopAndUseLabel(t *testing.T) {
	var s Sequence
	pos := ast.Pos{Lineno: 1}
	s.Addop(opNop, 0, pos)
	lbl := s.NewLabel()
	s.UseLabel(lbl)
	s.Addop(opLoadConst, 7, pos)
	s.Addop(opJumpAbs, int32(lbl.ID()), pos)
	if len(s.Instrs) != 3 {
		t.Fatalf("len = %d, want 3", len(s.Instrs))
	}
	s.ApplyLabelMap(fakeHasTarget)
	if got := s.Instrs[2].Oparg; got != 1 {
		t.Fatalf("jump target = %d, want 1", got)
	}
	// Idempotent.
	s.ApplyLabelMap(fakeHasTarget)
	if got := s.Instrs[2].Oparg; got != 1 {
		t.Fatalf("after second ApplyLabelMap target = %d, want 1", got)
	}
}

func TestApplyLabelMapPreservesNonJumps(t *testing.T) {
	var s Sequence
	pos := ast.Pos{Lineno: 1}
	lbl := s.NewLabel()
	s.UseLabel(lbl)
	s.Addop(opLoadConst, 42, pos)
	s.ApplyLabelMap(fakeHasTarget)
	if got := s.Instrs[0].Oparg; got != 42 {
		t.Fatalf("LOAD_CONST oparg = %d, want 42 (untouched)", got)
	}
}

func TestInsertShiftsLabels(t *testing.T) {
	var s Sequence
	pos := ast.Pos{Lineno: 1}
	s.Addop(opNop, 0, pos)
	lbl := s.NewLabel()
	s.UseLabel(lbl) // bound to index 1
	s.Addop(opLoadConst, 5, pos)
	// Insert a NOP at index 1; the label at index 1 should bump to 2.
	s.Insert(1, opNop, 0, pos)
	if got := s.labelmap[lbl.ID()]; got != 2 {
		t.Fatalf("label index = %d, want 2", got)
	}
	if len(s.Instrs) != 3 {
		t.Fatalf("len = %d, want 3", len(s.Instrs))
	}
}

func TestAddNested(t *testing.T) {
	var s Sequence
	child := &Sequence{}
	s.AddNested(child)
	if len(s.Nested) != 1 || s.Nested[0] != child {
		t.Fatal("AddNested did not attach child")
	}
}

func TestExceptHandlerLabelResolved(t *testing.T) {
	var s Sequence
	pos := ast.Pos{Lineno: 1}
	lbl := s.NewLabel()
	s.UseLabel(lbl) // bound to index 0
	s.Addop(opNop, 0, pos)
	s.Instrs[0].Handler.Label = lbl.ID()
	s.ApplyLabelMap(fakeHasTarget)
	if got := s.Instrs[0].Handler.Label; got != 0 {
		t.Fatalf("Handler.Label = %d, want 0", got)
	}
}

func TestSetAnnotationsCodePanicsOnDouble(t *testing.T) {
	var s Sequence
	s.SetAnnotationsCode(&Sequence{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on second SetAnnotationsCode")
		}
	}()
	s.SetAnnotationsCode(&Sequence{})
}

func TestAddopRejectsOutOfRangeOpcode(t *testing.T) {
	var s Sequence
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on opcode > MaxOpcode")
		}
	}()
	s.Addop(MaxOpcode+1, 0, ast.NoPos)
}

func TestAddopRejectsOutOfRangeOparg(t *testing.T) {
	var s Sequence
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on oparg >= MaxOparg")
		}
	}()
	s.Addop(opNop, MaxOparg, ast.NoPos)
}
