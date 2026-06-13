// Slot wrappers for the numeric protocol (tp_as_number). CPython's
// add_operators walks the slotdefs table and, for every non-NULL slot a
// type fills in, installs a wrapper_descriptor in tp_dict named after the
// corresponding dunder (__add__, __pow__, __neg__, ...). gopy has no
// PyType_Ready pass for static types, so AddNumberSlotWrappers reproduces
// the slotdefs rows for the numeric block and is called from each numeric
// type's init once its NumberMethods bundle is populated.
//
// The wrappers themselves mirror the wrap_* helpers: each unpacks and
// arity-checks its argument tuple exactly as check_num_args / check_pow_args
// do, then forwards to the type's Go slot. The Go slots already return the
// NotImplemented singleton for operand types they do not handle (long_add
// returning Py_NotImplemented), so the wrapper passes that straight through,
// just like the C slot wrapper returns whatever the slot produced.
//
// CPython: Objects/typeobject.c:11025 slotdefs (numeric block)
// CPython: Objects/typeobject.c:9288 wrap_binaryfunc_l / 9300 wrap_binaryfunc_r
// CPython: Objects/typeobject.c:9312 wrap_ternaryfunc / wrap_ternaryfunc_r
// CPython: Objects/typeobject.c:9249 wrap_unaryfunc / 9262 wrap_inquirypred

package objects

import "fmt"

// WrapperDescr is a slot wrapper descriptor: the type-level object that
// exposes a C-level slot (here a numeric NumberMethods entry) under its
// dunder name. It parallels MethodDescr but carries the distinct
// wrapper_descriptor type so type(int.__add__).__name__ matches CPython.
//
// CPython: Objects/descrobject.c:1471 PyWrapperDescr_Type
type WrapperDescr struct {
	Header
	name  string
	doc   string
	sig   string
	owner *Type
	// fn is the already arity-checked wrapper closure. self is the bound
	// receiver, args are the trailing positional arguments (the slot
	// wrapper protocol takes no keywords).
	fn func(self Object, args []Object) (Object, error)
}

// WrapperDescrType is the type singleton for slot wrappers.
//
// CPython: Objects/descrobject.c:1471 PyWrapperDescr_Type
var WrapperDescrType = NewType("wrapper_descriptor", []*Type{objectType})

func init() {
	WrapperDescrType.TpFlags |= TpFlagMethodDescriptor
	WrapperDescrType.Repr = wrapperDescrRepr
	WrapperDescrType.Str = wrapperDescrRepr
	WrapperDescrType.DescrGet = wrapperDescrGet
	WrapperDescrType.Call = wrapperDescrCall
	WrapperDescrType.Hash = identityHash
	addDescrIntrospectionDescriptors(WrapperDescrType)
	SetTypeDescr(WrapperDescrType, "__text_signature__", NewGetSetDescr("__text_signature__",
		func(o Object) (Object, error) {
			if d, ok := o.(*WrapperDescr); ok && d.sig != "" {
				return NewStr(d.sig), nil
			}
			return None(), nil
		},
		nil,
	))
}

// Name, Owner, Doc satisfy the descr introspection interfaces so
// __name__ / __objclass__ / __qualname__ / __doc__ resolve.
func (d *WrapperDescr) Name() string { return d.name }
func (d *WrapperDescr) Owner() *Type { return d.owner }
func (d *WrapperDescr) Doc() string  { return d.doc }

// CPython: Objects/descrobject.c:234 wrapperdescr_repr
func wrapperDescrRepr(o Object) (string, error) {
	d := o.(*WrapperDescr)
	return "<slot wrapper '" + d.name + "' of '" + d.owner.Name + "' objects>", nil
}

// wrapperDescrGet binds the slot wrapper to an instance. Class-level
// access returns the descriptor itself; an instance access returns a
// bound method that prepends the receiver, mirroring wrapperdescr_get
// producing a method-wrapper.
//
// CPython: Objects/descrobject.c:451 wrapperdescr_get
func wrapperDescrGet(descr Object, obj Object, _ *Type) (Object, error) {
	if obj == nil {
		return descr, nil
	}
	return NewBoundMethod(descr, obj), nil
}

// wrapperDescrCall is the unbound call: args[0] is the receiver, validated
// against the owning type the same way wrapperdescr_call checks
// PyObject_TypeCheck before invoking d_wrapped.
//
// CPython: Objects/descrobject.c:392 wrapperdescr_call
func wrapperDescrCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	d := o.(*WrapperDescr)
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of '%s' object needs an argument", d.name, d.owner.Name)
	}
	if !IsSubtype(args[0].Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, args[0].Type().Name)
	}
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: wrapper %s() takes no keyword arguments", d.name)
	}
	return d.fn(args[0], args[1:])
}

// newWrapperDescr builds a slot wrapper. doc is the human-readable
// docstring (the part after the signature line in slotdefs); sig is the
// Argument Clinic signature exposed via __text_signature__.
func newWrapperDescr(owner *Type, name, sig, doc string, fn func(self Object, args []Object) (Object, error)) *WrapperDescr {
	d := &WrapperDescr{name: name, sig: sig, doc: doc, owner: owner, fn: fn}
	d.init(WrapperDescrType)
	return d
}

// setWrapperIfAbsent installs the wrapper unless the name is already in
// the type's descriptor table, matching add_operators' "already defined
// wins" behavior.
func setWrapperIfAbsent(t *Type, name, sig, doc string, fn func(self Object, args []Object) (Object, error)) {
	if _, exists := typeDescrTable[t][name]; exists {
		return
	}
	SetTypeDescr(t, name, newWrapperDescr(t, name, sig, doc, fn))
}

// check_num_args for slot wrappers where the receiver is handed
// separately, so args holds only the trailing arguments and must number
// exactly n.
//
// CPython: Objects/typeobject.c:8847 check_num_args
func checkWrapArgs(args []Object, n int) error {
	if len(args) == n {
		return nil
	}
	return fmt.Errorf("TypeError: expected %d argument%s, got %d", n, plural(n), len(args))
}

// AddNumberSlotWrappers installs the dunder slot wrappers for every
// numeric slot t fills in, reproducing the BINSLOT/RBINSLOT/UNSLOT/NBSLOT/
// IBSLOT rows of slotdefs. Reflected (__radd__ ...) and in-place (__iadd__
// ...) names are only added when the matching forward / in-place slot is
// present.
//
// CPython: Objects/typeobject.c:11025 slotdefs (numeric block)
func AddNumberSlotWrappers(t *Type) {
	if t == nil || t.Number == nil {
		return
	}
	nb := t.Number
	addNumberBinaryWrappers(t, nb)
	addNumberUnaryWrappers(t, nb)
	addNumberInplaceWrappers(t, nb)
}

// addNumberBinaryWrappers installs the infix binary number dunders
// (__add__/__radd__ and friends, divmod and pow) from the type's number
// slots. Split out of AddNumberSlotWrappers to keep each grouping's
// cognitive complexity in check.
//
// CPython: Objects/typeobject.c slotdefs (binary nb_* SLOT entries)
func addNumberBinaryWrappers(t *Type, nb *NumberMethods) {
	binL := func(name, op string, slot func(a, b Object) (Object, error)) {
		if slot == nil {
			return
		}
		setWrapperIfAbsent(t, name, "($self, value, /)", "Return self"+op+"value.",
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 1); err != nil {
					return nil, err
				}
				return slot(self, args[0])
			})
	}
	binR := func(name, op string, slot func(a, b Object) (Object, error)) {
		if slot == nil {
			return
		}
		setWrapperIfAbsent(t, name, "($self, value, /)", "Return value"+op+"self.",
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 1); err != nil {
					return nil, err
				}
				return slot(args[0], self)
			})
	}
	// binNotInfix: divmod has no infix operator, so the doc is given whole.
	binNotInfix := func(name, doc string, slot func(a, b Object) (Object, error), reversed bool) {
		if slot == nil {
			return
		}
		setWrapperIfAbsent(t, name, "($self, value, /)", doc,
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 1); err != nil {
					return nil, err
				}
				if reversed {
					return slot(args[0], self)
				}
				return slot(self, args[0])
			})
	}

	binL("__add__", "+", nb.Add)
	binR("__radd__", "+", nb.Add)
	binL("__sub__", "-", nb.Subtract)
	binR("__rsub__", "-", nb.Subtract)
	binL("__mul__", "*", nb.Multiply)
	binR("__rmul__", "*", nb.Multiply)
	binL("__mod__", "%", nb.Remainder)
	binR("__rmod__", "%", nb.Remainder)
	binNotInfix("__divmod__", "Return divmod(self, value).", nb.Divmod, false)
	binNotInfix("__rdivmod__", "Return divmod(value, self).", nb.Divmod, true)
	binL("__lshift__", "<<", nb.Lshift)
	binR("__rlshift__", "<<", nb.Lshift)
	binL("__rshift__", ">>", nb.Rshift)
	binR("__rrshift__", ">>", nb.Rshift)
	binL("__and__", "&", nb.And)
	binR("__rand__", "&", nb.And)
	binL("__xor__", "^", nb.Xor)
	binR("__rxor__", "^", nb.Xor)
	binL("__or__", "|", nb.Or)
	binR("__ror__", "|", nb.Or)
	binL("__floordiv__", "//", nb.FloorDivide)
	binR("__rfloordiv__", "//", nb.FloorDivide)
	binL("__truediv__", "/", nb.TrueDivide)
	binR("__rtruediv__", "/", nb.TrueDivide)
	binL("__matmul__", "@", nb.MatrixMultiply)
	binR("__rmatmul__", "@", nb.MatrixMultiply)

	// __pow__ / __rpow__ are ternary: an optional modulus follows value.
	//
	// CPython: Objects/typeobject.c:9312 wrap_ternaryfunc
	if nb.Power != nil {
		setWrapperIfAbsent(t, "__pow__", "($self, value, mod=None, /)", "Return pow(self, value, mod).",
			powWrapper(nb.Power, false))
		setWrapperIfAbsent(t, "__rpow__", "($self, value, mod=None, /)", "Return pow(value, self, mod).",
			powWrapper(nb.Power, true))
	}
}

// addNumberUnaryWrappers installs the unary number dunders (__neg__,
// __abs__, __int__, __index__, __bool__, ...) from the type's number slots.
//
// CPython: Objects/typeobject.c slotdefs (unary nb_* SLOT entries)
func addNumberUnaryWrappers(t *Type, nb *NumberMethods) {
	unary := func(name, doc string, slot func(o Object) (Object, error)) {
		if slot == nil {
			return
		}
		setWrapperIfAbsent(t, name, "($self, /)", doc,
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 0); err != nil {
					return nil, err
				}
				return slot(self)
			})
	}

	unary("__neg__", "-self", nb.Negative)
	unary("__pos__", "+self", nb.Positive)
	unary("__abs__", "abs(self)", nb.Absolute)
	unary("__invert__", "~self", nb.Invert)
	unary("__int__", "int(self)", nb.Int)
	unary("__float__", "float(self)", nb.Float)
	if nb.Index != nil {
		unary("__index__",
			"Return self converted to an integer, if self is suitable for use as an index into a list.",
			nb.Index)
	}

	// __bool__ uses wrap_inquirypred: the slot returns a Go bool, wrapped
	// into the True/False singleton.
	//
	// CPython: Objects/typeobject.c:9262 wrap_inquirypred
	if nb.Bool != nil {
		setWrapperIfAbsent(t, "__bool__", "($self, /)", "True if self else False",
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 0); err != nil {
					return nil, err
				}
				b, err := nb.Bool(self)
				if err != nil {
					return nil, err
				}
				return NewBool(b), nil
			})
	}
}

// addNumberInplaceWrappers installs the in-place number dunders
// (__iadd__, __imul__, __ipow__, ...) from the type's number slots.
//
// CPython: Objects/typeobject.c slotdefs (in-place nb_* SLOT entries)
func addNumberInplaceWrappers(t *Type, nb *NumberMethods) {
	inplace := func(name, op string, slot func(a, b Object) (Object, error)) {
		if slot == nil {
			return
		}
		setWrapperIfAbsent(t, name, "($self, value, /)", "Return self"+op+"value.",
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 1); err != nil {
					return nil, err
				}
				return slot(self, args[0])
			})
	}

	inplace("__iadd__", "+=", nb.InPlaceAdd)
	inplace("__isub__", "-=", nb.InPlaceSubtract)
	inplace("__imul__", "*=", nb.InPlaceMultiply)
	inplace("__imod__", "%=", nb.InPlaceRemainder)
	inplace("__ilshift__", "<<=", nb.InPlaceLshift)
	inplace("__irshift__", ">>=", nb.InPlaceRshift)
	inplace("__iand__", "&=", nb.InPlaceAnd)
	inplace("__ixor__", "^=", nb.InPlaceXor)
	inplace("__ior__", "|=", nb.InPlaceOr)
	inplace("__ifloordiv__", "//=", nb.InPlaceFloorDivide)
	inplace("__itruediv__", "/=", nb.InPlaceTrueDivide)
	inplace("__imatmul__", "@=", nb.InPlaceMatrixMultiply)
	if nb.InPlacePower != nil {
		setWrapperIfAbsent(t, "__ipow__", "($self, value, /)", "Return self**=value.",
			func(self Object, args []Object) (Object, error) {
				if err := checkWrapArgs(args, 1); err != nil {
					return nil, err
				}
				return nb.InPlacePower(self, args[0], None())
			})
	}
}

// powWrapper builds a __pow__ / __rpow__ wrapper: 1 or 2 positional
// arguments (value, optional modulus), arity-checked by check_pow_args.
//
// CPython: Objects/typeobject.c:9219 check_pow_args / 9312 wrap_ternaryfunc
func powWrapper(slot func(a, b, mod Object) (Object, error), reversed bool) func(self Object, args []Object) (Object, error) {
	return func(self Object, args []Object) (Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("TypeError: expected 1 or 2 arguments, got %d", len(args))
		}
		mod := None()
		if len(args) == 2 {
			mod = args[1]
		}
		if reversed {
			return slot(args[0], self, mod)
		}
		return slot(self, args[0], mod)
	}
}
