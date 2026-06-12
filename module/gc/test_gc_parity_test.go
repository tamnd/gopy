// Behavior-parity tests against cpython/Lib/test/test_gc.py. Each
// test maps 1:1 to a scenario in CPython's GCTests class. Cases that
// rely on Python-level features gopy hasn't ported yet (user classes,
// __build_class__, capsules, the trashcan) are listed at the bottom
// as Skip-with-citation rather than dropped silently, so the gap is
// visible from `go test -v`.
//
// CPython source: Lib/test/test_gc.py (3.14)

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// linkContainer appends child and adds one extra reference to stand in
// for an external owner. container.Append already owns a strong ref to
// child (spec 1727 list ownership); the second Incref here simulates a
// root outside the candidate set. Mirrors linkInto in cycle_test.go.
func linkContainer(container *objects.List, child objects.Object) {
	container.Append(child)
	objects.Incref(child)
}

// CPython: Lib/test/test_gc.py:94 GCTests.test_list
//
//	l = []; l.append(l); del l; assertEqual(gc.collect(), 1)
func TestParityList(t *testing.T) {
	defer untrackAll()
	l := objects.NewList(nil)
	l.Append(l)
	Track(l)
	objects.Decref(l) // simulate del l: Append now owns the internal ref

	if got := Collect(2); got != 1 {
		t.Fatalf("Collect() = %d, want 1 (list self-cycle)", got)
	}
}

// CPython: Lib/test/test_gc.py:101 GCTests.test_dict
//
//	d = {}; d[1] = d; del d; assertEqual(gc.collect(), 1)
func TestParityDict(t *testing.T) {
	defer untrackAll()
	d := objects.NewDict()
	if err := d.SetItem(objects.NewInt(1), d); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	Track(d)
	objects.Decref(d) // simulate del d: drop the local variable reference

	if got := Collect(2); got != 1 {
		t.Fatalf("Collect() = %d, want 1 (dict self-cycle)", got)
	}
}

// CPython: Lib/test/test_gc.py:108 GCTests.test_tuple
//
//	l = []; t = (l,); l.append(t); del t,l; assertEqual(gc.collect(), 2)
//
// Tuples are immutable so the cycle has to be closed through a list.
// In gopy the optimization in untrackTuples removes tuples whose
// children are not tracked, which keeps the candidate set tight.
func TestParityTuple(t *testing.T) {
	defer untrackAll()
	l := objects.NewList(nil)
	tup := objects.NewTuple([]objects.Object{l})
	l.Append(tup)
	Track(l)
	Track(tup)
	objects.Decref(l) // simulate del t, l: NewTuple and Append now own the refs
	objects.Decref(tup)

	if got := Collect(2); got != 2 {
		t.Fatalf("Collect() = %d, want 2 (list+tuple cycle)", got)
	}
}

// CPython: Lib/test/test_gc.py:553 GCTests.test_get_referents
//
// gc.get_referents over list/tuple/dict reports the same elements
// the container holds. Atom values report nothing.
func TestParityGetReferents(t *testing.T) {
	defer untrackAll()
	one := objects.NewInt(1)
	three := objects.NewInt(3)
	five := objects.NewInt(5)

	alist := objects.NewList([]objects.Object{one, three, five})
	got := GetReferents(alist)
	if len(got) != 3 {
		t.Fatalf("list referents len = %d, want 3", len(got))
	}

	atuple := objects.NewTuple([]objects.Object{one, three, five})
	got = GetReferents(atuple)
	if len(got) != 3 {
		t.Fatalf("tuple referents len = %d, want 3", len(got))
	}

	adict := objects.NewDict()
	if err := adict.SetItem(one, three); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if err := adict.SetItem(five, objects.NewInt(7)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	got = GetReferents(adict)
	if len(got) != 4 {
		t.Fatalf("dict referents len = %d, want 4 (2 keys + 2 values)", len(got))
	}

	// Atom: int has no tp_traverse, so referents come back empty.
	got = GetReferents(one)
	if len(got) != 0 {
		t.Fatalf("int referents len = %d, want 0", len(got))
	}
}

// CPython: Lib/test/test_gc.py:576 GCTests.test_is_tracked
//
// Atom builtins are not tracked; mutable containers are. gopy follows
// the same model: Track is called by the type constructor (or by the
// runtime when an immutable acquires a tracked child). This test
// pins the membership model on the types gopy ships today.
func TestParityIsTracked(t *testing.T) {
	defer untrackAll()
	// Atoms: never tracked.
	for _, o := range []objects.Object{
		objects.NewInt(1),
		objects.NewStr("a"),
		objects.None(),
	} {
		if IsTracked(o) {
			t.Fatalf("IsTracked(%T) = true, want false (atom)", o)
		}
	}

	// Containers: tracked once Track is called. gopy doesn't auto-
	// track on construction yet, so the test mirrors what the runtime
	// does when it adds a freshly-allocated container to a generation.
	for _, o := range []objects.Object{
		objects.NewList(nil),
		objects.NewDict(),
		objects.NewSet(),
	} {
		Track(o)
		if !IsTracked(o) {
			t.Fatalf("IsTracked(%T) = false, want true (container)", o)
		}
	}
}

// CPython: Lib/test/test_gc.py:623 GCTests.test_is_finalized
//
// CPython's test resurrects the Lazarus instance from inside __del__
// (storage.append(self)) and then checks gc.is_finalized on the
// recovered handle. The bit must persist across resurrection: the
// FINALIZED flag travels with the object even after the GC moves on.
// gopy mirrors that by leaving the entry in state.finalized intact
// when handleResurrected pulls the object back out of reclaim.
func TestParityIsFinalized(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	defer func() {
		state.mu.Lock()
		state.finalized = make(map[objects.Object]struct{})
		state.mu.Unlock()
	}()

	a := objects.NewList(nil)
	a.Append(a)
	Track(a)
	objects.Decref(a) // simulate del a: Append now owns the internal ref
	if IsFinalized(a) {
		t.Fatalf("IsFinalized before Collect = true, want false")
	}

	RegisterFinalizer(a, func(o objects.Object) { objects.Incref(o) })

	if got := Collect(2); got != 0 {
		t.Fatalf("Collect = %d, want 0 (resurrection)", got)
	}
	if !IsTracked(a) {
		t.Fatalf("Lazarus not tracked after resurrection")
	}
	if !IsFinalized(a) {
		t.Fatalf("IsFinalized after resurrection = false, want true")
	}
}

// CPython: Lib/test/test_gc.py:382 GCTests.test_get_count
//
// gen0 count grows when a fresh container is tracked; gen1 and gen2
// stay at zero until a collection promotes survivors.
func TestParityGetCount(t *testing.T) {
	defer untrackAll()
	Collect(2)
	a0, b0, c0 := GetCount()
	if b0 != 0 || c0 != 0 {
		t.Fatalf("after full Collect: gen counts = %d,%d,%d, want _,0,0", a0, b0, c0)
	}

	x := objects.NewList(nil)
	Track(x)
	defer Untrack(x)
	a1, _, _ := GetCount()
	if a1 <= a0 {
		t.Fatalf("gen0 count after Track = %d, want > %d", a1, a0)
	}
}

// CPython: Lib/test/test_gc.py:396 GCTests.test_collect_generations
//
// After Collect(0), live objects move from gen0 to gen1.
func TestParityCollectGenerations(t *testing.T) {
	defer untrackAll()
	Collect(2)

	// External rooting: an Incref keeps refs > 0 so the cycle
	// detector sees it as reachable.
	x := objects.NewList(nil)
	objects.Incref(x)
	defer objects.Decref(x)
	Track(x)
	defer Untrack(x)

	Collect(0)
	_, b, c := GetCount()
	if b != 1 {
		t.Fatalf("gen1 count after Collect(0) = %d, want 1", b)
	}
	if c != 0 {
		t.Fatalf("gen2 count after Collect(0) = %d, want 0", c)
	}
}

// CPython: Lib/test/test_gc.py:825 GCTests.test_get_stats
//
// The stats panel is a 3-element list of dicts with collected,
// collections, uncollectable. Counters increment on the right gen.
func TestParityGetStats(t *testing.T) {
	defer untrackAll()
	stats := GetStats()
	if len(stats) != 3 {
		t.Fatalf("len(GetStats()) = %d, want 3", len(stats))
	}

	old := GetStats()
	Collect(0)
	new0 := GetStats()
	if new0[0].collections != old[0].collections+1 {
		t.Fatalf("gen0 collections delta = %d, want 1",
			new0[0].collections-old[0].collections)
	}
	if new0[1].collections != old[1].collections {
		t.Fatalf("gen1 collections changed by Collect(0)")
	}
	if new0[2].collections != old[2].collections {
		t.Fatalf("gen2 collections changed by Collect(0)")
	}

	Collect(2)
	new2 := GetStats()
	if new2[2].collections != old[2].collections+1 {
		t.Fatalf("gen2 collections delta after Collect(2) = %d, want 1",
			new2[2].collections-old[2].collections)
	}
}

// CPython: Lib/test/test_gc.py:851 GCTests.test_freeze
//
// gc.freeze() moves every tracked object to the permanent generation;
// gc.unfreeze() moves them back; get_freeze_count reports the size.
func TestParityFreeze(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	Track(a)
	defer Untrack(a)

	Freeze()
	if got := GetFreezeCount(); got <= 0 {
		t.Fatalf("GetFreezeCount after Freeze = %d, want > 0", got)
	}
	Unfreeze()
	if got := GetFreezeCount(); got != 0 {
		t.Fatalf("GetFreezeCount after Unfreeze = %d, want 0", got)
	}
}

// CPython: Lib/test/test_gc.py:857 GCTests.test_get_objects
//
// gc.get_objects() returns every tracked object including the
// just-tracked self-cycle.
func TestParityGetObjects(t *testing.T) {
	defer untrackAll()
	Collect(2)
	l := objects.NewList(nil)
	l.Append(l)
	Track(l)
	defer Untrack(l)

	found := false
	for _, o := range GetObjects(-1) {
		if o == l {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("get_objects did not include the freshly tracked list")
	}
}

// CPython: Lib/test/test_gc.py:866 GCTests.test_get_objects_generations
//
// Right after Track and before Collect, the object lives on gen0.
// After a full Collect, it moves to gen2.
func TestParityGetObjectsGenerations(t *testing.T) {
	defer untrackAll()
	Collect(2)

	l := objects.NewList(nil)
	objects.Incref(l)
	defer objects.Decref(l)
	Track(l)
	defer Untrack(l)

	found := false
	for _, o := range GetObjects(0) {
		if o == l {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("freshly tracked list not on gen0")
	}

	Collect(2)
	for _, o := range GetObjects(0) {
		if o == l {
			t.Fatalf("survivor still on gen0 after full Collect")
		}
	}
}

// CPython: Lib/test/test_gc.py:890
// GCTests.test_resurrection_only_happens_once_per_object
//
// A finalizer that resurrects the object must not fire a second time
// when the object is collected for real on a later cycle. PEP 442
// requires tp_finalize to run at most once per object, even across
// resurrections.
func TestParityResurrectionOnlyHappensOnce(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	a := objects.NewList(nil)
	a.Append(a)
	Track(a)
	objects.Decref(a) // simulate del a: Append now owns the internal ref

	calls := 0
	RegisterFinalizer(a, func(o objects.Object) {
		calls++
		if calls == 1 {
			objects.Incref(o)
		}
	})

	if got := Collect(2); got != 0 {
		t.Fatalf("first Collect = %d, want 0 (resurrection)", got)
	}
	if calls != 1 {
		t.Fatalf("finalizer fired %d times after first Collect, want 1", calls)
	}

	objects.Decref(a)
	if got := Collect(2); got != 1 {
		t.Fatalf("second Collect = %d, want 1", got)
	}
	if calls != 1 {
		t.Fatalf("finalizer re-fired across resurrections: %d calls", calls)
	}
}

// CPython: Lib/test/test_gc.py:926
// GCTests.test_resurrection_is_transitive
//
// When a Lazarus object resurrects through its finalizer, anything
// it still references (its `cargo`) survives too. gopy's
// handleResurrected pulls cargo back via tp_traverse from the
// resurrected object.
func TestParityResurrectionIsTransitive(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	laz := objects.NewList(nil)
	cargo := objects.NewList(nil)
	linkContainer(laz, cargo)
	linkContainer(cargo, laz)
	Track(laz)
	Track(cargo)

	RegisterFinalizer(laz, func(o objects.Object) {
		objects.Incref(o)
	})

	if got := Collect(2); got != 0 {
		t.Fatalf("Collect = %d, want 0 (transitive resurrection)", got)
	}
	if !IsTracked(laz) {
		t.Fatalf("Lazarus not tracked after resurrection")
	}
	if !IsTracked(cargo) {
		t.Fatalf("cargo not tracked after Lazarus resurrection (transitive)")
	}
}

// CPython: Lib/test/test_gc.py:960
// GCTests.test_resurrection_does_not_block_cleanup_of_other_objects
//
// A resurrecting cycle must not stop unrelated cycles from being
// reclaimed in the same Collect, and stats must report the real
// collected count (not inflated by the resurrected objects).
func TestParityResurrectionDoesNotBlockCleanup(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()

	// Cycle A: self-loop, no finalizer. Should be reclaimed.
	a := objects.NewList(nil)
	a.Append(a)
	Track(a)
	objects.Decref(a) // simulate del a: Append now owns the internal ref

	// Cycle Z: self-loop, resurrecting finalizer. Should survive.
	z := objects.NewList(nil)
	z.Append(z)
	Track(z)
	objects.Decref(z) // simulate del z: Append now owns the internal ref
	RegisterFinalizer(z, func(o objects.Object) {
		objects.Incref(o)
	})

	old := GetStats()
	got := Collect(2)
	if got != 1 {
		t.Fatalf("Collect = %d, want 1 (a reclaimed, z resurrected)", got)
	}
	if IsTracked(a) {
		t.Fatalf("a still tracked: resurrection blocked unrelated cleanup")
	}
	if !IsTracked(z) {
		t.Fatalf("z not tracked: resurrection failed to refloat")
	}
	new2 := GetStats()
	if delta := new2[2].collected - old[2].collected; delta != 1 {
		t.Fatalf("gen2 collected delta = %d, want 1 (resurrected must not count)", delta)
	}
}

// CPython: Lib/test/test_gc.py:665 GCTests.test_bug21435
//
// Original CPython bug: a cycle of two objects where one carries a
// finalizer that mutates an attribute. The repro just checked that
// the next Collect did not crash. gopy's port asserts the same: no
// panic, finalizer fires, both objects cleaned.
func TestParityBug21435(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)
	objects.Decref(a) // simulate del a, b: Append now owns the cross refs
	objects.Decref(b)

	fired := 0
	RegisterFinalizer(b, func(_ objects.Object) {
		fired++
	})

	if got := Collect(2); got != 2 {
		t.Fatalf("Collect = %d, want 2", got)
	}
	if fired != 1 {
		t.Fatalf("finalizer fired %d times, want 1", fired)
	}
}

// The following CPython tests rely on features gopy has not ported
// yet. Each Skip carries a citation and the gating spec/task so the
// gap is discoverable from `go test -v ./gc/...`.
func TestParityGapsTracked(t *testing.T) {
	cases := []struct{ name, why string }{
		{"test_class", "Lib/test/test_gc.py:118 — needs LOAD_BUILD_CLASS (#356)"},
		{"test_newstyleclass", "Lib/test/test_gc.py:126 — needs LOAD_BUILD_CLASS (#356)"},
		{"test_instance", "Lib/test/test_gc.py:133 — needs user classes (#356)"},
		{"test_newinstance", "Lib/test/test_gc.py:142 — needs user classes (#356)"},
		{"test_method", "Lib/test/test_gc.py:166 — needs bound methods on user classes (#356)"},
		{"test_legacy_finalizer", "Lib/test/test_gc.py:177 — gopy is PEP-442 only; tp_del intentionally absent"},
		{"test_function", "Lib/test/test_gc.py:228 — needs exec() and a function object cycle (#344)"},
		{"test_trashcan", "Lib/test/test_gc.py:408 — Go GC handles deep dealloc; trashcan is intentionally absent"},
		{"test_get_referents_on_capsule", "Lib/test/test_gc.py:1087 — no PyCapsule equivalent in gopy"},
		{"test_get_objects_during_gc", "Lib/test/test_gc.py:1104 — collector reentrancy not modeled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Skipf("CPython parity gap: %s", c.why)
		})
	}
}
