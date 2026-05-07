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
//
// CPython: Objects/longobject.c long_invert
func intInvert(o Object) (Object, error) {
	i := o.(*Int)
	return NewIntFromBig(new(big.Int).Not(&i.v)), nil
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
