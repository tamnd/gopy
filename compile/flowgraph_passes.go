// Sequence-level helper functions used by the CFG graph passes.
// The flat-sequence optimization passes that used to live here have been
// retired in favor of the cfgBuilder graph pipeline.
//
// CPython: Python/flowgraph.c:L3659 _PyCfg_OptimizeCodeUnit panel

package compile

import (
	"math/bits"
)

// MAX_INT_SIZE caps the bit-length of folded integer results. CPython
// runs unlimited-precision arithmetic and refuses to fold once the
// product / power / shift exceeds 128 bits; gopy stores constants as
// int64, so the effective ceiling is 63 (sign bit reserved).
//
// CPython: Python/flowgraph.c:1690 MAX_INT_SIZE
const maxIntFoldBits = 63

// In-place BINARY_OP suboperators not declared in codegen_expr_op.go.
// Values come from CPython's NB_INPLACE_* enum.
//
// CPython: Include/opcode.h NB_INPLACE_ADD etc.
const (
	nbInplAdd int32 = 13
	nbInplAnd int32 = 14
	nbInplLsh int32 = 16
	nbInplMul int32 = 18
	nbInplOr  int32 = 22
	nbInplRsh int32 = 24
	nbInplSub int32 = 23
	nbInplXor int32 = 25
)

// evalIntBinop computes the result of x <op> y for integer operands,
// or returns ok=false if the operator is one we do not fold (TRUE_DIVIDE,
// MATRIX_MULTIPLY, FLOOR_DIVIDE on a zero divisor, POWER with a
// negative exponent, etc.).
//
// CPython: Python/flowgraph.c:1791 eval_const_binop
func evalIntBinop(op int32, x, y int64) (int64, bool) {
	switch op {
	case nbAdd, nbInplAdd:
		return safeAdd(x, y)
	case nbSubtract, nbInplSub:
		return safeSub(x, y)
	case nbMult, nbInplMul:
		return safeMultiply(x, y)
	case nbAnd, nbInplAnd:
		return x & y, true
	case nbOr, nbInplOr:
		return x | y, true
	case nbXor, nbInplXor:
		return x ^ y, true
	case nbLShift, nbInplLsh:
		return safeLshift(x, y)
	case nbRShift, nbInplRsh:
		if y < 0 || y >= 64 {
			return 0, false
		}
		return x >> uint(y), true
	case nbPower:
		return safePower(x, y)
	case nbFloorDiv:
		if y == 0 {
			return 0, false
		}
		// Python floor div rounds toward negative infinity.
		q := x / y
		if (x%y != 0) && ((x < 0) != (y < 0)) {
			q--
		}
		return q, true
	case nbRemainder:
		if y == 0 {
			return 0, false
		}
		r := x % y
		if r != 0 && ((r < 0) != (y < 0)) {
			r += y
		}
		return r, true
	}
	return 0, false
}

// intBitLen reports the bit length of |v|, matching _PyLong_NumBits.
//
// CPython: Objects/longobject.c _PyLong_NumBits
func intBitLen(v int64) int {
	if v < 0 {
		v = -v
	}
	return bits.Len64(uint64(v))
}

// safeAdd returns x + y when the result fits in int64. Matches
// CPython's implicit "fits in MAX_INT_SIZE bits" guard for the
// gopy int64 const pool.
//
// CPython: Python/flowgraph.c:1799 PyNumber_Add (eval_const_binop)
func safeAdd(x, y int64) (int64, bool) {
	r := x + y
	if (y > 0 && r < x) || (y < 0 && r > x) {
		return 0, false
	}
	return r, true
}

// safeSub returns x - y when the result fits in int64.
//
// CPython: Python/flowgraph.c:1802 PyNumber_Subtract (eval_const_binop)
func safeSub(x, y int64) (int64, bool) {
	r := x - y
	if (y > 0 && r > x) || (y < 0 && r < x) {
		return 0, false
	}
	return r, true
}

// safeMultiply mirrors const_folding_safe_multiply for the int64
// path. CPython caps at 128 bits combined; we cap at 63 to keep the
// result in int64.
//
// CPython: Python/flowgraph.c:1696 const_folding_safe_multiply
func safeMultiply(x, y int64) (int64, bool) {
	if x == 0 || y == 0 {
		return 0, true
	}
	if intBitLen(x)+intBitLen(y) > maxIntFoldBits {
		return 0, false
	}
	return x * y, true
}

// safeLshift mirrors const_folding_safe_lshift for the int64 path.
//
// CPython: Python/flowgraph.c:1761 const_folding_safe_lshift
func safeLshift(x, y int64) (int64, bool) {
	if y < 0 {
		return 0, false
	}
	if x == 0 || y == 0 {
		return x, true
	}
	if y > maxIntFoldBits || intBitLen(x)+int(y) > maxIntFoldBits {
		return 0, false
	}
	return x << uint(y), true
}

// safePower mirrors const_folding_safe_power for the int64 path. Only
// folds non-negative exponents; negative exponents would produce a
// float, and gopy keeps the const pool homogeneous per slot.
//
// CPython: Python/flowgraph.c:1741 const_folding_safe_power
func safePower(x, y int64) (int64, bool) {
	if y < 0 {
		return 0, false
	}
	if x == 0 {
		if y == 0 {
			return 1, true
		}
		return 0, true
	}
	if y == 0 {
		return 1, true
	}
	xbits := intBitLen(x)
	if xbits == 0 {
		xbits = 1
	}
	if int64(xbits)*y > int64(maxIntFoldBits) {
		return 0, false
	}
	result := int64(1)
	base := x
	exp := y
	for exp > 0 {
		if exp&1 == 1 {
			r, ok := safeMultiply(result, base)
			if !ok {
				return 0, false
			}
			result = r
		}
		exp >>= 1
		if exp > 0 {
			r, ok := safeMultiply(base, base)
			if !ok {
				return 0, false
			}
			base = r
		}
	}
	return result, true
}

// appendConst returns the index of v in *consts, appending if not
// present. Linear search is fine here: the per-unit pool is small
// and flowgraph runs once per scope.
//
// CPython: Python/flowgraph.c add_const
func appendConst(consts *[]any, v any) int {
	if isComparableConst(v) {
		for i, c := range *consts {
			if isComparableConst(c) && c == v {
				return i
			}
		}
	}
	*consts = append(*consts, v)
	return len(*consts) - 1
}

// isComparableConst reports whether v can be used with the == operator
// without panicking. Tuples (modeled as []any in the const pool) are
// not directly comparable; CPython's add_const reuses by hash+eq, which
// gopy will revisit when 1713 lands the const-key port. Until then,
// duplicate tuple consts simply both land in the pool.
func isComparableConst(v any) bool {
	switch v.(type) {
	case nil, bool, int64, float64, string, complex128:
		return true
	}
	return false
}

// minConstSequenceSize mirrors CPython's MIN_CONST_SEQUENCE_SIZE: a
// list / set literal shorter than this stays as N pushes + BUILD_X
// because the LIST_EXTEND prelude would not pay off.
//
// CPython: Python/flowgraph.c:1585 MIN_CONST_SEQUENCE_SIZE
const minConstSequenceSize = 3

// hasJumpTarget reports whether op carries a label oparg, including the
// pseudo JUMP / JUMP_NO_INTERRUPT opcodes that have no opcode-metadata
// row. HasTarget alone returns false for the pseudo forms, which would
// leave their opargs unrewritten by the NOP-compaction pass.
func hasJumpTarget(op Opcode) bool {
	switch op {
	case JUMP, JUMP_NO_INTERRUPT, SETUP_FINALLY, SETUP_WITH, SETUP_CLEANUP:
		return true
	}
	return HasTarget(op)
}

// isFoldableUnary reports whether op is one of the three unary opcodes
// the folder rewrites.
//
// CPython: Python/flowgraph.c:1898 eval_const_unaryop opcode switch
func isFoldableUnary(op Opcode) bool {
	switch op {
	case UNARY_NEGATIVE, UNARY_INVERT, UNARY_NOT:
		return true
	}
	return false
}

// evalConstUnaryop applies one unary opcode to a const operand, mirroring
// CPython's PyNumber_Negative / PyNumber_Invert / bool(!x) dispatch.
// Returns ok=false when the operand type is not foldable (e.g. unary
// negate of a string) or would overflow the int64 representation.
//
// CPython: Python/flowgraph.c:1894 eval_const_unaryop
func evalConstUnaryop(op Opcode, operand any) (any, bool) {
	switch op {
	case UNARY_NEGATIVE:
		switch v := operand.(type) {
		case int64:
			if v == minInt64 {
				return nil, false
			}
			return -v, true
		case float64:
			return -v, true
		}
	case UNARY_INVERT:
		// CPython rejects ~bool with a SyntaxWarning. gopy's const pool
		// doesn't carry bool through this path (the AST folder promotes
		// `True` / `False` to int in numeric context), so the bool
		// guard is unreachable here.
		if v, ok := operand.(int64); ok {
			return ^v, true
		}
	case UNARY_NOT:
		b, ok := constTruthValue(operand)
		if !ok {
			return nil, false
		}
		return !b, true
	}
	return nil, false
}

const minInt64 int64 = -1 << 63

// constTruthValue mirrors PyObject_IsTrue for the const-pool value
// shapes gopy stores. Returns ok=false when the value's truthiness is
// not statically determinable (e.g. a user-typed value reaching the
// const pool via marshal round-trip).
//
// CPython: Python/flowgraph.c:1916 eval_const_unaryop UNARY_NOT case
func constTruthValue(v any) (bool, bool) {
	switch x := v.(type) {
	case nil:
		return false, true
	case bool:
		return x, true
	case int64:
		return x != 0, true
	case float64:
		return x != 0, true
	case string:
		return x != "", true
	case complex128:
		return real(x) != 0 || imag(x) != 0, true
	}
	return false, false
}
