package optimizer

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestWatcherInit_RegistersCallbacks confirms that WatcherInit lands
// the optimizer's dict and type watchers at the canonical IDs. Both
// halves now live on the objects-package callback tables so we drive
// real *objects.Dict and *objects.Type mutations to confirm the wires
// are hot.
func TestWatcherInit_RegistersCallbacks(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	// Dict half: a SetItem on a subscribed dict must reach the
	// reserved globals watcher and trip executor invalidation.
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
// watcher path's gate: a real Type subscribed to the reserved slot
// must trip executor invalidation through the public API when its
// version tag is invalidated.
func TestDispatchTypeMutation_FiresWatcherAndInvalidates(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	cls := objects.NewType("watched", []*objects.Type{objects.ObjectType()})
	clsPtr := unsafe.Pointer(cls)
	ExecutorDependsOn(exec, clsPtr)
	if err := objects.TypeWatch(TypeWatcherID, cls); err != nil {
		t.Fatalf("TypeWatch: %v", err)
	}

	// Force a tag allocation so InvalidateVersionTag actually has
	// something to clear; without this the version stays at 0 and a
	// real bypass would be possible.
	cls.VersionTag()

	prev := optStatExecutorsInvalidated
	cls.InvalidateVersionTag()

	if optStatExecutorsInvalidated <= prev {
		t.Errorf("type mutation should have triggered an invalidation")
	}
	if exec.VMData.Valid {
		t.Errorf("executor must be cleared after watched type mutates")
	}
	// The typeWatcherCallback unsubscribes after firing, so the bit
	// must be clear on the next pass.
	if err := objects.TypeUnwatch(TypeWatcherID, cls); err != nil {
		t.Fatalf("TypeUnwatch must be idempotent: %v", err)
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

// TestTypeAddWatcher_AssignsFreeSlot exercises the user-driven
// PyType_AddWatcher path. Slot 0 is reserved for the optimizer so
// the first user-allocated slot must be >= 1.
func TestTypeAddWatcher_AssignsFreeSlot(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp) // occupies slot 0

	id := TypeAddWatcher(interp, func(unsafe.Pointer) int {
		return 0
	})
	if id < 1 {
		t.Errorf("TypeAddWatcher should pick the first free slot >= 1 (after Init), got %d", id)
	}

	TypeClearWatcher(interp, id)
}
