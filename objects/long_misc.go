// Misc int slots: repr/str, hash, richcompare, and the unary panel
// (neg/abs/pos/bool/float). All of these are slot-shaped; the
// arithmetic and bitwise ports live in long_arith.go and
// long_bitwise.go respectively.
//
// CPython: Objects/longobject.c:1762 long_repr / long_hash / long_richcompare

package objects

import (
	"fmt"
	"math/big"
)

func intRepr(o Object) (string, error) {
	return o.(*Int).v.String(), nil
}

// intHash mirrors CPython's long_hash: reduce modulo 2^61-1 and
// negate when the source is negative, then remap the -1 sentinel to
// -2 so callers can distinguish "hash is literally -1" from the
// generic error sentinel.
//
// CPython: Objects/longobject.c:3551 long_hash
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

// intRichCmp implements all six relational operators for int/int.
// Mixed-type comparisons return NotImplemented so the abstract layer
// can ask the other operand.
//
// CPython: Objects/longobject.c:3450 long_richcompare
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

// intNeg / intAbs / intPos cover -x, abs(x), +x. Pos returns the
// same object for plain int; CPython hands back a fresh PyLong for
// subclasses, but the v0.6 panel does not yet support int subclasses.
//
// CPython: Objects/longobject.c long_neg / long_abs / long_long
func intNeg(o Object) (Object, error) {
	i := o.(*Int)
	if x, ok := compactInt(i); ok {
		if r, over := negOverflow(x); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Neg(&i.v)), nil
}

func intAbs(o Object) (Object, error) {
	i := o.(*Int)
	if x, ok := compactInt(i); ok {
		if r, over := absOverflow(x); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Abs(&i.v)), nil
}

func intPos(o Object) (Object, error) { return o, nil }

func intBool(o Object) (bool, error) {
	return o.(*Int).v.Sign() != 0, nil
}

func intFloat(o Object) (Object, error) {
	f, _ := new(big.Float).SetInt(&o.(*Int).v).Float64()
	return NewFloat(f), nil
}

// intPair is the operand-coercion helper shared by every binary int
// slot: returns (nil, nil, false) when either side is not an int so
// the caller can return NotImplemented. Accepts Bool too, since bool
// subclasses int and shares the same underlying big.Int layout.
//
// CPython: Objects/longobject.c CONVERT_BINOP macro
func intPair(a, b Object) (ai, bi *Int, ok bool) {
	ai, aok := asInt(a)
	bi, bok := asInt(b)
	if !aok || !bok {
		return nil, nil, false
	}
	return ai, bi, true
}

// asInt unwraps Int / Bool to their underlying *Int. Returns
// (nil, false) for any other Object so callers signal NotImplemented.
func asInt(o Object) (*Int, bool) {
	switch v := o.(type) {
	case *Int:
		return v, true
	case *Bool:
		return &v.Int, true
	}
	return nil, false
}
