// Fast-path arms for CALL_PY_EXACT_ARGS and
// CALL_BOUND_METHOD_EXACT_ARGS.
//
// CALL_PY_EXACT_ARGS fires when the specializer has decided the
// callable is a plain Python function whose declared argcount matches
// the number of positionals on the stack. The fast arm replaces three
// allocations the generic CALL path otherwise pays for every call:
//
//   1. the args []objects.Object slice the generic CALL builds out of
//      popped stackrefs,
//   2. the prepend in the method-shape branch
//      (append([]objects.Object{self}, args...)),
//   3. the Vectorcall slot lookup that lands in callPyFunction, which
//      then re-walks every positional / kw-only / *args / **kwargs
//      step to bind locals.
//
// Since _CHECK_FUNCTION_EXACT_ARGS already proves co_argcount ==
// oparg + hasSelf, there are no defaults, no varargs, no kwargs to
// service. We push the frame, copy the args straight off the value
// stack into LocalsPlus, and run Eval on the new frame.
//
// CALL_BOUND_METHOD_EXACT_ARGS is the same arm with one prefix step:
// unwrap the BoundMethod's im_func / im_self into the (callable,
// self_or_null) slots and then run the CALL_PY_EXACT_ARGS body.
//
// CPython: Python/bytecodes.c:4041 CALL_PY_EXACT_ARGS macro
// CPython: Python/bytecodes.c:4027 CALL_BOUND_METHOD_EXACT_ARGS macro
// CPython: Python/bytecodes.c:3998 _INIT_CALL_PY_EXACT_ARGS
// CPython: Python/bytecodes.c:4010 _PUSH_FRAME

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/stackref"
)

// fastCallPyExactArgs runs CALL_PY_EXACT_ARGS. The stack on entry is
// [callable, NULL_or_self, arg0, ..., arg(N-1)] where N == oparg.
// Returns ok=true when the arm took the dispatch; on guard miss
// ok=false and the caller routes through maybeDeopt.
//
// CPython: Python/bytecodes.c:4041 CALL_PY_EXACT_ARGS
func (e *evalState) fastCallPyExactArgs(oparg uint32) (int, bool, error) {
	argc := int(oparg)
	selfOrNull := e.peek(argc).AsObject()
	callable := e.peek(argc + 1).AsObject()
	fn, ok := callable.(*objects.Function)
	if !ok {
		return 0, false, nil
	}
	return e.callPyExactArgsCommon(fn, selfOrNull, argc)
}

// fastCallBoundMethodExactArgs runs CALL_BOUND_METHOD_EXACT_ARGS.
// Stack shape is [bound, NULL, arg0, ..., arg(N-1)]; the NULL slot
// must be null. The arm unwraps bound into (im_func, im_self) and
// then runs the CALL_PY_EXACT_ARGS body.
//
// CPython: Python/bytecodes.c:4027 CALL_BOUND_METHOD_EXACT_ARGS
func (e *evalState) fastCallBoundMethodExactArgs(oparg uint32) (int, bool, error) {
	argc := int(oparg)
	if e.peek(argc).AsObject() != nil {
		// _CHECK_CALL_BOUND_METHOD_EXACT_ARGS requires the null slot.
		return 0, false, nil
	}
	bm, ok := e.peek(argc + 1).AsObject().(*objects.BoundMethod)
	if !ok {
		return 0, false, nil
	}
	fn, ok := bm.Func().(*objects.Function)
	if !ok {
		return 0, false, nil
	}
	return e.callPyExactArgsCommon(fn, bm.Self(), argc)
}

// callPyExactArgsCommon is the shared body of the two arms.
// _CHECK_FUNCTION_VERSION (cells 2..3) and _CHECK_FUNCTION_EXACT_ARGS
// (co_argcount == oparg + hasSelf) gate the fast path; on guard miss
// the caller deopts. On hit, the arm pushes a frame, copies the args
// straight off the value stack into LocalsPlus, runs Eval on the new
// frame, then drops the call shape and pushes the result.
//
// CPython: Python/bytecodes.c:3864 _CHECK_FUNCTION_VERSION
// CPython: Python/bytecodes.c:3979 _CHECK_FUNCTION_EXACT_ARGS
// CPython: Python/bytecodes.c:3998 _INIT_CALL_PY_EXACT_ARGS
func (e *evalState) callPyExactArgsCommon(fn *objects.Function, selfOrNull objects.Object, argc int) (int, bool, error) {
	idx := e.instrIdx()
	cachedVer := specialize.CallFuncVersion(e.f.Code.Code, idx)
	if fn.Version == 0 || fn.Version != cachedVer {
		return 0, false, nil
	}
	co := fn.Code
	if co == nil {
		return 0, false, nil
	}
	hasSelf := 0
	if selfOrNull != nil {
		hasSelf = 1
	}
	if co.Argcount != argc+hasSelf {
		return 0, false, nil
	}
	stack := frameStackFor(e.ts)
	f2 := stack.Push(co, fn.Globals, fn.Builtins, fn)
	if hasSelf == 1 {
		f2.SetLocal(0, stackref.FromObject(selfOrNull))
	}
	for i := range argc {
		argObj := e.peek(argc - 1 - i).AsObject()
		f2.SetLocal(i+hasSelf, stackref.FromObject(argObj))
	}
	out, err := Eval(e.ts, f2)
	stack.Pop()
	if err != nil {
		return 0, true, err
	}
	e.drop(argc + 2)
	e.pushObject(out)
	return e.cacheAdvance(compile.CALL), true, nil
}
