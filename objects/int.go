package objects

import (
	"errors"
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

const (
	smallIntMin = -5
	smallIntMax = 256
)

// IntType is the type singleton for int. Mirrors PyLong_Type. Slots
// are wired in init() to break the variable-init dependency cycle
// (slots reference NewIntFromBig which references the small-int
// cache which references IntType).
//
// CPython: Objects/longobject.c:L6447 PyLong_Type
var IntType = NewType("int", []*Type{objectType})

// smallInts is the cache for -5..256, matching CPython. Filled in
// init() so the slice is fully built before any concrete Int is
// constructed.
//
// CPython: Objects/longobject.c:L19 _PyLong_SMALL_INTS
var smallInts [smallIntMax - smallIntMin + 1]*Int

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
		Negative:    intNeg,
		Positive:    intPos,
		Invert:      intInvert,
		Bool:        intBool,
		Int:         func(o Object) (Object, error) { return o, nil },
		Float:       intFloat,
	}
	for i := range smallInts {
		o := &Int{}
		o.init(IntType)
		o.v.SetInt64(int64(smallIntMin + i))
		smallInts[i] = o
	}
}

// NewInt builds an int from an int64. Returns the cached singleton
// for the small-int range.
//
// CPython: Objects/longobject.c:L322 PyLong_FromLong
func NewInt(x int64) *Int {
	if x >= smallIntMin && x <= smallIntMax {
		return smallInts[x-smallIntMin]
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
	if b.IsInt64() {
		x := b.Int64()
		if x >= smallIntMin && x <= smallIntMax {
			return smallInts[x-smallIntMin]
		}
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

func intAdd(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	out := new(big.Int).Add(&ai.v, &bi.v)
	return NewIntFromBig(out), nil
}

func intSub(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	out := new(big.Int).Sub(&ai.v, &bi.v)
	return NewIntFromBig(out), nil
}

func intMul(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	out := new(big.Int).Mul(&ai.v, &bi.v)
	return NewIntFromBig(out), nil
}

// intTrueDiv implements `a / b` for ints. CPython returns a float
// even for exact integer ratios, so we shadow that contract here.
//
// CPython: Objects/longobject.c long_true_divide
func intTrueDiv(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
	}
	af, _ := new(big.Float).SetInt(&ai.v).Float64()
	bf, _ := new(big.Float).SetInt(&bi.v).Float64()
	return NewFloat(af / bf), nil
}

// intFloorDiv implements `a // b` with Python floor semantics: the
// quotient is rounded toward negative infinity, not toward zero.
// big.Int.QuoRem is truncated division, so we adjust when the
// remainder is non-zero and the operand signs differ.
//
// CPython: Objects/longobject.c long_div
func intFloorDiv(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() == 0 {
		return nil, errors.New("ZeroDivisionError: integer division or modulo by zero")
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(&ai.v, &bi.v, r)
	if r.Sign() != 0 && (ai.v.Sign() < 0) != (bi.v.Sign() < 0) {
		q.Sub(q, big.NewInt(1))
	}
	return NewIntFromBig(q), nil
}

// intMod implements `a % b` with Python sign-of-divisor semantics.
// big.Int.QuoRem leaves a remainder with the sign of the dividend,
// so we add `b` when the signs differ to land on Python's contract.
//
// CPython: Objects/longobject.c long_mod
func intMod(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() == 0 {
		return nil, errors.New("ZeroDivisionError: integer division or modulo by zero")
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(&ai.v, &bi.v, r)
	if r.Sign() != 0 && (ai.v.Sign() < 0) != (bi.v.Sign() < 0) {
		r.Add(r, &bi.v)
	}
	return NewIntFromBig(r), nil
}

func intAnd(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	return NewIntFromBig(new(big.Int).And(&ai.v, &bi.v)), nil
}

func intOr(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	return NewIntFromBig(new(big.Int).Or(&ai.v, &bi.v)), nil
}

func intXor(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	return NewIntFromBig(new(big.Int).Xor(&ai.v, &bi.v)), nil
}

// intLshift / intRshift use uint shift counts; CPython raises on
// negative shift counts and on counts that overflow C long.
//
// CPython: Objects/longobject.c long_lshift / long_rshift
func intLshift(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() < 0 {
		return nil, errors.New("ValueError: negative shift count")
	}
	n, fits := bi.Int64()
	if !fits || n > (1<<31) {
		return nil, errors.New("OverflowError: shift count too large")
	}
	return NewIntFromBig(new(big.Int).Lsh(&ai.v, uint(n))), nil
}

func intRshift(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() < 0 {
		return nil, errors.New("ValueError: negative shift count")
	}
	n, fits := bi.Int64()
	if !fits || n > (1<<31) {
		return nil, errors.New("OverflowError: shift count too large")
	}
	return NewIntFromBig(new(big.Int).Rsh(&ai.v, uint(n))), nil
}

// intPower implements `pow(a, b)` and `pow(a, b, mod)` for ints. A
// negative exponent without a modulus falls through to a float
// promotion via NotImplemented; the abstract layer would normally
// retry against float's slot (no Power yet on Float, so the caller
// gets a TypeError until it lands).
//
// CPython: Objects/longobject.c long_pow
func intPower(a, b, mod Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() < 0 {
		// Negative exponent needs a float result; let the abstract
		// layer try the next slot.
		return notImplemented(), nil
	}
	var m *big.Int
	if mod != nil && mod != None() {
		mi, ok := mod.(*Int)
		if !ok {
			return notImplemented(), nil
		}
		if mi.v.Sign() == 0 {
			return nil, errors.New("ValueError: pow() 3rd argument cannot be 0")
		}
		m = &mi.v
	}
	out := new(big.Int).Exp(&ai.v, &bi.v, m)
	return NewIntFromBig(out), nil
}

func intNeg(o Object) (Object, error) {
	i := o.(*Int)
	out := new(big.Int).Neg(&i.v)
	return NewIntFromBig(out), nil
}

// intPos returns the int unchanged. CPython returns the same object
// for plain int but a fresh int for subclasses; the v0.6 panel does
// not yet support int subclasses so the same-object form is correct.
//
// CPython: Objects/longobject.c long_long
func intPos(o Object) (Object, error) { return o, nil }

// intInvert returns the bitwise complement, matching ~x == -(x+1).
//
// CPython: Objects/longobject.c long_invert
func intInvert(o Object) (Object, error) {
	i := o.(*Int)
	out := new(big.Int).Not(&i.v)
	return NewIntFromBig(out), nil
}

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
