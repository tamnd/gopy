package optimizer

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestBuiltinsWatcher_FiresOnMutation drives the slot-0 builtins
// watcher end-to-end: subscribe a dict to BUILTINS_WATCHER_ID, mutate
// it, and verify the interp's rare-event counter bumps + every
// dependent executor gets invalidated. Mirrors the contract
// CPython's pylifecycle.c:599 builtins_dict_watcher carries.
func TestBuiltinsWatcher_FiresOnMutation(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	d := objects.NewDict()
	dPtr := unsafe.Pointer(d)
	ExecutorDependsOn(exec, dPtr)
	if err := objects.DictWatch(BuiltinsWatcherID, d); err != nil {
		t.Fatalf("DictWatch: %v", err)
	}

	prevCounter := interp.BuiltinDictMutations
	prevInvalid := optStatExecutorsInvalidated
	if err := d.SetItem(objects.NewStr("print"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if interp.BuiltinDictMutations != prevCounter+1 {
		t.Errorf("BuiltinDictMutations=%d want %d (counter must bump on builtins mutation)",
			interp.BuiltinDictMutations, prevCounter+1)
	}
	if optStatExecutorsInvalidated <= prevInvalid {
		t.Errorf("builtins watcher must invalidate executors while under cap")
	}
	if exec.VMData.Valid {
		t.Errorf("dependent executor must be cleared on builtins mutation")
	}
}

// TestBuiltinsWatcher_StopsInvalidatingAtCap confirms the CPython
// rare-event cap: once BuiltinDictMutations exceeds the
// MaxAllowedBuiltinsModifications cap, the watcher continues to bump
// the counter but no longer invalidates executors. Matches
// pylifecycle.c:604 (`if (interp->rare_events.builtin_dict < cap)`).
func TestBuiltinsWatcher_StopsInvalidatingAtCap(t *testing.T) {
	interp := &state.Interpreter{
		JIT:                  true,
		BuiltinDictMutations: MaxAllowedBuiltinsModifications, // already at cap
	}
	WatcherInit(interp)

	_, exec := installLoopExecutor(t, interp)
	d := objects.NewDict()
	ExecutorDependsOn(exec, unsafe.Pointer(d))
	if err := objects.DictWatch(BuiltinsWatcherID, d); err != nil {
		t.Fatalf("DictWatch: %v", err)
	}

	prevInvalid := optStatExecutorsInvalidated
	if err := d.SetItem(objects.NewStr("k"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if interp.BuiltinDictMutations != MaxAllowedBuiltinsModifications+1 {
		t.Errorf("counter still bumps past cap, got %d want %d",
			interp.BuiltinDictMutations, MaxAllowedBuiltinsModifications+1)
	}
	if optStatExecutorsInvalidated != prevInvalid {
		t.Errorf("watcher must not invalidate once past cap; got %d invalidations",
			optStatExecutorsInvalidated-prevInvalid)
	}
	if !exec.VMData.Valid {
		t.Errorf("executor must survive when watcher is past cap")
	}
}

// TestEnsureBuiltinsSubscribed_StampsInterpBuiltins drives the helper
// that mirrors CPython's pylifecycle.c:1381 PyDict_Watch(0,
// interp->builtins): it both subscribes the dict to slot 0 and
// stamps interp.Builtins so the tier-2 globals folder recognizes the
// canonical builtins.
func TestEnsureBuiltinsSubscribed_StampsInterpBuiltins(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	builtins := objects.NewDict()
	EnsureBuiltinsSubscribed(interp, builtins)

	if interp.Builtins != objects.Object(builtins) {
		t.Errorf("EnsureBuiltinsSubscribed must stamp interp.Builtins")
	}

	// Driving a mutation now must reach the BUILTINS watcher; only
	// possible if EnsureBuiltinsSubscribed set the slot-0 bit on the
	// dict's watcher tag.
	prevCounter := interp.BuiltinDictMutations
	if err := builtins.SetItem(objects.NewStr("len"), objects.NewInt(0)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if interp.BuiltinDictMutations != prevCounter+1 {
		t.Errorf("builtins watcher must fire after EnsureBuiltinsSubscribed; counter=%d want %d",
			interp.BuiltinDictMutations, prevCounter+1)
	}
}

// TestEnsureBuiltinsSubscribed_DoesNotOverwriteExistingBuiltins
// confirms idempotency on the interp.Builtins stamp: a second call
// with a different dict leaves the first one intact (since the
// initial Builtins points at the canonical dict from interp init,
// later subscriptions for module-specific builtins must not steal
// the slot).
func TestEnsureBuiltinsSubscribed_DoesNotOverwriteExistingBuiltins(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)

	first := objects.NewDict()
	EnsureBuiltinsSubscribed(interp, first)
	second := objects.NewDict()
	EnsureBuiltinsSubscribed(interp, second)

	if interp.Builtins != objects.Object(first) {
		t.Errorf("EnsureBuiltinsSubscribed must not overwrite interp.Builtins on second call")
	}
}
