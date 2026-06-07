package objects

import (
	"fmt"
	"math/big"
)

// Int is the Python int. CPython stores arbitrary-precision integers
// as a sign + digit array on PyLong; gopy uses math/big.Int for the
// same semantics. The fast-path for machine-word ints lands in v0.4
// alongside the longobject.c port proper.
//
// CPython: Include/cpython/longintrepr.h:L11 _PyLongValue
type Int struct {
	Header
	v big.Int
	// attrs holds instance attributes for int subclass objects. Nil for
	// plain int instances; allocated by intSubclassSetAttr when first
	// written. Mirrors CPython's tp_dictoffset on int subclasses (which
	// CPython sets via type_new_descriptors when the subclass picks up
	// __dict__ from object).
	attrs *Dict
}

// IntType is the type singleton for int. Mirrors PyLong_Type. Slots
// are wired in init() to break the variable-init dependency cycle
// (slots reference NewIntFromBig which references the small-int
// cache which references IntType).
//
// CPython: Objects/longobject.c:L6447 PyLong_Type
var IntType = NewType("int", []*Type{objectType})

func init() {
	IntType.Repr = intRepr
	IntType.Str = intRepr
	IntType.Hash = intHash
	IntType.RichCmp = intRichCmp
	// CPython: Objects/longobject.c:6542 PyLong_Type.tp_richcompare slot wrapper
	BindRichCmpDescriptors(IntType)
	IntType.TpFlags |= TpFlagMatchSelf
	// PyLongObject layout sizes on a 64-bit build: tp_basicsize is the
	// offset of long_value.ob_digit (PyObject_HEAD = 16 bytes plus
	// _PyLongValue.lv_tag = 8 bytes), tp_itemsize is sizeof(digit) with
	// PYLONG_BITS_IN_DIGIT == 30.
	//
	// CPython: Objects/longobject.c:6542 PyLong_Type.tp_basicsize
	// CPython: Include/cpython/longintrepr.h:43 typedef uint32_t digit
	IntType.BaseSize = 24
	IntType.ItemSize = 4
	IntType.Number = &NumberMethods{
		Add:         intAdd,
		Subtract:    intSub,
		Multiply:    intMul,
		TrueDivide:  intTrueDiv,
		FloorDivide: intFloorDiv,
		Remainder:   intMod,
		And:         intAnd,
		Or:          intOr,
		Xor:         intXor,
		Lshift:      intLshift,
		Rshift:      intRshift,
		Power:       intPower,
		Divmod:      intDivmod,
		Negative:    intNeg,
		Positive:    intPos,
		Absolute:    intAbs,
		Invert:      intInvert,
		Bool:        intBool,
		Int:         func(o Object) (Object, error) { return o, nil },
		Float:       intFloat,
	}
	initSmallInts()
}

// NewInt builds an int from an int64. Returns the cached singleton
// for the small-int range.
//
// CPython: Objects/longobject.c:L322 PyLong_FromLong
func NewInt(x int64) *Int {
	if cached := smallIntFromInt64(x); cached != nil {
		return cached
	}
	o := &Int{}
	o.init(IntType)
	o.v.SetInt64(x)
	return o
}

// NewIntFromBig builds an int from a math/big.Int. Returns the
// cached singleton when the value fits the small-int window.
//
// CPython: Objects/longobject.c:L156 _PyLong_FromBytes (adapted from)
func NewIntFromBig(b *big.Int) *Int {
	if cached := smallIntFromBig(b); cached != nil {
		return cached
	}
	o := &Int{}
	o.init(IntType)
	o.v.Set(b)
	return o
}

// Int64 returns the value as int64 if it fits; ok is false otherwise.
//
// CPython: Objects/longobject.c:L666 PyLong_AsLongAndOverflow (adapted from)
func (i *Int) Int64() (val int64, ok bool) {
	if i.v.IsInt64() {
		return i.v.Int64(), true
	}
	return 0, false
}

// BigInt returns a copy of the underlying big.Int.
func (i *Int) BigInt() *big.Int {
	return new(big.Int).Set(&i.v)
}

// Sign returns -1, 0, or +1 mirroring big.Int.Sign without copying.
//
// CPython: Objects/longobject.c _PyLong_Sign
func (i *Int) Sign() int {
	return i.v.Sign()
}

// newIntAs builds an int tagged with t instead of IntType. Used by the
// int subtype path so a class like `class MyInt(int): pass` yields
// instances whose Type() is MyInt.
//
// CPython: Objects/longobject.c:3514 long_subtype_new
func newIntAs(b *big.Int, t *Type) *Int {
	o := &Int{}
	o.v.Set(b)
	o.init(t)
	return o
}

// intTpNew is the type-aware tp_new for int. When cls is IntType itself
// the value-side IntCtor runs. When cls is a strict subclass (e.g.
// re._constants._NamedIntConstant), the result is re-tagged so the
// instance's Type() is cls.
//
// CPython: Objects/longobject.c:3389 long_new + long_subtype_new
var intTpNew func(cls *Type, args []Object, kwargs map[string]Object) (Object, error)

// intSubclassGetAttr is the tp_getattro slot for user-defined int
// subclasses. Instance attributes from i.attrs win over non-data
// descriptors; data descriptors on the type still take priority.
//
// CPython: Objects/typeobject.c:5165 slot_tp_getattr_hook (int path)
func intSubclassGetAttr(o Object, name Object) (Object, error) {
	i, ok := o.(*Int)
	if !ok {
		return GenericGetAttr(o, name)
	}
	tp := i.Type()
	nameStr := attrNameStr(name)
	descr, _ := LookupDescriptor(tp, nameStr)
	if descr != nil {
		if dget := descr.Type().DescrGet; dget != nil {
			if descr.Type().DescrSet != nil {
				return dget(descr, o, tp)
			}
		}
	}
	if i.attrs != nil {
		if v, err := i.attrs.GetItem(name); err == nil {
			return v, nil
		}
	}
	if descr != nil {
		if dget := descr.Type().DescrGet; dget != nil {
			return dget(descr, o, tp)
		}
		return descr, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, nameStr)
}

// intSubclassSetAttr is the tp_setattro slot for user-defined int
// subclasses. Instance attributes land in i.attrs.
//
// CPython: Objects/object.c:2040 PyObject_GenericSetAttr (int-subclass path)
func intSubclassSetAttr(o Object, name Object, value Object) error {
	i, ok := o.(*Int)
	if !ok {
		return GenericSetAttr(o, name, value)
	}
	tp := i.Type()
	nameStr := attrNameStr(name)
	descr, _ := LookupDescriptor(tp, nameStr)
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	if i.attrs == nil {
		i.attrs = NewDict()
	}
	if value == nil {
		if _, err := i.attrs.GetItem(name); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, nameStr)
		}
		return i.attrs.DelItem(name)
	}
	return i.attrs.SetItem(name, value)
}

// SetIntTpNewBase wires the value-side constructor (int(value, [base])).
// The subtype path here runs the same constructor, then re-tags the
// *Int with cls. Mirrors the SetStrTpNewBase wiring in str.go.
func SetIntTpNewBase(fn func(args []Object, kwargs map[string]Object) (Object, error)) {
	intTpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		// CPython 3.14: long_new rejects direct bool construction via int.__new__
		// because bool does not set Py_TPFLAGS_BASETYPE.
		// CPython: Objects/longobject.c:3389 long_new_impl
		if cls == BoolType {
			return nil, fmt.Errorf("TypeError: int.__new__(%s) is not safe, use bool.__new__()", cls.Name)
		}
		out, err := fn(args, kwargs)
		if err != nil {
			return nil, err
		}
		if cls == nil || cls == IntType {
			return out, nil
		}
		i, ok := out.(*Int)
		if !ok {
			return out, nil
		}
		return newIntAs(&i.v, cls), nil
	}
	IntType.TpNew = intTpNew
}
