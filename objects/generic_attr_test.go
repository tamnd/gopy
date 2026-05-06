package objects

import (
	"errors"
	"testing"
)

// TestGenericGetAttrCallsDescriptor pins the data-descriptor path:
// LookupDescriptor finds a getset on the MRO and GenericGetAttr
// dispatches through DescrGet.
func TestGenericGetAttrCallsDescriptor(t *testing.T) {
	tp := NewType("attrtest", []*Type{objectType})
	d := NewGetSetDescr("x",
		func(owner Object) (Object, error) { return NewInt(42), nil },
		nil,
	)
	SetTypeDescr(tp, "x", d)

	holder := &Header{}
	holder.init(tp)
	got, err := GenericGetAttr(holder, NewStr("x"))
	if err != nil {
		t.Fatalf("GenericGetAttr: %v", err)
	}
	iv, ok := got.(*Int)
	if !ok {
		t.Fatalf("got %T, want *Int", got)
	}
	n, _ := iv.Int64()
	if n != 42 {
		t.Errorf("got %d, want 42", n)
	}
}

// TestGenericGetAttrPlainAttribute verifies that a class attribute
// without DescrGet (e.g. a function or constant) is returned
// unchanged. CPython's path returns descr directly when f is NULL and
// no instance dict supplies it.
func TestGenericGetAttrPlainAttribute(t *testing.T) {
	tp := NewType("attrtest_plain", []*Type{objectType})
	plain := NewInt(7)
	SetTypeDescr(tp, "k", plain)

	holder := &Header{}
	holder.init(tp)
	got, err := GenericGetAttr(holder, NewStr("k"))
	if err != nil {
		t.Fatalf("GenericGetAttr: %v", err)
	}
	if got != plain {
		t.Errorf("got %v, want plain attribute %v", got, plain)
	}
}

// TestGenericGetAttrInheritsFromBase walks the MRO: descriptor is on
// the base type, lookup happens on the subtype.
func TestGenericGetAttrInheritsFromBase(t *testing.T) {
	base := NewType("attrtest_base", []*Type{objectType})
	d := NewGetSetDescr("v",
		func(owner Object) (Object, error) { return NewInt(99), nil },
		nil,
	)
	SetTypeDescr(base, "v", d)
	sub := NewType("attrtest_sub", []*Type{base})

	holder := &Header{}
	holder.init(sub)
	got, err := GenericGetAttr(holder, NewStr("v"))
	if err != nil {
		t.Fatalf("GenericGetAttr: %v", err)
	}
	iv, _ := got.(*Int).Int64()
	if iv != 99 {
		t.Errorf("got %d, want 99", iv)
	}
}

func TestGenericGetAttrMissing(t *testing.T) {
	tp := NewType("attrtest_missing", []*Type{objectType})
	holder := &Header{}
	holder.init(tp)
	if _, err := GenericGetAttr(holder, NewStr("nope")); err == nil {
		t.Error("missing attribute should error")
	}
}

func TestGenericGetAttrRejectsNonString(t *testing.T) {
	tp := NewType("attrtest_nonstr", []*Type{objectType})
	holder := &Header{}
	holder.init(tp)
	if _, err := GenericGetAttr(holder, NewInt(0)); err == nil {
		t.Error("non-string name should error")
	}
}

// TestGenericSetAttrCallsDescriptor pins the data-descriptor write
// path: GenericSetAttr finds a getset with fset and dispatches to
// DescrSet.
func TestGenericSetAttrCallsDescriptor(t *testing.T) {
	captured := int64(0)
	tp := NewType("setattr_target", []*Type{objectType})
	d := NewGetSetDescr("x",
		func(owner Object) (Object, error) { return NewInt(captured), nil },
		func(owner Object, value Object) error {
			iv, ok := value.(*Int)
			if !ok {
				return errors.New("expect int")
			}
			captured, _ = iv.Int64()
			return nil
		},
	)
	SetTypeDescr(tp, "x", d)

	holder := &Header{}
	holder.init(tp)
	if err := GenericSetAttr(holder, NewStr("x"), NewInt(123)); err != nil {
		t.Fatalf("GenericSetAttr: %v", err)
	}
	if captured != 123 {
		t.Errorf("captured = %d, want 123", captured)
	}
}

// TestGenericSetAttrReadOnly checks that a getset descriptor with no
// setter raises AttributeError on assignment.
func TestGenericSetAttrReadOnly(t *testing.T) {
	tp := NewType("setattr_readonly", []*Type{objectType})
	d := NewGetSetDescr("x",
		func(owner Object) (Object, error) { return NewInt(1), nil },
		nil,
	)
	SetTypeDescr(tp, "x", d)
	holder := &Header{}
	holder.init(tp)
	if err := GenericSetAttr(holder, NewStr("x"), NewInt(5)); err == nil {
		t.Error("set on read-only descriptor should error")
	}
}

// TestGenericSetAttrMissing — type has no descriptor for name and no
// instance __dict__, so GenericSetAttr raises AttributeError.
func TestGenericSetAttrMissing(t *testing.T) {
	tp := NewType("setattr_missing", []*Type{objectType})
	holder := &Header{}
	holder.init(tp)
	if err := GenericSetAttr(holder, NewStr("y"), NewInt(0)); err == nil {
		t.Error("set with no descriptor and no dict should error")
	}
}
