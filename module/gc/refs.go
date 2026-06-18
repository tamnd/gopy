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
	// CPython: Python/gc.c:1430 gc_collect_main (interp->modules roots)
	if h := objects.GCStaticRootsHook; h != nil {
		h(func(o objects.Object) {
			if o == nil {
				return
			}
			if g, ok := tracked[o]; ok && g.flags&gcCollecting != 0 && g.refs == 0 {
				g.refs = 1
			}
		})
	}
}
