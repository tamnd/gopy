// int64 fast path for the Int arithmetic and bitwise slots.
// CPython folds the "both operands fit one machine digit" case into a
// branch at the top of long_add / long_sub / long_mul / long_and / etc.
// that does native arithmetic on stwodigits before falling back to the
// multi-digit path. gopy's storage is math/big, so the analog is to
// test IsInt64() on both operands, do the op in native int64 with an
// overflow guard, and only fall through to big.Int when the inputs (or
// the result) outgrow the machine word.
//
// CPython: Objects/longobject.c:3737 long_add (_PyLong_BothAreCompact branch)
// CPython: Objects/longobject.c:3785 long_sub (compact branch)
// CPython: Objects/longobject.c:4244 long_mul (compact branch)
// CPython: Objects/longobject.c:5600 long_and / :5611 long_xor / :5623 long_or
// CPython: Objects/longobject.c:5176 long_neg (compact branch)

package objects

import "math/bits"

// compactInt returns the int64 value of i when it fits, paired with
// ok=true. Mirrors CPython's _PyLong_IsCompact + medium_value combo at
// Include/internal/pycore_long.h:154.
//
// CPython: Include/internal/pycore_long.h:154 _PyLong_IsCompact
func compactInt(i *Int) (int64, bool) {
	if i.v.IsInt64() {
		return i.v.Int64(), true
	}
	return 0, false
}

// compactPair returns the int64 values of both operands when both fit.
// Used at the top of every binary fast path to gate the int64 branch.
//
// CPython: Include/internal/pycore_long.h:166 _PyLong_BothAreCompact
func compactPair(a, b *Int) (x, y int64, ok bool) {
	x, xok := compactInt(a)
	if !xok {
		return 0, 0, false
	}
	y, yok := compactInt(b)
	if !yok {
		return 0, 0, false
	}
	return x, y, true
}

// addOverflow returns a+b plus a bool that is true on signed int64
// overflow. Uses math/bits.Add64 to get the carry out of the unsigned
// addition then reconstructs the signed-overflow flag from the operand
// sign bits.
//
// CPython: bytecodes.c:_BINARY_OP_ADD_INT uses __builtin_add_overflow.
func addOverflow(a, b int64) (int64, bool) {
	r := a + b
	// signed overflow iff operands had the same sign but result flipped
	return r, ((a ^ r) & (b ^ r)) < 0
}

// subOverflow returns a-b plus a bool that is true on signed int64
// overflow. Same trick as addOverflow with the second operand negated.
func subOverflow(a, b int64) (int64, bool) {
	r := a - b
	return r, ((a ^ b) & (a ^ r)) < 0
}

// mulOverflow returns a*b plus a bool that is true on signed int64
// overflow. Uses bits.Mul64 on the absolute values and then checks
// whether the high word can be represented alongside the result sign.
func mulOverflow(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	// Compute |a|, |b|, and remember the result sign.
	ua, ub := uint64(a), uint64(b)
	neg := false
	if a < 0 {
		ua = uint64(-a)
		neg = !neg
	}
	if b < 0 {
		ub = uint64(-b)
		neg = !neg
	}
	hi, lo := bits.Mul64(ua, ub)
	if hi != 0 {
		return 0, true
	}
	// lo must fit in int64 with the result sign. The boundary cases are
	// int64Min (-2^63) where lo == 1<<63 and neg == true, and otherwise
	// lo <= int64Max (== 1<<63 - 1).
	if neg {
		if lo > 1<<63 {
			return 0, true
		}
		return -int64(lo), false
	}
	if lo > 1<<63-1 {
		return 0, true
	}
	return int64(lo), false
}

// negOverflow returns -a plus a bool that is true on signed int64
// overflow. The only overflow case is a == math.MinInt64.
func negOverflow(a int64) (int64, bool) {
	if a == -1<<63 {
		return 0, true
	}
	return -a, false
}

// absOverflow returns |a| plus a bool that is true on signed int64
// overflow. Same single overflow case as negOverflow.
func absOverflow(a int64) (int64, bool) {
	if a == -1<<63 {
		return 0, true
	}
	if a < 0 {
		return -a, false
	}
	return a, false
}
