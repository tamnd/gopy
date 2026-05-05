package objects

import (
	"strings"
	"testing"
)

// TestGetAttrRoutesThroughGetattro verifies the protocol's GetAttr
// dispatches through tp_getattro when set.
func TestGetAttrRoutesThroughGetattro(t *testing.T) {
	tp := NewType("attrholder", []*Type{ObjectType()})
	tp.Getattro = func(o Object, name Object) (Object, error) {
		if attrNameStr(name) == "x" {
			return NewInt(7), nil
		}
		return nil, &dummyAttrErr{"missing"}
	}
	holder := newDummyHolder(tp)

	got, err := GetAttr(holder, NewStr("x"))
	if err != nil {
		t.Fatalf("GetAttr: %v", err)
	}
	if v, _ := got.(*Int).Int64(); v != 7 {
		t.Errorf("got %v, want 7", v)
	}
}

// TestGetAttrNoSlotIsAttributeError verifies that a type with no
// tp_getattro produces the AttributeError text PyObject_GetAttr
// returns.
func TestGetAttrNoSlotIsAttributeError(t *testing.T) {
	tp := NewType("plain", []*Type{ObjectType()})
	holder := newDummyHolder(tp)

	_, err := GetAttr(holder, NewStr("missing"))
	if err == nil || !strings.Contains(err.Error(), "AttributeError") {
		t.Fatalf("err = %v, want AttributeError", err)
	}
}

// TestGetAttrRejectsNonStringName mirrors the PyUnicode_Check guard
// at the top of PyObject_GetAttr.
func TestGetAttrRejectsNonStringName(t *testing.T) {
	tp := NewType("plain", []*Type{ObjectType()})
	holder := newDummyHolder(tp)

	_, err := GetAttr(holder, NewInt(0))
	if err == nil || !strings.Contains(err.Error(), "attribute name must be string") {
		t.Fatalf("err = %v, want type-error on non-string name", err)
	}
}

// TestSetAttrRoutesThroughSetattro covers the Set/Del paths: set
// passes value, delete passes nil.
func TestSetAttrRoutesThroughSetattro(t *testing.T) {
	var lastName string
	var lastVal Object
	tp := NewType("settable", []*Type{ObjectType()})
	tp.Setattro = func(o Object, name Object, value Object) error {
		lastName = attrNameStr(name)
		lastVal = value
		return nil
	}
	holder := newDummyHolder(tp)

	if err := SetAttr(holder, NewStr("k"), NewInt(42)); err != nil {
		t.Fatalf("SetAttr: %v", err)
	}
	if lastName != "k" {
		t.Errorf("name = %q, want k", lastName)
	}
	if v, _ := lastVal.(*Int).Int64(); v != 42 {
		t.Errorf("value = %v, want 42", v)
	}

	if err := DelAttr(holder, NewStr("k")); err != nil {
		t.Fatalf("DelAttr: %v", err)
	}
	if lastVal != nil {
		t.Errorf("delete should pass nil value, got %T", lastVal)
	}
}

// TestSetAttrNoSlotIsTypeError covers the read-only / no-attributes
// branches at the bottom of PyObject_SetAttr.
func TestSetAttrNoSlotIsTypeError(t *testing.T) {
	tp := NewType("readonly", []*Type{ObjectType()})
	tp.Getattro = func(o Object, name Object) (Object, error) { return None(), nil }
	holder := newDummyHolder(tp)

	err := SetAttr(holder, NewStr("k"), NewInt(1))
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("err = %v, want read-only TypeError", err)
	}

	tp2 := NewType("inert", []*Type{ObjectType()})
	holder2 := newDummyHolder(tp2)
	err = SetAttr(holder2, NewStr("k"), NewInt(1))
	if err == nil || !strings.Contains(err.Error(), "no attributes") {
		t.Fatalf("err = %v, want no-attributes TypeError", err)
	}
}

// dummyHolder is a minimal Object used by the attr tests so we can
// pin a per-test Type without dragging in str/int slot wiring.
type dummyHolder struct {
	Header
}

func newDummyHolder(t *Type) *dummyHolder {
	h := &dummyHolder{}
	h.init(t)
	return h
}

type dummyAttrErr struct{ msg string }

func (e *dummyAttrErr) Error() string { return e.msg }
