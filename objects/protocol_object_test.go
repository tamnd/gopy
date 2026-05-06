package objects

import (
	"errors"
	"testing"
)

func TestLengthSequenceAndMapping(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1), NewInt(2), NewInt(3)})
	n, err := Length(tup)
	if err != nil || n != 3 {
		t.Fatalf("Length(tuple)=%d,%v; want 3,nil", n, err)
	}

	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	n, err = Length(d)
	if err != nil || n != 2 {
		t.Fatalf("Length(dict)=%d,%v; want 2,nil", n, err)
	}
}

func TestLengthNilAndUnsupported(t *testing.T) {
	if _, err := Length(nil); err == nil {
		t.Fatalf("Length(nil) should error")
	}
	if _, err := Length(NewInt(7)); err == nil {
		t.Fatalf("Length(int) should error")
	}
}

func TestGetItemSequence(t *testing.T) {
	l := NewList([]Object{NewInt(10), NewInt(20), NewInt(30)})
	v, err := GetItem(l, NewInt(1))
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	got, _ := v.(*Int).Int64()
	if got != 20 {
		t.Fatalf("got %d, want 20", got)
	}
}

func TestSetItemAndDelItemSequence(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2), NewInt(3)})
	if err := SetItem(l, NewInt(0), NewInt(99)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	v, _ := GetItem(l, NewInt(0))
	got, _ := v.(*Int).Int64()
	if got != 99 {
		t.Fatalf("after SetItem got %d, want 99", got)
	}
}

func TestSetItemMappingDelete(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(5))
	if err := DelItem(d, NewStr("k")); err != nil {
		t.Fatalf("DelItem: %v", err)
	}
	if n, _ := Length(d); n != 0 {
		t.Fatalf("dict not empty after DelItem: %d", n)
	}
}

func TestContainsSequence(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1), NewInt(2), NewInt(3)})
	ok, err := Contains(tup, NewInt(2))
	if err != nil || !ok {
		t.Fatalf("Contains 2: ok=%v err=%v", ok, err)
	}
	ok, err = Contains(tup, NewInt(99))
	if err != nil || ok {
		t.Fatalf("Contains 99: ok=%v err=%v", ok, err)
	}
}

func TestIterAndIterNext(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2)})
	it, err := Iter(l)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	v, err := IterNext(it)
	if err != nil {
		t.Fatalf("IterNext #1: %v", err)
	}
	if got, _ := v.(*Int).Int64(); got != 1 {
		t.Fatalf("first=%d, want 1", got)
	}
	v, err = IterNext(it)
	if err != nil {
		t.Fatalf("IterNext #2: %v", err)
	}
	if got, _ := v.(*Int).Int64(); got != 2 {
		t.Fatalf("second=%d, want 2", got)
	}
	if _, err := IterNext(it); !errors.Is(err, ErrStopIteration) {
		t.Fatalf("expected StopIteration, got %v", err)
	}
}

func TestIterNotIterable(t *testing.T) {
	if _, err := Iter(NewInt(1)); err == nil {
		t.Fatalf("Iter(int) should error")
	}
}

func TestTypeOf(t *testing.T) {
	if TypeOf(NewInt(1)) != IntType {
		t.Fatalf("TypeOf(Int) wrong")
	}
}

func TestIdentityHelpers(t *testing.T) {
	if !IsTrue(True()) {
		t.Fatalf("IsTrue(True)=false")
	}
	if !IsFalse(False()) {
		t.Fatalf("IsFalse(False)=false")
	}
	if !IsBool(True()) || !IsBool(False()) {
		t.Fatalf("IsBool failed")
	}
	if IsBool(NewInt(1)) {
		t.Fatalf("IsBool(int) should be false")
	}
}
