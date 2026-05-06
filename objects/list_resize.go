// list_resize is the spine of all list mutations: Append, Insert,
// extend, slice-assign, and Pop all funnel through it. The growth
// curve is the one CPython picks: mild overallocation rounded to a
// multiple of 4, with shrink only when the new size drops below half
// the current capacity. Matching the curve byte for byte keeps memory
// behavior comparable for benchmarks and tests that pin allocation
// counts.
//
// CPython: Objects/listobject.c:55 list_resize

package objects

// listResize grows or shrinks the underlying slice so it holds
// exactly newsize items, following CPython's overallocation curve.
//
// CPython: Objects/listobject.c:55 list_resize
func (l *List) listResize(newsize int) {
	allocated := cap(l.items)
	if allocated >= newsize && newsize >= allocated>>1 {
		l.items = l.items[:newsize]
		l.size = int64(newsize)
		return
	}
	if newsize == 0 {
		l.items = nil
		l.size = 0
		return
	}
	newAlloc := (newsize + newsize>>3 + 6) &^ 3
	// Don't overallocate when the jump is closer to the new
	// allocation than to the old size.
	if newsize-len(l.items) > newAlloc-newsize {
		newAlloc = (newsize + 3) &^ 3
	}
	buf := make([]Object, newsize, newAlloc)
	copy(buf, l.items[:min(len(l.items), newsize)])
	l.items = buf
	l.size = int64(newsize)
}

// listGrowthCurve returns the allocation that listResize would pick
// for the given (oldsize, newsize) pair. Exposed for tests; mirrors
// the inline computation in list_resize.
//
// CPython: Objects/listobject.c:55 list_resize
func listGrowthCurve(oldsize, newsize int) int {
	if newsize == 0 {
		return 0
	}
	newAlloc := (newsize + newsize>>3 + 6) &^ 3
	if newsize-oldsize > newAlloc-newsize {
		newAlloc = (newsize + 3) &^ 3
	}
	return newAlloc
}
