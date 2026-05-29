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
	// CPython: Objects/unionobject.c:234 union_instancecheck
	if ut, ok := cls.(*UnionType); ok {
		for i := 0; i < ut.Args().Len(); i++ {
			ok, err := objectIsInstance(inst, ut.Args().Item(i), depth+1)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if t, ok := cls.(*Type); ok {
		// When the metaclass is not exactly `type`, check __instancecheck__.
		// CPython: Objects/abstract.c:2589 object_isinstance
		if t.Type() != TypeType() {
			res, found, err := callMetaInstanceCheck(cls, "__instancecheck__", inst)
			if err != nil {
				return false, err
			}
			if found {
				return IsTrue(res), nil
			}
		}
		if IsSubtype(inst.Type(), t) {
			return true, nil
		}
		// CPython: Objects/abstract.c:2601 inst.__class__ fallback
		// When the type check fails, check inst.__class__ (may be overridden).
		icls, err := GetAttr(inst, NewStr("__class__"))
		if err != nil {
			if isAttributeError(err) {
				if ClearCurrentExceptionHook != nil {
					ClearCurrentExceptionHook()
				}
				return false, nil
			}
			return false, err
		}
		if icls == None() || icls == nil {
			return false, nil
		}
		if icls == inst.Type() {
			return false, nil
		}
		if ict, ok := icls.(*Type); ok {
			return IsSubtype(ict, t), nil
		}
		return false, nil
	}
	// cls is not a *Type — try __instancecheck__ on cls's type first.
	// CPython: Objects/abstract.c:2665 _PyObject_LookupSpecial(__instancecheck__)
	res, found, err := callMetaInstanceCheck(cls, "__instancecheck__", inst)
	if err != nil {
		return false, err
	}
	if found {
		return IsTrue(res), nil
	}
	// Fall back to the abstract class path: cls must have __bases__.
	// CPython: Objects/abstract.c:2580 check_class + abstract_issubclass
	if err := checkClass(cls, "isinstance() arg 2 must be a type, a tuple of types, or a union"); err != nil {
		return false, err
	}
	icls, err := GetAttr(inst, NewStr("__class__"))
	if err != nil || icls == nil {
		return false, nil
	}
	return abstractIsSubclass(icls, cls, depth+1)
}

// ObjectIsSubclass ports PyObject_IsSubclass. Supports a tuple of
// bases and metaclass __subclasscheck__ overrides. The first argument
// can be any class-like object (has __bases__), not only *Type.
//
// CPython: Objects/abstract.c:2742 object_issubclass
func ObjectIsSubclass(sub *Type, cls Object) (bool, error) {
	return objectIsSubclassObj(sub, cls, 0)
}

// ObjectIsSubclassObj is the same as ObjectIsSubclass but accepts any
// class-like object as the first argument.
func ObjectIsSubclassObj(sub, cls Object) (bool, error) {
	return objectIsSubclassObj(sub, cls, 0)
}

func objectIsSubclassObj(sub, cls Object, depth int) (bool, error) {
	if depth > isInstanceMaxDepth {
		return false, fmt.Errorf("RecursionError: maximum recursion depth exceeded in __subclasscheck__")
	}
	if tup, ok := cls.(*Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			ok, err := objectIsSubclassObj(sub, tup.Item(i), depth+1)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	// CPython: Objects/unionobject.c:257 union_subclasscheck
	if ut, ok := cls.(*UnionType); ok {
		for i := 0; i < ut.Args().Len(); i++ {
			ok, err := objectIsSubclassObj(sub, ut.Args().Item(i), depth+1)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if t, ok := cls.(*Type); ok {
		if t.Type() != TypeType() {
			res, found, err := callMetaInstanceCheck(cls, "__subclasscheck__", sub)
			if err != nil {
				return false, err
			}
			if found {
				return IsTrue(res), nil
			}
		}
		if st, ok := sub.(*Type); ok {
			return IsSubtype(st, t), nil
		}
		// sub is not a *Type — must have __bases__ to be a valid class arg.
		// CPython: Objects/abstract.c:2695 check_class for derived
		if err := checkClass(sub, "issubclass() arg 1 must be a class"); err != nil {
			return false, err
		}
		return abstractIsSubclass(sub, cls, depth+1)
	}
	// cls is not a *Type — try __subclasscheck__ on cls's type.
	// CPython: Objects/abstract.c:2700 _PyObject_LookupSpecial(__subclasscheck__)
	res, found, err := callMetaInstanceCheck(cls, "__subclasscheck__", sub)
	if err != nil {
		return false, err
	}
	if found {
		return IsTrue(res), nil
	}
	// Abstract class path: both cls and sub must have __bases__.
	// CPython: Objects/abstract.c:2580 check_class
	if err := checkClass(sub, "issubclass() arg 1 must be a class"); err != nil {
		return false, err
	}
	if err := checkClass(cls, "issubclass() arg 2 must be a class, a tuple of classes, or a union"); err != nil {
		return false, err
	}
	return abstractIsSubclass(sub, cls, depth+1)
}

// checkClass verifies that cls has a __bases__ attribute, which is CPython's
// test for whether an object is a "class" in the abstract sense.
// When __bases__ raises AttributeError the error is masked as TypeError,
// mirroring PyErr_SetString(PyExc_TypeError, ...) in CPython which replaces
// the current exception; we must also clear the live Python exception from
// the thread state so the synthesized TypeError reaches the try/except handler.
//
// CPython: Objects/abstract.c:2547 check_class
func checkClass(cls Object, errMsg string) error {
	bases, err := GetAttr(cls, NewStr("__bases__"))
	if err != nil || bases == nil || bases == None() {
		if err != nil {
			// Mask AttributeError; propagate everything else.
			// CPython: Objects/abstract.c:2553 PyErr_ExceptionMatches(PyExc_AttributeError)
			if isAttributeError(err) {
				if ClearCurrentExceptionHook != nil {
					ClearCurrentExceptionHook()
				}
				return fmt.Errorf("TypeError: %s", errMsg)
			}
			return err
		}
		return fmt.Errorf("TypeError: %s", errMsg)
	}
	return nil
}

// abstractIsSubclass walks derived's __mro__ or __bases__ to check whether
// derived is a subclass of cls. Neither argument needs to be a *Type.
//
// CPython: Objects/abstract.c:2505 abstract_issubclass
func abstractIsSubclass(derived, cls Object, depth int) (bool, error) {
	if depth > isInstanceMaxDepth {
		return false, fmt.Errorf("RecursionError: maximum recursion depth exceeded in __subclasscheck__")
	}
	// Identity shortcut.
	if eq, err := RichCmpBool(derived, cls, CompareEQ); err == nil && eq {
		return true, nil
	}
	// CPython's abstract_issubclass only uses __bases__, not __mro__. A
	// __mro__ shortcut is tempting but breaks when __getattr__ intercepts
	// the lookup and returns a tuple that happens to not contain cls, causing
	// the recursion to stop early and miss the RecursionError.
	//
	// CPython: Objects/abstract.c:2505 abstract_issubclass (only __bases__)
	// CPython: Objects/abstract.c:2509 abstract_get_bases uses
	// PyObject_GetOptionalAttr which masks AttributeError but propagates others.
	bases, basesErr := GetAttr(derived, NewStr("__bases__"))
	if basesErr != nil {
		if isAttributeError(basesErr) {
			if ClearCurrentExceptionHook != nil {
				ClearCurrentExceptionHook()
			}
			return false, nil
		}
		return false, basesErr
	}
	if bases == nil || bases == None() {
		return false, nil
	}
	seq, seqErr := SequenceList(bases)
	if seqErr != nil {
		return false, nil
	}
	for i := 0; i < seq.Len(); i++ {
		ok, err := abstractIsSubclass(seq.Item(i), cls, depth+1)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
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
