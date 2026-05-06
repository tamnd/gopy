// Port of builtin_round_impl. CPython dispatches through __round__
// by name; gopy does not yet have generic special-method lookup, so
// the int and float branches are inlined here. Other types raise
// TypeError, matching what _PyObject_MaybeCallSpecialNoArgs would do
// when the slot is missing.
//
// CPython: Python/bltinmodule.c:2601 builtin_round_impl
// CPython: Objects/floatobject.c:1034 float___round___impl
// CPython: Objects/longobject.c:6110 int___round___impl

package builtins

import (
	"fmt"
	"math"
	"math/big"

	"github.com/tamnd/gopy/objects"
)

// Round implements round(number[, ndigits]).
//
// CPython: Python/bltinmodule.c:2601 builtin_round_impl
func Round(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: round expected 1 or 2 arguments, got %d", len(args))
	}
	var ndigits objects.Object = objects.None()
	if len(args) == 2 {
		ndigits = args[1]
	}
	switch v := args[0].(type) {
	case *objects.Bool:
		return roundInt(&v.Int, ndigits)
	case *objects.Int:
		return roundInt(v, ndigits)
	case *objects.Float:
		return roundFloat(v.Float64(), ndigits)
	}
	return nil, fmt.Errorf("TypeError: type %s doesn't define __round__ method", args[0].Type().Name)
}

// roundInt mirrors int___round___impl. With None or no ndigits the
// integer is returned unchanged. With a non-negative ndigits the
// integer is also returned unchanged (rounding only matters for the
// fractional part, which an int does not have). With a negative
// ndigits we compute self - divmod_near(self, 10**-ndigits)[1].
//
// CPython: Objects/longobject.c:6110 int___round___impl
func roundInt(self *objects.Int, ndigits objects.Object) (objects.Object, error) {
	if objects.IsNone(ndigits) {
		return objects.NewIntFromBig(self.BigInt()), nil
	}
	n, err := indexAsInt(ndigits)
	if err != nil {
		return nil, err
	}
	if n.BigInt().Sign() >= 0 {
		return objects.NewIntFromBig(self.BigInt()), nil
	}
	nv, fits := n.Int64()
	if !fits {
		// CPython uses arbitrary-precision divmod_near. For a value
		// that won't fit in int64 the divisor 10**-ndigits already
		// dwarfs every gopy int, so the rounded result is 0.
		return objects.NewInt(0), nil
	}
	exp := -nv
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(exp), nil)
	_, r := divmodNear(self.BigInt(), pow)
	out := new(big.Int).Sub(self.BigInt(), r)
	return objects.NewIntFromBig(out), nil
}

// divmodNear returns (q, r) such that q*b + r == a and r is the
// remainder of minimum absolute value, breaking ties toward an even
// quotient. Mirrors _PyLong_DivmodNear's banker's-rounding behaviour
// without dragging in the full long arithmetic surface.
//
// CPython: Objects/longobject.c _PyLong_DivmodNear
func divmodNear(a, b *big.Int) (q, r *big.Int) {
	// Work with a positive divisor; the sign cancels back at the end.
	negDivisor := b.Sign() < 0
	bAbs := new(big.Int).Abs(b)
	q, r = new(big.Int).QuoRem(a, bAbs, new(big.Int))
	// Force r into [0, bAbs) so the comparison below behaves like
	// Python's // / %.
	if r.Sign() < 0 {
		r.Add(r, bAbs)
		q.Sub(q, big.NewInt(1))
	}
	// Compare 2*r against bAbs. Greater means we round up; equal is
	// the halfway case where we pick the even quotient.
	twiceR := new(big.Int).Lsh(r, 1)
	cmp := twiceR.Cmp(bAbs)
	roundUp := cmp > 0 || (cmp == 0 && q.Bit(0) == 1)
	if roundUp {
		q.Add(q, big.NewInt(1))
		r.Sub(r, bAbs)
	}
	if negDivisor {
		q.Neg(q)
	}
	return q, r
}

// roundFloat mirrors float___round___impl. The single-argument form
// returns an int; the two-argument form returns a float. Both use the
// banker's-rounding rule on halfway cases.
//
// CPython: Objects/floatobject.c:1034 float___round___impl
func roundFloat(x float64, ndigits objects.Object) (objects.Object, error) {
	if objects.IsNone(ndigits) {
		if !isFinite(x) {
			return nil, fmt.Errorf("ValueError: cannot convert float %v to integer", x)
		}
		rounded := math.Round(x)
		if math.Abs(x-rounded) == 0.5 {
			rounded = 2 * math.Round(x/2)
		}
		bi, _ := big.NewFloat(rounded).Int(nil)
		return objects.NewIntFromBig(bi), nil
	}
	n, err := indexAsInt(ndigits)
	if err != nil {
		return nil, err
	}
	nv, fits := n.Int64()
	if !fits {
		// Same clamp as float___round___impl's NDIGITS_MAX/_MIN: way
		// outside the meaningful precision range, so x rounds to
		// itself or to a signed zero.
		if n.BigInt().Sign() > 0 {
			return objects.NewFloat(x), nil
		}
		return objects.NewFloat(0.0 * x), nil
	}
	if !isFinite(x) {
		return objects.NewFloat(x), nil
	}
	const (
		ndigitsMax = 323
		ndigitsMin = -308
	)
	if nv > ndigitsMax {
		return objects.NewFloat(x), nil
	}
	if nv < ndigitsMin {
		return objects.NewFloat(0.0 * x), nil
	}
	z, ok := doubleRound(x, int(nv))
	if !ok {
		return nil, fmt.Errorf("OverflowError: overflow occurred during round")
	}
	return objects.NewFloat(z), nil
}

// doubleRound mirrors the fallback double_round in floatobject.c (the
// non-dtoa branch). Splits ndigits into two factors so the
// intermediate product cannot overflow.
//
// CPython: Objects/floatobject.c:974 double_round (fallback path)
func doubleRound(x float64, ndigits int) (float64, bool) {
	var pow1, pow2, y float64
	if ndigits >= 0 {
		if ndigits > 22 {
			pow1 = math.Pow(10, float64(ndigits-22))
			pow2 = 1e22
		} else {
			pow1 = math.Pow(10, float64(ndigits))
			pow2 = 1
		}
		y = (x * pow1) * pow2
		if !isFinite(y) {
			return x, true
		}
	} else {
		pow1 = math.Pow(10, float64(-ndigits))
		pow2 = 1
		y = x / pow1
	}
	z := math.Round(y)
	if math.Abs(y-z) == 0.5 {
		z = 2 * math.Round(y/2)
	}
	if ndigits >= 0 {
		z = (z / pow2) / pow1
	} else {
		z *= pow1
	}
	if !isFinite(z) {
		return 0, false
	}
	return z, true
}

func isFinite(x float64) bool {
	return !math.IsInf(x, 0) && !math.IsNaN(x)
}
