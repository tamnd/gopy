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
