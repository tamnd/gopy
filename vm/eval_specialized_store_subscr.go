// Fast-path arms for the STORE_SUBSCR family.
//
// CPython narrows STORE_SUBSCR on the container type at NOS (list /
// dict) and, for list, the int key at TOS. The stack shape is
// (value, container, sub) with sub on top.
//
// Cache layout (1 codeunit, _PyStoreSubscrCache):
//   cell 1   counter
//
// CPython: Python/bytecodes.c _STORE_SUBSCR_LIST_INT / _STORE_SUBSCR_DICT

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 6: STORE_SUBSCR_* arms migrate to typed op<NAME> bodies; cache decode/deopt/advance live in the generated harness.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// fastStoreSubscrListInt implements STORE_SUBSCR_LIST_INT.
//
// Guards: TOS is exact int that fits int64 and is non-negative; NOS
// is exact list whose length covers the index.
//
// CPython: Python/bytecodes.c _STORE_SUBSCR_LIST_INT
func (e *evalState) fastStoreSubscrListInt(oparg uint32) (int, bool) {
	sub, subOk := e.peek(0).AsObject().(*objects.Int)
	if !subOk || !objects.IsExactInt(sub) {
		return 0, false
	}
	idx, intOk := sub.Int64()
	if !intOk || idx < 0 {
		return 0, false
	}
	lst, listOk := e.peek(1).AsObject().(*objects.List)
	if !listOk || !objects.IsExactList(lst) {
		return 0, false
	}
	if int(idx) >= lst.Len() {
		return 0, false
	}
	value := e.peek(2).AsObject()
	lst.SetItem(int(idx), value)
	e.pop()
	e.pop()
	e.pop()
	_ = oparg
	return e.cacheAdvance(compile.STORE_SUBSCR), true
}

// fastStoreSubscrDict implements STORE_SUBSCR_DICT.
//
// Guards: NOS is exactly *Dict.
//
// CPython: Python/bytecodes.c _STORE_SUBSCR_DICT
func (e *evalState) fastStoreSubscrDict(oparg uint32) (int, bool, error) {
	d, ok := e.peek(1).AsObject().(*objects.Dict)
	if !ok || !objects.IsExactDict(d) {
		return 0, false, nil
	}
	sub := e.peek(0).AsObject()
	value := e.peek(2).AsObject()
	if err := d.SetItem(sub, value); err != nil {
		return 0, true, err
	}
	e.pop()
	e.pop()
	e.pop()
	_ = oparg
	return e.cacheAdvance(compile.STORE_SUBSCR), true, nil
}
