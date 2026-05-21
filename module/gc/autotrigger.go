// Threshold-driven automatic collection. CPython runs the collector
// out of two places: the allocator-side hook in _PyObject_GC_Link
// (which schedules a collection when generation-0's count crosses its
// threshold) and the eval-loop drain in _Py_RunGC (which actually
// invokes gc_collect_main with GENERATION_AUTO so gc_select_generation
// picks the oldest generation that needs collecting).
//
// gopy fuses both hooks into one synchronous call from Track. We have
// no eval-side deferred-trigger machinery yet, so collecting inline
// from the allocator path is the simplest faithful port. The
// re-entrancy guard (gcstate.collecting) prevents finalizers from
// recursing back through Track and starting a nested collection.
//
// CPython: Python/gc.c:1258 gc_select_generation
// CPython: Python/gc.c:1855 _PyObject_GC_Link
// CPython: Python/gc.c:1877 _Py_RunGC

package gc

// selectGeneration returns the oldest generation whose live count has
// crossed its threshold, or -1 when no generation needs collecting.
// The full-collection ratio (long_lived_pending / long_lived_total >=
// 25%) is the heuristic from issue #4074 that keeps gen-2 collection
// cost amortized linear in total long-lived objects.
//
// Must be called with state.mu held.
//
// CPython: Python/gc.c:1258 gc_select_generation
func selectGeneration() int {
	for i := NumGenerations - 1; i >= 0; i-- {
		if state.generations[i].count <= state.generations[i].threshold {
			continue
		}
		if i == NumGenerations-1 &&
			state.longLivedPending < state.longLivedTotal/4 {
			continue
		}
		return i
	}
	return -1
}

// maybeAutoCollect mirrors the post-link trigger block in
// _PyObject_GC_Link plus _Py_RunGC. Returns 0 when no collection
// happened. The caller (Track) must hold state.mu on entry; the lock
// is dropped while Collect runs and re-acquired before returning so
// Track's deferred Unlock stays valid.
//
// CPython: Python/gc.c:1866 _PyObject_GC_Link allocator hook
// CPython: Python/gc.c:1877 _Py_RunGC
func maybeAutoCollect() {
	if !state.enabled {
		return
	}
	if state.generations[0].threshold == 0 {
		return
	}
	if state.collecting {
		return
	}
	if state.generations[0].count <= state.generations[0].threshold {
		return
	}
	gen := selectGeneration()
	if gen < 0 {
		return
	}
	state.collecting = true
	state.mu.Unlock()
	// Collect re-acquires state.mu internally. We must not hold it
	// across the call because invokeGCCallback / weakref callbacks
	// run user code and could otherwise deadlock.
	Collect(gen)
	state.mu.Lock()
	state.collecting = false
}
