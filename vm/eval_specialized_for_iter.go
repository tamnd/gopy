// Fast-path arms for specialized FOR_ITER variants. CPython's
// _Py_Specialize_ForIter narrows FOR_ITER into LIST / TUPLE / RANGE /
// GEN based on the iterator type. The LIST / TUPLE / RANGE fast arms
// here skip the tp_iternext indirection on every iteration: they
// type-assert the iterator once, then walk its native fields directly.
// The GEN variant is intentionally not implemented here because it
// requires a generator-frame push/pop path the VM does not yet expose;
// the dispatch loop falls through to the generic FOR_ITER body for
// FOR_ITER_GEN until that lands.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_LIST) / FOR_ITER_TUPLE /
// FOR_ITER_RANGE (Python/bytecodes.c:3349 / 3412 / 3462).

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// fastForIter shared exit shape. On exhaustion the iterator stays on
// the stack and the dispatcher jumps past the loop body + the trailing
// END_FOR (oparg + 1 codeunits past the next instruction). On a hit
// the next value is pushed above the iterator.
//
// CPython: Python/bytecodes.c _ITER_JUMP_LIST / _TUPLE / _RANGE plus
// the inst(FOR_ITER) cache-skip step.

// fastForIterList implements FOR_ITER_LIST. Type-check the iterator,
// advance, and either push the next value or jump past the loop body.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_LIST) (Python/bytecodes.c:3349)
func (e *evalState) fastForIterList(oparg uint32) (int, bool) {
	it := e.peek(0).AsObject()
	v, exhausted, ok := objects.ListIterNextFast(it)
	if !ok {
		return 0, false
	}
	if exhausted {
		return e.forIterJump(oparg), true
	}
	e.pushObject(v)
	return e.cacheAdvance(compile.FOR_ITER), true
}

// fastForIterTuple implements FOR_ITER_TUPLE. Same shape as
// fastForIterList against tupleIterator.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_TUPLE) (Python/bytecodes.c:3412)
func (e *evalState) fastForIterTuple(oparg uint32) (int, bool) {
	it := e.peek(0).AsObject()
	v, exhausted, ok := objects.TupleIterNextFast(it)
	if !ok {
		return 0, false
	}
	if exhausted {
		return e.forIterJump(oparg), true
	}
	e.pushObject(v)
	return e.cacheAdvance(compile.FOR_ITER), true
}

// fastForIterRange implements FOR_ITER_RANGE. Range iterators carry a
// big.Int cur / stop / step triple (gopy's range_iterator is unified
// across the short / long CPython types). The fast arm still allocates
// the returned Int because gopy's PyLong representation does not pack
// small ints inline; the win is purely from skipping the tp_iternext
// dispatch.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_RANGE) (Python/bytecodes.c:3462)
func (e *evalState) fastForIterRange(oparg uint32) (int, bool) {
	it := e.peek(0).AsObject()
	v, exhausted, ok := objects.RangeIterNextFast(it)
	if !ok {
		return 0, false
	}
	if exhausted {
		return e.forIterJump(oparg), true
	}
	e.pushObject(v)
	return e.cacheAdvance(compile.FOR_ITER), true
}

// forIterJump computes the exhaustion jump target for FOR_ITER fast
// arms. e.jumpBy uses e.advance(), which reads the opcode byte at
// InstrPtr and looks up its cache stride; the opcodeCaches table only
// carries the base opcodes, so on a specialized variant byte (FOR_ITER_LIST,
// FOR_ITER_TUPLE, FOR_ITER_RANGE) it would return 0 and undercount the
// stride. We anchor the math on FOR_ITER's stride explicitly.
//
// CPython: Python/ceval_macros.h JUMPBY against
// _INLINE_CACHE_ENTRIES_FOR_ITER (Include/internal/pycore_opcode_metadata.h).
func (e *evalState) forIterJump(oparg uint32) int {
	return e.cacheAdvance(compile.FOR_ITER) + 2*int(oparg)
}
