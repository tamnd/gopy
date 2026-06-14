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
// the object. handleResurrected re-walks the post-finalize list to
// pull resurrected objects out of the reclaim path; see that helper
// for the matching CPython logic.
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
		if fn, ok := finalizers[g.obj]; ok {
			delete(finalizers, g.obj)
			fn(g.obj)
			continue
		}
		if slot := typeFinalize(g.obj); slot != nil {
			slot(g.obj)
		}
	}
}

// typeFinalize returns the tp_finalize slot for o's type, or nil if
// the type has none. The collector falls back to this when no
// explicit Finalizer was registered, so user classes with __del__
// (and built-in types that opt in) participate in cycle finalization.
//
// CPython: Objects/object.c:489 PyObject_CallFinalizer (tp_finalize lookup)
func typeFinalize(o objects.Object) func(objects.Object) {
	if o == nil {
		return nil
	}
	if t := o.Type(); t != nil && t.Finalize != nil {
		return t.Finalize
	}
	return nil
}

// handleResurrected re-runs deduce_unreachable on the post-finalize
// list. Finalizers may have taken a new reference (via objects.Incref
// from user code, or by stashing the object on a still-tracked
// container); those objects need to come back out of the reclaim path.
//
// On entry, every node on `unreachable` carries gcCollecting and
// gcUnreachable. Both flags are stripped so updateRefs / subtractRefs /
// moveUnreachable can re-evaluate the set: updateRefs reseeds refs
// from the live refcount, subtractRefs walks tp_traverse, and
// moveUnreachable splits survivors (resurrected) from the still-dead.
//
// On exit, `unreachable` holds the resurrected nodes (caller merges
// them back into a generation) and `stillUnreachable` holds the truly
// dead nodes the caller will reclaim.
//
// CPython: Python/gc.c:1261 handle_resurrected_objects
func handleResurrected(unreachable, stillUnreachable *gcHead, tracked map[objects.Object]*gcHead) error {
	for g := unreachable.next; g != unreachable; g = g.next {
		g.flags &^= gcUnreachable
		g.flags &^= gcCollecting
	}
	updateRefs(unreachable)
	if err := subtractRefs(unreachable, tracked); err != nil {
		return err
	}
	return moveUnreachable(unreachable, stillUnreachable, tracked)
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
		// A heap type being reclaimed must drop itself from every base's
		// tp_subclasses, the same teardown CPython's type_dealloc performs
		// via remove_all_subclasses. Without it __subclasses__() keeps
		// returning a dead type (bpo-46417).
		//
		// CPython: Objects/typeobject.c type_dealloc (remove_all_subclasses)
		if t, ok := g.obj.(*objects.Type); ok {
			objects.RemoveAllSubclasses(t)
		}
		delete(tracked, g.obj)
		if finalized != nil {
			delete(finalized, g.obj)
		}
	}
}
