// Phase 6 gates for spec 1704: type_new pipeline. Each test pins a
// specific row in the pipeline so a regression in qualname stamping or
// __init_subclass__ kwarg threading shows up here instead of
// downstream in the enum / dataclasses import paths.
//
// CPython: Objects/typeobject.c:4153 type_new

package objects

import "testing"

// gate 6.3: __qualname__ defaults to the type's __name__ for a fresh
// user class, but a class body that stamps a dotted qualname
// ("Outer.Inner") overrides that fallback. NewUserType copies the
// namespace entry into Type.Qualname so the getset returns the dotted
// form.
//
// CPython: Objects/typeobject.c:984 type_qualname
func TestTypeQualnameFromNamespace(t *testing.T) {
	ns := NewDict()
	if err := ns.SetItem(NewStr("__qualname__"), NewStr("Outer.Inner")); err != nil {
		t.Fatal(err)
	}
	cls := NewUserType("Inner", nil, ns)
	got, err := GetAttr(cls, NewStr("__qualname__"))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got.(*Unicode); !ok || s.v != "Outer.Inner" {
		t.Errorf("__qualname__ = %v, want 'Outer.Inner'", got)
	}
}

// Fallback path: when the body did not stamp __qualname__ explicitly
// (an in-Go-built user class), the getset returns the bare class name.
func TestTypeQualnameFallsBackToName(t *testing.T) {
	cls := NewUserType("Solo", nil, NewDict())
	got, err := GetAttr(cls, NewStr("__qualname__"))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got.(*Unicode); !ok || s.v != "Solo" {
		t.Errorf("__qualname__ = %v, want 'Solo'", got)
	}
}

// typeSetQualname writes the field and is rejected on built-in types
// (CPython's heap-type check).
//
// CPython: Objects/typeobject.c:1003 type_set_qualname
func TestTypeSetQualname(t *testing.T) {
	cls := NewUserType("X", nil, NewDict())
	if err := SetAttr(cls, NewStr("__qualname__"), NewStr("Module.X")); err != nil {
		t.Fatal(err)
	}
	if cls.Qualname != "Module.X" {
		t.Errorf("cls.Qualname = %q, want 'Module.X'", cls.Qualname)
	}
	if err := SetAttr(objectType, NewStr("__qualname__"), NewStr("nope")); err == nil {
		t.Error("setting __qualname__ on built-in objectType should fail")
	}
}

// gate 6.4: PEP 487 __init_subclass__ receives the class-creation
// kwargs. NewUserTypeKwargs threads them so a Base.__init_subclass__
// hook can read them.
//
// CPython: Objects/typeobject.c:4595 type_init_subclass
func TestInitSubclassReceivesKwargs(t *testing.T) {
	var seenKwargs map[string]Object
	hook := NewBuiltinFunction("__init_subclass__", func(args []Object, kw map[string]Object) (Object, error) {
		seenKwargs = kw
		return None(), nil
	})
	baseNS := NewDict()
	_ = baseNS.SetItem(NewStr("__init_subclass__"), hook)
	base := NewUserType("Base", nil, baseNS)

	_ = NewUserTypeKwargs("C", []*Type{base}, NewDict(), map[string]Object{
		"flag": NewInt(7),
	})
	if seenKwargs == nil {
		t.Fatal("__init_subclass__ was not called")
	}
	v, ok := seenKwargs["flag"]
	if !ok {
		t.Fatalf("kwargs missing 'flag', got %v", seenKwargs)
	}
	n, _ := v.(*Int).Int64()
	if n != 7 {
		t.Errorf("kwargs[flag] = %v, want 7", n)
	}
}

// Calling NewUserType (no kwargs) still routes through the same
// pipeline. The legacy entry point feeds nil kwargs so existing
// callers keep working.
func TestNewUserTypePassesEmptyKwargs(t *testing.T) {
	called := false
	hook := NewBuiltinFunction("__init_subclass__", func(_ []Object, kw map[string]Object) (Object, error) {
		called = true
		if len(kw) != 0 {
			t.Errorf("expected no kwargs, got %v", kw)
		}
		return None(), nil
	})
	baseNS := NewDict()
	_ = baseNS.SetItem(NewStr("__init_subclass__"), hook)
	base := NewUserType("Base", nil, baseNS)
	_ = NewUserType("C", []*Type{base}, NewDict())
	if !called {
		t.Error("__init_subclass__ was not called")
	}
}
