package objects

import (
	"errors"
	"testing"
)

func TestSequenceCheck(t *testing.T) {
	if !SequenceCheck(NewList(nil)) {
		t.Error("list should pass SequenceCheck")
	}
	if !SequenceCheck(NewTuple(nil)) {
		t.Error("tuple should pass SequenceCheck")
	}
	if SequenceCheck(NewDict()) {
		t.Error("dict should not pass SequenceCheck (excluded explicitly)")
	}
	if SequenceCheck(NewInt(0)) {
		t.Error("int should not pass SequenceCheck")
	}
}

func TestSequenceSize(t *testing.T) {
	got, err := SequenceSize(NewList([]Object{NewInt(1), NewInt(2), NewInt(3)}))
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("len = %d, want 3", got)
	}
}

func TestSequenceSizeRejectsMapping(t *testing.T) {
	_, err := SequenceSize(NewDict())
	if err == nil {
		t.Error("dict should fail SequenceSize")
	}
}

func TestSequenceConcatList(t *testing.T) {
	tp := NewType("seqcat", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Concat: func(a, b Object) (Object, error) {
			return NewInt(42), nil
		},
		GetItem: func(o Object, i int) (Object, error) { return NewInt(0), nil },
	}
	holder := &Header{}
	holder.init(tp)
	got, err := SequenceConcat(holder, holder)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 42 {
		t.Errorf("got %d, want 42", intVal(got, t))
	}
}

func TestSequenceConcatNoSlot(t *testing.T) {
	if _, err := SequenceConcat(NewInt(1), NewInt(2)); err == nil {
		t.Error("int + int should fail SequenceConcat")
	}
}

func TestSequenceRepeat(t *testing.T) {
	captured := 0
	tp := NewType("seqrep", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Repeat: func(o Object, n int) (Object, error) {
			captured = n
			return NewInt(int64(n) * 11), nil
		},
		GetItem: func(o Object, i int) (Object, error) { return NewInt(0), nil },
	}
	holder := &Header{}
	holder.init(tp)
	got, err := SequenceRepeat(holder, 5)
	if err != nil {
		t.Fatal(err)
	}
	if captured != 5 {
		t.Errorf("repeat got n=%d", captured)
	}
	if intVal(got, t) != 55 {
		t.Errorf("got %d, want 55", intVal(got, t))
	}
}

func TestSequenceInPlaceConcatPrefersInPlaceSlot(t *testing.T) {
	tp := NewType("inpcat", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Concat:        func(a, b Object) (Object, error) { return NewInt(1), nil },
		InPlaceConcat: func(a, b Object) (Object, error) { return NewInt(2), nil },
		GetItem:       func(o Object, i int) (Object, error) { return NewInt(0), nil },
	}
	holder := &Header{}
	holder.init(tp)
	got, err := SequenceInPlaceConcat(holder, holder)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 2 {
		t.Errorf("got %d, want 2 from InPlaceConcat", intVal(got, t))
	}
}

func TestSequenceInPlaceConcatFallsBackToConcat(t *testing.T) {
	tp := NewType("fbcat", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Concat:  func(a, b Object) (Object, error) { return NewInt(7), nil },
		GetItem: func(o Object, i int) (Object, error) { return NewInt(0), nil },
	}
	holder := &Header{}
	holder.init(tp)
	got, err := SequenceInPlaceConcat(holder, holder)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 7 {
		t.Errorf("got %d, want 7 from Concat", intVal(got, t))
	}
}

func TestSequenceInPlaceRepeatPrefersInPlaceSlot(t *testing.T) {
	tp := NewType("inprep", []*Type{objectType})
	tp.Sequence = &SequenceMethods{
		Repeat:        func(o Object, n int) (Object, error) { return NewInt(1), nil },
		InPlaceRepeat: func(o Object, n int) (Object, error) { return NewInt(int64(n) * 2), nil },
		GetItem:       func(o Object, i int) (Object, error) { return NewInt(0), nil },
	}
	holder := &Header{}
	holder.init(tp)
	got, err := SequenceInPlaceRepeat(holder, 4)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 8 {
		t.Errorf("got %d, want 8", intVal(got, t))
	}
}

func TestSequenceGetItemNegativeIndex(t *testing.T) {
	l := NewList([]Object{NewInt(10), NewInt(20), NewInt(30)})
	got, err := SequenceGetItem(l, -1)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 30 {
		t.Errorf("got %d, want 30 (last item)", intVal(got, t))
	}
}

func TestSequenceGetItemRejectsMapping(t *testing.T) {
	if _, err := SequenceGetItem(NewDict(), 0); err == nil {
		t.Error("dict[0] via SequenceGetItem should fail")
	}
}

func TestSequenceSetItem(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2), NewInt(3)})
	if err := SequenceSetItem(l, 1, NewInt(42)); err != nil {
		t.Fatal(err)
	}
	if intVal(l.Item(1), t) != 42 {
		t.Errorf("item[1] = %d, want 42", intVal(l.Item(1), t))
	}
}

func TestSequenceCountIndexContains(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2), NewInt(2), NewInt(3)})
	count, err := SequenceCount(l, NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	idx, err := SequenceIndex(l, NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Errorf("index = %d, want 1", idx)
	}
	contained, err := SequenceContains(l, NewInt(3))
	if err != nil {
		t.Fatal(err)
	}
	if !contained {
		t.Error("3 should be contained")
	}
	contained, err = SequenceContains(l, NewInt(99))
	if err != nil {
		t.Fatal(err)
	}
	if contained {
		t.Error("99 should not be contained")
	}
}

func TestSequenceIndexMissing(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2)})
	if _, err := SequenceIndex(l, NewInt(99)); err == nil {
		t.Error("missing element should ValueError")
	}
}

func TestSequenceList(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2)})
	got, err := SequenceList(l)
	if err != nil {
		t.Fatal(err)
	}
	if got == l {
		t.Error("SequenceList should copy lists, not return the same instance")
	}
	if got.Len() != 2 {
		t.Errorf("len = %d, want 2", got.Len())
	}
}

func TestSequenceListFromTuple(t *testing.T) {
	tup := NewTuple([]Object{NewInt(7), NewInt(8), NewInt(9)})
	got, err := SequenceList(tup)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 3 {
		t.Errorf("len = %d, want 3", got.Len())
	}
	if intVal(got.Item(0), t) != 7 {
		t.Errorf("item[0] = %d", intVal(got.Item(0), t))
	}
}

func TestSequenceTupleFromTuplePreserves(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1)})
	got, err := SequenceTuple(tup)
	if err != nil {
		t.Fatal(err)
	}
	if got != tup {
		t.Error("tuple should be returned as-is (immutable)")
	}
}

func TestSequenceTupleFromList(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2)})
	got, err := SequenceTuple(l)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 2 {
		t.Errorf("len = %d", got.Len())
	}
}

func TestSequenceFastListPassthrough(t *testing.T) {
	l := NewList([]Object{NewInt(1)})
	got, err := SequenceFast(l, "expected sequence")
	if err != nil {
		t.Fatal(err)
	}
	if got != l {
		t.Error("list should pass through unchanged")
	}
}

func TestSequenceFastOnIterable(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1), NewInt(2)})
	got, err := SequenceFast(tup, "expected sequence")
	if err != nil {
		t.Fatal(err)
	}
	if got != tup {
		t.Error("tuple should pass through")
	}
}

func TestSequenceCountErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	tp := NewType("itererr", []*Type{objectType})
	tp.IterNext = func(o Object) (Object, error) { return nil, want }
	tp.Iter = func(o Object) (Object, error) { return o, nil }
	holder := &Header{}
	holder.init(tp)
	if _, err := SequenceCount(holder, NewInt(0)); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}
