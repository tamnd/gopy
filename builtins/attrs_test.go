package builtins

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// attrFixture is a minimal Object whose Type backs attribute access
// with a Go map so the attribute-panel tests stay free of any
// concrete-type setup.
type attrFixture struct {
	objects.Header
	store map[string]objects.Object
}

func newAttrFixture() *attrFixture {
	tp := objects.NewType("fixture", []*objects.Type{objects.TypeType()})
	tp.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		key, _ := objects.Str(name)
		f := o.(*attrFixture)
		v, ok := f.store[key]
		if !ok {
			return nil, &attrErr{key}
		}
		return v, nil
	}
	tp.Setattro = func(o objects.Object, name objects.Object, value objects.Object) error {
		key, _ := objects.Str(name)
		f := o.(*attrFixture)
		if value == nil {
			delete(f.store, key)
			return nil
		}
		f.store[key] = value
		return nil
	}
	f := &attrFixture{store: map[string]objects.Object{}}
	f.Init(tp)
	return f
}

type attrErr struct{ name string }

func (e *attrErr) Error() string {
	return "AttributeError: '" + "fixture" + "' object has no attribute '" + e.name + "'"
}

// TestGetAttrTwoArg pins the basic dispatch.
func TestGetAttrTwoArg(t *testing.T) {
	f := newAttrFixture()
	f.store["x"] = objects.NewInt(7)
	v, err := GetAttr([]objects.Object{f, objects.NewStr("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*objects.Int).Int64(); got != 7 {
		t.Errorf("getattr(f, 'x') = %d, want 7", got)
	}
}

// TestGetAttrDefaultSwallowsAttributeError pins the three-argument
// branch: missing attr returns the default rather than raising.
func TestGetAttrDefaultSwallowsAttributeError(t *testing.T) {
	f := newAttrFixture()
	v, err := GetAttr([]objects.Object{f, objects.NewStr("nope"), objects.NewInt(99)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*objects.Int).Int64(); got != 99 {
		t.Errorf("getattr default = %d, want 99", got)
	}
}

// TestGetAttrMissingNoDefaultRaises pins the two-argument failure.
func TestGetAttrMissingNoDefaultRaises(t *testing.T) {
	f := newAttrFixture()
	_, err := GetAttr([]objects.Object{f, objects.NewStr("nope")}, nil)
	if err == nil || !strings.Contains(err.Error(), "AttributeError") {
		t.Fatalf("err = %v, want AttributeError", err)
	}
}

// TestHasAttrTrueAndFalse pins the True / False split.
func TestHasAttrTrueAndFalse(t *testing.T) {
	f := newAttrFixture()
	f.store["k"] = objects.NewInt(1)

	v, err := HasAttr([]objects.Object{f, objects.NewStr("k")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.True() {
		t.Errorf("hasattr(f, 'k') = %v, want True", v)
	}
	v, err = HasAttr([]objects.Object{f, objects.NewStr("missing")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.False() {
		t.Errorf("hasattr(f, 'missing') = %v, want False", v)
	}
}

// TestSetAttrAndDelAttrRoundTrip pins setattr then delattr.
func TestSetAttrAndDelAttrRoundTrip(t *testing.T) {
	f := newAttrFixture()
	if _, err := SetAttr([]objects.Object{f, objects.NewStr("k"), objects.NewInt(5)}, nil); err != nil {
		t.Fatal(err)
	}
	if v, ok := f.store["k"]; !ok {
		t.Fatal("setattr did not store value")
	} else if got, _ := v.(*objects.Int).Int64(); got != 5 {
		t.Errorf("stored %d, want 5", got)
	}

	if _, err := DelAttr([]objects.Object{f, objects.NewStr("k")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.store["k"]; ok {
		t.Errorf("delattr did not remove value")
	}
}

// TestSetAttrArityRejection pins the 3-arg requirement.
func TestSetAttrArityRejection(t *testing.T) {
	f := newAttrFixture()
	_, err := SetAttr([]objects.Object{f, objects.NewStr("k")}, nil)
	if err == nil {
		t.Fatal("expected TypeError on 2-arg setattr")
	}
}
