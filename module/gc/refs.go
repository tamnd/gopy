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
// The decrement floors at 0. CPython lets refs go negative on shared
// objects only when a heap inconsistency exists; in gopy, negatives
// happen routinely because containers (Frame's f_funcobj, Frame's
// locals/cells, function __closure__ cells, generator gi_frame, etc.)
// do not Incref what they store. Letting refs underflow then either
// pushes a still-live module-level Func into the unreachable set
// (refs > 0 branch, breaks recursive queens / conjoin) or hides a
// genuinely-dead self-cycle (refs != 0 branch, breaks weakref-callback
// tests). Flooring at 0 lets visit_reachable pull live objects back
// through their reachable container while still classifying
// no-external-ref cycles as unreachable.
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
		if g.refs > 0 {
			g.refs--
		}
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

// pinRoots restores a positive ref count on every candidate that
// declares itself a collector root via objects.GCRoot. gopy runs each
// generator / coroutine / async-generator body on its own goroutine;
// while a body is executing, the goroutine stack is the only live
// reference to that object and to the chain of sub-generators it is
// iterating. subtract_refs cannot see that Go-pointer reference, so it
// collapses the entire active spine to gc_refs == 0 and move_unreachable
// would reclaim the suspended children hanging off it mid-iteration. By
// re-floating the running root to 1 here, move_unreachable keeps it (and
// everything visit_reachable pulls in through its frame) alive, which is
// what CPython gets for free because an executing frame is rooted by
// tstate->current_frame.
//
// CPython: Python/gc.c:1208 gc_collect_main (tstate roots stay reachable)
func pinRoots(containers *gcHead, tracked map[objects.Object]*gcHead) {
	for g := containers.next; g != containers; g = g.next {
		if r, ok := g.obj.(objects.GCRoot); ok && r.GCRoot() && g.refs == 0 {
			g.refs = 1
		}
	}
	// Re-float every candidate an executing interpreter frame holds.
	// CPython roots these through tstate->current_frame; gopy's frame
	// arena is invisible to the collector, so the vm enumerates the
	// held objects and we pin the ones that landed on the candidate
	// list. move_unreachable's visit_reachable then pulls in whatever
	// the rooted object itself references (a suspended generator's
	// frame, its sub-generators, and so on).
	//
	// CPython: Python/gc.c:1430 gc_collect_main (tstate->current_frame roots)
	if h := objects.GCExecutingRootsHook; h != nil {
		h(func(o objects.Object) {
			if o == nil {
				return
			}
			if g, ok := tracked[o]; ok && g.flags&gcCollecting != 0 && g.refs == 0 {
				g.refs = 1
			}
		})
	}
	// Re-float the interpreter-lifetime singletons (sys.modules) that
	// gopy holds through a Go pointer. CPython roots these via
	// interp->modules; without this the whole module graph collapses to
	// gc_refs == 0 and a module global reachable only through its module
	// __dict__ is reclaimed while still live.
	//
	// We do not stop at the direct sys.modules entries. move_unreachable's
	// visit_reachable would, in principle, pull in the rest of the
	// strongly-reachable closure from the pinned modules. But gopy's
	// containers do not Incref what they store (Frame f_funcobj, closure
	// cells, instance __dict__, and so on), so subtract_refs over-decrements
	// intermediate nodes deep in that closure. The single-level pin then
	// leaves a live object reachable only through several under-counted
	// hops (for example _frozen_importlib._blocking_on's instance __dict__,
	// reached via module -> module __dict__ -> _WeakValueDictionary
	// instance -> instance __dict__) exposed to a false reclaim whenever
	// the partition order does not happen to resurrect every hop. CPython
	// never sees this because every one of those edges carries a counted
	// reference. We compensate by marking the entire closure reachable from
	// the static roots here, so survival no longer depends on refcount
	// accuracy along the chain.
	//
	// CPython: Python/gc.c:1430 gc_collect_main (interp->modules roots)
	if h := objects.GCStaticRootsHook; h != nil {
		markReachableClosure(h, tracked)
	}
}

// markReachableClosure pins every candidate strongly reachable from the
// interpreter-lifetime static roots (sys.modules) and floats it to
// refs >= 1 so move_unreachable keeps the whole live module graph.
//
// The walk recurses only through candidates (objects carrying the
// COLLECTING bit, i.e. members of the generations being collected). That
// keeps a young-generation collection cheap: the static roots are mostly
// long-lived modules promoted to the oldest generation, so traversing one
// of them lands immediately on its non-candidate __dict__ and stops. The
// false reclaim this guards against only arises in a full collection,
// where the deep chain (module -> module __dict__ -> _WeakValueDictionary
// instance -> instance __dict__) is entirely on the candidate list and
// gopy's no-Incref-on-store edges (instance __dict__) over-decrement an
// interior node out from under it; there the walk visits every candidate
// once, no worse than update_refs/subtract_refs already do for the same
// collection.
//
// CPython: Python/gc.c:1430 gc_collect_main (interp->modules roots)
func markReachableClosure(h func(pin func(objects.Object)), tracked map[objects.Object]*gcHead) {
	var stack []*gcHead
	seen := make(map[*gcHead]struct{})
	enqueue := func(o objects.Object) {
		if o == nil {
			return
		}
		g, ok := tracked[o]
		if !ok || g.flags&gcCollecting == 0 {
			// Not a candidate. Its reference to any candidate child is
			// external (subtract_refs never scans it), so that child keeps
			// its live refcount without our help; no need to recurse.
			return
		}
		if _, dup := seen[g]; dup {
			return
		}
		seen[g] = struct{}{}
		if g.refs == 0 {
			g.refs = 1
		}
		stack = append(stack, g)
	}
	// Seed from the direct root entries. The root objects themselves may be
	// non-candidates (immortal/old modules), so traverse each one level to
	// reach its candidate children even when the root is not enqueued.
	h(func(root objects.Object) {
		if root == nil {
			return
		}
		enqueue(root)
		if tr := root.Type().TpTraverse; tr != nil {
			_ = tr(root, func(child objects.Object) error {
				enqueue(child)
				return nil
			})
		}
	})
	for len(stack) > 0 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		tr := g.obj.Type().TpTraverse
		if tr == nil {
			continue
		}
		_ = tr(g.obj, func(child objects.Object) error {
			enqueue(child)
			return nil
		})
	}
}
