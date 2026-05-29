// Arithmetic int slots: add, sub, mul, true-div, floor-div, mod,
// pow, divmod. CPython splits these into long_add / long_sub / etc.
// in longobject.c; gopy collapses each one to a math/big call plus
// the one quirk Python adds (floor / sign-of-divisor for div and
// mod, NotImplemented fallthrough for negative pow).
//
// CPython: Objects/longobject.c:3107 long_add

package objects

import (
	"errors"
	"math"
	"math/big"
)

func intAdd(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if x, y, both := compactPair(ai, bi); both {
		if r, over := addOverflow(x, y); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Add(&ai.v, &bi.v)), nil
}

func intSub(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if x, y, both := compactPair(ai, bi); both {
		if r, over := subOverflow(x, y); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Sub(&ai.v, &bi.v)), nil
}

func intMul(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if x, y, both := compactPair(ai, bi); both {
		if r, over := mulOverflow(x, y); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Mul(&ai.v, &bi.v)), nil
}

// intTrueDiv implements `a / b` for ints. CPython returns a float
// even for exact integer ratios, so we shadow that contract here.
//
// CPython: Objects/longobject.c:4053 long_true_divide
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
// CPython: Objects/longobject.c:3654 long_div
func intFloorDiv(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
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
// CPython: Objects/longobject.c:3717 long_mod
func intMod(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(&ai.v, &bi.v, r)
	if r.Sign() != 0 && (ai.v.Sign() < 0) != (bi.v.Sign() < 0) {
		r.Add(r, &bi.v)
	}
	return NewIntFromBig(r), nil
}

// intDivmod returns (a // b, a % b) as a tuple, matching long_divmod.
// The quotient and remainder follow Python's floor / sign-of-divisor
// semantics already encoded by intFloorDiv and intMod.
//
// CPython: Objects/longobject.c:3736 long_divmod
func intDivmod(a, b Object) (Object, error) {
	q, err := intFloorDiv(a, b)
	if err != nil {
		return nil, err
	}
	if IsNotImplemented(q) {
		return q, nil
	}
	r, err := intMod(a, b)
	if err != nil {
		return nil, err
	}
	if IsNotImplemented(r) {
		return r, nil
	}
	return NewTuple([]Object{q, r}), nil
}

// intPower implements `pow(a, b)` and `pow(a, b, mod)` for ints.
// CPython promotes `int ** neg_int` (no modulus) to a float, so we do
// the same here via math.Pow on float64. The three-argument form with a
// negative exponent computes the modular multiplicative inverse via the
// extended Euclidean algorithm (big.Int.ModInverse).
//
// CPython: Objects/longobject.c:3837 long_pow
func intPower(a, b, mod Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if mod != nil && mod != None() {
		mi, mok := mod.(*Int)
		if !mok {
			return notImplemented(), nil
		}
		if mi.v.Sign() == 0 {
			return nil, errors.New("ValueError: pow() 3rd argument cannot be 0")
		}
		// Normalise modulus to positive for big.Int.Exp, which requires a
		// positive modulus. Apply Python floor-modulo convention at the end.
		// CPython: Objects/longobject.c:4916 (negate c if negative)
		negMod := mi.v.Sign() < 0
		absM := new(big.Int).Abs(&mi.v)
		if absM.Cmp(bigOne) == 0 {
			return NewInt(0), nil
		}
		var result *big.Int
		if bi.v.Sign() < 0 {
			// Negative exponent: compute modular inverse first, then power.
			// CPython: Objects/longobject.c:4955 long_invmod path
			negB := new(big.Int).Neg(&bi.v)
			inv := new(big.Int).ModInverse(&ai.v, absM)
			if inv == nil {
				return nil, errors.New("ValueError: base is not invertible for the given modulus")
			}
			result = new(big.Int).Exp(inv, negB, absM)
		} else {
			result = new(big.Int).Exp(&ai.v, &bi.v, absM)
		}
		// Convert Go's non-negative result to Python floor-modulo convention:
		// result is in [0, |m|); if m is negative and result != 0, subtract |m|.
		if negMod && result.Sign() != 0 {
			result.Sub(result, absM)
		}
		return NewIntFromBig(result), nil
	}
	if bi.v.Sign() < 0 {
		// CPython: Objects/longobject.c:3897 "zero to a negative power"
		if ai.v.Sign() == 0 {
			return nil, errors.New("ZeroDivisionError: zero to a negative power")
		}
		af, _ := new(big.Float).SetInt(&ai.v).Float64()
		bf, _ := new(big.Float).SetInt(&bi.v).Float64()
		return NewFloat(math.Pow(af, bf)), nil
	}
	return NewIntFromBig(new(big.Int).Exp(&ai.v, &bi.v, nil)), nil
}

var bigOne = big.NewInt(1)
