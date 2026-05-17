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
	case compile.LOAD_CONST:
		return e.opLOAD_CONST(oparg)
	case compile.LOAD_FAST_CHECK:
		return e.opLOAD_FAST_CHECK(oparg)
	case compile.DELETE_FAST:
		return e.opDELETE_FAST(oparg)
	case compile.RETURN_VALUE:
		return e.opRETURN_VALUE(oparg)
	case compile.INTERPRETER_EXIT:
		return e.opINTERPRETER_EXIT(oparg)
	case compile.IS_OP:
		return e.opIS_OP(oparg)
	case compile.BUILD_LIST:
		return e.opBUILD_LIST(oparg)
	case compile.BUILD_TUPLE:
		return e.opBUILD_TUPLE(oparg)
	case compile.BUILD_SLICE:
		return e.opBUILD_SLICE(oparg)
	case compile.POP_JUMP_IF_TRUE, compile.POP_JUMP_IF_FALSE,
		compile.POP_JUMP_IF_NONE, compile.POP_JUMP_IF_NOT_NONE:
		return e.opPOP_JUMP_IF(op, oparg)
	}
	return 0, nil, nil, false, false, nil
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

// CPython: Python/bytecodes.c RETURN_VALUE: (value --) returns from frame.
func (e *evalState) opRETURN_VALUE(_ uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	v := e.popObject()
	return 0, v, nil, true, true, nil
}

// CPython: Python/bytecodes.c INTERPRETER_EXIT terminates the eval loop with TOS.
func (e *evalState) opINTERPRETER_EXIT(_ uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	v := e.popObject()
	return 0, v, nil, true, true, nil
}

// CPython: Python/bytecodes.c COPY: pushes a duplicate of stack[-oparg].
// CPython: Python/bytecodes.c IS_OP: pushes (a is b) negated when oparg==1.
func (e *evalState) opIS_OP(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	b := e.popObject()
	a := e.popObject()
	eq := (a == b)
	if oparg == 1 {
		eq = !eq
	}
	e.pushObject(objects.NewBool(eq))
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c BUILD_LIST: pop oparg values, push list.
func (e *evalState) opBUILD_LIST(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	n := int(oparg)
	items := make([]objects.Object, n)
	for i := n - 1; i >= 0; i-- {
		items[i] = e.popObject()
	}
	e.pushObject(objects.NewList(items))
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c BUILD_TUPLE: pop oparg values, push tuple.
func (e *evalState) opBUILD_TUPLE(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	n := int(oparg)
	items := make([]objects.Object, n)
	for i := n - 1; i >= 0; i-- {
		items[i] = e.popObject()
	}
	e.pushObject(objects.NewTuple(items))
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c BUILD_SLICE: oparg in {2,3} for start:stop[:step].
func (e *evalState) opBUILD_SLICE(oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	var step objects.Object
	if oparg == 3 {
		step = e.popObject()
	}
	stop := e.popObject()
	start := e.popObject()
	e.pushObject(objects.NewSlice(start, stop, step))
	return e.advance(), nil, nil, false, true, nil
}

// CPython: Python/bytecodes.c POP_JUMP_IF_{TRUE,FALSE,NONE,NOT_NONE}.
func (e *evalState) opPOP_JUMP_IF(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	v := e.popObject()
	var take bool
	switch op {
	case compile.POP_JUMP_IF_NONE:
		take = (v == objects.None())
	case compile.POP_JUMP_IF_NOT_NONE:
		take = (v != objects.None())
	default:
		truthy, terr := objects.IsTruthy(v)
		if terr != nil {
			return 0, nil, nil, false, true, terr
		}
		if op == compile.POP_JUMP_IF_TRUE {
			take = truthy
		} else {
			take = !truthy
		}
	}
	if take {
		return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
	}
	return e.advance(), nil, nil, false, true, nil
}
