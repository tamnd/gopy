package frame

import (
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

func mkCode(nlocals, ncells, nfree, stacksize int) *objects.Code {
	co := &objects.Code{Stacksize: stacksize}
	co.Varnames = make([]string, nlocals)
	co.Cellvars = make([]string, ncells)
	co.Freevars = make([]string, nfree)
	return co
}

func TestSizeFor(t *testing.T) {
	co := mkCode(3, 1, 2, 5)
	if got, want := SizeFor(co), 3+1+2+5; got != want {
		t.Errorf("SizeFor = %d, want %d", got, want)
	}
	if NLocalsOf(co) != 3 || NCellsOf(co) != 1 || NFreeOf(co) != 2 {
		t.Errorf("count helpers off: %d/%d/%d", NLocalsOf(co), NCellsOf(co), NFreeOf(co))
	}
	if NLocalsPlusOf(co) != 6 {
		t.Errorf("NLocalsPlusOf = %d, want 6", NLocalsPlusOf(co))
	}
	if CellsStart(co) != 3 || FreesStart(co) != 4 || StackStart(co) != 6 {
		t.Errorf("layout offsets off: cells=%d frees=%d stack=%d",
			CellsStart(co), FreesStart(co), StackStart(co))
	}
}

func TestFrameInit(t *testing.T) {
	co := mkCode(2, 0, 0, 4)
	var f Frame
	f.Init(co, nil, nil, nil, nil)
	if f.Code != co || f.InstrPtr != 0 || f.StackTop != 0 {
		t.Errorf("Init left bad state: %+v", f)
	}
	if len(f.LocalsPlus) != SizeFor(co) {
		t.Errorf("LocalsPlus len = %d, want %d", len(f.LocalsPlus), SizeFor(co))
	}
	for i, r := range f.LocalsPlus {
		if !r.IsNull() {
			t.Errorf("slot %d not null after Init", i)
		}
	}
}

func TestFrameStackOps(t *testing.T) {
	co := mkCode(0, 0, 0, 4)
	var f Frame
	f.Init(co, nil, nil, nil, nil)

	r1 := stackref.FromObject(objects.NewInt(1))
	r2 := stackref.FromObject(objects.NewInt(2))
	r3 := stackref.FromObject(objects.NewInt(3))

	f.PushStack(r1)
	f.PushStack(r2)
	f.PushStack(r3)
	if f.StackTop != 3 {
		t.Errorf("StackTop = %d, want 3", f.StackTop)
	}
	if f.PeekStack(0).AsObject() != r3.AsObject() {
		t.Error("PeekStack(0) wrong")
	}
	if f.PeekStack(2).AsObject() != r1.AsObject() {
		t.Error("PeekStack(2) wrong")
	}

	if f.PopStack().AsObject() != r3.AsObject() {
		t.Error("Pop 1 wrong")
	}
	if f.PopStack().AsObject() != r2.AsObject() {
		t.Error("Pop 2 wrong")
	}
	if f.StackTop != 1 {
		t.Errorf("StackTop after pops = %d, want 1", f.StackTop)
	}
}

func TestFrameLocalAccess(t *testing.T) {
	co := mkCode(3, 0, 0, 0)
	var f Frame
	f.Init(co, nil, nil, nil, nil)

	r := stackref.FromObject(objects.NewInt(42))
	f.SetLocal(1, r)
	if f.LocalAt(1).AsObject() != r.AsObject() {
		t.Error("SetLocal/LocalAt round-trip failed")
	}
	if !f.LocalAt(0).IsNull() {
		t.Error("untouched local should be null")
	}
}

func TestFrameStackPushPop(t *testing.T) {
	s := New()
	co := mkCode(1, 0, 0, 2)

	f1 := s.Push(co, nil, nil, nil, nil)
	if s.Depth() != 1 {
		t.Errorf("Depth after 1 push = %d", s.Depth())
	}
	f2 := s.Push(co, nil, nil, nil, f1)
	if f2.Previous != f1 {
		t.Error("frame chain broken")
	}
	if s.Depth() != 2 {
		t.Errorf("Depth after 2 push = %d", s.Depth())
	}
	s.Pop()
	s.Pop()
	if s.Depth() != 0 {
		t.Errorf("Depth after pops = %d", s.Depth())
	}
	s.Pop() // empty pop is a no-op
}

func TestFrameStackChunkGrowth(t *testing.T) {
	s := New()
	co := mkCode(0, 0, 0, 0)
	var prev *Frame
	for i := 0; i < ChunkSize+5; i++ {
		prev = s.Push(co, nil, nil, nil, prev)
	}
	if s.Depth() != ChunkSize+5 {
		t.Errorf("Depth = %d", s.Depth())
	}
	// Walk back via Previous; should hit ChunkSize+5 frames.
	count := 0
	for f := prev; f != nil; f = f.Previous {
		count++
	}
	if count != ChunkSize+5 {
		t.Errorf("chain walk count = %d, want %d", count, ChunkSize+5)
	}
}

func TestOwnerKindValues(t *testing.T) {
	if OwnedByEval != 0 {
		t.Error("OwnedByEval must be 0 (zero value)")
	}
	if OwnedByGenerator == OwnedByEval {
		t.Error("OwnerKind values must be distinct")
	}
}
