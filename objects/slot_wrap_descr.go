// Slot wrappers for the descriptor protocol. CPython's add_operators
// walks slotdefs and installs a method-descriptor wrapper named
// __get__, __set__, __delete__ for every type whose tp_descr_get or
// tp_descr_set is set. gopy has no central PyType_Ready pass for
// built-in types, so the wrappers are exposed by an explicit call
// from each affected type's init.
//
// CPython: Objects/typeobject.c:9685 wrap_descr_get
// CPython: Objects/typeobject.c:9706 wrap_descr_set
// CPython: Objects/typeobject.c:9721 wrap_descr_delete
// CPython: Objects/typeobject.c:10989 slotdefs (descr_get/descr_set/descr_delete rows)

package objects

import "fmt"

// addDescriptorSlotWrappers exposes __get__, __set__, __delete__ on t
// when the corresponding C-level slot is set. Mirrors the slotdefs
// rows for tp_descr_get and tp_descr_set: add_operators installs the
// wrapper unless the type already has the name in tp_dict. The check
// against the existing descriptor table preserves that
// already-defined-wins behavior.
//
// CPython: Objects/typeobject.c add_operators
func addDescriptorSlotWrappers(t *Type) {
	if t == nil {
		return
	}
	if t.DescrGet != nil {
		if _, exists := typeDescrTable[t]["__get__"]; !exists {
			SetTypeDescr(t, "__get__", NewMethodDescr(t, "__get__", makeWrapDescrGet(t)))
		}
	}
	if t.DescrSet != nil {
		if _, exists := typeDescrTable[t]["__set__"]; !exists {
			SetTypeDescr(t, "__set__", NewMethodDescr(t, "__set__", makeWrapDescrSet(t)))
		}
		if _, exists := typeDescrTable[t]["__delete__"]; !exists {
			SetTypeDescr(t, "__delete__", NewMethodDescr(t, "__delete__", makeWrapDescrDelete(t)))
		}
	}
}

// makeWrapDescrGet returns the (self, obj, type=None) wrapper for
// t.DescrGet. None maps to NULL/nil for both obj and type, matching
// wrap_descr_get's normalisation. Capturing t (rather than reading
// args[0].Type()) keeps the wrapper consistent with the slot the
// caller asked to expose, the same way CPython embeds the function
// pointer in the wrapper.
//
// CPython: Objects/typeobject.c:9685 wrap_descr_get
func makeWrapDescrGet(t *Type) func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("TypeError: expected 1 or 2 arguments, got %d", len(args)-1)
		}
		self := args[0]
		obj := args[1]
		var typ *Type
		if IsNone(obj) {
			obj = nil
		}
		if len(args) == 3 {
			if tp, ok := args[2].(*Type); ok {
				typ = tp
			} else if !IsNone(args[2]) {
				return nil, fmt.Errorf("TypeError: __get__: type must be a type, not %s", typeNameOf(args[2]))
			}
		}
		if obj == nil && typ == nil {
			return nil, fmt.Errorf("TypeError: __get__(None, None) is invalid")
		}
		return t.DescrGet(self, obj, typ)
	}
}

// makeWrapDescrSet returns the (self, obj, value) wrapper for
// t.DescrSet, returning None on success.
//
// CPython: Objects/typeobject.c:9706 wrap_descr_set
func makeWrapDescrSet(t *Type) func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args)-1)
		}
		if err := t.DescrSet(args[0], args[1], args[2]); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// makeWrapDescrDelete returns the (self, obj) wrapper for t.DescrSet
// invoked with a nil value, matching wrap_descr_delete's call shape.
//
// CPython: Objects/typeobject.c:9721 wrap_descr_delete
func makeWrapDescrDelete(t *Type) func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args)-1)
		}
		if err := t.DescrSet(args[0], args[1], nil); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// AddIterSlotWrappers exposes __iter__ and __next__ on t when the
// matching C-level slots are populated, mirroring CPython's
// add_operators rows for tp_iter / tp_iternext. Iterator types call
// this from their init after they set Iter/IterNext so `dir(it)`,
// `getattr(it, '__next__')`, and pickle's `__reduce__` round-trip
// all see the same names CPython exposes through slotdefs.
//
// CPython: Objects/typeobject.c add_operators / slotdefs rows for
// tp_iter (Objects/typeobject.c:9929 slot_tp_iter) and tp_iternext
// (Objects/typeobject.c:8421 slot_tp_iternext).
func AddIterSlotWrappers(t *Type) {
	if t == nil {
		return
	}
	if t.Iter != nil {
		if _, exists := typeDescrTable[t]["__iter__"]; !exists {
			SetTypeDescr(t, "__iter__", NewMethodDescr(t, "__iter__", makeWrapIter(t)))
		}
	}
	if t.IterNext != nil {
		if _, exists := typeDescrTable[t]["__next__"]; !exists {
			d := NewMethodDescr(t, "__next__", makeWrapIterNext(t))
			// CPython: Objects/typeobject.c slotdefs tp_iternext wrapper doc
			d.doc = "Implement next(self)."
			SetTypeDescr(t, "__next__", d)
		}
	}
}

// makeWrapIter returns a self-only wrapper for t.Iter, matching
// CPython's `wrap_unaryfunc` shape for tp_iter.
//
// CPython: Objects/typeobject.c wrap_unaryfunc (slotdefs row for tp_iter)
func makeWrapIter(t *Type) func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: expected 0 arguments, got %d", len(args)-1)
		}
		return t.Iter(args[0])
	}
}

// makeWrapIterNext returns a self-only wrapper for t.IterNext. The
// ErrStopIteration sentinel surfaces as a real StopIteration exception
// when callers go through the Python-level __next__, mirroring
// CPython's `next_wrapper` which calls PyIter_Next and converts the
// NULL+no-error case into StopIteration.
//
// CPython: Objects/typeobject.c next_wrapper (slotdefs row for tp_iternext)
func makeWrapIterNext(t *Type) func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: expected 0 arguments, got %d", len(args)-1)
		}
		v, err := t.IterNext(args[0])
		if err != nil {
			return nil, err
		}
		// Mirror PyIter_Next: return an owned reference.
		// CPython: Objects/abstract.c:2840 PyIter_Next (returns new ref)
		Incref(v)
		return v, nil
	}
}

// AddCallSlotWrapper exposes __call__ on t when t advertises either
// tp_call (Call) or PEP 590 vectorcall (Vectorcall). Mirrors CPython's
// slotdefs entry for tp_call which add_operators resolves to wrap_call.
// Without this, `fn.__call__`, `bound_method.__call__`,
// `Type.__call__(instance, *args)` shapes raise AttributeError on types
// whose only callable hook is one of the slot fields.
//
// CPython: Objects/typeobject.c wrap_call (slotdefs row for tp_call)
func AddCallSlotWrapper(t *Type) {
	if t == nil || (t.Call == nil && t.Vectorcall == nil) {
		return
	}
	if _, exists := typeDescrTable[t]["__call__"]; exists {
		return
	}
	SetTypeDescr(t, "__call__", NewMethodDescr(t, "__call__", makeWrapCall()))
}

// makeWrapCall returns the (self, *args, **kwargs) wrapper for the
// callable slot. The receiver is the first positional argument
// (CPython's wrap_call receives self via the descriptor binding); the
// remaining positionals and the kwargs dict feed through the standard
// Call helper so vectorcall-only types route the same as tp_call types.
//
// CPython: Objects/typeobject.c wrap_call
func makeWrapCall() func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __call__() missing self argument")
		}
		self := args[0]
		rest := NewTuple(args[1:])
		var kw *Dict
		if len(kwargs) > 0 {
			kw = NewDict()
			for k, v := range kwargs {
				if err := kw.SetItem(NewStr(k), v); err != nil {
					return nil, err
				}
			}
		}
		return Call(self, rest, kw)
	}
}
