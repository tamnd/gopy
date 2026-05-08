// Tests for WeakSet. Pin the surface that maps onto Lib/_weakrefset.py:
// add/remove/discard/contains, iteration filtering dead refs, weak
// semantics under gc.Collect, and the _remove callback wiring.

package weakref

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/module/gc"
	"github.com/tamnd/gopy/objects"
)

// makeList returns a fresh list seeded with v so that two list values
// compare unequal under Python equality. Empty lists would dedup.
func makeList(v int) *objects.List {
	l := objects.NewList(nil)
	l.Append(objects.NewInt(int64(v)))
	return l
}

func TestWeakSetAddContainsLen(t *testing.T) {
	s, err := NewWeakSet(nil)
	if err != nil {
		t.Fatalf("NewWeakSet: %v", err)
	}
	a := makeList(1)
	b := makeList(2)
	if err := s.Add(a); err != nil {
		t.Fatalf("Add a: %v", err)
	}
	if err := s.Add(b); err != nil {
		t.Fatalf("Add b: %v", err)
	}
	if got := s.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	if ok, err := s.Contains(a); err != nil || !ok {
		t.Fatalf("Contains(a) = %v, %v", ok, err)
	}
	if ok, err := s.Contains(b); err != nil || !ok {
		t.Fatalf("Contains(b) = %v, %v", ok, err)
	}
}

func TestWeakSetAddDedupes(t *testing.T) {
	s, _ := NewWeakSet(nil)
	a := makeList(1)
	for i := 0; i < 3; i++ {
		if err := s.Add(a); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if got := s.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1 after dedup", got)
	}
}

func TestWeakSetDiscardAndRemove(t *testing.T) {
	s, _ := NewWeakSet(nil)
	a := makeList(1)
	b := makeList(2)
	_ = s.Add(a)
	_ = s.Add(b)

	if err := s.Discard(a); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if ok, _ := s.Contains(a); ok {
		t.Fatalf("Contains(a) after Discard, want false")
	}
	if err := s.Discard(a); err != nil {
		t.Fatalf("Discard missing should be no-op, got %v", err)
	}
	if err := s.Remove(a); err == nil {
		t.Fatalf("Remove(missing) should error")
	}
	if err := s.Remove(b); err != nil {
		t.Fatalf("Remove(b): %v", err)
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}

func TestWeakSetIteration(t *testing.T) {
	s, _ := NewWeakSet(nil)
	a := makeList(1)
	b := makeList(2)
	_ = s.Add(a)
	_ = s.Add(b)

	it, err := objects.Iter(s)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	seen := map[objects.Object]bool{}
	for {
		v, err := objects.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) || v == nil {
			break
		}
		if err != nil {
			t.Fatalf("IterNext: %v", err)
		}
		seen[v] = true
	}
	if !seen[a] || !seen[b] {
		t.Fatalf("Iter saw %v, want a and b", seen)
	}
}

func TestWeakSetClearAndCopy(t *testing.T) {
	s, _ := NewWeakSet(nil)
	a := makeList(1)
	_ = s.Add(a)

	cp, err := s.Copy()
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if ok, _ := cp.Contains(a); !ok {
		t.Fatalf("copy missing a")
	}

	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("Len after Clear = %d", s.Len())
	}
	if ok, _ := cp.Contains(a); !ok {
		t.Fatalf("Clear on original affected copy")
	}
}

func TestWeakSetUpdate(t *testing.T) {
	s, _ := NewWeakSet(nil)
	src := objects.NewList(nil)
	a := makeList(1)
	b := makeList(2)
	src.Append(a)
	src.Append(b)

	if err := s.Update(src); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
}

func TestWeakSetPopReturnsLiveItem(t *testing.T) {
	s, _ := NewWeakSet(nil)
	a := makeList(1)
	_ = s.Add(a)

	got, err := s.Pop()
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got != a {
		t.Fatalf("Pop = %v, want a", got)
	}
	if _, err := s.Pop(); err == nil {
		t.Fatalf("Pop on empty should error")
	}
}

// TestWeakSetDropsDeadReferentViaCallback wires the WeakSet through
// the gc weakref-callback path: track the referent, collect, observe
// the entry vanish. This is the spec-critical scenario — every other
// container method depends on the _remove callback firing.
func TestWeakSetDropsDeadReferentViaCallback(t *testing.T) {
	s, _ := NewWeakSet(nil)
	a := objects.NewList(nil)
	a.Append(a) // self-cycle so gc.Collect can reclaim it
	gc.Track(a)
	if err := s.Add(a); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if s.Len() != 1 {
		t.Fatalf("pre-collect Len = %d, want 1", s.Len())
	}

	// Drop the local strong reference. The cycle detector reclaims a
	// (it's only kept alive by the cycle plus s.refs's *Weakref, which
	// doesn't keep referent alive), the weakref's callback fires, and
	// the entry leaves s.refs.
	objects.Decref(a)
	if got := gc.Collect(2); got != 1 {
		t.Fatalf("Collect reclaimed %d, want 1", got)
	}
	if s.Len() != 0 {
		t.Fatalf("post-collect Len = %d, want 0", s.Len())
	}
}
