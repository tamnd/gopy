// PyObject_IsInstance / PyObject_IsSubclass shared port. Lives in
// objects/ so the VM (vm/eval_match.go MATCH_CLASS), the builtins
// (builtins/reflect.go isinstance/issubclass), and any other consumer
// route through one function. Metaclass __instancecheck__ /
// __subclasscheck__ overrides take precedence over the MRO fast path,
// matching CPython's PyObject_IsInstance.
//
// CPython: Objects/abstract.c:2632 object_recursive_isinstance
// CPython: Objects/abstract.c:2742 object_issubclass

package objects

import "fmt"

// isInstanceMaxDepth mirrors _Py_GUARD_RECURSION / Py_GUARD_RECURSION_ENTER
// threshold used by object_recursive_isinstance: CPython allows up to
// Py_DEFAULT_RECURSION_LIMIT levels of tuple nesting in the cls argument.
// We use 500 (half the default) to leave room for the Python call stack.
//
// CPython: Objects/abstract.c:2634 Py_GUARD_RECURSION_ENTER
const isInstanceMaxDepth = 500

// ObjectIsInstance ports PyObject_IsInstance. Supports a single type,
// a tuple of types, and metaclass __instancecheck__ overrides
// (ABCMeta's virtual-subclass registry).
//
// CPython: Objects/abstract.c:2632 object_recursive_isinstance
func ObjectIsInstance(inst, cls Object) (bool, error) {
	return objectIsInstance(inst, cls, 0)
}

func objectIsInstance(inst, cls Object, depth int) (bool, error) {
	if depth > isInstanceMaxDepth {
		return false, fmt.Errorf("RecursionError: maximum recursion depth exceeded in __instancecheck__")
	}
	if t, ok := cls.(*Type); ok && inst.Type() == t {
		return true, nil
	}
	if tup, ok := cls.(*Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			ok, err := objectIsInstance(inst, tup.Item(i), depth+1)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	t, ok := cls.(*Type)
	if !ok {
		return false, fmt.Errorf("TypeError: isinstance() arg 2 must be a type, a tuple of types, or a union")
	}
	if t.Type() != TypeType() {
		res, ok, err := callMetaInstanceCheck(cls, "__instancecheck__", inst)
		if err != nil {
			return false, err
		}
		if ok {
			return IsTrue(res), nil
		}
	}
	return IsSubtype(inst.Type(), t), nil
}

// ObjectIsSubclass ports PyObject_IsSubclass. Supports a tuple of
// bases and metaclass __subclasscheck__ overrides.
//
// CPython: Objects/abstract.c:2742 object_issubclass
func ObjectIsSubclass(sub *Type, cls Object) (bool, error) {
	return objectIsSubclass(sub, cls, 0)
}

func objectIsSubclass(sub *Type, cls Object, depth int) (bool, error) {
	if depth > isInstanceMaxDepth {
		return false, fmt.Errorf("RecursionError: maximum recursion depth exceeded in __subclasscheck__")
	}
	if tup, ok := cls.(*Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			ok, err := objectIsSubclass(sub, tup.Item(i), depth+1)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	t, ok := cls.(*Type)
	if !ok {
		return false, fmt.Errorf("TypeError: issubclass() arg 2 must be a class, a tuple of classes, or a union")
	}
	if t.Type() != TypeType() {
		res, ok, err := callMetaInstanceCheck(cls, "__subclasscheck__", sub)
		if err != nil {
			return false, err
		}
		if ok {
			return IsTrue(res), nil
		}
	}
	return IsSubtype(sub, t), nil
}

// callMetaInstanceCheck looks up name on cls's metaclass and, when
// installed, calls cls.name(arg). Returns (result, true, nil) on a
// real dispatch, (nil, false, nil) when no descriptor is present.
//
// CPython: Objects/typeobject.c:1976 _PyObject_LookupSpecial
func callMetaInstanceCheck(cls Object, name string, arg Object) (Object, bool, error) {
	descr, owner := LookupDescriptor(cls.Type(), name)
	if descr == nil {
		return nil, false, nil
	}
	bound := descr
	if dg := descr.Type().DescrGet; dg != nil {
		b, err := dg(descr, cls, owner)
		if err != nil {
			return nil, false, err
		}
		bound = b
	}
	res, err := CallOneArg(bound, arg)
	if err != nil {
		return nil, false, err
	}
	return res, true, nil
}
