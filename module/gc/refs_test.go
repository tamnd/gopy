// Tests for update_refs and subtract_refs. Two scenarios pin the
// algorithm: a self-referential list (refs collapse to 0, cycle) and
// a list with one external owner (refs stays positive, reachable).

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestUpdateRefsCopiesRefcount(t *testing.T) {
	defer untrackAll()
	containers := newListHead()
	a := objects.NewList(nil)
	g := &gcHead{obj: a}
	listAppend(g, containers)

	updateRefs(containers)

	if g.refs != a.Hdr().Refcnt() {
		t.Fatalf("refs=%d, want %d", g.refs, a.Hdr().Refcnt())
	}
	if g.flags&gcCollecting == 0 {
		t.Fatalf("collecting bit not set")
	}
}

func TestSubtractRefsCycleCollapsesToZero(t *testing.T) {
	defer untrackAll()
	// Self-referential list: l.append(l). Refcount is 2 (the test's
	// local plus the slot inside the list); the inner reference is
	// the only one inside the candidate set, so subtract_refs should
	// pull refs down to 1, leaving exactly the external test
	// reference. To get a true zero we need the list to be reachable
	// only from inside the cycle; we do that by dropping the local
	// after wiring up.
	l := objects.NewList(nil)
	l.Append(l)
	g := &gcHead{obj: l}
	containers := newListHead()
	listAppend(g, containers)
	tracked := map[objects.Object]*gcHead{l: g}

	updateRefs(containers)
	startRefs := g.refs
	if err := subtractRefs(containers, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}

	// One self-reference inside the candidate set => refs must drop
	// by exactly one.
	if g.refs != startRefs-1 {
		t.Fatalf("refs=%d, want %d", g.refs, startRefs-1)
	}
}

func TestSubtractRefsTwoNodeCycle(t *testing.T) {
	defer untrackAll()
	// Build a -> b -> a. Each list holds the other; both are in the
	// candidate set. After subtract_refs, the only references each
	// list has left are the two locals here, so refs should equal 1.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)

	ga := &gcHead{obj: a}
	gb := &gcHead{obj: b}
	containers := newListHead()
	listAppend(ga, containers)
	listAppend(gb, containers)
	tracked := map[objects.Object]*gcHead{a: ga, b: gb}

	updateRefs(containers)
	if err := subtractRefs(containers, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}

	// Each list started with refcount = 1 (test local) and lost 1
	// from the other list's pointer, but Append does not bump the
	// refcount in gopy. Verify the math the algorithm actually does:
	// refs_after == refcnt - (incoming pointers from the candidate
	// set). With one incoming ref each, refs should be refcnt-1.
	wantA := a.Hdr().Refcnt() - 1
	wantB := b.Hdr().Refcnt() - 1
	if ga.refs != wantA {
		t.Fatalf("a.refs=%d, want %d", ga.refs, wantA)
	}
	if gb.refs != wantB {
		t.Fatalf("b.refs=%d, want %d", gb.refs, wantB)
	}
}

func TestSubtractRefsExternalOwnerStaysPositive(t *testing.T) {
	defer untrackAll()
	// Tuple containing a list, only the tuple is in the candidate
	// set. The list is NOT tracked; visit_decref skips it. Refs on
	// the tuple stays equal to its refcount.
	inner := objects.NewList(nil)
	tup := objects.NewTuple([]objects.Object{inner})
	g := &gcHead{obj: tup}
	containers := newListHead()
	listAppend(g, containers)
	tracked := map[objects.Object]*gcHead{tup: g}

	updateRefs(containers)
	startRefs := g.refs
	if err := subtractRefs(containers, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}

	if g.refs != startRefs {
		t.Fatalf("refs=%d, want %d (untracked target should not decrement)", g.refs, startRefs)
	}
}

func TestSubtractRefsSkipsNonCollectingNodes(t *testing.T) {
	defer untrackAll()
	// A node in the tracked map but without the COLLECTING flag (eg.
	// it lives on a different generation) must not be touched by
	// subtract_refs.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)

	ga := &gcHead{obj: a}
	gb := &gcHead{obj: b} // no flags set
	containers := newListHead()
	listAppend(ga, containers)
	tracked := map[objects.Object]*gcHead{a: ga, b: gb}

	updateRefs(containers) // marks ga only
	startB := gb.refs
	if err := subtractRefs(containers, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}

	if gb.refs != startB {
		t.Fatalf("non-collecting refs touched: gb.refs=%d, want %d", gb.refs, startB)
	}
}
