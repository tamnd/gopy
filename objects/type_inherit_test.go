package objects

import "testing"

// TestInheritSlots_FullMROCopiesEveryAncestor confirms that the
// inherit_slots port walks the full MRO and not just direct bases.
// A slot defined two hops up (GrandBase) must reach the bottom of a
// three-level hierarchy when none of the intermediate classes
// re-defined it.
//
// CPython: Objects/typeobject.c:8712 type_ready_inherit (MRO walk)
func TestInheritSlots_FullMROCopiesEveryAncestor(t *testing.T) {
	addCalled := 0
	grandBase := NewType("GrandBase", []*Type{ObjectType()})
	grandBase.Number = &NumberMethods{
		Add: func(a, b Object) (Object, error) {
			addCalled++
			return NewInt(42), nil
		},
	}

	middle := NewType("Middle", []*Type{grandBase})
	// Middle deliberately leaves Number nil; inheritance must reach
	// it through the MRO walk inside fixupSlotDispatchers.

	leaf := NewUserType("Leaf", []*Type{middle}, NewDict())

	if leaf.Number == nil {
		t.Fatalf("Leaf must inherit a Number bundle from GrandBase")
	}
	if leaf.Number.Add == nil {
		t.Fatalf("Leaf.Number.Add must be inherited, got nil")
	}
	out, err := leaf.Number.Add(NewInt(1), NewInt(2))
	if err != nil {
		t.Fatalf("inherited Add failed: %v", err)
	}
	v, ok := out.(*Int)
	if !ok {
		t.Fatalf("inherited Add returned %T, want *Int", out)
	}
	if n, ok2 := v.Int64(); !ok2 || n != 42 {
		t.Errorf("inherited Add returned %v, want Int(42)", out)
	}
	if addCalled != 1 {
		t.Errorf("inherited Add fired %d times, want 1", addCalled)
	}
}

// TestInheritSlots_FillsNilFieldsInOwnBundle covers the case where
// the subclass declared its own dunder (allocating an empty bundle)
// and still needs to inherit the other slots from its base. The old
// inheritProtocolTables path returned early when the subclass's
// bundle was non-nil, dropping every other slot on the floor.
//
// CPython: Objects/typeobject.c:8258 COPYNUM (per-slot fallthrough)
func TestInheritSlots_FillsNilFieldsInOwnBundle(t *testing.T) {
	base := NewType("Base", []*Type{ObjectType()})
	baseAdd := func(a, b Object) (Object, error) { return NewInt(1), nil }
	baseMul := func(a, b Object) (Object, error) { return NewInt(2), nil }
	base.Number = &NumberMethods{Add: baseAdd, Multiply: baseMul}

	// Pre-allocate Leaf with only Subtract: simulate fixupRichCmpAndBool
	// or another fixup pass that allocates an empty bundle before
	// inheritSlotsAllMRO runs. The MRO walk must fill in Add + Multiply
	// from base while leaving Subtract alone.
	leaf := NewType("Leaf", []*Type{base})
	leafSub := func(a, b Object) (Object, error) { return NewInt(3), nil }
	leaf.Number = &NumberMethods{Subtract: leafSub}

	inheritSlotsAllMRO(leaf)

	if leaf.Number.Add == nil || leaf.Number.Multiply == nil {
		t.Fatalf("nil slots in own bundle must be filled from base")
	}
	if leaf.Number.Subtract == nil {
		t.Fatalf("inheritance must not overwrite own slots")
	}
	out, _ := leaf.Number.Subtract(NewInt(0), NewInt(0))
	if v, _ := out.(*Int).Int64(); v != 3 {
		t.Errorf("Subtract overwritten: got %d, want 3", v)
	}
}

// TestInheritSlots_SubclassBundleIsIndependent verifies a subclass's
// per-type fixup (e.g. installing slotNbBool when __bool__ exists)
// does not bleed into the base's slot table. Sharing the bundle
// pointer would cause this; copying it (current behaviour) does
// not.
//
// CPython: Objects/typeobject.c:8685 type_ready_inherit_as_structs
// (CPython shares because slot_tp_* dispatchers do not write back
// into the bundle; gopy's fixup installs per-type slot dispatchers
// directly so the bundle must be unique per subclass.)
func TestInheritSlots_SubclassBundleIsIndependent(t *testing.T) {
	base := NewType("Base", []*Type{ObjectType()})
	base.Number = &NumberMethods{
		Bool: func(o Object) (bool, error) { return false, nil },
	}

	leaf := NewType("Leaf", []*Type{base})
	inheritSlotsAllMRO(leaf)

	// Overwrite Leaf's Bool. Base must keep its original.
	leaf.Number.Bool = func(o Object) (bool, error) { return true, nil }

	got, _ := base.Number.Bool(nil)
	if got {
		t.Errorf("base bundle mutated by leaf override")
	}
}

// TestInheritSlots_TypeOverridesHashSkipsPair confirms the
// richcompare/hash inheritance dance: when the namespace defines
// __hash__, neither slot is inherited from base. CPython's
// overrides_hash returns true and the if-guard at typeobject.c:8366
// skips the COPYSLOT pair.
//
// CPython: Objects/typeobject.c:8366 (richcompare/hash dance)
func TestInheritSlots_TypeOverridesHashSkipsPair(t *testing.T) {
	base := NewType("HashBase", []*Type{ObjectType()})
	base.Hash = func(o Object) (int64, error) { return 99, nil }
	base.RichCmp = func(a, b Object, op CompareOp) (Object, error) {
		return None(), nil
	}

	// Build a Leaf whose namespace declares __hash__. NewUserType
	// drives fixupSlotDispatchers which runs inherit_slots before the
	// fixup writes; the inheritance must skip Hash/RichCmp because the
	// override flag is set.
	ns := NewDict()
	_ = ns.SetItem(NewStr("__hash__"), None())
	leaf := NewUserType("HashLeaf", []*Type{base}, ns)

	// fixupHashAndIter sees __hash__ in the namespace and installs the
	// generic slot dispatcher. What we test here is that base's C-level
	// Hash function does NOT leak through: lookupDunderCallable for
	// __hash__=None is false (None signals unhashable), so the fixup
	// installs identityHash. The inherit step must have left t.Hash
	// nil for that branch to fire.
	if leaf.Hash == nil {
		t.Fatalf("Hash must be installed by the fixup pass, got nil")
	}
	v, _ := leaf.Hash(NewInt(7))
	if v == 99 {
		t.Errorf("base Hash leaked through despite __hash__ override")
	}
}
