// Tests for the gc.garbage list and finalizer-driven resurrection.
// Pins three behaviors: DEBUG_SAVEALL appends every reclaim
// candidate to gc.garbage, the module's gc.garbage attribute and the
// runtime view share one underlying list, and a finalizer that takes
// a fresh reference to its object stops the reclaim.
//
// CPython parity points:
//   - Python/gc.c:1142 delete_garbage SAVEALL branch
//   - Python/gc.c:1261 handle_resurrected_objects
//   - Lib/test/test_gc.py GCTests.test_saveall / test_finalizer

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// resetGarbage clears the gc.garbage list back to empty so each test
// starts from a known state. The package-level state is shared across
// tests, so this runs in defer.
func resetGarbage() {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.garbage == nil {
		return
	}
	for state.garbage.Len() > 0 {
		state.garbage.SetSlice(0, state.garbage.Len(), nil)
	}
}

func TestGarbageListEmptyByDefault(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	if got := Garbage(); len(got) != 0 {
		t.Fatalf("Garbage() = %v, want empty without DEBUG_SAVEALL", got)
	}
}

func TestGarbageListPopulatedUnderSaveAll(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	defer SetDebug(0)
	if _, err := buildModule(); err != nil {
		t.Fatalf("buildModule: %v", err)
	}

	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	SetDebug(DebugSaveAll)
	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	got := Garbage()
	if len(got) != 2 {
		t.Fatalf("Garbage() length = %d, want 2", len(got))
	}
	seen := map[objects.Object]bool{}
	for _, o := range got {
		seen[o] = true
	}
	if !seen[a] || !seen[b] {
		t.Fatalf("Garbage() = %v, want both cycle members", got)
	}
}

func TestGarbageListSharedWithModuleAttr(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	defer SetDebug(0)
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	listObj, err := mod.Dict().GetItem(objects.NewStr("garbage"))
	if err != nil {
		t.Fatalf("get gc.garbage: %v", err)
	}
	moduleList, ok := listObj.(*objects.List)
	if !ok {
		t.Fatalf("gc.garbage type = %T, want *objects.List", listObj)
	}

	a := objects.NewList(nil)
	a.Append(a)
	Track(a)

	SetDebug(DebugSaveAll)
	if Collect(2) != 1 {
		t.Fatalf("self-cycle did not reclaim")
	}
	if moduleList.Len() != 1 {
		t.Fatalf("module gc.garbage length = %d, want 1", moduleList.Len())
	}
	if moduleList.Item(0) != a {
		t.Fatalf("module gc.garbage[0] = %v, want %v", moduleList.Item(0), a)
	}
}

func TestResurrectionViaFinalizerPullsObjectBack(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	// Two-node cycle. The finalizer on `a` takes a fresh reference
	// to `a` (mirroring user code stashing the object in a global
	// during __del__). a must survive; b is reachable through a's
	// tp_traverse, so it must survive too.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)
	defer Untrack(a)
	defer Untrack(b)

	finalizerCalls := 0
	RegisterFinalizer(a, func(o objects.Object) {
		finalizerCalls++
		objects.Incref(o)
	})

	got := Collect(2)
	if got != 0 {
		t.Fatalf("Collect reclaimed %d, want 0 after resurrection", got)
	}
	if finalizerCalls != 1 {
		t.Fatalf("finalizer fired %d times, want 1", finalizerCalls)
	}
	if !IsTracked(a) {
		t.Fatalf("resurrected object a no longer tracked")
	}
	if !IsTracked(b) {
		t.Fatalf("transitive survivor b no longer tracked")
	}
}

func TestResurrectionDoesNotRefireFinalizer(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	// PEP 442: tp_finalize runs at most once. After resurrection,
	// the gcFinalized bit must persist so a follow-up Collect does
	// not re-call the finalizer even when the object dies for real.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	calls := 0
	RegisterFinalizer(a, func(o objects.Object) {
		calls++
		if calls == 1 {
			objects.Incref(o)
		}
	})

	if got := Collect(2); got != 0 {
		t.Fatalf("first Collect reclaimed %d, want 0 (resurrection)", got)
	}
	if calls != 1 {
		t.Fatalf("finalizer fired %d times after first Collect, want 1", calls)
	}

	// Drop the resurrection ref so a really is dead this time.
	objects.Decref(a)
	if got := Collect(2); got != 2 {
		t.Fatalf("second Collect reclaimed %d, want 2", got)
	}
	if calls != 1 {
		t.Fatalf("finalizer re-fired after resurrection: %d calls", calls)
	}
}

func TestSaveAllSkippedWhenDisabled(t *testing.T) {
	defer untrackAll()
	defer resetGarbage()
	defer SetDebug(0)

	a := objects.NewList(nil)
	a.Append(a)
	Track(a)

	SetDebug(DebugCollectable | DebugUncollectable)
	if Collect(2) != 1 {
		t.Fatalf("self-cycle did not reclaim")
	}
	if got := Garbage(); len(got) != 0 {
		t.Fatalf("Garbage() = %v, want empty without DEBUG_SAVEALL", got)
	}
}
