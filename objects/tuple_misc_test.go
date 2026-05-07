package objects

import (
	"strings"
	"testing"
)

func TestTupleCount(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1), NewInt(2), NewInt(2), NewInt(3), NewInt(2)})
	n, err := tup.Count(NewInt(2))
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Fatalf("Count(2) = %d, want 3", n)
	}
	if n, _ = tup.Count(NewInt(99)); n != 0 {
		t.Fatalf("Count(99) = %d, want 0", n)
	}
}

func TestTupleIndex(t *testing.T) {
	tup := NewTuple([]Object{NewInt(10), NewInt(20), NewInt(30), NewInt(20)})
	i, err := tup.Index(NewInt(20), 0, tup.Len())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if i != 1 {
		t.Fatalf("Index(20) = %d, want 1", i)
	}
	i, err = tup.Index(NewInt(20), 2, tup.Len())
	if err != nil {
		t.Fatalf("Index from 2: %v", err)
	}
	if i != 3 {
		t.Fatalf("Index(20, 2) = %d, want 3", i)
	}
}

func TestTupleIndexNegativeBounds(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1), NewInt(2), NewInt(3), NewInt(4)})
	i, err := tup.Index(NewInt(3), -3, -1)
	if err != nil {
		t.Fatalf("Index with negative bounds: %v", err)
	}
	if i != 2 {
		t.Fatalf("got %d, want 2", i)
	}
}

func TestTupleIndexNotFound(t *testing.T) {
	tup := NewTuple([]Object{NewInt(1), NewInt(2)})
	_, err := tup.Index(NewInt(99), 0, tup.Len())
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("expected ValueError, got %v", err)
	}
}
