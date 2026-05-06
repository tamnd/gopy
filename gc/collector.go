// gc_collect_main from CPython gc.c. The driver wires the algorithm
// pieces together: pick the candidate set out of generations 0..gen,
// run update_refs / subtract_refs / move_unreachable to partition
// reachable from unreachable, run the per-cycle finalizers, drop the
// unreachable entries, and promote survivors to the next generation.
//
// What gopy leaves out:
//
//   - gc_select_generation auto-trigger lands with 1613-K.x once we
//     wire allocator hooks.
//   - delete_garbage's tp_clear loop is replaced by reclaimUnreachable
//     because Go's GC reclaims memory once references drop.
//   - clearAllFreeLists (gc_gil.c) is a no-op in gopy: the runtime
//     does not maintain CPython-style per-type free lists.
//
// CPython: Python/gc.c:1430 gc_collect_main
// CPython: Modules/gcmodule.c:1822 gc_collect_impl

package gc

// Collect runs a collection on generations 0..gen and returns the
// number of objects reclaimed. The argument is clamped into the
// [0, NumGenerations) range so callers can pass gc.collect()'s
// optional generation through unchecked. When the collector is
// disabled (gc.disable()) Collect returns 0 without touching state.
//
// Weakref callbacks queued by handleWeakrefs run after the collector
// lock has been released. CPython does the same so callbacks can
// safely take the GIL, allocate, or trigger another collection.
//
// CPython: Python/gc.c:1430 gc_collect_main
func Collect(gen int) int {
	if gen < 0 {
		gen = 0
	}
	if gen >= NumGenerations {
		gen = NumGenerations - 1
	}
	state.mu.Lock()
	if !state.enabled {
		state.mu.Unlock()
		return 0
	}
	collected, pending := collectMain(gen)
	state.mu.Unlock()
	invokeWeakrefCallbacks(pending)
	return collected
}

// collectMain is the lock-held inner driver. Mirrors gc_collect_main
// minus the auto-trigger. Returns the reclaim count and any pending
// weakref callbacks; the caller invokes the callbacks after dropping
// state.mu.
//
// CPython: Python/gc.c:1430 gc_collect_main
func collectMain(gen int) (int, []pendingCallback) {
	young := newListHead()
	for i := 0; i <= gen; i++ {
		listMerge(state.generations[i].head, young)
		state.generations[i].count = 0
	}

	updateRefs(young)
	if err := subtractRefs(young, state.tracked); err != nil {
		listMerge(young, state.generations[gen].head)
		return 0, nil
	}

	unreachable := newListHead()
	if err := moveUnreachable(young, unreachable, state.tracked); err != nil {
		listMerge(young, state.generations[gen].head)
		listMerge(unreachable, state.generations[gen].head)
		return 0, nil
	}

	untrackTuples(young, state.tracked)

	pending := handleWeakrefs(unreachable, state.weakrefs)

	finalizeGarbage(unreachable, state.finalizers)
	collected := listSize(unreachable)
	reclaimUnreachable(unreachable, state.tracked)
	clearUnreachableMask(young)
	clearAllFreeLists()

	// Promote survivors. CPython moves them to the next-older
	// generation unless we are already at the top; the same mapping
	// applies here.
	dest := gen
	if gen+1 < NumGenerations {
		dest = gen + 1
		state.generations[dest].count++
	}
	listMerge(young, state.generations[dest].head)

	return collected, pending
}

// clearAllFreeLists is the gopy stand-in for CPython's
// _PyGC_ClearAllFreeLists (gc_gil.c). CPython drops cached objects
// from per-type free lists on every collection; gopy has no such
// caches, so the call is a no-op kept here to match the call site
// shape and make the absence explicit.
//
// CPython: Python/gc_gil.c _PyGC_ClearAllFreeLists
func clearAllFreeLists() {}
