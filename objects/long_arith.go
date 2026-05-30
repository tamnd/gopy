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

// intTrueDiv implements `a / b` for ints with last-bit-exact IEEE-754
// rounding. Ports CPython's long_true_divide shift+divmod algorithm:
// scale operands so the quotient lands in the float53 mantissa window,
// take a bigint divmod, apply round-half-to-even on the remainder, then
// ldexp the integer mantissa back into a float64. This is the only way
// to get bit-exact parity with CPython on subnormals and half-way cases.
//
// CPython: Objects/longobject.c:4504 long_true_divide
func intTrueDiv(a, b Object) (Object, error) {
	ai, bi, ok := intPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bi.v.Sign() == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
	}
	negative := (ai.v.Sign() < 0) != (bi.v.Sign() < 0)
	if ai.v.Sign() == 0 {
		if negative {
			return NewFloat(math.Copysign(0, -1)), nil
		}
		return NewFloat(0), nil
	}
	// Work with absolute values.
	aAbs := new(big.Int).Abs(&ai.v)
	bAbs := new(big.Int).Abs(&bi.v)

	const dblMantDig = 53
	const dblMaxExp = 1024
	const dblMinExp = -1021

	// Overflow threshold from long_true_divide: the smallest a/b that
	// rounds to +Inf is 2 to the DBL_MAX_EXP minus 2 to the
	// (DBL_MAX_EXP minus DBL_MANT_DIG minus 1).
	dblMinOverflow := new(big.Int).Lsh(big.NewInt(1), dblMaxExp)
	dblMinOverflow.Sub(dblMinOverflow, new(big.Int).Lsh(big.NewInt(1), dblMaxExp-dblMantDig-1))
	threshold := new(big.Int).Mul(dblMinOverflow, bAbs)
	if aAbs.Cmp(threshold) >= 0 {
		return nil, errors.New("OverflowError: integer division result too large for a float")
	}

	// d satisfies 2**(d-1) <= a/b < 2**d
	d := aAbs.BitLen() - bAbs.BitLen()
	// Adjust: if a >= 2**d * b (positive d) or a*2**(-d) >= b (negative d), d += 1
	if d >= 0 {
		shifted := new(big.Int).Lsh(bAbs, uint(d))
		if aAbs.Cmp(shifted) >= 0 {
			d++
		}
	} else {
		shifted := new(big.Int).Lsh(aAbs, uint(-d))
		if shifted.Cmp(bAbs) >= 0 {
			d++
		}
	}

	var exp int
	if d < dblMinExp {
		exp = dblMinExp - dblMantDig
	} else {
		exp = d - dblMantDig
	}

	scaledA, scaledB := new(big.Int).Set(aAbs), new(big.Int).Set(bAbs)
	if exp < 0 {
		scaledA.Lsh(scaledA, uint(-exp))
	} else if exp > 0 {
		scaledB.Lsh(scaledB, uint(exp))
	}

	q, r := new(big.Int), new(big.Int)
	q.QuoRem(scaledA, scaledB, r)

	// Round half-to-even: 2*r > b, or 2*r == b and q odd.
	twoR := new(big.Int).Lsh(r, 1)
	cmp := twoR.Cmp(scaledB)
	if cmp > 0 || (cmp == 0 && q.Bit(0) == 1) {
		q.Add(q, bigOne)
	}

	// Convert q * 2^exp to float64 via math.Ldexp on the int mantissa.
	// q fits in DBL_MANT_DIG+1 bits at this point.
	qf, _ := new(big.Float).SetPrec(dblMantDig + 1).SetInt(q).Float64()
	result := math.Ldexp(qf, exp)
	if math.IsInf(result, 0) {
		return nil, errors.New("OverflowError: integer division result too large for a float")
	}
	if negative {
		result = -result
	}
	return NewFloat(result), nil
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
