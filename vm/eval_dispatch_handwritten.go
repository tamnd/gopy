// Spec 1714 phase 5: hand-written op<NAME> bodies that the dispatcher
// routes to ahead of the legacy trySimple panel. Each method's
// signature follows the spec's design rule: name op<NAME>, receiver
// *evalState, oparg uint32, returns (next, ok, err) where ok=false
// means "this panel does not handle the op". RETURN_VALUE and the
// other exit_frame arms park the terminal value on e.retVal and
// return errFrameReturn so the loop driver can pick it up.
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
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// dispatchHandwritten routes the migrated opcodes. Returns ok=false
// when op is not in this panel; the dispatcher then falls through to
// the next layer.
func (e *evalState) dispatchHandwritten(op compile.Opcode, oparg uint32) (next int, ok bool, err error) {
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
	case compile.POP_JUMP_IF_TRUE, compile.POP_JUMP_IF_FALSE,
		compile.POP_JUMP_IF_NONE, compile.POP_JUMP_IF_NOT_NONE:
		return e.opPOP_JUMP_IF(op, oparg)
	case compile.CHECK_EG_MATCH:
		return e.opCHECK_EG_MATCH(oparg)
	}
	return 0, false, nil
}

// opCHECK_EG_MATCH pops (exc_value, match_type) and pushes (rest, match)
// where match is None when no leaf of exc_value satisfies match_type
// and rest is None when every leaf matched. The opcode underpins
// `except* T as e:` arms: codegen follows with COPY 1 / POP_JUMP_IF_NONE
// to skip the handler body on no match, otherwise binds match to the
// handler name and accumulates rest into the reraise pile.
//
// CPython: Python/bytecodes.c CHECK_EG_MATCH
// CPython: Python/ceval.c:2295 _PyEval_ExceptionGroupMatch
func (e *evalState) opCHECK_EG_MATCH(_ uint32) (next int, ok bool, err error) {
	matchTypeObj := e.popObject()
	excValueObj := e.popObject()
	// None exc_value: both outputs are None (no group, no leaves).
	if excValueObj == objects.None() {
		e.pushObject(objects.None())
		e.pushObject(objects.None())
		return e.advance(), true, nil
	}
	exc, ok := excValueObj.(*pyerrors.Exception)
	if !ok {
		// Non-exception payload: treat as no-match, leave on the rest slot.
		e.pushObject(excValueObj)
		e.pushObject(objects.None())
		return e.advance(), true, nil
	}
	// matchType is either an exception class or a tuple of classes
	// (CPython's `PyErr_GivenExceptionMatches` accepts both shapes).
	matchTypes, mterr := flattenExceptionMatchTypes(matchTypeObj)
	if mterr != nil {
		return 0, true, mterr
	}
	// Direct match on the value itself: wrap a bare exception into an
	// ExceptionGroup so the caller can bind a group to the `as` name
	// uniformly, matching CPython's _PyEval_ExceptionGroupMatch.
	if matchesAnyType(exc.ExcType, matchTypes) {
		if pyerrors.IsExceptionGroup(exc.ExcType) {
			e.pushObject(objects.None())
			e.pushObject(exc)
		} else {
			wrap := pyerrors.New(pyerrors.PyExc_ExceptionGroup, objects.NewTuple([]objects.Object{
				objects.NewStr(""),
				objects.NewTuple([]objects.Object{exc}),
			}))
			e.pushObject(objects.None())
			e.pushObject(wrap)
		}
		return e.advance(), true, nil
	}
	// Partial match path: walk into the group.
	if !pyerrors.IsExceptionGroup(exc.ExcType) {
		// Leaf exception that didn't match: nothing caught, push as rest.
		e.pushObject(exc)
		e.pushObject(objects.None())
		return e.advance(), true, nil
	}
	match, rest := splitExceptionGroupAny(exc, matchTypes)
	var matchOut, restOut objects.Object = objects.None(), objects.None()
	if match != nil {
		matchOut = match
	}
	if rest != nil {
		restOut = rest
	}
	e.pushObject(restOut)
	e.pushObject(matchOut)
	return e.advance(), true, nil
}

// flattenExceptionMatchTypes accepts either an exception class or a
// tuple of exception classes (the two shapes CPython's
// PyErr_GivenExceptionMatches handles) and returns the flat slice.
//
// CPython: Python/errors.c:218 PyErr_GivenExceptionMatches
func flattenExceptionMatchTypes(o objects.Object) ([]*objects.Type, error) {
	if t, ok := o.(*objects.Type); ok {
		return []*objects.Type{t}, nil
	}
	if tup, ok := o.(*objects.Tuple); ok {
		out := make([]*objects.Type, 0, tup.Len())
		for i := 0; i < tup.Len(); i++ {
			t, tok := tup.Item(i).(*objects.Type)
			if !tok {
				return nil, fmt.Errorf("TypeError: catching classes that do not inherit from BaseException is not allowed")
			}
			out = append(out, t)
		}
		return out, nil
	}
	return nil, fmt.Errorf("TypeError: catching classes that do not inherit from BaseException is not allowed")
}

// matchesAnyType reports whether exc's class inherits from any of the
// types in matchTypes, matching CPython's tuple-of-classes behavior in
// PyErr_GivenExceptionMatches.
func matchesAnyType(exc *objects.Type, matchTypes []*objects.Type) bool {
	for _, t := range matchTypes {
		if pyerrors.IsSubtype(exc, t) {
			return true
		}
	}
	return false
}

// splitExceptionGroupAny wraps SplitExceptionGroup with the tuple-of-types
// flattening that CHECK_EG_MATCH supports. CPython recursively splits
// against each type; we partition once over the flat type set so a leaf
// joins `match` whenever it satisfies any of the requested types.
//
// CPython: Objects/exceptions.c:1326 exceptiongroup_split_recursive
func splitExceptionGroupAny(group *pyerrors.Exception, matchTypes []*objects.Type) (match, rest *pyerrors.Exception) {
	if group == nil {
		return nil, nil
	}
	if matchesAnyType(group.ExcType, matchTypes) {
		return group, nil
	}
	leaves := pyerrors.ExceptionGroupLeaves(group)
	if leaves == nil {
		return nil, group
	}
	var matched, rested []*pyerrors.Exception
	for _, leaf := range leaves {
		if pyerrors.IsExceptionGroup(leaf.ExcType) {
			subMatch, subRest := splitExceptionGroupAny(leaf, matchTypes)
			if subMatch != nil {
				matched = append(matched, subMatch)
			}
			if subRest != nil {
				rested = append(rested, subRest)
			}
			continue
		}
		if matchesAnyType(leaf.ExcType, matchTypes) {
			matched = append(matched, leaf)
		} else {
			rested = append(rested, leaf)
		}
	}
	if len(matched) > 0 {
		match = deriveGroupHere(group, matched)
	}
	if len(rested) > 0 {
		rest = deriveGroupHere(group, rested)
	}
	return match, rest
}

// deriveGroupHere is the local mirror of errors.deriveGroup. Kept here
// to avoid exporting the helper purely for this caller.
//
// CPython: Objects/exceptions.c:1414 exceptiongroup_subset
func deriveGroupHere(src *pyerrors.Exception, subset []*pyerrors.Exception) *pyerrors.Exception {
	if src == nil || len(subset) == 0 {
		return nil
	}
	var message objects.Object = objects.NewStr("")
	if src.Args != nil && src.Args.Len() >= 1 {
		message = src.Args.Item(0)
	}
	items := make([]objects.Object, len(subset))
	for i, exc := range subset {
		items[i] = exc
	}
	leaves := objects.NewTuple(items)
	return pyerrors.New(src.ExcType, objects.NewTuple([]objects.Object{message, leaves}))
}

// CPython: Python/bytecodes.c LOAD_CONST: (-- value) reads from frame->code->co_consts[oparg].
func (e *evalState) opLOAD_CONST(oparg uint32) (next int, ok bool, err error) {
	co := e.f.Code
	if int(oparg) >= len(co.Consts) {
		return 0, true, fmt.Errorf("vm: LOAD_CONST index %d out of range", oparg)
	}
	e.pushObject(e.constAt(int(oparg)))
	return e.advance(), true, nil
}

// CPython: Python/bytecodes.c LOAD_FAST_CHECK: same observable shape; the check is
// already covered by LOAD_FAST's IsNull guard, so the body coincides.
func (e *evalState) opLOAD_FAST_CHECK(oparg uint32) (next int, ok bool, err error) {
	ref := e.localAt(int(oparg))
	if ref.IsNull() {
		return 0, true, formatExcUnbound(e.f.Code, int(oparg))
	}
	e.push(ref.Dup())
	return e.advance(), true, nil
}

// CPython: Python/bytecodes.c DELETE_FAST: clears local oparg, error if unbound.
func (e *evalState) opDELETE_FAST(oparg uint32) (next int, ok bool, err error) {
	old := e.localAt(int(oparg))
	if old.IsNull() {
		return 0, true, formatExcUnbound(e.f.Code, int(oparg))
	}
	old.Close()
	e.setLocal(int(oparg), stackref.Null)
	return e.advance(), true, nil
}

// CPython: Python/bytecodes.c RETURN_VALUE: (value --) returns from frame.
// Parks the popped TOS on e.retVal and raises the errFrameReturn sentinel
// so the loop unwinds without threading retVal/retDone through every
// dispatch return.
func (e *evalState) opRETURN_VALUE(_ uint32) (next int, ok bool, err error) {
	e.retVal = e.popObject()
	return 0, true, errFrameReturn
}

// CPython: Python/bytecodes.c INTERPRETER_EXIT terminates the eval loop with TOS.
func (e *evalState) opINTERPRETER_EXIT(_ uint32) (next int, ok bool, err error) {
	e.retVal = e.popObject()
	return 0, true, errFrameReturn
}

// CPython: Python/bytecodes.c COPY: pushes a duplicate of stack[-oparg].
// CPython: Python/bytecodes.c IS_OP: pushes (a is b) negated when oparg==1.
func (e *evalState) opIS_OP(oparg uint32) (next int, ok bool, err error) {
	b := e.popObject()
	a := e.popObject()
	eq := (a == b)
	if oparg == 1 {
		eq = !eq
	}
	// DECREF_INPUTS: IS_OP compares identities and pushes a fresh bool,
	// retaining neither operand, so both owned references are released the
	// same way COMPARE_OP releases its operands. This relies on every
	// producer that can feed IS_OP handing back an owned reference (the
	// weakref dereference and container element reads were the borrowed
	// holdouts; both now incref on the way out).
	//
	// CPython: Python/bytecodes.c IS_OP (b = ...; a = ...; DECREF_INPUTS())
	objects.Decref(a)
	objects.Decref(b)
	e.pushObject(objects.NewBool(eq))
	return e.advance(), true, nil
}

// CPython: Python/bytecodes.c POP_JUMP_IF_{TRUE,FALSE,NONE,NOT_NONE}.
func (e *evalState) opPOP_JUMP_IF(op compile.Opcode, oparg uint32) (next int, ok bool, err error) {
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
			objects.Decref(v)
			return 0, true, terr
		}
		if op == compile.POP_JUMP_IF_TRUE {
			take = truthy
		} else {
			take = !truthy
		}
	}
	// DECREF_INPUTS: the value-on-top is owned (LOAD_FAST_BORROW and every
	// other producer push an owned reference), so it is released once the
	// branch condition is read. The _NONE/_NOT_NONE forms close the object
	// itself; the _TRUE/_FALSE forms close the bool TO_BOOL already produced,
	// which is immortal and so a no-op. CPython mirrors this: _IS_NONE closes
	// value and _POP_JUMP_IF_{TRUE,FALSE} close the condition.
	//
	// CPython: Python/bytecodes.c _IS_NONE / POP_JUMP_IF_{TRUE,FALSE,NONE,NOT_NONE}
	objects.Decref(v)
	if take {
		return e.jumpBy(int(oparg) + 1), true, nil
	}
	return e.advance(), true, nil
}
