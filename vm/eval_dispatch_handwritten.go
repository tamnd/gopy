// Spec 1714 phase 5: hand-written op<NAME> bodies that the dispatcher
// routes to ahead of the legacy trySimple panel. Each method's
// signature follows the spec's design rule: name op<NAME>, receiver
// *evalState, oparg uint32, returns the dispatch contract tuple. The
// harness layer (vm/dispatch.go) does the dispatch; the body lives
// here.
//
// As the action translator gains coverage the equivalent generated
// arms in vm/eval_dispatch_gen.go take over and the body moves out of
// this file. Until then this file is the single owner of the
// migrated opcodes.
//
// CPython: Python/bytecodes.c per-op definitions.

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// dispatchHandwritten routes the migrated opcodes. Returns ok=false
// when op is not in this panel; the dispatcher then falls through to
// the next layer.
func (e *evalState) dispatchHandwritten(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	switch op {
	case compile.PUSH_NULL:
		return e.opPUSH_NULL(oparg)
	case compile.LOAD_CONST:
		return e.opLOAD_CONST(oparg)
	case compile.LOAD_FAST, compile.LOAD_FAST_BORROW:
		// LOAD_FAST_BORROW (3.13+) is the same observable shape as
		// LOAD_FAST under our model: Go's GC handles the lifetime, so
		// the borrow-vs-own distinction collapses to one body.
		return e.opLOAD_FAST(oparg)
	case compile.LOAD_FAST_CHECK:
		return e.opLOAD_FAST_CHECK(oparg)
	case compile.LOAD_FAST_AND_CLEAR:
		return e.opLOAD_FAST_AND_CLEAR(oparg)
	case compile.STORE_FAST:
		return e.opSTORE_FAST(oparg)
	case compile.DELETE_FAST:
		return e.opDELETE_FAST(oparg)
	case compile.JUMP_FORWARD:
		return e.opJUMP_FORWARD(oparg)
	case compile.RETURN_VALUE:
		return e.opRETURN_VALUE(oparg)
	}
	return 0, nil, nil, false, false, nil
}

// CPython: Python/bytecodes.c PUSH_NULL: (-- res) { res = PyStackRef_NULL; }
func (e *evalState) opPUSH_NULL(_ uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	e.push(stackref.Null)
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c LOAD_CONST: (-- value) reads from frame->code->co_consts[oparg].
func (e *evalState) opLOAD_CONST(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	co := e.f.Code
	if int(oparg) >= len(co.Consts) {
		return 0, nil, nil, false, true, fmt.Errorf("vm: LOAD_CONST index %d out of range", oparg)
	}
	obj, werr := wrapConst(co.Consts[oparg])
	if werr != nil {
		return 0, nil, nil, false, true, werr
	}
	e.pushObject(obj)
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c LOAD_FAST / LOAD_FAST_BORROW.
func (e *evalState) opLOAD_FAST(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	ref := e.localAt(int(oparg))
	if ref.IsNull() {
		return 0, nil, nil, false, true, fmt.Errorf("vm: LOAD_FAST: local %d unbound", oparg)
	}
	e.push(ref.Dup())
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c LOAD_FAST_CHECK: same observable shape; the check is
// already covered by LOAD_FAST's IsNull guard, so the body coincides.
func (e *evalState) opLOAD_FAST_CHECK(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	ref := e.localAt(int(oparg))
	if ref.IsNull() {
		return 0, nil, nil, false, true, fmt.Errorf("vm: LOAD_FAST_CHECK: local %d unbound", oparg)
	}
	e.push(ref.Dup())
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c LOAD_FAST_AND_CLEAR: pushes local, then nulls it.
func (e *evalState) opLOAD_FAST_AND_CLEAR(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	ref := e.localAt(int(oparg))
	e.setLocal(int(oparg), stackref.Null)
	e.push(ref)
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c STORE_FAST: (value --) writes oparg.
func (e *evalState) opSTORE_FAST(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	ref := e.pop()
	old := e.localAt(int(oparg))
	old.Close()
	e.setLocal(int(oparg), ref)
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c DELETE_FAST: clears local oparg, error if unbound.
func (e *evalState) opDELETE_FAST(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	old := e.localAt(int(oparg))
	if old.IsNull() {
		return 0, nil, nil, false, true, fmt.Errorf("vm: DELETE_FAST: local %d unbound", oparg)
	}
	old.Close()
	e.setLocal(int(oparg), stackref.Null)
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c JUMP_FORWARD: JUMPBY(oparg).
func (e *evalState) opJUMP_FORWARD(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c RETURN_VALUE: (value --) returns from frame.
func (e *evalState) opRETURN_VALUE(_ uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	v := e.popObject()
	return 0, v, nil, true, true, nil
}
