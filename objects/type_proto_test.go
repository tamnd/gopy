package objects

import "testing"

// TypeGetName returns the tail component for static (built-in) types
// so callers see the bare name rather than a dotted prefix.
//
// CPython: Objects/typeobject.c:5430 PyType_GetName
func TestTypeGetNameStaticReturnsTail(t *testing.T) {
	tp := &Type{Name: "pkg.subpkg.Widget"}
	got := unicodeOf(TypeGetName(tp))
	if got != "Widget" {
		t.Errorf("got %q, want Widget", got)
	}
}

// TypeGetName on a heap (user) type returns the stamped name verbatim.
//
// CPython: Objects/typeobject.c:1457 type_name
func TestTypeGetNameUserReturnsName(t *testing.T) {
	tp := NewUserType("Foo", nil, NewDict())
	got := unicodeOf(TypeGetName(tp))
	if got != "Foo" {
		t.Errorf("got %q, want Foo", got)
	}
}

// TypeGetQualName falls back to the name when no qualname is set.
//
// CPython: Objects/typeobject.c:1469 type_qualname
func TestTypeGetQualNameFallsBackToName(t *testing.T) {
	tp := NewUserType("Bar", nil, NewDict())
	got := unicodeOf(TypeGetQualName(tp))
	if got != "Bar" {
		t.Errorf("got %q, want Bar", got)
	}
}

// TypeGetQualName uses the stamped qualname when present.
//
// CPython: Objects/typeobject.c:1469 type_qualname
func TestTypeGetQualNameUsesQualname(t *testing.T) {
	tp := NewUserType("Inner", nil, NewDict())
	tp.Qualname = "Outer.Inner"
	got := unicodeOf(TypeGetQualName(tp))
	if got != "Outer.Inner" {
		t.Errorf("got %q, want Outer.Inner", got)
	}
}

// TypeGetModuleName on a static type with a dot returns the prefix.
//
// CPython: Objects/typeobject.c:1537 type_module
func TestTypeGetModuleNameStaticDotted(t *testing.T) {
	tp := &Type{Name: "pkg.Thing"}
	mod, err := TypeGetModuleName(tp)
	if err != nil {
		t.Fatal(err)
	}
	if unicodeOf(mod) != "pkg" {
		t.Errorf("got %q, want pkg", unicodeOf(mod))
	}
}

// TypeGetModuleName on an undotted built-in name reports "builtins".
//
// CPython: Objects/typeobject.c:1556 type_module (builtins fallback)
func TestTypeGetModuleNameStaticBuiltins(t *testing.T) {
	tp := &Type{Name: "Thing"}
	mod, err := TypeGetModuleName(tp)
	if err != nil {
		t.Fatal(err)
	}
	if unicodeOf(mod) != "builtins" {
		t.Errorf("got %q, want builtins", unicodeOf(mod))
	}
}

// TypeGetModuleName on a user type with no __module__ raises
// AttributeError, matching CPython's type_module heap-type branch.
//
// CPython: Objects/typeobject.c:1543 type_module
func TestTypeGetModuleNameUserMissingErrors(t *testing.T) {
	tp := NewUserType("Foo", nil, NewDict())
	if _, err := TypeGetModuleName(tp); err == nil {
		t.Error("expected AttributeError on user type without __module__")
	}
}

// TypeGetFullyQualifiedName on a user type with module + qualname
// joins the two with a dot.
//
// CPython: Objects/typeobject.c:1589 _PyType_GetFullyQualifiedName
func TestTypeGetFullyQualifiedNameUserModuleQualname(t *testing.T) {
	tp := NewUserType("Inner", nil, NewDict())
	tp.Module = "mymod"
	tp.Qualname = "Outer.Inner"
	got, err := TypeGetFullyQualifiedName(tp)
	if err != nil {
		t.Fatal(err)
	}
	if unicodeOf(got) != "mymod.Outer.Inner" {
		t.Errorf("got %q, want mymod.Outer.Inner", unicodeOf(got))
	}
}

// TypeGetFullyQualifiedName strips the module when it's "builtins" or
// "__main__", matching CPython's _PyType_GetFullyQualifiedName.
//
// CPython: Objects/typeobject.c:1607 _PyType_GetFullyQualifiedName
func TestTypeGetFullyQualifiedNameStripsBuiltins(t *testing.T) {
	tp := NewUserType("Foo", nil, NewDict())
	tp.Module = "builtins"
	got, _ := TypeGetFullyQualifiedName(tp)
	if unicodeOf(got) != "Foo" {
		t.Errorf("got %q, want Foo", unicodeOf(got))
	}
	tp.Module = "__main__"
	got, _ = TypeGetFullyQualifiedName(tp)
	if unicodeOf(got) != "Foo" {
		t.Errorf("__main__: got %q, want Foo", unicodeOf(got))
	}
}

// TypeGetFullyQualifiedName on a static type returns tp_name verbatim,
// matching CPython's heap-type guard at the head of the function.
//
// CPython: Objects/typeobject.c:1591 _PyType_GetFullyQualifiedName
func TestTypeGetFullyQualifiedNameStatic(t *testing.T) {
	tp := &Type{Name: "pkg.Thing"}
	got, _ := TypeGetFullyQualifiedName(tp)
	if unicodeOf(got) != "pkg.Thing" {
		t.Errorf("got %q, want pkg.Thing", unicodeOf(got))
	}
}

// TypeGetFlags returns the tp_flags bitset.
//
// CPython: Objects/typeobject.c:3906 PyType_GetFlags
func TestTypeGetFlags(t *testing.T) {
	tp := &Type{TpFlags: TpFlagMapping}
	if TypeGetFlags(tp) != TpFlagMapping {
		t.Errorf("got %d, want %d", TypeGetFlags(tp), TpFlagMapping)
	}
}

// TypeSupportsWeakrefs is true for any non-nil type because gopy
// carries the weakref list head on every Header.
//
// CPython: Objects/typeobject.c:3913 PyType_SUPPORTS_WEAKREFS
func TestTypeSupportsWeakrefs(t *testing.T) {
	if !TypeSupportsWeakrefs(IntType) {
		t.Error("built-in types should support weakrefs")
	}
	if TypeSupportsWeakrefs(nil) {
		t.Error("nil type should not")
	}
}

// TypeGenericAlloc hands back a fresh Instance bound to t.
//
// CPython: Objects/typeobject.c:2457 PyType_GenericAlloc
func TestTypeGenericAlloc(t *testing.T) {
	tp := NewUserType("Foo", nil, NewDict())
	obj, err := TypeGenericAlloc(tp, 0)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type() != tp {
		t.Errorf("got type %v, want %v", obj.Type(), tp)
	}
}

// TypeGenericNew is the tp_new default and forwards to TypeGenericAlloc.
//
// CPython: Objects/typeobject.c:2471 PyType_GenericNew
func TestTypeGenericNew(t *testing.T) {
	tp := NewUserType("Foo", nil, NewDict())
	obj, err := TypeGenericNew(tp, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type() != tp {
		t.Errorf("got type %v, want %v", obj.Type(), tp)
	}
}

// CalculateMetaclass picks the more-derived metaclass among
// metatype and the types of bases.
//
// CPython: Objects/typeobject.c:3921 _PyType_CalculateMetaclass
func TestCalculateMetaclassPicksMostDerived(t *testing.T) {
	winner, err := CalculateMetaclass(TypeType(), []*Type{IntType})
	if err != nil {
		t.Fatal(err)
	}
	if winner != TypeType() {
		t.Errorf("got %v, want TypeType", winner)
	}
}

// CalculateMetaclass returns nil + TypeError when no winner exists.
//
// CPython: Objects/typeobject.c:3945 _PyType_CalculateMetaclass
func TestCalculateMetaclassConflict(t *testing.T) {
	m1 := NewType("M1", []*Type{TypeType()})
	m2 := NewType("M2", []*Type{TypeType()})
	b1 := &Type{Name: "B1"}
	b1.init(m1)
	b2 := &Type{Name: "B2"}
	b2.init(m2)
	if _, err := CalculateMetaclass(TypeType(), []*Type{b1, b2}); err == nil {
		t.Error("expected metaclass conflict error")
	}
}
