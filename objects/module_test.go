package objects

import (
	"strings"
	"testing"
)

// TestNewModuleStoresName pins that NewModule sets __name__ in the dict.
func TestNewModuleStoresName(t *testing.T) {
	m := NewModule("mymod")
	v, err := m.Dict().GetItem(NewStr("__name__"))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(*strStub)
	if !ok || s.v != "mymod" {
		t.Errorf("__name__ = %v, want mymod", v)
	}
}

// TestModuleSetGetAttr pins round-trip through moduleSetattr/moduleGetattr.
func TestModuleSetGetAttr(t *testing.T) {
	m := NewModule("mod")
	if err := moduleSetattr(m, NewStr("x"), NewInt(42)); err != nil {
		t.Fatal(err)
	}
	v, err := moduleGetattr(m, NewStr("x"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*Int).Int64(); got != 42 {
		t.Errorf("x = %d, want 42", got)
	}
}

// TestModuleDelAttr pins value==nil as a delete.
func TestModuleDelAttr(t *testing.T) {
	m := NewModule("mod")
	_ = moduleSetattr(m, NewStr("x"), NewInt(1))
	if err := moduleSetattr(m, NewStr("x"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleGetattr(m, NewStr("x")); err == nil {
		t.Fatal("expected AttributeError after delete")
	}
}

// TestModuleGetAttrMissing pins the AttributeError path.
func TestModuleGetAttrMissing(t *testing.T) {
	m := NewModule("mod")
	_, err := moduleGetattr(m, NewStr("missing"))
	if err == nil {
		t.Fatal("expected error for missing attr")
	}
	if !strings.Contains(err.Error(), "AttributeError") {
		t.Errorf("error = %v, want AttributeError", err)
	}
}

// TestModulePEP562Getattr pins the __getattr__ fallback (PEP 562).
func TestModulePEP562Getattr(t *testing.T) {
	m := NewModule("mod")
	hook := NewBuiltinFunction("__getattr__", func(args []Object, _ map[string]Object) (Object, error) {
		return NewStr("hooked"), nil
	})
	_ = moduleSetattr(m, NewStr("__getattr__"), hook)
	v, err := moduleGetattr(m, NewStr("dynamic"))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(*strStub)
	if !ok || s.v != "hooked" {
		t.Errorf("got %v, want hooked", v)
	}
}

// TestModuleReprBare pins the <module 'name'> form.
func TestModuleReprBare(t *testing.T) {
	m := NewModule("mymod")
	s, err := moduleRepr(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "mymod") || strings.Contains(s, "from") {
		t.Errorf("repr = %q, want bare form", s)
	}
}

// TestModuleReprWithFile pins the <module 'name' from 'file'> form.
func TestModuleReprWithFile(t *testing.T) {
	m := NewModule("mymod")
	_ = moduleSetattr(m, NewStr("__file__"), NewStr("/path/to/mod.py"))
	s, err := moduleRepr(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "from") || !strings.Contains(s, "/path/to/mod.py") {
		t.Errorf("repr = %q, want file form", s)
	}
}

// TestModuleStateRoundtrip pins State/SetState accessors.
func TestModuleStateRoundtrip(t *testing.T) {
	m := NewModule("mod")
	if m.State() != nil {
		t.Error("expected nil state initially")
	}
	m.SetState(42)
	if m.State().(int) != 42 {
		t.Error("State() did not return stored value")
	}
}
