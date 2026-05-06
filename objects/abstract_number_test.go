package objects

import (
	"errors"
	"testing"
)

func intVal(o Object, t *testing.T) int64 {
	t.Helper()
	iv, ok := o.(*Int)
	if !ok {
		t.Fatalf("expected *Int, got %T", o)
	}
	v, _ := iv.Int64()
	return v
}

// TestNumberAddIntInt verifies the LHS forward dispatch reaches Int.Add.
func TestNumberAddIntInt(t *testing.T) {
	got, err := NumberAdd(NewInt(2), NewInt(3))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 5 {
		t.Errorf("2 + 3 = %d, want 5", intVal(got, t))
	}
}

// TestNumberAddSequenceFallback pins the sq_concat fallback path:
// when the number protocol returns NotImplemented on both sides,
// PyNumber_Add reaches into the LHS sequence's Concat slot.
func TestNumberAddSequenceFallback(t *testing.T) {
	tp := NewType("concatter", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Concat: func(a, b Object) (Object, error) {
			return NewInt(1234), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberAdd(holder, NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 1234 {
		t.Errorf("sequence-concat path returned %d, want 1234", intVal(got, t))
	}
}

// TestNumberMultiplySequenceRepeat covers a * n routing through
// sq_repeat when the number protocol declines.
func TestNumberMultiplySequenceRepeat(t *testing.T) {
	captured := 0
	tp := NewType("repeater", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Repeat: func(o Object, n int) (Object, error) {
			captured = n
			return NewInt(int64(n * 10)), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberMultiply(holder, NewInt(3))
	if err != nil {
		t.Fatal(err)
	}
	if captured != 3 {
		t.Errorf("sq_repeat got n=%d, want 3", captured)
	}
	if intVal(got, t) != 30 {
		t.Errorf("got %d, want 30", intVal(got, t))
	}
}

func TestNumberBitwise(t *testing.T) {
	cases := []struct {
		name string
		fn   func(a, b Object) (Object, error)
		a, b int64
		want int64
	}{
		{"and", NumberAnd, 0xff, 0x0f, 0x0f},
		{"or", NumberOr, 0xf0, 0x0f, 0xff},
		{"xor", NumberXor, 0xff, 0x0f, 0xf0},
		{"lshift", NumberLshift, 1, 4, 16},
		{"rshift", NumberRshift, 32, 2, 8},
	}
	for _, c := range cases {
		got, err := c.fn(NewInt(c.a), NewInt(c.b))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if intVal(got, t) != c.want {
			t.Errorf("%s: got %d, want %d", c.name, intVal(got, t), c.want)
		}
	}
}

func TestNumberMatrixMultiplyTypeError(t *testing.T) {
	_, err := NumberMatrixMultiply(NewInt(1), NewInt(2))
	if err == nil {
		t.Error("int @ int should TypeError (no nb_matrix_multiply)")
	}
}

// TestNumberMatrixMultiplyDispatches plumbs a custom type with a
// MatrixMultiply slot to verify the dispatch reaches it.
func TestNumberMatrixMultiplyDispatches(t *testing.T) {
	tp := NewType("matmuller", []*Type{objectType})
	tp.Number = &NumberMethods{
		MatrixMultiply: func(a, b Object) (Object, error) {
			return NewInt(99), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberMatrixMultiply(holder, NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 99 {
		t.Errorf("got %d, want 99", intVal(got, t))
	}
}

// TestNumberInPlaceFallsBack: int has no InPlaceAdd slot, so a += b
// must fall back to NumberAdd and produce a fresh int.
func TestNumberInPlaceFallsBack(t *testing.T) {
	got, err := NumberInPlaceAdd(NewInt(10), NewInt(5))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 15 {
		t.Errorf("10 += 5 fell back to %d, want 15", intVal(got, t))
	}
}

// TestNumberInPlacePrefersSlot wires an InPlaceAdd slot and verifies
// it wins over the regular Add slot.
func TestNumberInPlacePrefersSlot(t *testing.T) {
	tp := NewType("inplacer", []*Type{objectType})
	tp.Number = &NumberMethods{
		Add: func(a, b Object) (Object, error) { return NewInt(1), nil },
		InPlaceAdd: func(a, b Object) (Object, error) {
			return NewInt(2), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberInPlaceAdd(holder, NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 2 {
		t.Errorf("InPlaceAdd preferred wrong path: got %d", intVal(got, t))
	}
}

// TestNumberInPlaceSlotNotImplementedFallsBack verifies that an
// in-place slot returning NotImplemented falls through to the
// non-in-place op.
func TestNumberInPlaceSlotNotImplementedFallsBack(t *testing.T) {
	tp := NewType("inplace_ni", []*Type{objectType})
	tp.Number = &NumberMethods{
		Add: func(a, b Object) (Object, error) { return NewInt(7), nil },
		InPlaceAdd: func(a, b Object) (Object, error) {
			return NotImplemented(), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberInPlaceAdd(holder, NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 7 {
		t.Errorf("got %d, want 7 from fallback Add", intVal(got, t))
	}
}

// TestNumberInPlaceMultiplySequenceRepeat: a *= 2 reaches sq_repeat
// (or sq_inplace_repeat once that's plumbed) when the number protocol
// declines.
func TestNumberInPlaceMultiplySequenceRepeat(t *testing.T) {
	tp := NewType("inplace_repeater", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Repeat: func(o Object, n int) (Object, error) {
			return NewInt(int64(n * 100)), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberInPlaceMultiply(holder, NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 200 {
		t.Errorf("got %d, want 200", intVal(got, t))
	}
}

func TestNumberPower(t *testing.T) {
	got, err := NumberPower(NewInt(2), NewInt(10), nil)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 1024 {
		t.Errorf("2 ** 10 = %d, want 1024", intVal(got, t))
	}
}

func TestNumberInPlacePowerFallsBack(t *testing.T) {
	got, err := NumberInPlacePower(NewInt(3), NewInt(3), nil)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 27 {
		t.Errorf("3 **= 3 = %d, want 27", intVal(got, t))
	}
}

func TestNumberUnary(t *testing.T) {
	neg, err := NumberNegative(NewInt(5))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(neg, t) != -5 {
		t.Errorf("-5 = %d, want -5", intVal(neg, t))
	}
	abs, err := NumberAbsolute(NewInt(-7))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(abs, t) != 7 {
		t.Errorf("abs(-7) = %d", intVal(abs, t))
	}
}

func TestNumberInvert(t *testing.T) {
	got, err := NumberInvert(NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != -1 {
		t.Errorf("~0 = %d, want -1", intVal(got, t))
	}
}

// TestNumberUnaryUnsupported pins the TypeError for a type that
// doesn't expose the unary slot.
func TestNumberUnaryUnsupported(t *testing.T) {
	if _, err := NumberInvert(NewStr("x")); err == nil {
		t.Error("~'x' should TypeError")
	}
}

func TestNumberIndexInt(t *testing.T) {
	got, err := NumberIndex(NewInt(42))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 42 {
		t.Errorf("got %d, want 42", intVal(got, t))
	}
}

func TestNumberIndexBool(t *testing.T) {
	got, err := NumberIndex(True())
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 1 {
		t.Error("True __index__ should be 1")
	}
	got, err = NumberIndex(False())
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 0 {
		t.Error("False __index__ should be 0")
	}
}

func TestNumberIndexCustom(t *testing.T) {
	tp := NewType("indexer", []*Type{objectType})
	tp.Number = &NumberMethods{
		Index: func(o Object) (Object, error) {
			return NewInt(13), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	got, err := NumberIndex(holder)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 13 {
		t.Errorf("__index__ = %d, want 13", intVal(got, t))
	}
}

func TestNumberIndexRejectsNonInt(t *testing.T) {
	tp := NewType("badindexer", []*Type{objectType})
	tp.Number = &NumberMethods{
		Index: func(o Object) (Object, error) {
			return NewStr("nope"), nil
		},
	}
	holder := &Header{}
	holder.init(tp)
	_, err := NumberIndex(holder)
	if err == nil {
		t.Error("non-int __index__ result should TypeError")
	}
}

func TestNumberIndexUnsupported(t *testing.T) {
	if _, err := NumberIndex(NewStr("x")); err == nil {
		t.Error("str cannot __index__")
	}
}

func TestIndexCheck(t *testing.T) {
	if !IndexCheck(NewInt(0)) {
		t.Error("int should be index-able")
	}
	if !IndexCheck(True()) {
		t.Error("bool should be index-able")
	}
	if IndexCheck(NewStr("x")) {
		t.Error("str should not be index-able")
	}
}

// TestNumberBinaryUnsupported pins the TypeError text path: number
// op against a type with no number slot raises with the canonical
// CPython message.
func TestNumberBinaryUnsupported(t *testing.T) {
	if _, err := NumberSubtract(NewStr("a"), NewStr("b")); err == nil {
		t.Error("str - str should TypeError")
	}
}

// TestNumberAddPropagatesError verifies that a slot returning a real
// error short-circuits without falling through to the RHS.
func TestNumberAddPropagatesError(t *testing.T) {
	want := errors.New("boom")
	tp := NewType("erradder", []*Type{objectType})
	tp.Number = &NumberMethods{
		Add: func(a, b Object) (Object, error) { return nil, want },
	}
	holder := &Header{}
	holder.init(tp)
	if _, err := NumberAdd(holder, NewInt(0)); !errors.Is(err, want) {
		t.Errorf("got err %v, want %v", err, want)
	}
}
