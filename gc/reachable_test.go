// Tests for move_unreachable and friends. Each scenario constructs
// a small object graph by hand, runs update_refs/subtract_refs to
// prime the gcHead.refs values the way the real collector would,
// then checks that move_unreachable lands the right partition.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// buildCandidates lays out a list of (obj, gcHead) pairs into a fresh
// young list and returns the list head plus the tracked map.
func buildCandidates(objs []objects.Object) (young *gcHead, tracked map[objects.Object]*gcHead) {
	young = newListHead()
	tracked = make(map[objects.Object]*gcHead, len(objs))
	for _, o := range objs {
		g := &gcHead{obj: o}
		listAppend(g, young)
		tracked[o] = g
	}
	return young, tracked
}

// listObjects walks a list and returns the contained objects in
// order. Used to assert which objects ended up where.
func listObjects(list *gcHead) []objects.Object {
	var out []objects.Object
	for g := list.next; g != list; g = g.next {
		out = append(out, g.obj)
	}
	return out
}

func TestMoveUnreachableSinglePureCycle(t *testing.T) {
	// Two-list cycle. Each list refs the other; nothing else points
	// at either. After update_refs/subtract_refs both nodes have
	// refs == 0 and should land in unreachable.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)

	// Start refcounts that mimic "no external owner": objects.NewList
	// hands out refcount 1 (the test local). Pretend the test locals
	// are gone by lowering refcnt accounting to one inbound edge.
	// Instead of mutating refcnt, we hand-set refs after update_refs
	// so the test pins move_unreachable in isolation.

	young, tracked := buildCandidates([]objects.Object{a, b})
	updateRefs(young)
	if err := subtractRefs(young, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}
	// Force "no external owner": with a single test-local each, both
	// land at refs == 0 only after we drop that local. Simulate by
	// zeroing the leftover.
	for _, g := range tracked {
		g.refs = 0
	}

	unreachable := newListHead()
	if err := moveUnreachable(young, unreachable, tracked); err != nil {
		t.Fatalf("moveUnreachable: %v", err)
	}

	if got := len(listObjects(young)); got != 0 {
		t.Fatalf("young not empty: %d items", got)
	}
	got := listObjects(unreachable)
	if len(got) != 2 {
		t.Fatalf("unreachable size = %d, want 2", len(got))
	}
}

func TestMoveUnreachableExternalOwnerKeepsAll(t *testing.T) {
	// Two-list cycle with an external owner pointing at a only. b is
	// reachable transitively from a. Result: both stay on young.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)

	young, tracked := buildCandidates([]objects.Object{a, b})
	updateRefs(young)
	if err := subtractRefs(young, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}
	// External owner of a: bump its refs by one so it survives.
	tracked[a].refs++
	// b has zero external owners.
	tracked[b].refs--
	if tracked[b].refs < 0 {
		tracked[b].refs = 0
	}

	unreachable := newListHead()
	if err := moveUnreachable(young, unreachable, tracked); err != nil {
		t.Fatalf("moveUnreachable: %v", err)
	}

	if got := len(listObjects(unreachable)); got != 0 {
		t.Fatalf("unreachable should be empty, got %d", got)
	}
	if got := len(listObjects(young)); got != 2 {
		t.Fatalf("young size = %d, want 2", got)
	}
	if tracked[a].flags&gcCollecting != 0 {
		t.Fatalf("a still has COLLECTING bit")
	}
	if tracked[b].flags&gcCollecting != 0 {
		t.Fatalf("b still has COLLECTING bit")
	}
}

func TestMoveUnreachableForwardReferenceResurrects(t *testing.T) {
	// Order-sensitive case: a is rooted, b sits BEFORE a in young
	// and is referenced only by a. The naive scan would see b first
	// (refs == 0), move it to unreachable, and only then visit a.
	// visit_reachable on a -> b must yank b back.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)

	// Insert b first, then a. That orders the young list b, a.
	young, tracked := buildCandidates([]objects.Object{b, a})
	updateRefs(young)
	if err := subtractRefs(young, tracked); err != nil {
		t.Fatalf("subtractRefs: %v", err)
	}
	// Make a externally rooted, b not.
	tracked[a].refs = 1
	tracked[b].refs = 0

	unreachable := newListHead()
	if err := moveUnreachable(young, unreachable, tracked); err != nil {
		t.Fatalf("moveUnreachable: %v", err)
	}

	if got := len(listObjects(unreachable)); got != 0 {
		t.Fatalf("unreachable should be empty (b resurrected), got %d", got)
	}
	if got := len(listObjects(young)); got != 2 {
		t.Fatalf("young size = %d, want 2", got)
	}
}

func TestClearUnreachableMaskWipesFlag(t *testing.T) {
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	list := newListHead()
	ga := &gcHead{obj: a, flags: gcUnreachable | gcFinalized}
	gb := &gcHead{obj: b, flags: gcUnreachable}
	listAppend(ga, list)
	listAppend(gb, list)

	clearUnreachableMask(list)

	if ga.flags&gcUnreachable != 0 {
		t.Fatalf("a still has UNREACHABLE")
	}
	if ga.flags&gcFinalized == 0 {
		t.Fatalf("FINALIZED bit was clobbered")
	}
	if gb.flags&gcUnreachable != 0 {
		t.Fatalf("b still has UNREACHABLE")
	}
}

func TestUntrackTuplesDropsAtomTuple(t *testing.T) {
	// Tuple holding only a string — string is not tracked, so the
	// tuple has no tracked children and untrack_tuples should pull
	// it off the list.
	atom := objects.NewStr("x")
	tup := objects.NewTuple([]objects.Object{atom})

	young, tracked := buildCandidates([]objects.Object{tup})
	untrackTuples(young, tracked)

	if got := len(listObjects(young)); got != 0 {
		t.Fatalf("young size = %d, want 0", got)
	}
	if _, ok := tracked[tup]; ok {
		t.Fatalf("tuple still in tracked map")
	}
}

func TestUntrackTuplesKeepsContainerTuple(t *testing.T) {
	// Tuple holding a (tracked) list — must stay on young.
	inner := objects.NewList(nil)
	tup := objects.NewTuple([]objects.Object{inner})

	young, tracked := buildCandidates([]objects.Object{inner, tup})
	untrackTuples(young, tracked)

	if got := len(listObjects(young)); got != 2 {
		t.Fatalf("young size = %d, want 2 (tuple+inner kept)", got)
	}
}
