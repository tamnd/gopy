package gc

import "testing"

// TestUniqueIDPool_AssignSequential confirms the first batch of ids
// come out 1, 2, 3, ... after the pool grows from empty.
func TestUniqueIDPool_AssignSequential(t *testing.T) {
	var p UniqueIDPool
	for want := int64(1); want <= int64(poolMinSize); want++ {
		obj := new(int)
		got := p.AssignUniqueID(obj)
		if got != want {
			t.Errorf("AssignUniqueID got %d, want %d", got, want)
		}
		if p.Lookup(got) != obj {
			t.Errorf("Lookup(%d) did not return the registered object", got)
		}
	}
}

// TestUniqueIDPool_ReleaseReusesLIFO confirms ReleaseUniqueID pushes
// onto a LIFO freelist (matching the upstream behavior where the
// most-recently-released id is the first to be reassigned).
func TestUniqueIDPool_ReleaseReusesLIFO(t *testing.T) {
	var p UniqueIDPool
	a := p.AssignUniqueID("a")
	b := p.AssignUniqueID("b")
	c := p.AssignUniqueID("c")

	p.ReleaseUniqueID(b)
	p.ReleaseUniqueID(a)

	// LIFO: a was released last, so it must come back first.
	if got := p.AssignUniqueID("a2"); got != a {
		t.Errorf("first reassign after free(a, b) got %d, want %d", got, a)
	}
	if got := p.AssignUniqueID("b2"); got != b {
		t.Errorf("second reassign got %d, want %d", got, b)
	}
	// Still untouched
	if p.Lookup(c) != "c" {
		t.Errorf("c slot must still hold 'c' after the round trip")
	}
}

// TestUniqueIDPool_GrowsAcrossPoolMinSize confirms a sequence longer
// than POOL_MIN_SIZE drives at least one resize and keeps assigning
// monotonic ids.
func TestUniqueIDPool_GrowsAcrossPoolMinSize(t *testing.T) {
	var p UniqueIDPool
	const n = poolMinSize*3 + 1
	for i := 1; i <= n; i++ {
		got := p.AssignUniqueID(i)
		if got != int64(i) {
			t.Errorf("AssignUniqueID(#%d) got %d, want %d", i, got, i)
		}
	}
	if p.Size() < n {
		t.Errorf("pool should have grown to at least %d, got Size=%d", n, p.Size())
	}
}

// TestUniqueIDPool_ReleaseRejectsOutOfRange confirms ReleaseUniqueID
// silently ignores invalid ids rather than corrupting the pool.
func TestUniqueIDPool_ReleaseRejectsOutOfRange(t *testing.T) {
	var p UniqueIDPool
	a := p.AssignUniqueID("a")
	p.ReleaseUniqueID(0)                   // sentinel
	p.ReleaseUniqueID(-5)                  // negative
	p.ReleaseUniqueID(int64(p.size) + 100) // past end

	// Pool must still be well-formed: a is reusable.
	p.ReleaseUniqueID(a)
	if got := p.AssignUniqueID("a2"); got != a {
		t.Errorf("after a round of bogus releases the pool lost track of id %d (got %d)", a, got)
	}
}

// TestUniqueIDPool_FinalizeClears confirms Finalize frees the table
// and resets the bookkeeping so Lookup returns nil and Size==0.
func TestUniqueIDPool_FinalizeClears(t *testing.T) {
	var p UniqueIDPool
	id := p.AssignUniqueID("a")
	if p.Lookup(id) != "a" {
		t.Fatalf("setup: Lookup must see the inserted object")
	}
	p.Finalize()
	if p.Size() != 0 {
		t.Errorf("Finalize should zero size, got %d", p.Size())
	}
	if p.Lookup(id) != nil {
		t.Errorf("Lookup after Finalize must be nil")
	}
}
