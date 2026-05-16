// Fast-path arms for the UNPACK_SEQUENCE family.
//
// UNPACK_SEQUENCE pops a sequence and pushes its items so that
// items[0] ends up on TOS. The three specialized variants narrow on
// container type and (for TWO_TUPLE) the fixed two-element shape.
//
// Cache layout (1 codeunit, _PyUnpackSequenceCache):
//   cell 1   counter
//
// CPython: Python/bytecodes.c _UNPACK_SEQUENCE_TWO_TUPLE / _TUPLE / _LIST

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 6: UNPACK_SEQUENCE_* arms migrate to typed op<NAME> bodies; cache decode/deopt/advance live in the generated harness.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// fastUnpackSequenceTwoTuple implements UNPACK_SEQUENCE_TWO_TUPLE.
//
// CPython: Python/bytecodes.c _UNPACK_SEQUENCE_TWO_TUPLE
func (e *evalState) fastUnpackSequenceTwoTuple(oparg uint32) (int, bool) {
	if oparg != 2 {
		return 0, false
	}
	seq, ok := e.peek(0).AsObject().(*objects.Tuple)
	if !ok || !objects.IsExactTuple(seq) {
		return 0, false
	}
	if seq.Len() != 2 {
		return 0, false
	}
	e.pop()
	e.pushObject(seq.Item(1))
	e.pushObject(seq.Item(0))
	return e.cacheAdvance(compile.UNPACK_SEQUENCE), true
}

// fastUnpackSequenceTuple implements UNPACK_SEQUENCE_TUPLE.
//
// CPython: Python/bytecodes.c _UNPACK_SEQUENCE_TUPLE
func (e *evalState) fastUnpackSequenceTuple(oparg uint32) (int, bool) {
	seq, ok := e.peek(0).AsObject().(*objects.Tuple)
	if !ok || !objects.IsExactTuple(seq) {
		return 0, false
	}
	n := int(oparg)
	if seq.Len() != n {
		return 0, false
	}
	e.pop()
	for i := n - 1; i >= 0; i-- {
		e.pushObject(seq.Item(i))
	}
	return e.cacheAdvance(compile.UNPACK_SEQUENCE), true
}

// fastUnpackSequenceList implements UNPACK_SEQUENCE_LIST.
//
// CPython: Python/bytecodes.c _UNPACK_SEQUENCE_LIST
func (e *evalState) fastUnpackSequenceList(oparg uint32) (int, bool) {
	seq, ok := e.peek(0).AsObject().(*objects.List)
	if !ok || !objects.IsExactList(seq) {
		return 0, false
	}
	n := int(oparg)
	if seq.Len() != n {
		return 0, false
	}
	e.pop()
	for i := n - 1; i >= 0; i-- {
		e.pushObject(seq.Item(i))
	}
	return e.cacheAdvance(compile.UNPACK_SEQUENCE), true
}
