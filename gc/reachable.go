// move_unreachable, visit_reachable, clear_unreachable_mask, and
// untrack_tuples from CPython gc.c. After update_refs and subtract_refs
// have computed gcHead.refs as "external references", this pass
// partitions the candidate list into the survivors (refs > 0 plus
// everything reachable from them) and the unreachable set (the rest).
//
// CPython: Python/gc.c:497 move_unreachable
// CPython: Python/gc.c:430 visit_reachable
// CPython: Python/gc.c:728 clear_unreachable_mask
// CPython: Python/gc.c:721 untrack_tuples

package gc

import (
	"errors"

	"github.com/tamnd/gopy/objects"
)

// moveUnreachable splits young into "definitely reachable" (still on
// young) and "tentatively unreachable" (moved to unreachable). Walks
// young left-to-right: nodes with refs > 0 stay and have their
// outgoing edges traversed via visit_reachable, which can pull
// already-moved nodes back when a survivor turns out to point at
// them. Nodes with refs == 0 move to the unreachable list with the
// UNREACHABLE flag set.
//
// CPython: Python/gc.c:497 move_unreachable
func moveUnreachable(young, unreachable *gcHead, tracked map[objects.Object]*gcHead) error {
	visit := makeVisitReachable(young, tracked)
	gc := young.next
	for gc != young {
		if gc.refs > 0 {
			tr := gc.obj.Type().TpTraverse
			if tr != nil {
				if err := tr(gc.obj, visit); err != nil {
					return err
				}
			}
			gc.flags &^= gcCollecting
			gc = gc.next
			continue
		}
		next := gc.next
		listRemove(gc)
		listAppend(gc, unreachable)
		gc.flags |= gcUnreachable
		gc = next
	}
	return nil
}

// makeVisitReachable returns the closure passed to tp_traverse during
// the survivor pass. It pulls back any tentatively-unreachable target
// or bumps a forward reference whose refs already collapsed to zero.
//
// CPython: Python/gc.c:430 visit_reachable
func makeVisitReachable(young *gcHead, tracked map[objects.Object]*gcHead) objects.Visitor {
	return func(target objects.Object) error {
		if target == nil {
			return nil
		}
		g, ok := tracked[target]
		if !ok {
			return nil
		}
		if g.flags&gcCollecting == 0 {
			return nil
		}
		if g.flags&gcUnreachable != 0 {
			// Resurrect. Move from unreachable back to young's
			// tail so the outer loop visits it again, and prime
			// refs at 1 so the next pass marks it reachable.
			listRemove(g)
			listAppend(g, young)
			g.flags &^= gcUnreachable
			g.refs = 1
			return nil
		}
		if g.refs == 0 {
			// Forward reference: the outer loop hasn't reached g
			// yet, but a survivor points at it. Bump to 1 so
			// when we get there it travels the reachable arm.
			g.refs = 1
		}
		return nil
	}
}

// clearUnreachableMask wipes the UNREACHABLE flag from every node in
// the list. Called once the unreachable set has been finalized and
// the bit is no longer needed.
//
// CPython: Python/gc.c:728 clear_unreachable_mask
func clearUnreachableMask(list *gcHead) {
	for g := list.next; g != list; g = g.next {
		g.flags &^= gcUnreachable
	}
}

// untrackTuples removes from head any tuple whose items are not
// themselves tracked. CPython runs this as an optimization at the
// end of every collection to keep the candidate list short on the
// next pass.
//
// CPython: Python/gc.c:721 untrack_tuples
func untrackTuples(head *gcHead, tracked map[objects.Object]*gcHead) {
	gc := head.next
	for gc != head {
		next := gc.next
		if gc.obj.Type() == objects.TupleType && tupleHasNoTrackedChild(gc.obj, tracked) {
			listRemove(gc)
			delete(tracked, gc.obj)
		}
		gc = next
	}
}

// tupleHasNoTrackedChild reports whether t holds zero tracked
// elements. The tuple itself is the receiver, so we walk its items
// via tp_traverse and stop at the first tracked target.
func tupleHasNoTrackedChild(t objects.Object, tracked map[objects.Object]*gcHead) bool {
	tr := t.Type().TpTraverse
	if tr == nil {
		return true
	}
	hasTracked := false
	err := tr(t, func(child objects.Object) error {
		if child == nil {
			return nil
		}
		if _, ok := tracked[child]; ok {
			hasTracked = true
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		// On unexpected error, conservatively keep the tuple tracked.
		return false
	}
	return !hasTracked
}

// errStopWalk short-circuits a tp_traverse without bubbling a real
// error.
var errStopWalk = errors.New("gc: stop walk")
