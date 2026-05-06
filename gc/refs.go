// update_refs and subtract_refs from CPython gc.c. The two-pass
// dance computes, for every object on the candidate list, the number
// of references that come from outside the list. Pass one copies the
// live refcount into gcHead.refs; pass two walks every container with
// its tp_traverse and decrements the refs slot of any tracked target.
// What survives is the external-only count: a positive value means
// the object is reachable from a non-collected root, zero means every
// reference comes from inside the candidate set and the object is
// part of a cycle.
//
// CPython: Python/gc.c:392 update_refs
// CPython: Python/gc.c:482 subtract_refs

package gc

import (
	"github.com/tamnd/gopy/objects"
)

// updateRefs primes gcHead.refs with the live refcount and marks
// every node as part of the current candidate set.
//
// CPython: Python/gc.c:392 update_refs
func updateRefs(containers *gcHead) {
	for g := containers.next; g != containers; g = g.next {
		g.refs = g.obj.Hdr().Refcnt()
		g.flags |= gcCollecting
	}
}

// subtractRefs walks every container in the candidate list and runs
// tp_traverse with a visitor that decrements the refs slot of any
// tracked target carrying the COLLECTING bit. tracked maps live
// objects to their gcHead so the visitor can land on the right node;
// the caller (gc.Collect) supplies it under state.mu.
//
// CPython: Python/gc.c:482 subtract_refs
func subtractRefs(containers *gcHead, tracked map[objects.Object]*gcHead) error {
	visit := func(target objects.Object) error {
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
		g.refs--
		return nil
	}
	for g := containers.next; g != containers; g = g.next {
		tr := g.obj.Type().TpTraverse
		if tr == nil {
			continue
		}
		if err := tr(g.obj, visit); err != nil {
			return err
		}
	}
	return nil
}
