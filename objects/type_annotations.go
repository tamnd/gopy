// PEP 649 / 749 lazy __annotations__ and __annotate__ on type objects.
// Mirrors type_get_annotate / type_set_annotate / type_get_annotations /
// type_set_annotations from Objects/typeobject.c.
//
// CPython 3.14 splits storage into __annotate__ (user-set) and a
// cache key __annotations_cache__ (filled by the lazy getter). gopy
// collapses both onto a single __annotations__ slot in the type's
// descriptor table; the simpler scheme matches gopy's tp_dict model
// and the observable surface is identical for the cases this port
// targets (class body emits __annotate__, getattr resolves once and
// caches, setattr drops the cache).
//
// CPython: Objects/typeobject.c:1990 type_get_annotate
// CPython: Objects/typeobject.c:2030 type_set_annotate
// CPython: Objects/typeobject.c:2069 type_get_annotations
// CPython: Objects/typeobject.c:2139 type_set_annotations

package objects

import "fmt"

// typeGetAnnotate returns the __annotate__ callable from tp's own
// descriptor table or None when absent. Built-in (non-user) types
// raise AttributeError to match CPython's HEAPTYPE gate.
//
// Only the type's OWN dict is consulted, not the MRO, matching
// CPython's PyDict_GetItemRef(PyType_GetDict(type), __annotate_func__).
// Without this restriction, B(A).__annotate__ would inherit A's annotate
// function and B.__annotations__ would return A's dict instead of {}.
//
// Lookup order mirrors CPython type_get_annotate: __annotate__ (user-defined
// in class body via def __annotate__) wins over __annotate_func__ (compiler-
// generated synthetic for the class's own annotations).
//
// CPython: Objects/typeobject.c:1990 type_get_annotate
func typeGetAnnotate(tp *Type) (Object, error) {
	if !tp.IsUser {
		return nil, fmt.Errorf("AttributeError: type object '%s' has no attribute '__annotate__'", tp.Name)
	}
	if ann := lookupTypeMember(tp, "__annotate__"); ann != nil {
		return ann, nil
	}
	if ann := lookupTypeMember(tp, "__annotate_func__"); ann != nil {
		return ann, nil
	}
	return None(), nil
}

// typeSetAnnotate stores __annotate__ on tp. Setting a non-None
// callable invalidates any previously cached __annotations__ so the
// next read re-runs the new annotate. Deletion is rejected; built-in
// types are immutable.
//
// CPython: Objects/typeobject.c:2030 type_set_annotate
func typeSetAnnotate(tp *Type, value Object) error {
	if value == nil {
		return fmt.Errorf("TypeError: cannot delete __annotate__ attribute")
	}
	if !tp.IsUser {
		return fmt.Errorf("TypeError: cannot set '__annotate__' attribute of immutable type '%s'", tp.Name)
	}
	if !IsNone(value) && !Callable(value) {
		return fmt.Errorf("TypeError: __annotate__ must be callable or None")
	}
	SetTypeDescr(tp, "__annotate_func__", value)
	if !IsNone(value) {
		DelTypeDescr(tp, "__annotations_cache__")
	}
	tp.InvalidateVersionTag()
	return nil
}

// typeGetAnnotations returns the cached __annotations__ dict or
// materializes it by calling __annotate__(VALUE) on first miss.
// Cached back into the descriptor table under __annotations_cache__ on
// success, matching CPython 3.14's storage key.
//
// Both the cache and annotate lookups use the type's OWN dict, not MRO,
// matching CPython's PyDict_GetItemRef(PyType_GetDict(type), ...) calls.
// Without this restriction, B(A).__annotations__ inherits A's dict.
//
// CPython: Objects/typeobject.c:2069 type_get_annotations
func typeGetAnnotations(tp *Type) (Object, error) {
	if !tp.IsUser {
		return nil, fmt.Errorf("AttributeError: type object '%s' has no attribute '__annotations__'", tp.Name)
	}
	if cached := lookupTypeMember(tp, "__annotations_cache__"); cached != nil {
		return cached, nil
	}
	annotate := lookupTypeMember(tp, "__annotate__")
	if annotate == nil {
		annotate = lookupTypeMember(tp, "__annotate_func__")
	}
	var out Object
	if annotate != nil && Callable(annotate) {
		v, err := Call(annotate, NewTuple([]Object{NewInt(1)}), nil)
		if err != nil {
			return nil, err
		}
		d, ok := v.(*Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: __annotate__ returned non-dict of type '%s'", v.Type().Name)
		}
		out = d
	} else {
		out = NewDict()
	}
	SetTypeDescr(tp, "__annotations_cache__", out)
	tp.InvalidateVersionTag()
	return out, nil
}

// typeSetAnnotations writes __annotations__ on tp or deletes it, then
// drops __annotate__ so a stale annotate function never overrides the
// explicit write. Stores under __annotations_cache__ (CPython 3.14 key).
//
// CPython: Objects/typeobject.c:2139 type_set_annotations
func typeSetAnnotations(tp *Type, value Object) error {
	if !tp.IsUser {
		return fmt.Errorf("TypeError: cannot set '__annotations__' attribute of immutable type '%s'", tp.Name)
	}
	if value != nil {
		SetTypeDescr(tp, "__annotations_cache__", value)
	} else if !DelTypeDescr(tp, "__annotations_cache__") {
		return fmt.Errorf("AttributeError: __annotations__")
	}
	DelTypeDescr(tp, "__annotate_func__")
	DelTypeDescr(tp, "__annotate__")
	tp.InvalidateVersionTag()
	return nil
}
