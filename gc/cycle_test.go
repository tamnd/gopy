// v0.10 release-gate tests for the cycle collector. Each scenario
// is shaped after the corresponding case in cpython/Lib/test/test_gc.py
// and pins the observable behavior we want before declaring v0.10
// done. Subset coverage: cycle reclaim, finalizer fires once,
// transitive reclaim through a chain. Weakref interaction lands with
// 1613-I.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestCycleSelfReferenceReclaimed(t *testing.T) {
	defer untrackAll()
	// l holds itself: a one-node cycle. After Collect the gcHead
	// must be off all generation lists and out of the tracked map.
	l := objects.NewList(nil)
	l.Append(l)
	Track(l)

	collected := Collect(2)
	if collected < 1 {
		t.Fatalf("Collect reclaimed %d, want >= 1", collected)
	}
	if IsTracked(l) {
		t.Fatalf("self-cycle still tracked after Collect")
	}
}

func TestCycleTwoNodesReclaimed(t *testing.T) {
	defer untrackAll()
	// Classic cpython/Lib/test/test_gc.py shape: two lists, a -> b,
	// b -> a, no external owner. Both nodes should be reclaimed in
	// one Collect.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	collected := Collect(2)
	if collected != 2 {
		t.Fatalf("Collect reclaimed %d, want 2", collected)
	}
	if IsTracked(a) || IsTracked(b) {
		t.Fatalf("cycle members still tracked: a=%v b=%v",
			IsTracked(a), IsTracked(b))
	}
}

func TestCycleFinalizerFiresOnce(t *testing.T) {
	defer untrackAll()
	// Each node gets a finalizer. After Collect, every finalizer must
	// have fired exactly once. A second Collect must not invoke them
	// again.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	calls := map[objects.Object]int{}
	RegisterFinalizer(a, func(o objects.Object) { calls[o]++ })
	RegisterFinalizer(b, func(o objects.Object) { calls[o]++ })

	if Collect(2) != 2 {
		t.Fatalf("first Collect did not reclaim cycle")
	}
	if calls[a] != 1 || calls[b] != 1 {
		t.Fatalf("finalizers fired %v, want each once", calls)
	}

	// A redundant Collect must not re-fire anything.
	if got := Collect(2); got != 0 {
		t.Fatalf("second Collect = %d, want 0", got)
	}
	if calls[a] != 1 || calls[b] != 1 {
		t.Fatalf("finalizers re-fired after second Collect: %v", calls)
	}
}

// linkInto wires container.Append plus the matching refcount bump
// that CPython's container ops apply. Container Append in
// objects/list.go does not yet maintain refcounts (that lands with
// 1679-A); these tests bump it explicitly so the cycle algorithm
// has accurate inputs.
func linkInto(container *objects.List, child objects.Object) {
	container.Append(child)
	objects.Incref(child)
}

func TestCycleChainReachableSurvives(t *testing.T) {
	defer untrackAll()
	// rooted -> [a, b], plus a/b form a cycle. External root bumps
	// rooted's refcount; nothing in the chain should be reclaimed.
	rooted := objects.NewList(nil)
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	linkInto(a, b)
	linkInto(b, a)
	linkInto(rooted, a)
	linkInto(rooted, b)
	objects.Incref(rooted)
	defer func() {
		// Manual cleanup of the simulated container refs so the
		// suite-wide refcount accounting stays balanced.
		objects.Decref(rooted)
		objects.Decref(a)
		objects.Decref(b)
		objects.Decref(a)
		objects.Decref(b)
	}()

	Track(rooted)
	Track(a)
	Track(b)

	if got := Collect(2); got != 0 {
		t.Fatalf("Collect reclaimed %d, want 0 (rooted chain)", got)
	}
	if !IsTracked(rooted) || !IsTracked(a) || !IsTracked(b) {
		t.Fatalf("rooted chain dropped: rooted=%v a=%v b=%v",
			IsTracked(rooted), IsTracked(a), IsTracked(b))
	}
}
