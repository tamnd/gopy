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

// IntMaxStrDigitsHook returns the current sys.int_max_str_digits limit.
// Zero means unlimited. module/sys sets this during init so the
// objects layer can enforce the limit on int->str without pulling in
// sys as a dependency.
//
// CPython: Objects/longobject.c:30 _MAX_STR_DIGITS_ERROR_FMT_TO_STR
var IntMaxStrDigitsHook func() int32

// checkIntToStrLimit raises ValueError when n's decimal repr would
// exceed sys.int_max_str_digits. Uses bit length as a lower bound on
// the decimal digit count so the fast path stays O(1). Power-of-2
// bases skip the check at the caller.
//
// CPython: Objects/longobject.c:2049 long_to_decimal_string_internal
// (pre-check using 10/3 >= log2(10))
func checkIntToStrLimit(bitLen int) error {
	if IntMaxStrDigitsHook == nil {
		return nil
	}
	limit := IntMaxStrDigitsHook()
	if limit <= 0 {
		return nil
	}
	// log10(2) ≈ 0.30103. Use 30/100 (a strict under-estimate of
	// log10(2)) so (bit_len - 1) * 30/100 + 1 is a safe lower bound on
	// the decimal digit count of a non-zero integer with bitLen bits.
	if bitLen <= 0 {
		return nil
	}
	minDigits := (bitLen-1)*30/100 + 1
	if int32(minDigits) > limit {
		return fmt.Errorf("ValueError: Exceeds the limit (%d digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit", limit)
	}
	return nil
}

func intRepr(o Object) (string, error) {
	i := o.(*Int)
	if err := checkIntToStrLimit(i.v.BitLen()); err != nil {
		return "", err
	}
	s := i.v.String()
	// Exact post-check: the bit-length lower bound only catches huge
	// values; a number right at the boundary may pass the pre-check
	// but still exceed the limit. The actual digit count without sign
	// is what CPython compares to.
	//
	// CPython: Objects/longobject.c:2135 long_to_decimal_string_internal
	if IntMaxStrDigitsHook != nil {
		if limit := IntMaxStrDigitsHook(); limit > 0 {
			n := len(s)
			if n > 0 && s[0] == '-' {
				n--
			}
			if int32(n) > limit {
				return "", fmt.Errorf("ValueError: Exceeds the limit (%d digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit", limit)
			}
		}
	}
	return s, nil
}

// intHash mirrors CPython's long_hash: reduce modulo 2^61-1 and
// negate when the source is negative, then remap the -1 sentinel to
// -2 so callers can distinguish "hash is literally -1" from the
// generic error sentinel.
//
// CPython: Objects/longobject.c:3551 long_hash
// intHash ports long_hash: compute |n| mod (2^61-1), negate if n < 0.
//
// CPython: Objects/longobject.c:3397 long_hash
func intHash(o Object) (int64, error) {
	const modulus int64 = (1 << 61) - 1
	n := &o.(*Int).v
	sign := n.Sign()
	abs := new(big.Int).Abs(n)
	abs.Mod(abs, big.NewInt(modulus))
	h := abs.Int64()
	if sign < 0 {
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
	ai, ok := asInt(a)
	if !ok {
		return nil, fmt.Errorf("intRichCmp: lhs is %T", a)
	}
	bi, ok := asInt(b)
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

// intNeg / intAbs / intPos cover -x, abs(x), +x. For plain int, Pos
// returns self. For subclasses (including bool), all three return a
// fresh plain int so that e.g. +True → int(1), -True → int(-1).
//
// CPython: Objects/longobject.c long_neg / long_abs / long_long
func intNeg(o Object) (Object, error) {
	i, ok := asInt(o)
	if !ok {
		return notImplemented(), nil
	}
	if x, ok := compactInt(i); ok {
		if r, over := negOverflow(x); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Neg(&i.v)), nil
}

func intAbs(o Object) (Object, error) {
	i, ok := asInt(o)
	if !ok {
		return notImplemented(), nil
	}
	if x, ok := compactInt(i); ok {
		if r, over := absOverflow(x); !over {
			return NewInt(r), nil
		}
	}
	return NewIntFromBig(new(big.Int).Abs(&i.v)), nil
}

// intPos returns self for plain int; for subclasses (bool included)
// returns a fresh int copy. CPython: Objects/longobject.c long_long
// (PyLong_CheckExact branch returns self, else _PyLong_Copy).
func intPos(o Object) (Object, error) {
	i, ok := asInt(o)
	if !ok {
		return notImplemented(), nil
	}
	if o.Type() == IntType {
		return o, nil
	}
	return NewIntFromBig(new(big.Int).Set(&i.v)), nil
}

func intBool(o Object) (bool, error) {
	i, ok := asInt(o)
	if !ok {
		return false, fmt.Errorf("TypeError: not an int-like object")
	}
	return i.v.Sign() != 0, nil
}

func intFloat(o Object) (Object, error) {
	i, ok := asInt(o)
	if !ok {
		return notImplemented(), nil
	}
	f, _ := new(big.Float).SetInt(&i.v).Float64()
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
