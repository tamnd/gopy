// Tests for finalize_garbage and reclaim_unreachable. Each scenario
// builds the unreachable list directly so the test pins the post-
// move_unreachable behavior in isolation.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func buildUnreachable(objs []objects.Object) (list *gcHead, tracked map[objects.Object]*gcHead) {
	list = newListHead()
	tracked = make(map[objects.Object]*gcHead, len(objs))
	for _, o := range objs {
		g := &gcHead{obj: o, flags: gcCollecting | gcUnreachable}
		listAppend(g, list)
		tracked[o] = g
	}
	return list, tracked
}

func TestFinalizeGarbageRunsEachFinalizerOnce(t *testing.T) {
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	unreachable, _ := buildUnreachable([]objects.Object{a, b})

	calls := map[objects.Object]int{}
	fin := map[objects.Object]Finalizer{
		a: func(o objects.Object) { calls[o]++ },
		b: func(o objects.Object) { calls[o]++ },
	}

	finalizeGarbage(unreachable, fin, nil)

	if calls[a] != 1 || calls[b] != 1 {
		t.Fatalf("calls=%v, want each fired once", calls)
	}
	if _, ok := fin[a]; ok {
		t.Fatalf("finalizer for a not cleared")
	}
	if _, ok := fin[b]; ok {
		t.Fatalf("finalizer for b not cleared")
	}
}

func TestFinalizeGarbageSkipsFinalizedFlag(t *testing.T) {
	// An object that already carries the FINALIZED flag (because a
	// previous collection ran its finalizer) must not be invoked a
	// second time, even if a Finalizer entry somehow got
	// reregistered.
	a := objects.NewList(nil)
	unreachable, _ := buildUnreachable([]objects.Object{a})
	for g := unreachable.next; g != unreachable; g = g.next {
		g.flags |= gcFinalized
	}

	calls := 0
	fin := map[objects.Object]Finalizer{
		a: func(o objects.Object) { calls++ },
	}

	finalizeGarbage(unreachable, fin, nil)

	if calls != 0 {
		t.Fatalf("finalizer fired despite FINALIZED bit: %d", calls)
	}
	if _, ok := fin[a]; !ok {
		t.Fatalf("finalizer entry should not have been consumed")
	}
}

func TestFinalizeGarbageNoFinalizerIsNoop(t *testing.T) {
	a := objects.NewList(nil)
	unreachable, _ := buildUnreachable([]objects.Object{a})
	fin := map[objects.Object]Finalizer{}

	finalizeGarbage(unreachable, fin, nil)

	if unreachable.next == unreachable {
		t.Fatalf("list should still hold the object")
	}
	if unreachable.next.flags&gcFinalized == 0 {
		t.Fatalf("FINALIZED bit should be set even without a finalizer")
	}
}

func TestReclaimUnreachableEmptiesListAndTracked(t *testing.T) {
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	unreachable, tracked := buildUnreachable([]objects.Object{a, b})

	reclaimUnreachable(unreachable, tracked, nil)

	if !listIsEmpty(unreachable) {
		t.Fatalf("unreachable not emptied")
	}
	if len(tracked) != 0 {
		t.Fatalf("tracked size = %d, want 0", len(tracked))
	}
}
