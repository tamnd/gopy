package gc

import "testing"

// TestIndexPool_AllocSequential confirms AllocIndex hands out 0, 1, 2,
// ... when no indices have been freed.
func TestIndexPool_AllocSequential(t *testing.T) {
	var p IndexPool
	for want := int32(0); want < 8; want++ {
		got, err := p.AllocIndex()
		if err != nil {
			t.Fatalf("AllocIndex(%d): unexpected error %v", want, err)
		}
		if got != want {
			t.Errorf("AllocIndex(%d) = %d, want %d", want, got, want)
		}
	}
	p.Fini()
}

// TestIndexPool_FreeReusesSmallest confirms freed indices are reused
// in min-heap order on subsequent AllocIndex calls.
func TestIndexPool_FreeReusesSmallest(t *testing.T) {
	var p IndexPool
	for i := 0; i < 5; i++ {
		if _, err := p.AllocIndex(); err != nil {
			t.Fatalf("AllocIndex: %v", err)
		}
	}
	// Free out of order; AllocIndex should still return the smallest.
	p.FreeIndex(3)
	p.FreeIndex(0)
	p.FreeIndex(4)
	p.FreeIndex(1)

	want := []int32{0, 1, 3, 4}
	for _, w := range want {
		got, err := p.AllocIndex()
		if err != nil {
			t.Fatalf("AllocIndex: %v", err)
		}
		if got != w {
			t.Errorf("AllocIndex got %d, want %d", got, w)
		}
	}
	// Pool is now drained; next alloc must extend.
	got, err := p.AllocIndex()
	if err != nil {
		t.Fatalf("AllocIndex: %v", err)
	}
	if got != 5 {
		t.Errorf("AllocIndex after drain = %d, want 5", got)
	}
	p.Fini()
}

// TestIndexPool_TLBCGenerationBumps confirms every Alloc and Free
// increments the TLBC generation counter.
func TestIndexPool_TLBCGenerationBumps(t *testing.T) {
	var p IndexPool
	prev := p.TLBCGeneration
	if _, err := p.AllocIndex(); err != nil {
		t.Fatalf("AllocIndex: %v", err)
	}
	if p.TLBCGeneration <= prev {
		t.Errorf("AllocIndex did not bump TLBCGeneration (%d -> %d)", prev, p.TLBCGeneration)
	}
	prev = p.TLBCGeneration
	p.FreeIndex(0)
	if p.TLBCGeneration <= prev {
		t.Errorf("FreeIndex did not bump TLBCGeneration (%d -> %d)", prev, p.TLBCGeneration)
	}
	p.Fini()
}

// TestIndexPool_HeapOrderUnderManyFrees stresses the min-heap by
// allocating a batch, freeing them in shuffled order, and checking
// that subsequent allocations come back in ascending order.
func TestIndexPool_HeapOrderUnderManyFrees(t *testing.T) {
	var p IndexPool
	const n = 64
	for i := 0; i < n; i++ {
		if _, err := p.AllocIndex(); err != nil {
			t.Fatalf("AllocIndex: %v", err)
		}
	}
	// Free in a deterministic non-sorted permutation.
	order := []int32{17, 3, 42, 0, 31, 8, 55, 12, 1, 63, 22, 7, 38, 19, 49, 2}
	for _, idx := range order {
		p.FreeIndex(idx)
	}
	// Sorted snapshot of the same set.
	want := []int32{0, 1, 2, 3, 7, 8, 12, 17, 19, 22, 31, 38, 42, 49, 55, 63}
	for _, w := range want {
		got, err := p.AllocIndex()
		if err != nil {
			t.Fatalf("AllocIndex: %v", err)
		}
		if got != w {
			t.Errorf("AllocIndex got %d, want %d", got, w)
		}
	}
	p.Fini()
}

// TestIndexPool_FiniMarksHeapInvalid confirms Fini drops storage and
// pins size / capacity to -1, matching the upstream sentinel that
// makes use-after-fini bugs surface fast.
func TestIndexPool_FiniMarksHeapInvalid(t *testing.T) {
	var p IndexPool
	if _, err := p.AllocIndex(); err != nil {
		t.Fatalf("AllocIndex: %v", err)
	}
	p.FreeIndex(0)
	p.Fini()
	if p.FreeIndices.size != -1 {
		t.Errorf("Fini should pin size to -1, got %d", p.FreeIndices.size)
	}
	if p.FreeIndices.capacity != -1 {
		t.Errorf("Fini should pin capacity to -1, got %d", p.FreeIndices.capacity)
	}
	if p.FreeIndices.values != nil {
		t.Errorf("Fini should release values backing array")
	}
}

// TestIndexPool_HeapEnsureCapacityGrows checks the first-ensure path
// jumps from zero to the 1024 baseline and that subsequent grows
// double the capacity.
func TestIndexPool_HeapEnsureCapacityGrows(t *testing.T) {
	var h IndexHeap
	if err := h.heapEnsureCapacity(1); err != nil {
		t.Fatalf("heapEnsureCapacity(1): %v", err)
	}
	if h.capacity != 1024 {
		t.Errorf("first grow: capacity = %d, want 1024", h.capacity)
	}
	if err := h.heapEnsureCapacity(2048); err != nil {
		t.Fatalf("heapEnsureCapacity(2048): %v", err)
	}
	if h.capacity < 2048 {
		t.Errorf("second grow: capacity = %d, want >= 2048", h.capacity)
	}
}
