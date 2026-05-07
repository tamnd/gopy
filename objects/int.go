package objects

import (
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
