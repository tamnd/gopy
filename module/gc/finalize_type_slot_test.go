// Type.Finalize fallback for finalizeGarbage and Finalize. The cycle
// collector first looks for an explicit RegisterFinalizer entry; if
// there is none, it dispatches through tp_finalize. User classes get
// the slot populated by slot_tp_finalize once 1672 ports the class
// machinery; built-in types fill it in directly when they need
// pre-reclaim cleanup.
//
// CPython: Objects/typeobject.c slot_tp_finalize
// CPython: Objects/object.c:489 PyObject_CallFinalizer

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestFinalizeGarbageDispatchesToTypeSlot(t *testing.T) {
	a := objects.NewList(nil)
	listType := a.Type()
	saved := listType.Finalize
	defer func() { listType.Finalize = saved }()

	calls := 0
	listType.Finalize = func(_ objects.Object) { calls++ }

	unreachable, _ := buildUnreachable([]objects.Object{a})
	finalizeGarbage(unreachable, map[objects.Object]Finalizer{}, nil)

	if calls != 1 {
		t.Errorf("type-slot finalizer fired %d times, want 1", calls)
	}
}

func TestFinalizeGarbageExplicitOverridesTypeSlot(t *testing.T) {
	a := objects.NewList(nil)
	listType := a.Type()
	saved := listType.Finalize
	defer func() { listType.Finalize = saved }()

	slotCalls := 0
	listType.Finalize = func(_ objects.Object) { slotCalls++ }

	explicitCalls := 0
	fin := map[objects.Object]Finalizer{
		a: func(_ objects.Object) { explicitCalls++ },
	}

	unreachable, _ := buildUnreachable([]objects.Object{a})
	finalizeGarbage(unreachable, fin, nil)

	if explicitCalls != 1 {
		t.Errorf("explicit finalizer fired %d times, want 1", explicitCalls)
	}
	if slotCalls != 0 {
		t.Errorf("type-slot finalizer fired %d times despite explicit entry, want 0", slotCalls)
	}
}

func TestFinalizeFunctionDispatchesToTypeSlot(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	listType := a.Type()
	saved := listType.Finalize
	defer func() { listType.Finalize = saved }()

	calls := 0
	listType.Finalize = func(_ objects.Object) { calls++ }

	Finalize(a)
	if calls != 1 {
		t.Errorf("Finalize via type slot fired %d times, want 1", calls)
	}
}

func TestCollectInvokesTypeSlotForCycle(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)
	objects.Decref(a) // simulate del a, b: Append now owns the cross refs
	objects.Decref(b)

	listType := a.Type()
	saved := listType.Finalize
	defer func() { listType.Finalize = saved }()

	calls := 0
	listType.Finalize = func(_ objects.Object) { calls++ }

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	if calls != 2 {
		t.Errorf("type-slot finalizer fired %d times, want 2 (one per cycle node)", calls)
	}
}
