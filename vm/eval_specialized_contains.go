// Fast-path arms for the CONTAINS_OP family.
//
// CONTAINS_OP has two specialized variants: DICT when TOS is exactly
// a dict, SET when TOS is exactly a set or frozenset. Both call the
// type's Contains and XOR the boolean result with oparg (oparg == 1
// means `not in`).
//
// Cache layout (1 codeunit, _PyContainsOpCache):
//   cell 1   counter
//
// CPython: Python/bytecodes.c _CONTAINS_OP_SET / _CONTAINS_OP_DICT

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// pushContainsResult finalises the stack shape for a CONTAINS_OP fast
// path hit: pop both operands and push True/False per `res ^ oparg`.
func (e *evalState) pushContainsResult(res bool, oparg uint32) {
	e.pop()
	e.pop()
	flipped := res
	if oparg&1 != 0 {
		flipped = !res
	}
	if flipped {
		e.pushObject(objects.True())
	} else {
		e.pushObject(objects.False())
	}
}

// fastContainsOpDict implements CONTAINS_OP_DICT.
//
// Guards: TOS (right) is exactly a *Dict.
//
// CPython: Python/bytecodes.c _CONTAINS_OP_DICT
func (e *evalState) fastContainsOpDict(oparg uint32) (int, bool, error) {
	right, ok := e.peek(0).AsObject().(*objects.Dict)
	if !ok || !objects.IsExactDict(right) {
		return 0, false, nil
	}
	left := e.peek(1).AsObject()
	res, err := right.Contains(left)
	if err != nil {
		return 0, true, err
	}
	e.pushContainsResult(res, oparg)
	return e.cacheAdvance(compile.CONTAINS_OP), true, nil
}

// fastContainsOpSet implements CONTAINS_OP_SET. Accepts both exact set
// and exact frozenset.
//
// CPython: Python/bytecodes.c _CONTAINS_OP_SET
func (e *evalState) fastContainsOpSet(oparg uint32) (int, bool, error) {
	right, ok := e.peek(0).AsObject().(*objects.Set)
	if !ok || (!objects.IsExactSet(right) && !objects.IsExactFrozenSet(right)) {
		return 0, false, nil
	}
	left := e.peek(1).AsObject()
	res, err := right.Contains(left)
	if err != nil {
		return 0, true, err
	}
	e.pushContainsResult(res, oparg)
	return e.cacheAdvance(compile.CONTAINS_OP), true, nil
}
