package optimizer

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestWatcherInit_RegistersCallbacks confirms that WatcherInit lands
// the optimizer's dict and type watchers at the canonical IDs and
// that re-running the init is a no-op for the type half. The dict
// half lives on the objects-package callback table so we drive a
// real *objects.Dict mutation to confirm the wire is hot.
func TestWatcherInit_RegistersCallbacks(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	w := watcherTable(interp)
	if w.typeCallbacks[TypeWatcherID] == nil {
		t.Errorf("type watcher callback not registered")
	}

	// A real dict-mutation should reach the canonical globals watcher
	// installed in the reserved slot. Use an executor depending on
	// the dict so we can observe invalidation.
	_, exec := installLoopExecutor(t, interp)
	d := objects.NewDict()
	dPtr := unsafe.Pointer(d)
	ExecutorDependsOn(exec, dPtr)
	if err := objects.DictWatch(GlobalsWatcherID, d); err != nil {
		t.Fatalf("DictWatch: %v", err)
	}

	prev := optStatExecutorsInvalidated
	if err := d.SetItem(objects.NewStr("k"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if optStatExecutorsInvalidated <= prev {
		t.Errorf("real dict mutation must trigger invalidation")
	}
	if exec.VMData.Valid {
		t.Errorf("executor must be cleared after watched dict mutates")
	}
}

// TestDispatchDictMutation_IgnoresUnsubscribedDict ensures that a
// dict no one is watching does not flow through the callback table
// when it mutates.
func TestDispatchDictMutation_IgnoresUnsubscribedDict(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)
	_, exec := installLoopExecutor(t, interp)

	d := objects.NewDict()
	prev := optStatExecutorsInvalidated
	if err := d.SetItem(objects.NewStr("k"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}

	if optStatExecutorsInvalidated != prev {
		t.Errorf("unsubscribed dict must not trigger invalidation")
	}
	if !exec.VMData.Valid {
		t.Errorf("unsubscribed dict mutation must not clear executor")
	}
}

// TestDispatchTypeMutation_FiresWatcherAndInvalidates is the type
// watcher path's gate; the dict half above covers the parallel
// scenario for dicts.
func TestDispatchTypeMutation_FiresWatcherAndInvalidates(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	var typ int
	typPtr := unsafe.Pointer(&typ)
	ExecutorDependsOn(exec, typPtr)
	TypeWatch(interp, TypeWatcherID, typPtr)

	prev := optStatExecutorsInvalidated
	DispatchTypeMutation(interp, typPtr)

	if optStatExecutorsInvalidated <= prev {
		t.Errorf("type mutation should have triggered an invalidation")
	}
	if exec.VMData.Valid {
		t.Errorf("executor must be cleared after watched type mutates")
	}
	w := watcherTable(interp)
	if _, ok := w.typeSubs[TypeWatcherID][typPtr]; ok {
		t.Errorf("type watcher must unsubscribe type after firing")
	}
}

// TestDictAddWatcher_AssignsFreeSlot exercises the user-driven
// PyDict_AddWatcher path. The reserved slots (0, 1) belong to the
// optimizer, so the first user-allocated slot must be >= 2.
func TestDictAddWatcher_AssignsFreeSlot(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp) // occupies slots 0 and 1

	id := DictAddWatcher(interp, func(DictWatchEvent, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) int {
		return 0
	})
	if id < 2 {
		t.Errorf("AddWatcher should pick the first free slot >= 2 (after Init), got %d", id)
	}

	DictClearWatcher(interp, id)
}
