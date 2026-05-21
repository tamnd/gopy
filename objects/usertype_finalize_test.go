// fixupFinalize / slotTpFinalize gates. A user class that defines
// __del__ must have its Type.Finalize slot wired so the cycle
// collector dispatches to __del__ when reclaiming an unreachable
// cycle. Without the wiring, Type.Finalize stays nil even when
// __del__ is present and the user finalizer never runs.
//
// CPython: Objects/typeobject.c:10336 update_one_slot (tp_finalize entry)
// CPython: Objects/typeobject.c:10585 slot_tp_finalize

package objects

import "testing"

func TestFixupFinalizeWiresSlotWhenDunderDelPresent(t *testing.T) {
	called := 0
	delFn := NewBuiltinFunction("__del__", func(args []Object, _ map[string]Object) (Object, error) {
		called++
		return None(), nil
	})
	ns := NewDict()
	_ = ns.SetItem(NewStr("__del__"), delFn)
	C := NewUserType("C", nil, ns)
	if C.Finalize == nil {
		t.Fatal("C.Finalize should be wired to slotTpFinalize when __del__ is defined")
	}
	instance := NewInstance(C)
	C.Finalize(instance)
	if called != 1 {
		t.Errorf("user __del__ fired %d times, want 1", called)
	}
}

func TestFixupFinalizeLeavesSlotNilWithoutDunderDel(t *testing.T) {
	C := NewUserType("C", nil, NewDict())
	if C.Finalize != nil {
		t.Errorf("C.Finalize should stay nil without __del__, got non-nil")
	}
}

// A subclass inherits __del__ from its base, so the subclass's
// Finalize slot must also be wired even when the subclass body itself
// does not define __del__. Mirrors CPython, where update_one_slot
// walks the MRO.
//
// CPython: Objects/typeobject.c:10336 update_one_slot (MRO walk)
func TestFixupFinalizeInheritsDunderDelFromBase(t *testing.T) {
	called := 0
	delFn := NewBuiltinFunction("__del__", func(args []Object, _ map[string]Object) (Object, error) {
		called++
		return None(), nil
	})
	baseNS := NewDict()
	_ = baseNS.SetItem(NewStr("__del__"), delFn)
	base := NewUserType("Base", nil, baseNS)

	sub := NewUserType("Sub", []*Type{base}, NewDict())
	if sub.Finalize == nil {
		t.Fatal("Sub.Finalize should be wired through inherited __del__")
	}
	sub.Finalize(NewInstance(sub))
	if called != 1 {
		t.Errorf("inherited __del__ fired %d times, want 1", called)
	}
}
