package optimizer

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/state"
)

// TestWatcherInit_RegistersCallbacks confirms that WatcherInit lands
// the optimizer's dict and type watchers at the canonical IDs and
// that re-running the init is a no-op.
func TestWatcherInit_RegistersCallbacks(t *testing.T) {
	interp := &state.Interpreter{}
	WatcherInit(interp)

	w := watcherTable(interp)
	if w.dictCallbacks[GlobalsWatcherID] == nil {
		t.Errorf("globals watcher callback not registered")
	}
	if w.dictCallbacks[BuiltinsWatcherID] == nil {
		t.Errorf("builtins watcher callback not registered")
	}
	if w.typeCallbacks[TypeWatcherID] == nil {
		t.Errorf("type watcher callback not registered")
	}

	WatcherInit(interp)
	if w.dictCallbacks[GlobalsWatcherID] == nil {
		t.Errorf("re-init dropped the globals watcher")
	}
}

// TestDispatchDictMutation_FiresWatcherAndInvalidates wires the
// watcher up against an executor that depends on a synthetic dict,
// fires DispatchDictMutation, and asserts the executor is invalidated.
func TestDispatchDictMutation_FiresWatcherAndInvalidates(t *testing.T) {
	interp := &state.Interpreter{}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	var dict int
	dictPtr := unsafe.Pointer(&dict)
	ExecutorDependsOn(exec, dictPtr)
	DictWatch(interp, GlobalsWatcherID, dictPtr)

	prev := optStatExecutorsInvalidated
	DispatchDictMutation(interp, DictEventModified, dictPtr, nil, nil)

	if optStatExecutorsInvalidated <= prev {
		t.Errorf("dict mutation should have triggered an invalidation")
	}
	if exec.VMData.Valid {
		t.Errorf("executor must be cleared after watched dict mutates")
	}

	// The callback unsubscribes the dict; firing again must not
	// invalidate (and there's nothing left to invalidate).
	w := watcherTable(interp)
	if _, ok := w.dictSubs[GlobalsWatcherID][dictPtr]; ok {
		t.Errorf("globals watcher must unsubscribe dict after firing")
	}
}

// TestDispatchDictMutation_IgnoresUnsubscribedDict ensures the
// dispatch path is silent for dicts that were never registered.
func TestDispatchDictMutation_IgnoresUnsubscribedDict(t *testing.T) {
	interp := &state.Interpreter{}
	WatcherInit(interp)
	_, exec := installLoopExecutor(t, interp)

	var dict int
	prev := optStatExecutorsInvalidated
	DispatchDictMutation(interp, DictEventModified, unsafe.Pointer(&dict), nil, nil)

	if optStatExecutorsInvalidated != prev {
		t.Errorf("unsubscribed dict must not trigger invalidation")
	}
	if !exec.VMData.Valid {
		t.Errorf("unsubscribed dict mutation must not clear executor")
	}
}

// TestDispatchTypeMutation_FiresWatcherAndInvalidates is the type
// watcher analog of the dict test.
func TestDispatchTypeMutation_FiresWatcherAndInvalidates(t *testing.T) {
	interp := &state.Interpreter{}
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

// TestDictAddWatcher_AssignsFreeSlot covers the user-driven
// PyDict_AddWatcher API (used outside the optimizer's own canonical
// callbacks).
func TestDictAddWatcher_AssignsFreeSlot(t *testing.T) {
	interp := &state.Interpreter{}
	WatcherInit(interp) // occupies slots 0 and 1

	id := DictAddWatcher(interp, func(DictWatchEvent, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) int {
		return 0
	})
	if id < 2 {
		t.Errorf("AddWatcher should pick the first free slot >= 2 (after Init), got %d", id)
	}

	DictClearWatcher(interp, id)
	w := watcherTable(interp)
	if w.dictCallbacks[id] != nil {
		t.Errorf("ClearWatcher must release the slot")
	}
}
