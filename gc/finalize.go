// finalize_garbage from CPython gc.c. After move_unreachable has
// partitioned the candidate set, the unreachable list holds objects
// whose only references come from inside the cycle. CPython runs
// tp_finalize on each one before reclaiming memory; gopy invokes
// the per-object Finalizer registered via RegisterFinalizer.
//
// Legacy __del__ handling (has_legacy_finalizer plus
// move_legacy_finalizer_reachable) is not modeled: gopy's Finalizer
// API is the tp_finalize-style entry from PEP 442, so every
// finalizer is safe in cycles. Pre-3.4 __del__ finalizers and the
// gc.garbage list they feed are out of scope for v0.10.
//
// CPython: Python/gc.c:1067 finalize_garbage

package gc

import (
	"github.com/tamnd/gopy/objects"
)

// finalizeGarbage walks the unreachable list and runs the registered
// finalizer for each entry. Finalizers run with state.mu held; this
// matches CPython holding the GIL across tp_finalize and keeps the
// queue from being observed half-drained by a second collector pass.
//
// Resurrection: a finalizer is allowed to take a new reference to
// the object. CPython detects this by re-walking the list after
// finalization runs and pulling resurrected objects out of the
// reclaim path. gopy relies on Go's own GC to keep resurrected
// objects alive: any new reference taken by user code roots the
// object normally, and the next gc.Collect cycle will re-evaluate it.
//
// CPython: Python/gc.c:1067 finalize_garbage
func finalizeGarbage(unreachable *gcHead, finalizers map[objects.Object]Finalizer, finalized map[objects.Object]struct{}) {
	for g := unreachable.next; g != unreachable; g = g.next {
		if g.flags&gcFinalized != 0 {
			continue
		}
		g.flags |= gcFinalized
		if finalized != nil {
			finalized[g.obj] = struct{}{}
		}
		fn, ok := finalizers[g.obj]
		if !ok {
			continue
		}
		delete(finalizers, g.obj)
		fn(g.obj)
	}
}

// reclaimUnreachable drops every entry on unreachable from the
// tracked map and unlinks it from the list. Stand-in for CPython's
// delete_garbage: in CPython that loop calls tp_clear and decrements
// refcounts; in gopy the Go runtime owns memory so we just stop
// referencing the gcHead and drop the tracked-map entry. The Go GC
// reclaims unreachable cycles on its own schedule.
//
// CPython: Python/gc.c:1198 delete_garbage
func reclaimUnreachable(unreachable *gcHead, tracked map[objects.Object]*gcHead, finalized map[objects.Object]struct{}) {
	for unreachable.next != unreachable {
		g := unreachable.next
		listRemove(g)
		delete(tracked, g.obj)
		if finalized != nil {
			delete(finalized, g.obj)
		}
	}
}
