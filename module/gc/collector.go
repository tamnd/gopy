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

import (
	"runtime"

	"github.com/tamnd/gopy/objects"
)

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
	cbList := state.callbacks
	state.mu.Unlock()
	invokeGCCallback(cbList, "start", gen, 0, 0)

	state.mu.Lock()
	collected, pending := collectMain(gen)
	state.mu.Unlock()

	// Force Go GC so any weakref finalizers registered via
	// runtime.SetFinalizer on dropped objects can fire. Without this,
	// support.gc_collect() returns before the underlying Go runtime
	// has a chance to reclaim user objects whose only post-Decref
	// keep-alive is Go's tracing collector. Two passes are needed
	// because the first GC marks finalizers runnable; the second
	// reclaims the now-finalized memory.
	//
	// CPython: Modules/gcmodule.c:1822 gc_collect_impl (CPython's
	// refcount makes collection synchronous, so the second pass
	// approximates the same end state for gopy's Go-backed objects).
	runtime.GC()
	runtime.GC()

	invokeWeakrefCallbacks(pending)
	invokeGCCallback(cbList, "stop", gen, collected, 0)
	return collected
}

// invokeGCCallback calls each entry in the gc.callbacks list with
// (phase, info). info is the dict {"generation": gen, "collected": n,
// "uncollectable": u}. Errors raised by the callbacks are ignored,
// matching CPython's PyErr_FormatUnraisable path.
//
// CPython: Python/gc.c invoke_gc_callback
func invokeGCCallback(cbs *objects.List, phase string, gen, collected, uncollectable int) {
	if cbs == nil || cbs.Len() == 0 {
		return
	}
	info := objects.NewDict()
	_ = info.SetItem(objects.NewStr("generation"), objects.NewInt(int64(gen)))
	_ = info.SetItem(objects.NewStr("collected"), objects.NewInt(int64(collected)))
	_ = info.SetItem(objects.NewStr("uncollectable"), objects.NewInt(int64(uncollectable)))
	args := objects.NewTuple([]objects.Object{objects.NewStr(phase), info})
	for i := 0; i < cbs.Len(); i++ {
		cb := cbs.Item(i)
		if cb == nil {
			continue
		}
		_, _ = objects.Call(cb, args, nil)
	}
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
	pinRoots(young)

	unreachable := newListHead()
	if err := moveUnreachable(young, unreachable, state.tracked); err != nil {
		listMerge(young, state.generations[gen].head)
		listMerge(unreachable, state.generations[gen].head)
		return 0, nil
	}

	untrackTuples(young, state.tracked)

	pending := handleWeakrefs(unreachable, state.weakrefs, state.weakProxies)

	// Finalizers (tp_finalize) run user Python code that may allocate
	// and therefore re-enter Track. Drop state.mu while finalizing so
	// concurrent goroutines (e.g., a generator processing GeneratorExit
	// in its own goroutine) can still allocate. state.collecting stays
	// true, which maybeAutoCollect checks before re-triggering, so we
	// won't recursively start a nested collection.
	//
	// CPython: Python/gc.c:1067 finalize_garbage (runs under the GIL;
	// gopy approximates this by dropping the collector mutex.)
	state.mu.Unlock()
	finalizeGarbage(unreachable, state.finalizers, state.finalized)
	state.mu.Lock()

	// Resurrection check. handleResurrected re-runs deduce on the
	// post-finalize list so any object a finalizer refloated comes
	// back out of the reclaim path. The caller-visible behavior
	// matches CPython: resurrected objects survive into the next
	// generation; only stillUnreachable enters delete_garbage.
	stillUnreachable := newListHead()
	if err := handleResurrected(unreachable, stillUnreachable, state.tracked); err != nil {
		// On a traversal error we fall back to treating everything as
		// still unreachable. Mirrors CPython's behavior of pressing
		// on after PyErr_FormatUnraisable.
		listMerge(unreachable, stillUnreachable)
	}

	// Promote survivors. CPython moves them to the next-older
	// generation unless we are already at the top; the same mapping
	// applies here. The long_lived counters mirror the bookkeeping
	// from gc_collect_main: a NUM_GENERATIONS-2 collection adds its
	// survivors to long_lived_pending, while a full collection
	// resets pending and republishes total.
	//
	// CPython: Python/gc.c:1399 long_lived_pending bookkeeping
	dest := gen
	if gen+1 < NumGenerations {
		dest = gen + 1
		state.generations[dest].count++
	}
	survivors := listSize(young)
	if gen+1 < NumGenerations {
		if gen == NumGenerations-2 {
			state.longLivedPending += survivors
		}
	} else {
		state.longLivedPending = 0
		state.longLivedTotal = survivors
	}

	// Resurrected objects (still on the now-empty `unreachable` head)
	// move to the destination generation alongside the regular
	// survivors. Clear residual flags first.
	clearUnreachableMask(unreachable)
	for g := unreachable.next; g != unreachable; g = g.next {
		g.flags &^= gcCollecting
	}
	listMerge(unreachable, state.generations[dest].head)

	collected := listSize(stillUnreachable)
	if state.debug&DebugSaveAll != 0 && state.garbage != nil {
		appendGarbage(state.garbage, stillUnreachable)
	}
	reclaimUnreachable(stillUnreachable, state.tracked, state.finalized)
	clearUnreachableMask(young)
	clearAllFreeLists()

	state.stats[gen].collections++
	state.stats[gen].collected += collected

	listMerge(young, state.generations[dest].head)

	return collected, pending
}

// appendGarbage appends every node on `list` to the gc.garbage list.
// CPython does this inline in delete_garbage when DEBUG_SAVEALL is on.
//
// CPython: Python/gc.c:1142 delete_garbage SAVEALL branch
func appendGarbage(garbage *objects.List, list *gcHead) {
	for g := list.next; g != list; g = g.next {
		garbage.Append(g.obj)
	}
}
