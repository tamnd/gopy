// Direct coverage for __slots__ on user-defined types: the descriptor
// install pass, the per-instance storage layout, and the no-__dict__
// invariant when __slots__ omits "__dict__". The vm-level test suite
// (vm/slots_test.go) drives the full source path.

package objects

import (
	"strings"
	"testing"
)

// nsWithSlots builds a class body namespace that declares
// __slots__ = (slots...) and stores the extra (key, value) pairs.
func nsWithSlots(slots []string, extra map[string]Object) *Dict {
	ns := NewDict()
	items := make([]Object, len(slots))
	for i, s := range slots {
		items[i] = NewStr(s)
	}
	_ = ns.SetItem(NewStr("__slots__"), NewTuple(items))
	for k, v := range extra {
		_ = ns.SetItem(NewStr(k), v)
	}
	return ns
}

func TestSlotsAssignAndRead(t *testing.T) {
	cls := NewUserType("C", nil, nsWithSlots([]string{"x", "y"}, nil))
	if cls.HasDict {
		t.Fatalf("C.HasDict = true, want false (no __dict__ in __slots__)")
	}
	if got, want := cls.Slots, []string{"x", "y"}; !slicesEqual(got, want) {
		t.Fatalf("C.Slots = %v, want %v", got, want)
	}

	inst := NewInstance(cls)
	if err := SetAttr(inst, NewStr("x"), NewInt(7)); err != nil {
		t.Fatalf("set x: %v", err)
	}
	got, err := GetAttr(inst, NewStr("x"))
	if err != nil {
		t.Fatalf("get x: %v", err)
	}
	n, _ := got.(*Int).Int64()
	if n != 7 {
		t.Fatalf("x = %d, want 7", n)
	}
}

func TestSlotsRejectExtraAttribute(t *testing.T) {
	cls := NewUserType("C", nil, nsWithSlots([]string{"x"}, nil))
	inst := NewInstance(cls)
	err := SetAttr(inst, NewStr("y"), NewInt(1))
	if err == nil || !strings.Contains(err.Error(), "AttributeError") {
		t.Fatalf("set y: err = %v, want AttributeError", err)
	}
}

func TestSlotsUnsetReadRaises(t *testing.T) {
	cls := NewUserType("C", nil, nsWithSlots([]string{"x"}, nil))
	inst := NewInstance(cls)
	_, err := GetAttr(inst, NewStr("x"))
	if err == nil || !strings.Contains(err.Error(), "AttributeError") {
		t.Fatalf("get unset x: err = %v, want AttributeError", err)
	}
}

func TestSlotsExplicitDictAllowed(t *testing.T) {
	cls := NewUserType("C", nil, nsWithSlots([]string{"x", "__dict__"}, nil))
	if !cls.HasDict {
		t.Fatalf("C.HasDict = false, want true (__dict__ in __slots__)")
	}
	if got, want := cls.Slots, []string{"x"}; !slicesEqual(got, want) {
		t.Fatalf("C.Slots = %v, want %v (dict filtered)", got, want)
	}
	inst := NewInstance(cls)
	if err := SetAttr(inst, NewStr("y"), NewInt(2)); err != nil {
		t.Fatalf("set y: %v (should land in __dict__)", err)
	}
	got, err := GetAttr(inst, NewStr("y"))
	if err != nil {
		t.Fatalf("get y: %v", err)
	}
	n, _ := got.(*Int).Int64()
	if n != 2 {
		t.Fatalf("y = %d, want 2", n)
	}
}

func TestSlotsConflictWithClassVariable(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on slot/class-variable conflict")
		}
		s, _ := r.(error)
		if s == nil || !strings.Contains(s.Error(), "ValueError") {
			t.Fatalf("panic = %v, want ValueError", r)
		}
	}()
	NewUserType("C", nil, nsWithSlots([]string{"x"}, map[string]Object{
		"x": NewInt(1),
	}))
}

func TestSlotsRejectNonIdentifier(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on bad slot name")
		}
		s, _ := r.(error)
		if s == nil || !strings.Contains(s.Error(), "TypeError") {
			t.Fatalf("panic = %v, want TypeError", r)
		}
	}()
	NewUserType("C", nil, nsWithSlots([]string{"1bad"}, nil))
}

func TestSlotsStringAccepted(t *testing.T) {
	// __slots__ = "x" should be treated as a single-name iterable.
	ns := NewDict()
	_ = ns.SetItem(NewStr("__slots__"), NewStr("x"))
	cls := NewUserType("C", nil, ns)
	if got, want := cls.Slots, []string{"x"}; !slicesEqual(got, want) {
		t.Fatalf("C.Slots = %v, want %v", got, want)
	}
	if cls.HasDict {
		t.Fatalf("C.HasDict = true, want false")
	}
}

func TestSlotsInheritDictFromBase(t *testing.T) {
	// Base has __dict__ (no __slots__); child declares __slots__ but
	// still inherits the base's per-instance dict.
	base := NewUserType("B", nil, NewDict())
	if !base.HasDict {
		t.Fatalf("base.HasDict = false, want true")
	}
	child := NewUserType("C", []*Type{base}, nsWithSlots([]string{"x"}, nil))
	if !child.HasDict {
		t.Fatalf("child.HasDict = false, want true (inherited from B)")
	}
	inst := NewInstance(child)
	if err := SetAttr(inst, NewStr("y"), NewInt(3)); err != nil {
		t.Fatalf("set y: %v (should land in inherited __dict__)", err)
	}
}

// TestSlotsInheritFromParent covers the CPython behavior where a
// subclass with an empty __slots__ tuple inherits its parent's slot
// names. Without the layout-base walk in installSlots / NewInstance
// the MemberDescr lookup finds the parent's MemberDescr at index 0
// but inst.slots has length 0, so the store raises AttributeError.
//
// CPython: Objects/typeobject.c:4404 type_new_descriptors (slotoffset
// = ctx->base->tp_basicsize)
func TestSlotsInheritFromParent(t *testing.T) {
	parent := NewUserType("A", nil, nsWithSlots([]string{"_x"}, nil))
	child := NewUserType("B", []*Type{parent}, nsWithSlots(nil, nil))
	if got, want := child.SlotsBase, 1; got != want {
		t.Fatalf("child.SlotsBase = %d, want %d", got, want)
	}
	inst := NewInstance(child)
	if err := SetAttr(inst, NewStr("_x"), NewInt(11)); err != nil {
		t.Fatalf("set _x on subclass: %v", err)
	}
	got, err := GetAttr(inst, NewStr("_x"))
	if err != nil {
		t.Fatalf("get _x: %v", err)
	}
	n, _ := got.(*Int).Int64()
	if n != 11 {
		t.Fatalf("_x = %d, want 11", n)
	}
}

// TestSlotsInheritAndExtend confirms that a subclass declaring its
// own __slots__ keeps the parent's slot accessible (at the inherited
// index) and lands its own slot above the parent's range.
func TestSlotsInheritAndExtend(t *testing.T) {
	parent := NewUserType("A", nil, nsWithSlots([]string{"_x"}, nil))
	child := NewUserType("B", []*Type{parent}, nsWithSlots([]string{"_y"}, nil))
	inst := NewInstance(child)
	if err := SetAttr(inst, NewStr("_x"), NewInt(1)); err != nil {
		t.Fatalf("set _x: %v", err)
	}
	if err := SetAttr(inst, NewStr("_y"), NewInt(2)); err != nil {
		t.Fatalf("set _y: %v", err)
	}
	gx, _ := GetAttr(inst, NewStr("_x"))
	gy, _ := GetAttr(inst, NewStr("_y"))
	if n, _ := gx.(*Int).Int64(); n != 1 {
		t.Fatalf("_x = %d, want 1", n)
	}
	if n, _ := gy.(*Int).Int64(); n != 2 {
		t.Fatalf("_y = %d, want 2", n)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
