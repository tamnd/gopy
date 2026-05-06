package objects

import (
	"testing"
)

// TestSetAddContains pins basic insert and lookup.
func TestSetAddContains(t *testing.T) {
	s := NewSet()
	if err := s.Add(NewInt(1)); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Contains(NewInt(1))
	if err != nil || !ok {
		t.Errorf("Contains(1) = %v %v, want true nil", ok, err)
	}
	ok, err = s.Contains(NewInt(2))
	if err != nil || ok {
		t.Errorf("Contains(2) = %v %v, want false nil", ok, err)
	}
}

// TestSetLen pins the length counter.
func TestSetLen(t *testing.T) {
	s := NewSet()
	for i := range 5 {
		_ = s.Add(NewInt(int64(i)))
	}
	if s.Len() != 5 {
		t.Errorf("Len = %d, want 5", s.Len())
	}
}

// TestSetDeduplicate pins that adding the same key twice keeps Len at 1.
func TestSetDeduplicate(t *testing.T) {
	s := NewSet()
	_ = s.Add(NewStr("x"))
	_ = s.Add(NewStr("x"))
	if s.Len() != 1 {
		t.Errorf("Len after dup = %d, want 1", s.Len())
	}
}

// TestSetDiscard pins removal.
func TestSetDiscard(t *testing.T) {
	s := NewSet()
	_ = s.Add(NewInt(7))
	if err := s.Discard(NewInt(7)); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 0 {
		t.Errorf("Len after discard = %d, want 0", s.Len())
	}
}

// TestSetDiscardMiss pins that discard on a missing key is a no-op.
func TestSetDiscardMiss(t *testing.T) {
	s := NewSet()
	if err := s.Discard(NewInt(99)); err != nil {
		t.Errorf("discard miss: %v", err)
	}
}

// TestFrozensetImmutable pins that Add returns an error on a frozenset.
func TestFrozensetImmutable(t *testing.T) {
	fs, err := NewFrozenset([]Object{NewInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Add(NewInt(2)); err == nil {
		t.Fatal("expected error adding to frozenset")
	}
}

// TestFrozensetContains pins lookup on a frozenset.
func TestFrozensetContains(t *testing.T) {
	fs, err := NewFrozenset([]Object{NewInt(10), NewInt(20)})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := fs.Contains(NewInt(10))
	if err != nil || !ok {
		t.Errorf("Contains(10) = %v %v, want true nil", ok, err)
	}
	ok, err = fs.Contains(NewInt(30))
	if err != nil || ok {
		t.Errorf("Contains(30) = %v %v, want false nil", ok, err)
	}
}

// TestSetEqualByValue pins == between two sets with equal content.
func TestSetEqualByValue(t *testing.T) {
	a := NewSet()
	_ = a.Add(NewInt(1))
	_ = a.Add(NewInt(2))
	b := NewSet()
	_ = b.Add(NewInt(2))
	_ = b.Add(NewInt(1))
	eq, err := RichCmpBool(a, b, CompareEQ)
	if err != nil || !eq {
		t.Errorf("set eq = %v %v, want true nil", eq, err)
	}
}

// TestSetNotEqual pins != when cardinalities differ.
func TestSetNotEqual(t *testing.T) {
	a := NewSet()
	_ = a.Add(NewInt(1))
	b := NewSet()
	_ = b.Add(NewInt(1))
	_ = b.Add(NewInt(2))
	eq, err := RichCmpBool(a, b, CompareEQ)
	if err != nil || eq {
		t.Errorf("set eq (diff len) = %v %v, want false nil", eq, err)
	}
}

// TestSetReprEmpty pins the set() repr for an empty set.
func TestSetReprEmpty(t *testing.T) {
	s := NewSet()
	r, err := setRepr(s)
	if err != nil || r != "set()" {
		t.Errorf("repr = %q err=%v, want set()", r, err)
	}
}

// TestFrozensetReprEmpty pins the frozenset() repr for an empty frozenset.
func TestFrozensetReprEmpty(t *testing.T) {
	fs, _ := NewFrozenset(nil)
	r, err := frozensetRepr(fs)
	if err != nil || r != "frozenset()" {
		t.Errorf("repr = %q err=%v, want frozenset()", r, err)
	}
}

// TestFrozensetHashStable pins that repeated calls return the same hash.
func TestFrozensetHashStable(t *testing.T) {
	fs, _ := NewFrozenset([]Object{NewInt(1), NewInt(2)})
	h1, err := Hash(fs)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := Hash(fs)
	if h1 != h2 {
		t.Errorf("hash not stable: %d != %d", h1, h2)
	}
}

// TestSetUnhashable pins that a mutable set is not hashable.
func TestSetUnhashable(t *testing.T) {
	s := NewSet()
	if _, err := Hash(s); err == nil {
		t.Error("expected unhashable error for set")
	}
}

// TestSetIterator pins iteration over set members.
func TestSetIterator(t *testing.T) {
	s := NewSet()
	for i := range 3 {
		_ = s.Add(NewInt(int64(i)))
	}
	it, err := setIter(s)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		_, err := it.Type().IterNext(it)
		if err != nil {
			break
		}
		count++
	}
	if count != 3 {
		t.Errorf("iterator yielded %d items, want 3", count)
	}
}
