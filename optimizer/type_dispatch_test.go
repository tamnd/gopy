package optimizer

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestDictMutationHook_FiresWatcherFromMutate confirms that any
// objects.Dict mutation (insert / delete / resize) drives
// DispatchDictMutation through the watcher and clears every
// executor whose dependency bloom names the mutated dict.
func TestDictMutationHook_FiresWatcherFromMutate(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	d := objects.NewDict()
	dPtr := unsafe.Pointer(d)
	ExecutorDependsOn(exec, dPtr)
	DictWatch(interp, GlobalsWatcherID, dPtr)

	prev := optStatExecutorsInvalidated
	if err := d.SetItem(objects.NewStr("k"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if optStatExecutorsInvalidated <= prev {
		t.Errorf("dict mutation should drive ExecutorsInvalidateDependency through the watcher")
	}
	if exec.VMData.Valid {
		t.Errorf("executor must be cleared after the dependent dict mutates")
	}
}

// TestTypeModifiedHook_FiresWatcherFromInvalidate confirms that
// objects.Type.InvalidateVersionTag dispatches through the optimizer
// watcher and clears any executor whose dependency bloom names the
// mutated type.
func TestTypeModifiedHook_FiresWatcherFromInvalidate(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	cls := objects.NewType("watched", []*objects.Type{objects.ObjectType()})
	clsPtr := unsafe.Pointer(cls)
	ExecutorDependsOn(exec, clsPtr)
	TypeWatch(interp, TypeWatcherID, clsPtr)

	// Force a tag allocation so InvalidateVersionTag actually has
	// something to clear; without this the version stays at 0 and a
	// real bypass would be possible.
	cls.VersionTag()

	prev := optStatExecutorsInvalidated
	cls.InvalidateVersionTag()

	if optStatExecutorsInvalidated <= prev {
		t.Errorf("InvalidateVersionTag should drive ExecutorsInvalidateDependency through the watcher")
	}
	if exec.VMData.Valid {
		t.Errorf("executor must be cleared after the dependent type mutates")
	}
}
