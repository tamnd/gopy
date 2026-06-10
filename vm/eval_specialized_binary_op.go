// Fast-path arms for the BINARY_OP family.
//
// CPython 3.14 emits 13 specialized variants for BINARY_OP (excluding
// JIT-only BINARY_OP_EXTEND). Each variant is pre-narrowed by the
// specializer to a fixed pair of exact types and a fixed suboperator,
// so the arm here does not consult the NB_* oparg: the opcode itself
// dictates the operation.
//
// Cache layout: BINARY_OP carries 5 inline cache codeunits
// (compile.CacheCount(BINARY_OP) == 5). After a hit the PC advances
// past one (opcode, oparg) pair plus those 5 zero codeunits via
// cacheAdvance.
//
// CPython: Python/bytecodes.c _BINARY_OP_ADD_INT, _BINARY_OP_SUBTRACT_INT,
//          _BINARY_OP_MULTIPLY_INT, _BINARY_OP_ADD_FLOAT,
//          _BINARY_OP_SUBTRACT_FLOAT, _BINARY_OP_MULTIPLY_FLOAT,
//          _BINARY_OP_ADD_UNICODE, _BINARY_OP_INPLACE_ADD_UNICODE,
//          _BINARY_OP_SUBSCR_LIST_INT, _BINARY_OP_SUBSCR_TUPLE_INT,
//          _BINARY_OP_SUBSCR_STR_INT, _BINARY_OP_SUBSCR_DICT,
//          _BINARY_OP_SUBSCR_LIST_SLICE.

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 6: BINARY_OP_* arms migrate to typed op<NAME> bodies; cache decode/deopt/advance live in the generated harness.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"math"
	"math/bits"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// peekIntInt returns left/right as int64 when both stack tops are
// exactly int and fit int64. Mirrors the CPython _GUARD_BOTH_INT
// guard pair plus the inline PyLong_FitsInLong check.
func (e *evalState) peekIntInt() (left, right int64, ok bool) {
	r, rOk := e.peek(0).AsObject().(*objects.Int)
	l, lOk := e.peek(1).AsObject().(*objects.Int)
	if !lOk || !rOk || !objects.IsExactInt(l) || !objects.IsExactInt(r) {
		return 0, 0, false
	}
	lv, lok := l.Int64()
	rv, rok := r.Int64()
	if !lok || !rok {
		return 0, 0, false
	}
	return lv, rv, true
}

// peekFloatFloat is the float twin of peekIntInt.
func (e *evalState) peekFloatFloat() (left, right float64, ok bool) {
	r, rOk := e.peek(0).AsObject().(*objects.Float)
	l, lOk := e.peek(1).AsObject().(*objects.Float)
	if !lOk || !rOk || !objects.IsExactFloat(l) || !objects.IsExactFloat(r) {
		return 0, 0, false
	}
	return l.Float64(), r.Float64(), true
}

// addOverflow reports left+right with overflow detection.
func addOverflow(left, right int64) (int64, bool) {
	sum, carry := bits.Add64(uint64(left), uint64(right), 0)
	// Signed overflow when both operands share sign but result differs.
	if ((left ^ int64(sum)) & (right ^ int64(sum))) < 0 {
		return 0, false
	}
	_ = carry
	return int64(sum), true
}

// subOverflow reports left-right with overflow detection.
func subOverflow(left, right int64) (int64, bool) {
	diff, _ := bits.Sub64(uint64(left), uint64(right), 0)
	if ((left ^ right) & (left ^ int64(diff))) < 0 {
		return 0, false
	}
	return int64(diff), true
}

// mulOverflow reports left*right with overflow detection.
func mulOverflow(left, right int64) (int64, bool) {
	if left == 0 || right == 0 {
		return 0, true
	}
	prod := left * right
	if prod/right != left {
		return 0, false
	}
	// Edge case: math.MinInt64 * -1 wraps to itself without tripping the
	// division check on the divisor=-1 path.
	if left == math.MinInt64 && right == -1 {
		return 0, false
	}
	return prod, true
}

// pushBinaryResult finalizes a BINARY_OP fast-path hit: pop both
// operands, run DECREF_INPUTS on them, and push the result. CPython's
// _BINARY_OP_* macros end with DECREF_INPUTS()+PUSH(res) after computing
// res from left+right. The caller owns res: arithmetic arms build a fresh
// object while the container-element subscr arms incref the borrowed slot
// before handing it here, so the steal in pushObject lands an owned value.
//
// CPython: Python/bytecodes.c BINARY_OP family (res = ...; DECREF_INPUTS())
func (e *evalState) pushBinaryResult(res objects.Object) {
	b := e.popObject()
	a := e.popObject()
	objects.Decref(a)
	objects.Decref(b)
	e.pushObject(res)
}

// fastBinaryOpAddInt: both operands exact int, both fit int64, sum
// also fits int64. Overflow falls back to the adaptive parent so the
// generic NB_ADD path can route through big.Int.
//
// CPython: Python/bytecodes.c _BINARY_OP_ADD_INT
func (e *evalState) fastBinaryOpAddInt() (int, bool) {
	l, r, ok := e.peekIntInt()
	if !ok {
		return 0, false
	}
	sum, ok := addOverflow(l, r)
	if !ok {
		return 0, false
	}
	e.pushBinaryResult(objects.NewInt(sum))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpSubtractInt.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBTRACT_INT
func (e *evalState) fastBinaryOpSubtractInt() (int, bool) {
	l, r, ok := e.peekIntInt()
	if !ok {
		return 0, false
	}
	diff, ok := subOverflow(l, r)
	if !ok {
		return 0, false
	}
	e.pushBinaryResult(objects.NewInt(diff))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpMultiplyInt.
//
// CPython: Python/bytecodes.c _BINARY_OP_MULTIPLY_INT
func (e *evalState) fastBinaryOpMultiplyInt() (int, bool) {
	l, r, ok := e.peekIntInt()
	if !ok {
		return 0, false
	}
	prod, ok := mulOverflow(l, r)
	if !ok {
		return 0, false
	}
	e.pushBinaryResult(objects.NewInt(prod))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpAddFloat.
//
// CPython: Python/bytecodes.c _BINARY_OP_ADD_FLOAT
func (e *evalState) fastBinaryOpAddFloat() (int, bool) {
	l, r, ok := e.peekFloatFloat()
	if !ok {
		return 0, false
	}
	e.pushBinaryResult(objects.NewFloat(l + r))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpSubtractFloat.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBTRACT_FLOAT
func (e *evalState) fastBinaryOpSubtractFloat() (int, bool) {
	l, r, ok := e.peekFloatFloat()
	if !ok {
		return 0, false
	}
	e.pushBinaryResult(objects.NewFloat(l - r))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpMultiplyFloat.
//
// CPython: Python/bytecodes.c _BINARY_OP_MULTIPLY_FLOAT
func (e *evalState) fastBinaryOpMultiplyFloat() (int, bool) {
	l, r, ok := e.peekFloatFloat()
	if !ok {
		return 0, false
	}
	e.pushBinaryResult(objects.NewFloat(l * r))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpAddUnicode: both operands exact str, push concat. The
// inplace variant shares the body because gopy strings are immutable
// (CPython's inplace path mutates the LHS when refcount=1; the
// observable behavior is the same concat).
//
// CPython: Python/bytecodes.c _BINARY_OP_ADD_UNICODE,
//
//	_BINARY_OP_INPLACE_ADD_UNICODE
func (e *evalState) fastBinaryOpAddUnicode() (int, bool) {
	r, rOk := e.peek(0).AsObject().(*objects.Unicode)
	l, lOk := e.peek(1).AsObject().(*objects.Unicode)
	if !lOk || !rOk || !objects.IsExactStr(l) || !objects.IsExactStr(r) {
		return 0, false
	}
	e.pushBinaryResult(objects.NewStr(l.Value() + r.Value()))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpSubscrListInt: list[int]. Negative indices are folded
// per Python semantics; out-of-range falls back so the generic path
// raises IndexError.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBSCR_LIST_INT
func (e *evalState) fastBinaryOpSubscrListInt() (int, bool) {
	idxObj, iOk := e.peek(0).AsObject().(*objects.Int)
	lst, lOk := e.peek(1).AsObject().(*objects.List)
	if !lOk || !iOk || !objects.IsExactList(lst) || !objects.IsExactInt(idxObj) {
		return 0, false
	}
	idx, ok := idxObj.Int64()
	if !ok {
		return 0, false
	}
	if idx < 0 {
		idx += int64(lst.Len())
	}
	if idx < 0 || idx >= int64(lst.Len()) {
		return 0, false
	}
	// Item returns a borrowed slot; own it before DECREF_INPUTS drops the
	// list so the value survives onto the stack.
	res := lst.Item(int(idx))
	objects.Incref(res)
	e.pushBinaryResult(res)
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpSubscrTupleInt.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBSCR_TUPLE_INT
func (e *evalState) fastBinaryOpSubscrTupleInt() (int, bool) {
	idxObj, iOk := e.peek(0).AsObject().(*objects.Int)
	tup, tOk := e.peek(1).AsObject().(*objects.Tuple)
	if !tOk || !iOk || !objects.IsExactTuple(tup) || !objects.IsExactInt(idxObj) {
		return 0, false
	}
	idx, ok := idxObj.Int64()
	if !ok {
		return 0, false
	}
	if idx < 0 {
		idx += int64(tup.Len())
	}
	if idx < 0 || idx >= int64(tup.Len()) {
		return 0, false
	}
	// Item returns a borrowed slot; own it before DECREF_INPUTS.
	res := tup.Item(int(idx))
	objects.Incref(res)
	e.pushBinaryResult(res)
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpSubscrStrInt: ASCII-only fast path; non-ASCII strings
// fall back so the generic path walks runes.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBSCR_STR_INT
func (e *evalState) fastBinaryOpSubscrStrInt() (int, bool) {
	idxObj, iOk := e.peek(0).AsObject().(*objects.Int)
	s, sOk := e.peek(1).AsObject().(*objects.Unicode)
	if !sOk || !iOk || !objects.IsExactStr(s) || !objects.IsExactInt(idxObj) {
		return 0, false
	}
	if !s.IsASCII() {
		return 0, false
	}
	idx, ok := idxObj.Int64()
	if !ok {
		return 0, false
	}
	v := s.Value()
	if idx < 0 {
		idx += int64(len(v))
	}
	if idx < 0 || idx >= int64(len(v)) {
		return 0, false
	}
	e.pushBinaryResult(objects.NewStr(string(v[idx])))
	return e.cacheAdvance(compile.BINARY_OP), true
}

// fastBinaryOpSubscrDict: dict[key]. On miss the arm falls back so the
// generic NB_SUBSCR path raises KeyError with the right key.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBSCR_DICT
func (e *evalState) fastBinaryOpSubscrDict() (next int, ok bool, err error) {
	key := e.peek(0).AsObject()
	d, dOk := e.peek(1).AsObject().(*objects.Dict)
	if !dOk || !objects.IsExactDict(d) {
		return 0, false, nil
	}
	v, gerr := d.GetItem(key)
	if gerr != nil {
		// Key miss or hash failure: defer to the generic path so the
		// slow handler can raise KeyError with the right shape.
		return 0, false, nil //nolint:nilerr // intentional fast-path fallback
	}
	// GetItem returns a borrowed value slot; own it before DECREF_INPUTS.
	objects.Incref(v)
	e.pushBinaryResult(v)
	return e.cacheAdvance(compile.BINARY_OP), true, nil
}

// fastBinaryOpSubscrListSlice: list[slice]. Routes through the list
// type's Mapping.GetItem since it already implements the full
// slice-extraction logic CPython does inline. Win comes from skipping
// the BINARY_OP suboperator dispatch and the abstract-protocol walk.
//
// CPython: Python/bytecodes.c _BINARY_OP_SUBSCR_LIST_SLICE
func (e *evalState) fastBinaryOpSubscrListSlice() (next int, ok bool, err error) {
	key, kOk := e.peek(0).AsObject().(*objects.Slice)
	lst, lOk := e.peek(1).AsObject().(*objects.List)
	if !lOk || !kOk || !objects.IsExactList(lst) || !objects.IsExactSlice(key) {
		return 0, false, nil
	}
	v, gerr := objects.ListType.Mapping.GetItem(lst, key)
	if gerr != nil {
		return 0, false, gerr
	}
	e.pushBinaryResult(v)
	return e.cacheAdvance(compile.BINARY_OP), true, nil
}
