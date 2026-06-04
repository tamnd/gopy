// Bitwise int slots: And, Or, Xor, Invert, Lshift, Rshift. Big.Int
// already implements two's-complement bitwise ops with the right
// semantics for Python (a & b on negatives uses the infinite-bit
// two's-complement view), so each slot is a thin wrapper.
//
// CPython: Objects/longobject.c:5016 long_and / long_or / long_xor

package objects

import (
	"errors"
	"math/big"
)

// shiftCountLimit caps the shift exponent at the same value CPython
// rejects beyond. Larger counts are an OverflowError because they
// would produce values too big to represent.
//
// CPython: Objects/longobject.c long_lshift (PY_SSIZE_T_MAX guard)
const shiftCountLimit = 1 << 31

func intAnd(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if x, y, both := compactPair(ai, bi); both {
		return NewInt(x & y), nil
	}
	return NewIntFromBig(new(big.Int).And(&ai.v, &bi.v)), nil
}

func intOr(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if x, y, both := compactPair(ai, bi); both {
		return NewInt(x | y), nil
	}
	return NewIntFromBig(new(big.Int).Or(&ai.v, &bi.v)), nil
}

func intXor(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if x, y, both := compactPair(ai, bi); both {
		return NewInt(x ^ y), nil
	}
	return NewIntFromBig(new(big.Int).Xor(&ai.v, &bi.v)), nil
}

// intLshift mirrors long_lshift_method: 0 << anything is 0 even when
// the shift count is huge, a negative shift count is a ValueError, and
// a count that does not fit in int64 is "too many digits in integer".
//
// CPython: Objects/longobject.c:5406 long_lshift_method
func intLshift(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() < 0 {
		return nil, errors.New("ValueError: negative shift count")
	}
	if ai.v.Sign() == 0 {
		return NewInt(0), nil
	}
	n, fits := bi.Int64()
	if !fits || n > shiftCountLimit {
		return nil, errors.New("OverflowError: too many digits in integer")
	}
	return NewIntFromBig(new(big.Int).Lsh(&ai.v, uint(n))), nil
}

// intRshift mirrors long_rshift: 0 >> anything is 0, a count beyond
// int64 collapses to 0 for a non-negative left operand and -1 for a
// negative one (matching the sign-extending shift semantics), and a
// negative shift count is a ValueError.
//
// CPython: Objects/longobject.c:5308 long_rshift
func intRshift(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() < 0 {
		return nil, errors.New("ValueError: negative shift count")
	}
	if ai.v.Sign() == 0 {
		return NewInt(0), nil
	}
	n, fits := bi.Int64()
	if !fits || n > shiftCountLimit {
		if ai.v.Sign() < 0 {
			return NewInt(-1), nil
		}
		return NewInt(0), nil
	}
	return NewIntFromBig(new(big.Int).Rsh(&ai.v, uint(n))), nil
}

// intInvert returns the bitwise complement, matching ~x == -(x+1).
// For bool operands a DeprecationWarning is issued (deprecated in 3.12).
//
// CPython: Objects/longobject.c:5158 long_invert (compact branch at
// :5164 uses ~medium_value(v) before falling through to long_add).
// CPython: Objects/longobject.c:5140 long_invert (bool deprecation warn)
func intInvert(o Object) (Object, error) {
	i, ok := asInt(o)
	if !ok {
		return notImplemented(), nil
	}
	if _, isBool := o.(*Bool); isBool {
		if DeprecWarnHook != nil {
			if err := DeprecWarnHook("bitwise inversion of booleans is deprecated. Use `not` instead."); err != nil {
				return nil, err
			}
		}
	}
	if x, ok := compactInt(i); ok {
		// ~x never overflows in two's complement int64.
		return NewInt(^x), nil
	}
	return NewIntFromBig(new(big.Int).Not(&i.v)), nil
}

// boolAnd / boolOr / boolXor return bool when both operands are bool,
// otherwise return int. CPython: Objects/boolobject.c (inherits
// long_and/or/xor but wraps the result in PyBool_FromLong).
//
// CPython: Objects/boolobject.c:L24 PyBool_Type
func boolAnd(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	_, aIsBool := a.(*Bool)
	_, bIsBool := b.(*Bool)
	if x, y, both := compactPair(ai, bi); both {
		r := x & y
		if aIsBool && bIsBool {
			return NewBool(r != 0), nil
		}
		return NewInt(r), nil
	}
	r := new(big.Int).And(&ai.v, &bi.v)
	if aIsBool && bIsBool {
		return NewBool(r.Sign() != 0), nil
	}
	return NewIntFromBig(r), nil
}

func boolOr(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	_, aIsBool := a.(*Bool)
	_, bIsBool := b.(*Bool)
	if x, y, both := compactPair(ai, bi); both {
		r := x | y
		if aIsBool && bIsBool {
			return NewBool(r != 0), nil
		}
		return NewInt(r), nil
	}
	r := new(big.Int).Or(&ai.v, &bi.v)
	if aIsBool && bIsBool {
		return NewBool(r.Sign() != 0), nil
	}
	return NewIntFromBig(r), nil
}

func boolXor(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	_, aIsBool := a.(*Bool)
	_, bIsBool := b.(*Bool)
	if x, y, both := compactPair(ai, bi); both {
		r := x ^ y
		if aIsBool && bIsBool {
			return NewBool(r != 0), nil
		}
		return NewInt(r), nil
	}
	r := new(big.Int).Xor(&ai.v, &bi.v)
	if aIsBool && bIsBool {
		return NewBool(r.Sign() != 0), nil
	}
	return NewIntFromBig(r), nil
}
