// User __del__ end-to-end gates. P13.2 wired Type.Finalize for user
// classes; these tests exercise the integration path: build a user
// class with __del__, drop the instance into an unreachable cycle,
// run Collect, and verify the user method fires. The resurrection
// case proves __del__ can stash a reference to refloat the object,
// mirroring CPython's PEP-442 semantics.
//
// CPython: Lib/test/test_gc.py GCTests.test_finalizer
// CPython: Lib/test/test_gc.py:890 GCTests.test_resurrection_only_happens_once_per_object

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// buildUserClassWithDel constructs a user class whose __del__ slot is
// the supplied Go closure wrapped in a BuiltinFunction. The returned
// type runs through fixupSlotDispatchers, so Type.Finalize is wired
// to slotTpFinalize.
func buildUserClassWithDel(name string, del func(self objects.Object)) *objects.Type {
	delFn := objects.NewBuiltinFunction("__del__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) > 0 {
			del(args[0])
		}
		return objects.None(), nil
	})
	ns := objects.NewDict()
	_ = ns.SetItem(objects.NewStr("__del__"), delFn)
	return objects.NewUserType(name, nil, ns)
}

// A user class with __del__ has its method called when the cycle
// collector reclaims an unreachable instance. Without P13.2's
// fixupFinalize wiring, the method never ran.
func TestUserDelFiresDuringCycleCollect(t *testing.T) {
	defer untrackAll()
	calls := 0
	C := buildUserClassWithDel("C", func(_ objects.Object) { calls++ })

	a := objects.NewInstance(C)
	b := objects.NewInstance(C)
	// a -> b -> a inside dicts; Instance attribute storage uses dict,
	// and tp_traverse on Instance must walk the dict for the cycle
	// collector to see the back-edge.
	_ = a.Dict().SetItem(objects.NewStr("ref"), b)
	_ = b.Dict().SetItem(objects.NewStr("ref"), a)
	Track(a)
	Track(b)

	if got := Collect(2); got != 2 {
		t.Fatalf("Collect = %d, want 2", got)
	}
	if calls != 2 {
		t.Errorf("user __del__ fired %d times, want 2", calls)
	}
}

// A user __del__ that stashes self via Incref keeps the instance
// alive: handleResurrected re-runs deduce_unreachable on the
// post-finalize list and pulls the resurrected entry back out of the
// reclaim path. The gcFinalized bit must persist so a follow-up
// Collect does not run __del__ a second time (PEP 442).
func TestUserDelResurrectionSurvivesCycleCollect(t *testing.T) {
	defer untrackAll()
	calls := 0
	C := buildUserClassWithDel("C", func(self objects.Object) {
		calls++
		if calls == 1 {
			objects.Incref(self)
		}
	})

	a := objects.NewInstance(C)
	_ = a.Dict().SetItem(objects.NewStr("self"), a)
	Track(a)

	if got := Collect(2); got != 0 {
		t.Fatalf("Collect = %d, want 0 (resurrection)", got)
	}
	if calls != 1 {
		t.Fatalf("user __del__ fired %d times, want 1", calls)
	}
	if !IsTracked(a) {
		t.Fatalf("resurrected instance dropped from tracked map")
	}
	if !IsFinalized(a) {
		t.Fatalf("IsFinalized after resurrection = false, want true")
	}

	// Drop the resurrection ref and collect again. The instance must
	// die this time, and __del__ must NOT re-fire.
	objects.Decref(a)
	if got := Collect(2); got != 1 {
		t.Fatalf("second Collect = %d, want 1", got)
	}
	if calls != 1 {
		t.Errorf("user __del__ re-fired across resurrections: %d calls", calls)
	}
}
