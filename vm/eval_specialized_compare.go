// Fast-path arms for the COMPARE_OP family.
//
// Each variant validates both operands are the expected exact type,
// computes the bitmask CPython uses for the comparison result, and
// pushes True/False according to the oparg low 4 bits.
//
// Cache layout (1 codeunit, _PyCompareOpCache):
//   cell 1   counter
//
// CPython: Python/bytecodes.c _COMPARE_OP_FLOAT / _COMPARE_OP_INT / _COMPARE_OP_STR
// CPython: Include/internal/pycore_code.h:502 COMPARISON_BIT

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// comparisonBit mirrors the COMPARISON_BIT macro: returns 1 for
// unordered (only reachable on NaN inputs), 2 for x<y, 4 for x>y,
// 8 for x==y.
//
// CPython: Include/internal/pycore_code.h:502 COMPARISON_BIT
func comparisonBitFloat(x, y float64) uint32 {
	shift := uint32(0)
	if x >= y {
		shift += 2
	}
	if x <= y {
		shift++
	}
	return 1 << shift
}

func comparisonBitInt(x, y int64) uint32 {
	shift := uint32(0)
	if x >= y {
		shift += 2
	}
	if x <= y {
		shift++
	}
	return 1 << shift
}

// fastCompareOpFloat: both operands are exact float; push True/False
// per the oparg comparison mask.
//
// CPython: Python/bytecodes.c _COMPARE_OP_FLOAT
func (e *evalState) fastCompareOpFloat(oparg uint32) (int, bool) {
	right, rOk := e.peek(0).AsObject().(*objects.Float)
	left, lOk := e.peek(1).AsObject().(*objects.Float)
	if !lOk || !rOk || !objects.IsExactFloat(left) || !objects.IsExactFloat(right) {
		return 0, false
	}
	bit := comparisonBitFloat(left.Float64(), right.Float64())
	e.pop()
	e.pop()
	if bit&oparg != 0 {
		e.pushObject(objects.True())
	} else {
		e.pushObject(objects.False())
	}
	return e.cacheAdvance(compile.COMPARE_OP), true
}

// fastCompareOpInt: both operands are exact int and fit int64. The
// specializer already filtered on Int64() success, but the arm
// re-checks since a mutation could have grown either operand.
//
// CPython: Python/bytecodes.c _COMPARE_OP_INT
func (e *evalState) fastCompareOpInt(oparg uint32) (int, bool) {
	right, rOk := e.peek(0).AsObject().(*objects.Int)
	left, lOk := e.peek(1).AsObject().(*objects.Int)
	if !lOk || !rOk || !objects.IsExactInt(left) || !objects.IsExactInt(right) {
		return 0, false
	}
	lv, lok := left.Int64()
	rv, rok := right.Int64()
	if !lok || !rok {
		return 0, false
	}
	bit := comparisonBitInt(lv, rv)
	e.pop()
	e.pop()
	if bit&oparg != 0 {
		e.pushObject(objects.True())
	} else {
		e.pushObject(objects.False())
	}
	return e.cacheAdvance(compile.COMPARE_OP), true
}

// fastCompareOpStr: both operands are exact str; only == and != reach
// this arm. The result follows CPython's ((COMPARISON_NOT_EQUALS + eq)
// & oparg) formula so == maps to bit 8 (when equal) and != maps to
// bit 7 (when not equal).
//
// CPython: Python/bytecodes.c _COMPARE_OP_STR
func (e *evalState) fastCompareOpStr(oparg uint32) (int, bool) {
	right, rOk := e.peek(0).AsObject().(*objects.Unicode)
	left, lOk := e.peek(1).AsObject().(*objects.Unicode)
	if !lOk || !rOk || !objects.IsExactStr(left) || !objects.IsExactStr(right) {
		return 0, false
	}
	var eq uint32
	if left.Value() == right.Value() {
		eq = 1
	}
	const notEquals = 7 // COMPARISON_NOT_EQUALS
	e.pop()
	e.pop()
	if (notEquals+eq)&oparg != 0 {
		e.pushObject(objects.True())
	} else {
		e.pushObject(objects.False())
	}
	return e.cacheAdvance(compile.COMPARE_OP), true
}
