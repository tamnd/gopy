// Phase 7 gates for spec 1704: inherit_slots audit. A user class that
// subclasses a built-in must pick up the base type's C-level slot
// table so methods like __len__, __getitem__, and arithmetic dunders
// keep working without a per-subclass fixup pass.
//
// TpNew inheritance is exercised end-to-end by the gate scripts that
// run `gopy -c 'class L(list): ...'`; here we pin the in-package slot
// inheritance only, since ListType.TpNew is wired by builtins/ which
// the objects unit tests cannot import (it would cycle).
//
// CPython: Objects/typeobject.c:7521 inherit_slots

package objects

import "testing"

// A list subclass inherits the Sequence slot table and Iter from the
// base, so len() / iter() route through the inherited pointers.
func TestUserListSubclassInheritsSlots(t *testing.T) {
	L := NewUserType("L", []*Type{ListType}, NewDict())
	if L.Sequence == nil || L.Sequence.Length == nil {
		t.Fatal("L.Sequence.Length should be inherited from list")
	}
	if L.Iter == nil {
		t.Fatal("L.Iter should be inherited from list")
	}
}

// A user class subclass inherits the inherited slot tables as fresh
// copies, not pointer aliases, so per-subclass fixup writes never
// mutate the base.
func TestSubclassSlotTableIsCopyNotAlias(t *testing.T) {
	baseLen := ListType.Sequence.Length
	L := NewUserType("L", []*Type{ListType}, NewDict())
	if L.Sequence == ListType.Sequence {
		t.Fatal("L.Sequence should be a copy, not aliased to ListType.Sequence")
	}
	if ListType.Sequence.Length == nil {
		t.Fatal("base ListType.Sequence.Length got cleared")
	}
	if fnPtr(baseLen) != fnPtr(ListType.Sequence.Length) {
		t.Errorf("base ListType.Sequence.Length was mutated by subclass fixup")
	}
}

// A dict subclass already had TpNew wiring before this phase (it's set
// directly in objects/dict.go); the inherit pass must not regress that.
func TestUserDictSubclassKeepsTpNew(t *testing.T) {
	D := NewUserType("D", []*Type{DictType}, NewDict())
	if D.TpNew == nil {
		t.Fatal("D.TpNew should be inherited from dict")
	}
	if D.Mapping == nil || D.Mapping.GetItem == nil {
		t.Fatal("D.Mapping.GetItem should be inherited from dict")
	}
}
