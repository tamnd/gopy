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

func intRepr(o Object) (string, error) {
	return o.(*Int).v.String(), nil
}

// intHash mirrors CPython's long_hash: reduce modulo 2^61-1, fold
// the sign in. v0.2 ships a placeholder that uses big.Int's Mod with
// the same modulus so the result matches CPython exactly for ints
// that fit the modulus path.
//
// CPython: Objects/longobject.c:L3551 long_hash
func intHash(o Object) (int64, error) {
	const modulus int64 = (1 << 61) - 1
	v := new(big.Int).Set(&o.(*Int).v)
	m := big.NewInt(modulus)
	v.Mod(v, m)
	h := v.Int64()
	if o.(*Int).v.Sign() < 0 {
		h = -h
	}
	if h == -1 {
		h = -2
	}
	return h, nil
}

func intRichCmp(a, b Object, op CompareOp) (Object, error) {
	ai, ok := a.(*Int)
	if !ok {
		return nil, fmt.Errorf("intRichCmp: lhs is %T", a)
	}
	bi, ok := b.(*Int)
	if !ok {
		return notImplemented(), nil
	}
	c := ai.v.Cmp(&bi.v)
	var res bool
	switch op {
	case CompareLT:
		res = c < 0
	case CompareLE:
		res = c <= 0
	case CompareEQ:
		res = c == 0
	case CompareNE:
		res = c != 0
	case CompareGT:
		res = c > 0
	case CompareGE:
		res = c >= 0
	}
	if res {
		return True(), nil
	}
	return False(), nil
}

func intNeg(o Object) (Object, error) {
	i := o.(*Int)
	out := new(big.Int).Neg(&i.v)
	return NewIntFromBig(out), nil
}

// intAbs returns |x|, matching long_abs.
//
// CPython: Objects/longobject.c long_abs
func intAbs(o Object) (Object, error) {
	i := o.(*Int)
	out := new(big.Int).Abs(&i.v)
	return NewIntFromBig(out), nil
}

// intPos returns the int unchanged. CPython returns the same object
// for plain int but a fresh int for subclasses; the v0.6 panel does
// not yet support int subclasses so the same-object form is correct.
//
// CPython: Objects/longobject.c long_long
func intPos(o Object) (Object, error) { return o, nil }

func intBool(o Object) (bool, error) {
	return o.(*Int).v.Sign() != 0, nil
}

func intFloat(o Object) (Object, error) {
	i := o.(*Int)
	f, _ := new(big.Float).SetInt(&i.v).Float64()
	return NewFloat(f), nil
}

func intPair(a, b Object) (ai, bi *Int, ok bool) {
	aok, bok := false, false
	ai, aok = a.(*Int)
	bi, bok = b.(*Int)
	if !aok || !bok {
		return nil, nil, false
	}
	return ai, bi, true
}
