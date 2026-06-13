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
	"math/big"

	"github.com/tamnd/gopy/objects"
)

// Round implements round(number, ndigits=None). Both arguments are
// positional-or-keyword, matching the clinic signature; CPython then
// dispatches through number.__round__, so any type defining the special
// method works, not just int and float.
//
// CPython: Python/bltinmodule.c:2601 builtin_round_impl
func Round(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	number, ndigits, err := bindRoundArgs(args, kwargs)
	if err != nil {
		return nil, err
	}
	switch v := number.(type) {
	case *objects.Bool:
		return roundInt(&v.Int, ndigits)
	case *objects.Int:
		return roundInt(v, ndigits)
	case *objects.Float:
		return roundFloat(v.Float64(), ndigits)
	}
	// Generic delegation: _PyObject_LookupSpecial(number, "__round__").
	// The lookup is TYPE-level only, so a __round__ assigned to the
	// instance (rather than the class) does not satisfy the slot, matching
	// CPython where t.__round__ = lambda: ... still raises TypeError.
	// None ndigits calls __round__() with no args, otherwise __round__(ndigits).
	//
	// CPython: Python/bltinmodule.c:2613 builtin_round_impl
	round, lookErr := objects.LookupSpecial(number, "__round__")
	if lookErr != nil {
		return nil, lookErr
	}
	if round == nil {
		return nil, fmt.Errorf("TypeError: type %s doesn't define __round__ method", number.Type().Name)
	}
	if objects.IsNone(ndigits) {
		return objects.CallNoArgs(round)
	}
	return objects.CallOneArg(round, ndigits)
}

// bindRoundArgs maps the positional/keyword arguments onto (number,
// ndigits), rejecting duplicates and unknown keywords the way the
// clinic-generated parser does.
//
// CPython: Python/clinic/bltinmodule.c.h builtin_round
func bindRoundArgs(args []objects.Object, kwargs map[string]objects.Object) (number, ndigits objects.Object, err error) {
	ndigits = objects.None()
	if len(args) > 2 {
		return nil, nil, fmt.Errorf("TypeError: round expected at most 2 arguments, got %d", len(args))
	}
	ndigitsSet := false
	if len(args) >= 1 {
		number = args[0]
	}
	if len(args) >= 2 {
		ndigits = args[1]
		ndigitsSet = true
	}
	for k, v := range kwargs {
		switch k {
		case "number":
			if number != nil {
				return nil, nil, fmt.Errorf("TypeError: argument for round() given by name ('number') and position (1)")
			}
			number = v
		case "ndigits":
			if ndigitsSet {
				return nil, nil, fmt.Errorf("TypeError: argument for round() given by name ('ndigits') and position (2)")
			}
			ndigits = v
			ndigitsSet = true
		default:
			return nil, nil, fmt.Errorf("TypeError: '%s' is an invalid keyword argument for round()", k)
		}
	}
	if number == nil {
		return nil, nil, fmt.Errorf("TypeError: round() missing required argument 'number' (pos 1)")
	}
	return number, ndigits, nil
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
// quotient. Mirrors _PyLong_DivmodNear's banker's-rounding behavior
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
	return objects.FloatRoundImpl(x, ndigits)
}
