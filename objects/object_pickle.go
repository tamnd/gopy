// Port of the helpers Objects/typeobject.c uses to drive the default
// __reduce_ex__ result: _PyObject_GetNewArguments, _PyObject_GetItemsIter,
// object_getstate / object_getstate_default, _common_reduce and
// reduce_newobj. The override-detection prologue in objectReduceExDescr
// (object.go) feeds into commonReduce here once it decides to fall
// through to the default reducer.
//
// CPython: Objects/typeobject.c:7503 _PyObject_GetNewArguments
// CPython: Objects/typeobject.c:7594 _PyObject_GetItemsIter
// CPython: Objects/typeobject.c:7336 object_getstate_default
// CPython: Objects/typeobject.c:7460 object_getstate
// CPython: Objects/typeobject.c:7635 reduce_newobj
// CPython: Objects/typeobject.c:7747 _common_reduce

package objects

import (
	"fmt"
)

// commonReduce dispatches the default __reduce_ex__ result based on
// the pickle protocol: proto >= 2 uses reduce_newobj which builds the
// (__newobj__, (cls, *args), state, listitems, dictitems) 5-tuple;
// proto < 2 hands off to copyreg._reduce_ex(self, proto) which
// produces a (_reconstructor, (cls, base, state), dict?) tuple.
//
// CPython: Objects/typeobject.c:7747 _common_reduce
func commonReduce(self Object, proto int) (Object, error) {
	if proto >= 2 {
		return reduceNewobj(self)
	}
	if CopyregLookup == nil {
		return nil, fmt.Errorf("TypeError: cannot pickle '%s' object: copyreg not loaded", self.Type().Name)
	}
	fn, err := CopyregLookup("_reduce_ex")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple([]Object{self, NewInt(int64(proto))}), nil)
}

// reduceNewobj is the protocol-2+ default reducer. It collects the
// __new__ arguments, the persistable state, and any list / dict items
// iterator, then packs them into the 5-tuple pickle expects.
//
// CPython: Objects/typeobject.c:7635 reduce_newobj
func reduceNewobj(self Object) (Object, error) {
	if self.Type().TpNew == nil && self.Type().Call == nil {
		// CPython gates on tp_new != NULL. gopy types that lack TpNew
		// still construct through the type's Call slot, so accept that
		// shape too rather than rejecting subclasses that go through
		// typeCallViaTpNew.
		return nil, fmt.Errorf("TypeError: cannot pickle '%s' object", self.Type().Name)
	}
	args, kwargs, err := objectGetNewArguments(self)
	if err != nil {
		return nil, err
	}
	if CopyregLookup == nil {
		return nil, fmt.Errorf("TypeError: cannot pickle '%s' object: copyreg not loaded", self.Type().Name)
	}
	hasArgs := args != nil
	var newobj Object
	var newargs *Tuple
	switch {
	case kwargs == nil || kwargs.Len() == 0:
		newobj, err = CopyregLookup("__newobj__")
		if err != nil {
			return nil, err
		}
		items := []Object{self.Type()}
		if args != nil {
			for i := 0; i < args.Len(); i++ {
				items = append(items, args.Item(i))
			}
		}
		newargs = NewTuple(items)
	case args != nil:
		newobj, err = CopyregLookup("__newobj_ex__")
		if err != nil {
			return nil, err
		}
		newargs = NewTuple([]Object{self.Type(), args, kwargs})
	default:
		return nil, fmt.Errorf("SystemError: bad internal call (kwargs without args)")
	}
	required := !hasArgs && !isListInstance(self) && !isDictInstance(self)
	state, err := objectGetState(self, required)
	if err != nil {
		return nil, err
	}
	listitems, dictitems, err := objectGetItemsIter(self)
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{newobj, newargs, state, listitems, dictitems}), nil
}

// objectGetNewArguments mirrors _PyObject_GetNewArguments: prefer
// __getnewargs_ex__ which returns (args, kwargs); fall back to
// __getnewargs__ for the legacy form that returns only args. Returns
// (nil, nil, nil) when neither is defined, which signals "type
// constructs without arguments".
//
// CPython: Objects/typeobject.c:7503 _PyObject_GetNewArguments
func objectGetNewArguments(self Object) (*Tuple, *Dict, error) {
	if v, err := LookupSpecial(self, "__getnewargs_ex__"); err != nil {
		return nil, nil, err
	} else if v != nil {
		out, err := Call(v, NewTuple(nil), nil)
		if err != nil {
			return nil, nil, err
		}
		tup, ok := out.(*Tuple)
		if !ok {
			return nil, nil, fmt.Errorf("TypeError: __getnewargs_ex__ should return a tuple, not '%s'", out.Type().Name)
		}
		if tup.Len() != 2 {
			return nil, nil, fmt.Errorf("ValueError: __getnewargs_ex__ should return a tuple of length 2, not %d", tup.Len())
		}
		a, ok := tup.Item(0).(*Tuple)
		if !ok {
			return nil, nil, fmt.Errorf("TypeError: first item of the tuple returned by __getnewargs_ex__ must be a tuple, not '%s'", tup.Item(0).Type().Name)
		}
		k, ok := tup.Item(1).(*Dict)
		if !ok {
			return nil, nil, fmt.Errorf("TypeError: second item of the tuple returned by __getnewargs_ex__ must be a dict, not '%s'", tup.Item(1).Type().Name)
		}
		return a, k, nil
	}
	if v, err := LookupSpecial(self, "__getnewargs__"); err != nil {
		return nil, nil, err
	} else if v != nil {
		out, err := Call(v, NewTuple(nil), nil)
		if err != nil {
			return nil, nil, err
		}
		tup, ok := out.(*Tuple)
		if !ok {
			return nil, nil, fmt.Errorf("TypeError: __getnewargs__ should return a tuple, not '%s'", out.Type().Name)
		}
		return tup, nil, nil
	}
	return nil, nil, nil
}

// objectGetItemsIter mirrors _PyObject_GetItemsIter: list subclasses
// hand over an iterator over their items; dict subclasses hand over an
// iterator over (key, value) pairs from .items(). Everything else gets
// None for both. The pickle machinery uses these to drive APPEND /
// APPENDS / SETITEM / SETITEMS without depending on a custom reducer.
//
// CPython: Objects/typeobject.c:7594 _PyObject_GetItemsIter
func objectGetItemsIter(self Object) (Object, Object, error) {
	listitems, dictitems := None(), None()
	if isListInstance(self) {
		it, err := Iter(self)
		if err != nil {
			return nil, nil, err
		}
		listitems = it
	}
	if isDictInstance(self) {
		itemsFn, err := GetAttr(self, NewStr("items"))
		if err != nil {
			return nil, nil, err
		}
		items, err := Call(itemsFn, NewTuple(nil), nil)
		if err != nil {
			return nil, nil, err
		}
		it, err := Iter(items)
		if err != nil {
			return nil, nil, err
		}
		dictitems = it
	}
	return listitems, dictitems, nil
}

// objectGetState mirrors object_getstate: defer to a user-supplied
// __getstate__ when present (and overridden), otherwise call the
// default that exposes the instance __dict__ plus slot values.
//
// CPython: Objects/typeobject.c:7460 object_getstate
func objectGetState(self Object, required bool) (Object, error) {
	getstate, err := LookupAttrString(self, "__getstate__")
	if err != nil {
		return nil, err
	}
	if getstate == nil {
		return objectGetStateDefault(self, required)
	}
	// Override detection: object.__getstate__ on the type means the
	// instance inherits the default. The base descriptor is the
	// MethodDescr we register on objectType.
	clsGetstate, _ := LookupDescriptor(self.Type(), "__getstate__")
	baseGetstate, _ := LookupDescriptor(objectType, "__getstate__")
	if clsGetstate != nil && baseGetstate != nil && clsGetstate == baseGetstate {
		return objectGetStateDefault(self, required)
	}
	return Call(getstate, NewTuple(nil), nil)
}

// objectGetStateDefault produces the state tuple CPython's default
// __getstate__ would return: the instance __dict__ (or None when
// empty), optionally paired with a dict of slot values for classes
// that declare __slots__.
//
// CPython: Objects/typeobject.c:7336 object_getstate_default
func objectGetStateDefault(self Object, required bool) (Object, error) {
	state := None()
	switch v := self.(type) {
	case AttrDictHolder:
		if d := v.AttrDict(); d != nil && d.Len() > 0 {
			state = d
		}
	case *Instance:
		if d := v.Dict(); d != nil && d.Len() > 0 {
			state = d
		}
	}
	// CPython compares tp_basicsize against the base size plus the dict,
	// weakref and slot pointers it can account for; a built-in type that
	// carries extra C-level state (gopy's OpaqueCState) trips the guard
	// and cannot be pickled by the default reducer. The instance __dict__
	// (when present) is exposed in slot/dict state above, so the guard
	// only fires for the opaque tail.
	//
	// CPython: Objects/typeobject.c:7363 object_getstate_default
	if required && self.Type().OpaqueCState {
		return nil, fmt.Errorf("TypeError: cannot pickle '%s' object", self.Type().Name)
	}
	// Collect slot values for the names copyreg._slotnames computes (or
	// the cached __slotnames__ list), reading each through normal getattr
	// so a class __getattr__ participates the way CPython's
	// PyObject_GetOptionalAttr does.
	//
	// CPython: Objects/typeobject.c:7389 object_getstate_default (slot loop)
	slotnamesObj, err := typeGetSlotNames(self.Type())
	if err != nil {
		return nil, err
	}
	slotnames, ok := slotnamesObj.(*List)
	if !ok || slotnames.Len() == 0 {
		return state, nil
	}
	slots := NewDict()
	slotnamesSize := slotnames.Len()
	for i := range slotnamesSize {
		name := slotnames.Item(i)
		v, err := GetAttr(self, name)
		if err != nil {
			if isAttributeError(err) {
				continue // slot not set on this instance, not an error
			}
			return nil, err
		}
		if setErr := slots.SetItem(name, v); setErr != nil {
			return nil, setErr
		}
		// The list lives on the class, so it may mutate while we iterate.
		// CPython: Objects/typeobject.c:7421 object_getstate_default
		if slotnames.Len() != slotnamesSize {
			return nil, fmt.Errorf("RuntimeError: __slotnames__ changed size during iteration")
		}
	}
	if slots.Len() > 0 {
		return NewTuple([]Object{state, slots}), nil
	}
	return state, nil
}

// typeGetSlotNames mirrors _PyType_GetSlotNames: return the type's
// cached __slotnames__ when present (own dict only, no MRO walk), else
// ask copyreg._slotnames to compute and cache it. The result is either
// None or a list of slot-attribute names.
//
// CPython: Objects/typeobject.c:7287 _PyType_GetSlotNames
func typeGetSlotNames(t *Type) (Object, error) {
	if sn := typeOwnDictItem(t, "__slotnames__"); sn != nil {
		if sn != None() {
			if _, ok := sn.(*List); !ok {
				return nil, fmt.Errorf("TypeError: %s.__slotnames__ should be a list or None, not %s", t.Name, sn.Type().Name)
			}
		}
		return sn, nil
	}
	if CopyregLookup == nil {
		return None(), nil
	}
	fn, err := CopyregLookup("_slotnames")
	if err != nil {
		return nil, err
	}
	sn, err := Call(fn, NewTuple([]Object{t}), nil)
	if err != nil {
		return nil, err
	}
	if sn != None() {
		if _, ok := sn.(*List); !ok {
			return nil, fmt.Errorf("TypeError: copyreg._slotnames didn't return a list or None")
		}
	}
	return sn, nil
}

// typeOwnDictItem reads name from t's own attribute dict without walking
// the MRO, the way CPython's lookup_tp_dict + PyDict_GetItemRef do.
func typeOwnDictItem(t *Type, name string) Object {
	if t.ClassAttrDict != nil {
		if v, err := t.ClassAttrDict.GetItem(NewStr(name)); err == nil {
			return v
		}
	}
	if descrs, ok := typeDescrTable[t]; ok {
		if v, ok := descrs[name]; ok {
			return v
		}
	}
	return nil
}

// isListInstance reports whether self is exactly a list or a subclass
// thereof. Mirrors PyList_Check used by reduce_newobj.
func isListInstance(self Object) bool {
	if _, ok := self.(*List); ok {
		return true
	}
	return false
}

// isDictInstance reports whether self is exactly a dict or a subclass
// thereof. Mirrors PyDict_Check.
func isDictInstance(self Object) bool {
	if _, ok := self.(*Dict); ok {
		return true
	}
	return false
}
