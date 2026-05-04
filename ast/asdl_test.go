package ast

import "testing"

func TestSeqEmpty(t *testing.T) {
	s := NewSeq[int](0)
	if s == nil {
		t.Fatal("zero-length seq should be non-nil")
	}
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", s.Len())
	}
}

func TestSeqGetSet(t *testing.T) {
	s := NewSeq[int](3)
	s.Set(0, 10)
	s.Set(1, 20)
	s.Set(2, 30)
	if s.Get(0)+s.Get(1)+s.Get(2) != 60 {
		t.Fatal("Get/Set round-trip failed")
	}
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}
}

func TestSeqNilLen(t *testing.T) {
	var s Seq[string]
	if s.Len() != 0 {
		t.Fatalf("nil Len() = %d, want 0", s.Len())
	}
}
