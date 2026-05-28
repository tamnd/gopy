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

// intLshift / intRshift use uint shift counts; CPython raises on
// negative shift counts and on counts that overflow C long.
//
// CPython: Objects/longobject.c long_lshift / long_rshift
func intLshift(a, b Object) (Object, error) {
	ai, n, err := shiftOperands(a, b)
	if err != nil {
		return nil, err
	}
	if ai == nil {
		return notImplemented(), nil
	}
	return NewIntFromBig(new(big.Int).Lsh(&ai.v, n)), nil
}

func intRshift(a, b Object) (Object, error) {
	ai, n, err := shiftOperands(a, b)
	if err != nil {
		return nil, err
	}
	if ai == nil {
		return notImplemented(), nil
	}
	return NewIntFromBig(new(big.Int).Rsh(&ai.v, n)), nil
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

// shiftOperands extracts the (int, shift count) pair shared by lshift
// and rshift. Returns (nil, 0, nil) when the second operand is not an
// int so the caller can return NotImplemented.
//
// CPython: Objects/longobject.c long_lshift (operand validation)
func shiftOperands(a, b Object) (*Int, uint, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return nil, 0, nil
	}
	if bi.v.Sign() < 0 {
		return nil, 0, errors.New("ValueError: negative shift count")
	}
	n, fits := bi.Int64()
	if !fits || n > shiftCountLimit {
		return nil, 0, errors.New("OverflowError: shift count too large")
	}
	return ai, uint(n), nil
}
