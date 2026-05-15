package dataclasses

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// callMake invokes make_dataclass(name, fields, **kwargs) and asserts
// it returns a *Type. Centralizes the boilerplate around the variadic
// kwargs map.
func callMake(t *testing.T, name string, fields []objects.Object, kwargs map[string]objects.Object) *objects.Type {
	t.Helper()
	out, err := makeDataclassBuiltin(
		[]objects.Object{objects.NewStr(name), objects.NewList(fields)}, kwargs)
	if err != nil {
		t.Fatalf("make_dataclass: %v", err)
	}
	cls, ok := out.(*objects.Type)
	if !ok {
		t.Fatalf("make_dataclass returned %T, want *Type", out)
	}
	return cls
}

func expectErr(t *testing.T, args []objects.Object, kwargs map[string]objects.Object, wantSubstr string) {
	t.Helper()
	_, err := makeDataclassBuiltin(args, kwargs)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("err = %q, want substring %q", err.Error(), wantSubstr)
	}
}

// TestMakeDataclassBareNameField: a string entry gets the 'typing.Any'
// annotation marker and no default.
//
// CPython: Lib/dataclasses.py:1674 _ANY_MARKER branch
func TestMakeDataclassBareNameField(t *testing.T) {
	cls := callMake(t, "C", []objects.Object{objects.NewStr("x")}, nil)
	annObj, err := objects.GetAttr(cls, objects.NewStr("__annotations__"))
	if err != nil {
		t.Fatalf("__annotations__: %v", err)
	}
	ann := annObj.(*objects.Dict)
	v, err := ann.GetItem(objects.NewStr("x"))
	if err != nil {
		t.Fatalf("ann['x']: %v", err)
	}
	if s, ok := v.(*objects.Unicode); !ok || s.Value() != "typing.Any" {
		t.Errorf("ann['x'] = %v, want 'typing.Any'", v)
	}
}

// TestMakeDataclassTupleFields exercises (name, type) and (name, type,
// field()) shapes round-tripping through the decorator: the resulting
// class is a dataclass and the field defaults take effect.
//
// CPython: Lib/dataclasses.py:1677-1693 field tuple parsing
func TestMakeDataclassTupleFields(t *testing.T) {
	defField := newField(objects.NewInt(7), missingValue, true, true,
		objects.None(), true, nil, missingValue, objects.None())
	fields := []objects.Object{
		objects.NewTuple([]objects.Object{objects.NewStr("x"), objects.NewStr("int")}),
		objects.NewTuple([]objects.Object{objects.NewStr("y"), objects.NewStr("int"), defField}),
	}
	cls := callMake(t, "Point", fields, nil)
	inst, err := objects.Call(cls, objects.NewTuple([]objects.Object{objects.NewInt(3)}), nil)
	if err != nil {
		t.Fatalf("Point(3): %v", err)
	}
	xv, err := objects.GetAttr(inst, objects.NewStr("x"))
	if err != nil {
		t.Fatalf("inst.x: %v", err)
	}
	if iv, _ := xv.(*objects.Int).Int64(); iv != 3 {
		t.Errorf("inst.x = %d, want 3", iv)
	}
	yv, err := objects.GetAttr(inst, objects.NewStr("y"))
	if err != nil {
		t.Fatalf("inst.y: %v", err)
	}
	if iv, _ := yv.(*objects.Int).Int64(); iv != 7 {
		t.Errorf("inst.y = %d, want 7 (field default)", iv)
	}
}

// TestMakeDataclassRejectsInvalidIdentifier: '2x' is not a valid
// identifier.
//
// CPython: Lib/dataclasses.py:1685 isidentifier check
func TestMakeDataclassRejectsInvalidIdentifier(t *testing.T) {
	expectErr(t,
		[]objects.Object{objects.NewStr("C"), objects.NewList([]objects.Object{objects.NewStr("2x")})},
		nil, "must be valid identifiers")
}

// TestMakeDataclassRejectsKeyword: 'class' is a reserved word.
//
// CPython: Lib/dataclasses.py:1687 keyword.iskeyword check
func TestMakeDataclassRejectsKeyword(t *testing.T) {
	expectErr(t,
		[]objects.Object{objects.NewStr("C"), objects.NewList([]objects.Object{objects.NewStr("class")})},
		nil, "must not be keywords")
}

// TestMakeDataclassRejectsDuplicate: same name twice in fields list.
//
// CPython: Lib/dataclasses.py:1689 duplicate check
func TestMakeDataclassRejectsDuplicate(t *testing.T) {
	expectErr(t,
		[]objects.Object{objects.NewStr("C"), objects.NewList([]objects.Object{
			objects.NewStr("x"), objects.NewStr("x"),
		})}, nil, "duplicated")
}

// TestMakeDataclassNamespaceKwarg: user-supplied namespace entries land
// on the class body. The classic example is providing a method.
//
// CPython: Lib/dataclasses.py:1729 exec_body_callback (ns.update)
func TestMakeDataclassNamespaceKwarg(t *testing.T) {
	ns := objects.NewDict()
	marker := objects.NewInt(99)
	_ = ns.SetItem(objects.NewStr("MARKER"), marker)
	cls := callMake(t, "C",
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{"namespace": ns})
	got, err := objects.GetAttr(cls, objects.NewStr("MARKER"))
	if err != nil {
		t.Fatalf("MARKER: %v", err)
	}
	if iv, _ := got.(*objects.Int).Int64(); iv != 99 {
		t.Errorf("MARKER = %d, want 99", iv)
	}
}

// TestMakeDataclassModuleKwarg sets __module__.
//
// CPython: Lib/dataclasses.py:1749 cls.__module__ = module
func TestMakeDataclassModuleKwarg(t *testing.T) {
	cls := callMake(t, "C",
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{"module": objects.NewStr("my.pkg")})
	got, err := objects.GetAttr(cls, objects.NewStr("__module__"))
	if err != nil {
		t.Fatalf("__module__: %v", err)
	}
	if s, ok := got.(*objects.Unicode); !ok || s.Value() != "my.pkg" {
		t.Errorf("__module__ = %v, want 'my.pkg'", got)
	}
}

// TestMakeDataclassBasesKwarg: derives from a user-supplied base type
// and inherits its methods.
//
// CPython: Lib/dataclasses.py:1735 types.new_class(..., bases, ...)
func TestMakeDataclassBasesKwarg(t *testing.T) {
	base := objects.NewUserTypeKwargs("Base", nil, objects.NewDict(), nil)
	objects.SetTypeDescr(base, "TAG", objects.NewStr("base"))
	cls := callMake(t, "C",
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{
			"bases": objects.NewTuple([]objects.Object{base}),
		})
	got, err := objects.GetAttr(cls, objects.NewStr("TAG"))
	if err != nil {
		t.Fatalf("TAG: %v", err)
	}
	if s, ok := got.(*objects.Unicode); !ok || s.Value() != "base" {
		t.Errorf("TAG = %v, want 'base'", got)
	}
}

// TestMakeDataclassFlagsForwarded: frozen=True is passed through to the
// decorator so attribute assignment raises FrozenInstanceError.
//
// CPython: Lib/dataclasses.py:1753 decorator(... frozen=frozen ...)
func TestMakeDataclassFlagsForwarded(t *testing.T) {
	cls := callMake(t, "C",
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{"frozen": objects.NewBool(true)})
	inst, err := objects.Call(cls, objects.NewTuple([]objects.Object{objects.NewInt(1)}), nil)
	if err != nil {
		t.Fatalf("C(1): %v", err)
	}
	if err := objects.SetAttr(inst, objects.NewStr("x"), objects.NewInt(2)); err == nil {
		t.Error("expected FrozenInstanceError, got nil")
	}
}

// TestMakeDataclassListFieldEntry: lists are accepted in lieu of
// tuples for individual field entries, mirroring Python's "iterable of
// either (name) or (name, type) or (name, type, Field) objects"
// contract.
//
// CPython: Lib/dataclasses.py:1641 docstring
func TestMakeDataclassListFieldEntry(t *testing.T) {
	cls := callMake(t, "C", []objects.Object{
		objects.NewList([]objects.Object{objects.NewStr("x"), objects.NewStr("int")}),
	}, nil)
	annObj, _ := objects.GetAttr(cls, objects.NewStr("__annotations__"))
	ann := annObj.(*objects.Dict)
	if _, err := ann.GetItem(objects.NewStr("x")); err != nil {
		t.Errorf("ann['x']: %v", err)
	}
}

// TestMakeDataclassMissingArgs: zero or one positional argument is a
// TypeError.
//
// CPython: Lib/dataclasses.py:1635 signature
func TestMakeDataclassMissingArgs(t *testing.T) {
	expectErr(t, nil, nil, "missing required arguments")
	expectErr(t, []objects.Object{objects.NewStr("C")}, nil, "missing required arguments")
}

// TestMakeDataclassRejectsBadKwarg: an unknown kwarg surfaces as the
// same TypeError dataclass() would raise, since the leftover kwargs are
// forwarded straight through.
//
// CPython: Lib/dataclasses.py:1753 decorator(...) error path
func TestMakeDataclassRejectsBadKwarg(t *testing.T) {
	expectErr(t,
		[]objects.Object{objects.NewStr("C"), objects.NewList([]objects.Object{objects.NewStr("x")})},
		map[string]objects.Object{"bogus": objects.NewBool(true)},
		"unexpected keyword argument")
}
