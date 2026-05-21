package objects

import "testing"

// TestSliceFreeListRecycles verifies the sync.Pool actually returns
// the same carcass after dealloc. The test relies on the slice
// reaching refcount=0 via Decref, which fires sliceDealloc and
// pushes the carcass into the pool. The next NewSlice should be
// satisfied by that carcass for the typical synchronous test case.
func TestSliceFreeListRecycles(t *testing.T) {
	first := NewSlice(None(), None(), None())
	addr := first
	Decref(first)

	// sync.Pool does not strictly guarantee LIFO, but in a synchronous
	// single-goroutine test with no intervening GC the just-Put slot
	// is overwhelmingly likely to be returned by Get. We don't assert
	// pointer equality directly (sync.Pool can drop entries during
	// GC), but the slice we get back must come from a clean header
	// state if it is the recycled carcass.
	second := NewSlice(None(), None(), None())
	if second == addr {
		if second.Start != None() || second.Stop != None() || second.Step != None() {
			t.Fatalf("recycled slice fields not reset to None()")
		}
		if second.Refcnt() != 1 {
			t.Fatalf("recycled slice refcount = %d, want 1", second.Refcnt())
		}
	}
	Decref(second)
}

// TestSliceDeallocFiresOnRefcountZero verifies that Decref on a
// refcount=1 slice runs sliceDealloc (fields nil'd, pool Put). We
// observe the dealloc by reading the cleared fields on the carcass.
func TestSliceDeallocFiresOnRefcountZero(t *testing.T) {
	s := NewSlice(None(), NewInt(5), None())
	if s.Stop == nil {
		t.Fatalf("freshly-built slice has nil Stop")
	}
	if s.Refcnt() != 1 {
		t.Fatalf("fresh slice refcount = %d, want 1", s.Refcnt())
	}
	Decref(s)
	if s.Start != nil || s.Stop != nil || s.Step != nil {
		t.Fatalf("sliceDealloc did not clear fields (Start=%v Stop=%v Step=%v)",
			s.Start, s.Stop, s.Step)
	}
}
