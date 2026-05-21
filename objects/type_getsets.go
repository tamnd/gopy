// Type-level getset descriptors: __name__, __qualname__, __bases__,
// __mro__, __module__, __dict__. CPython registers these on PyType_Type
// via type_getsets so every class exposes them; gopy mirrors that by
// installing GetSetDescr entries in typeType's descriptor table.
//
// CPython: Objects/typeobject.c:1145 type_getsets

package objects

import (
	"fmt"
	"strings"
)

func init() {
	register := func(name string, get func(o Object) (Object, error), set func(o Object, v Object) error) {
		SetTypeDescr(typeType, name, NewGetSetDescr(name, get, set))
	}
	register("__name__", typeGetName, typeSetName)
	register("__qualname__", typeGetQualname, typeSetQualname)
	register("__module__", typeGetModule, typeSetModule)
	register("__bases__", typeGetBases, nil)
	register("__mro__", typeGetMRO, nil)
	register("__doc__", typeGetDoc, typeSetDoc)
}

// typeGetName mirrors type_name. Heap types return ht_name verbatim;
// static types take the tail after the last dot of tp_name so a
// "datetime.date" tp_name reports __name__ == "date".
//
// CPython: Objects/typeobject.c:1457 type_name
func typeGetName(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__name__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.IsUser {
		return NewStr(t.Name), nil
	}
	return NewStr(tailName(t.Name)), nil
}

// typeSetName writes t.Name. Only allowed on user-defined types.
//
// CPython: Objects/typeobject.c:1024 type_set_name
func typeSetName(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__name__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__name__' attribute of immutable type '%s'", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__name__' attribute")
	}
	s, ok := v.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: can only assign string to %s.__name__, not '%s'", t.Name, typeNameOf(v))
	}
	t.Name = s.v
	t.InvalidateVersionTag()
	return nil
}

// typeGetQualname mirrors type_qualname. Heap types return ht_qualname
// verbatim; static types fall back to _PyType_Name which strips the
// dotted module prefix from tp_name.
//
// CPython: Objects/typeobject.c:1470 type_qualname
func typeGetQualname(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__qualname__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.IsUser {
		if t.Qualname != "" {
			return NewStr(t.Qualname), nil
		}
		return NewStr(t.Name), nil
	}
	return NewStr(tailName(t.Name)), nil
}

// typeSetQualname writes t.Qualname. Only allowed on heap (user) types,
// matching CPython's check against Py_TPFLAGS_HEAPTYPE.
//
// CPython: Objects/typeobject.c:1003 type_set_qualname
func typeSetQualname(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__qualname__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__qualname__' attribute of immutable type '%s'", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__qualname__' attribute")
	}
	s, ok := v.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: can only assign string to %s.__qualname__, not '%s'", t.Name, typeNameOf(v))
	}
	t.Qualname = s.v
	t.InvalidateVersionTag()
	return nil
}

// typeGetModule mirrors type_module: heap (user) types return their
// __module__ field (raising AttributeError when unset), static types
// take the leading dotted component of tp_name or fall back to
// "builtins". CPython's strrchr lookup on tp_name is the convention
// that lets a C extension type pin its __module__ at registration via
// the "module.name" form.
//
// CPython: Objects/typeobject.c:1538 type_module
func typeGetModule(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__module__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.IsUser {
		if t.Module == "" {
			return nil, fmt.Errorf("AttributeError: __module__")
		}
		return NewStr(t.Module), nil
	}
	if i := strings.LastIndexByte(t.Name, '.'); i >= 0 {
		return NewStr(t.Name[:i]), nil
	}
	return NewStr("builtins"), nil
}

// typeSetModule writes t.Module.
//
// CPython: Objects/typeobject.c:939 type_set_module
func typeSetModule(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__module__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__module__' attribute of immutable type '%s'", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__module__' attribute")
	}
	s, ok := v.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: can only assign string to %s.__module__, not '%s'", t.Name, typeNameOf(v))
	}
	t.Module = s.v
	t.InvalidateVersionTag()
	return nil
}

// typeGetBases returns a tuple of t.Bases. Mirrors type_bases.
//
// CPython: Objects/typeobject.c:1077 type_get_bases
func typeGetBases(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__bases__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	items := make([]Object, len(t.Bases))
	for i, b := range t.Bases {
		items[i] = b
	}
	return NewTuple(items), nil
}

// typeGetMRO returns a tuple of t.MRO. Mirrors type_mro.
//
// CPython: Objects/typeobject.c:1183 type_get_mro
func typeGetMRO(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__mro__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	items := make([]Object, len(t.MRO))
	for i, b := range t.MRO {
		items[i] = b
	}
	return NewTuple(items), nil
}

// typeGetDoc returns the type's __doc__. Looks in the type's own
// descriptor table first (user classes store their docstring there
// via STORE_NAME "__doc__"); returns None when absent.
//
// CPython: Objects/typeobject.c:1200 type_get_doc
func typeGetDoc(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__doc__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if v, ok2 := typeDescrTable[t]["__doc__"]; ok2 {
		return v, nil
	}
	return None(), nil
}

// typeSetDoc sets __doc__ on a user-defined type.
//
// CPython: Objects/typeobject.c:1227 type_set_doc
func typeSetDoc(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__doc__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__doc__' attribute of immutable type '%s'", t.Name)
	}
	SetTypeDescr(t, "__doc__", v)
	return nil
}
