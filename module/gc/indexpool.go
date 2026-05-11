// Package gc port of Python/index_pool.c. Allocates the smallest free
// int32 index from a pool and releases indices back. The free-threaded
// build uses this to hand each thread a globally unique slot in every
// code object's thread-local bytecode array. The GIL build does not
// instantiate any IndexPool, so until nogil lands here this file
// compiles into the gc package as inert infrastructure.
//
// CPython: Python/index_pool.c (Py_GIL_DISABLED #ifdef block)

package gc

import (
	"errors"

	"github.com/tamnd/gopy/pysync"
)

// IndexHeap is a min-heap of int32. Used as the freelist of released
// indices so AllocIndex always returns the smallest available value.
//
// CPython: Include/internal/pycore_interp_structs.h:725-734 _PyIndexHeap
type IndexHeap struct {
	values   []int32
	size     int
	capacity int
}

// IndexPool is the unbounded pool of indices. Indices are dispensed
// starting from 0; freed indices are pushed onto FreeIndices and
// reused on the next allocation.
//
// CPython: Include/internal/pycore_interp_structs.h:738-750 _PyIndexPool
type IndexPool struct {
	mu             pysync.Mutex
	FreeIndices    IndexHeap
	NextIndex      int32
	TLBCGeneration uint32
}

// ErrIndexPoolNoMemory is returned when AllocIndex cannot grow the
// freelist to cover the next outstanding index. CPython raises
// MemoryError here; gopy returns the error so the caller can decide.
//
// CPython: Python/index_pool.c:167 PyErr_NoMemory
var ErrIndexPoolNoMemory = errors.New("index pool: out of memory")

// swapValues swaps two slots in values. Mirrors the C helper.
//
// CPython: Python/index_pool.c:10-16 swap
func swapValues(values []int32, i, j int) {
	values[i], values[j] = values[j], values[i]
}

// heapTrySwap swaps i and j when the heap order is violated. Returns
// true when a swap actually happened.
//
// CPython: Python/index_pool.c:18-37 heap_try_swap
func (h *IndexHeap) heapTrySwap(i, j int) bool {
	if i < 0 || i >= h.size {
		return false
	}
	if j < 0 || j >= h.size {
		return false
	}
	if i <= j {
		if h.values[i] <= h.values[j] {
			return false
		}
	} else if h.values[j] <= h.values[i] {
		return false
	}
	swapValues(h.values, i, j)
	return true
}

// parent returns the parent index in the binary heap.
//
// CPython: Python/index_pool.c:39-43 parent
func parent(i int) int {
	return (i - 1) / 2
}

// leftChild returns the left child index.
//
// CPython: Python/index_pool.c:45-49 left_child
func leftChild(i int) int {
	return 2*i + 1
}

// rightChild returns the right child index.
//
// CPython: Python/index_pool.c:51-55 right_child
func rightChild(i int) int {
	return 2*i + 2
}

// heapAdd inserts val into the heap, sifting up to restore heap order.
// Caller must guarantee size < capacity.
//
// CPython: Python/index_pool.c:57-70 heap_add
func (h *IndexHeap) heapAdd(val int32) {
	h.values[h.size] = val
	h.size++
	for cur := h.size - 1; cur > 0; cur = parent(cur) {
		if !h.heapTrySwap(cur, parent(cur)) {
			break
		}
	}
}

// heapMinChild returns the smaller of cur's two children, or -1 when
// cur is a leaf.
//
// CPython: Python/index_pool.c:72-87 heap_min_child
func (h *IndexHeap) heapMinChild(i int) int {
	l := leftChild(i)
	r := rightChild(i)
	if l < h.size {
		if r < h.size {
			if h.values[l] < h.values[r] {
				return l
			}
			return r
		}
		return l
	}
	if r < h.size {
		return r
	}
	return -1
}

// heapPop removes and returns the smallest value in the heap.
//
// CPython: Python/index_pool.c:89-108 heap_pop
func (h *IndexHeap) heapPop() int32 {
	result := h.values[0]
	h.values[0] = h.values[h.size-1]
	h.size--
	cur := 0
	for cur < h.size {
		minChild := h.heapMinChild(cur)
		if minChild > -1 && h.heapTrySwap(cur, minChild) {
			cur = minChild
		} else {
			break
		}
	}
	return result
}

// heapEnsureCapacity grows the heap to at least limit slots. Returns
// ErrIndexPoolNoMemory if the doubling overflows.
//
// CPython: Python/index_pool.c:110-135 heap_ensure_capacity
func (h *IndexHeap) heapEnsureCapacity(limit int) error {
	if h.capacity > limit {
		return nil
	}
	newCap := h.capacity
	if newCap == 0 {
		newCap = 1024
	}
	for newCap > 0 && newCap < limit {
		newCap <<= 1
	}
	if newCap <= 0 {
		return ErrIndexPoolNoMemory
	}
	newValues := make([]int32, newCap)
	if h.values != nil {
		copy(newValues, h.values[:h.capacity])
	}
	h.values = newValues
	h.capacity = newCap
	return nil
}

// heapFini releases the heap's backing storage and pins the size /
// capacity fields to -1 so a use-after-fini bug shows up cleanly.
//
// CPython: Python/index_pool.c:137-146 heap_fini
func (h *IndexHeap) heapFini() {
	h.values = nil
	h.size = -1
	h.capacity = -1
}

// AllocIndex returns the smallest available index. Callers receive 0
// on the first call, 1 on the second, and so on; freed indices are
// reused before NextIndex grows.
//
// CPython: Python/index_pool.c:151-180 _PyIndexPool_AllocIndex
func (p *IndexPool) AllocIndex() (int32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var index int32
	if p.FreeIndices.size == 0 {
		// Pre-grow the freelist so FreeIndex can never fail with an
		// allocation error. CPython documents this design choice in a
		// long comment on the upstream function.
		if err := p.FreeIndices.heapEnsureCapacity(int(p.NextIndex) + 1); err != nil {
			return -1, err
		}
		index = p.NextIndex
		p.NextIndex++
	} else {
		index = p.FreeIndices.heapPop()
	}
	p.TLBCGeneration++
	return index, nil
}

// FreeIndex returns index to the pool. Cannot fail because AllocIndex
// already grew the freelist to fit every outstanding index.
//
// CPython: Python/index_pool.c:182-189 _PyIndexPool_FreeIndex
func (p *IndexPool) FreeIndex(index int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TLBCGeneration++
	p.FreeIndices.heapAdd(index)
}

// Fini releases the pool's storage. The PyMutex itself does not need
// teardown.
//
// CPython: Python/index_pool.c:191-195 _PyIndexPool_Fini
func (p *IndexPool) Fini() {
	p.FreeIndices.heapFini()
}
