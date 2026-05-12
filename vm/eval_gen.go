// Generator, coroutine, and context-manager bytecode arms.
// Ports RETURN_GENERATOR, YIELD_VALUE, SEND, GET_YIELD_FROM_ITER,
// GET_AWAITABLE, GET_AITER, GET_ANEXT, END_ASYNC_FOR, CLEANUP_THROW,
// and WITH_EXCEPT_START from Python/bytecodes.c.
//
// CPython: Python/bytecodes.c generator / coroutine / with sections

package vm

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// genResult bundles the dispatch outcome for the generator / coroutine
// / with panel. ok=false means tryGen did not match the opcode and the
// caller should fall through to the next dispatch layer.
type genResult struct {
	next    int
	retVal  objects.Object
	retErr  error
	retDone bool
	ok      bool
}

// tryGen handles generator / coroutine / with opcodes. ok=false on the
// returned genResult means the opcode is not in this panel; err is a
// dispatch-level failure.
func (e *evalState) tryGen(op compile.Opcode, oparg uint32) (genResult, error) {
	switch op {
	case compile.RETURN_GENERATOR:
		return e.execReturnGenerator()

	case compile.YIELD_VALUE:
		return e.execYieldValue(oparg)

	case compile.SEND:
		return e.execSend(oparg)

	case compile.GET_YIELD_FROM_ITER:
		return e.execGetYieldFromIter()

	case compile.GET_AWAITABLE:
		return e.execGetAwaitable(oparg)

	case compile.GET_AITER:
		return e.execGetAiter()

	case compile.GET_ANEXT:
		return e.execGetAnext()

	case compile.END_ASYNC_FOR:
		return e.execEndAsyncFor()

	case compile.CLEANUP_THROW:
		return e.execCleanupThrow()

	case compile.WITH_EXCEPT_START:
		return e.execWithExceptStart()

	case compile.LOAD_SPECIAL:
		return e.execLoadSpecial(oparg)
	}
	return genResult{}, nil
}

// specialMethodNames maps the LOAD_SPECIAL oparg to the dunder name to
// look up on the owner's type. Order matches CPython's _PySpecialMethod
// enum (SPECIAL___ENTER__, SPECIAL___EXIT__, SPECIAL___AENTER__,
// SPECIAL___AEXIT__).
//
// CPython: Include/internal/pycore_ceval.h:380 _PySpecialMethod
var specialMethodNames = [...]string{
	"__enter__",
	"__exit__",
	"__aenter__",
	"__aexit__",
}

// execLoadSpecial ports LOAD_SPECIAL: pop owner, look the named dunder
// up on the owner's type via the MRO walk (not on the instance), bind
// through tp_descr_get if present, and push the resulting (attr,
// self_or_null) pair. Lets the CALL handler treat the pair the same as
// a LOAD_ATTR-with-self-shape produced for ordinary method calls.
//
// CPython: Python/bytecodes.c LOAD_SPECIAL
// CPython: Include/internal/pycore_object.h _PyObject_LookupSpecialMethod
func (e *evalState) execLoadSpecial(oparg uint32) (genResult, error) {
	if int(oparg) >= len(specialMethodNames) {
		return genResult{ok: true}, fmt.Errorf("vm: LOAD_SPECIAL: oparg %d out of range", oparg)
	}
	name := specialMethodNames[oparg]
	owner := e.popObject()
	t := owner.Type()
	descr, _ := objects.LookupDescriptor(t, name)
	if descr == nil {
		return genResult{ok: true}, fmt.Errorf(
			"AttributeError: '%s' object has no attribute '%s'", t.Name, name)
	}
	// If the descriptor implements __get__, bind it through the
	// descriptor protocol and push (bound, NULL). Otherwise push the
	// raw descriptor with the owner as self_or_null so CALL prepends
	// it before invoking.
	if dg := descr.Type().DescrGet; dg != nil {
		bound, err := dg(descr, owner, t)
		if err != nil {
			return genResult{ok: true}, err
		}
		e.pushObject(bound)
		e.push(stackref.Null)
		return genResult{next: e.advance(), ok: true}, nil
	}
	e.pushObject(descr)
	e.pushObject(owner)
	return genResult{next: e.advance(), ok: true}, nil
}

// execReturnGenerator ports RETURN_GENERATOR: clones the current frame
// into a generator-owned heap copy, marks the arena slot so the
// caller's natural Pop tears it down without clearing locals, and
// returns the new generator to the caller.
//
// Unlike CPython, which pops the running frame off the eval stack
// inside RETURN_GENERATOR and resumes the caller in-loop, gopy nests
// Eval calls per Python call. The pending defer in callPyFunction
// owns the chunk-slot teardown; this opcode just hands off the frame
// contents to the generator.
//
// CPython: Python/bytecodes.c:4982 RETURN_GENERATOR
func (e *evalState) execReturnGenerator() (genResult, error) {
	name := e.f.Code.Name

	// Skip the POP_TOP that insert_prefix_instructions emits right
	// after RETURN_GENERATOR. The two together have a net stack
	// effect of zero (RETURN_GENERATOR pushed an item that POP_TOP
	// drops); we never materialize the push, so we also skip the pop.
	//
	// CPython: Python/flowgraph.c:3760 insert_prefix_instructions
	// (the RETURN_GENERATOR + POP_TOP pair)
	resumeIP := e.advance() + 2

	// Copy the frame state into a heap-owned record the generator
	// goroutine will run. Mark the arena slot OwnedByGenerator so the
	// caller's defer Pop in callPyFunction skips the LocalsPlus
	// teardown that would otherwise race with the live goroutine.
	savedFrame := &frame.Frame{}
	*savedFrame = *e.f
	savedFrame.InstrPtr = resumeIP
	savedFrame.PrevInstr = e.f.InstrPtr
	savedFrame.Owner = frame.OwnedByGenerator
	e.f.Owner = frame.OwnedByGenerator
	savedTS := e.ts

	// Determine the object type from the code flags.
	//
	// CPython: Python/bytecodes.c:4982 RETURN_GENERATOR (CO_GENERATOR /
	// CO_COROUTINE / CO_ASYNC_GENERATOR dispatch)
	flags := uint32(e.f.Code.Flags)
	var (
		yieldCh chan objects.GenMsg
		sendCh  chan objects.GenMsg
		retVal  objects.Object
	)
	switch {
	case flags&compile.CoCoroutine != 0:
		c := objects.NewCoroutine(name)
		yieldCh, sendCh = c.YieldCh, c.SendCh
		retVal = c
	case flags&compile.CoAsyncGenerator != 0:
		ag := objects.NewAsyncGenerator(name)
		yieldCh, sendCh = ag.YieldCh, ag.SendCh
		retVal = ag
	default:
		g := objects.NewGenerator(name)
		yieldCh, sendCh = g.YieldCh, g.SendCh
		retVal = g
	}

	go func() {
		// Register the generator's goroutine with the active-thread map
		// so currentThread() (used by sys.exc_info and friends) resolves
		// to savedTS. Without this, hook-driven builtins running inside
		// the generator body see a nil thread and return defaults.
		prev := setActiveThread(savedTS)
		defer restoreActiveThread(prev)

		// Block until the first Send() call. The first message must be
		// None (enforced by Generator.Send); we discard it here because
		// the generator body begins from the frame's IP, not a yield
		// point, so there is no stack slot waiting for the sent value.
		msg := <-sendCh
		if msg.Err != nil {
			// close() before first next(): just signal StopIteration.
			yieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
			return
		}
		// Run the generator body. yieldCh/sendCh are threaded through
		// evalState so YIELD_VALUE can reach them.
		ge := &evalState{
			ts:       savedTS,
			f:        savedFrame,
			breaker:  breakerFor(savedTS),
			genYield: yieldCh,
			genSend:  sendCh,
		}
		_, runErr := ge.run()
		if runErr != nil && !errors.Is(runErr, objects.ErrStopIteration) {
			yieldCh <- objects.GenMsg{Err: runErr}
		} else {
			yieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
		}
	}()

	// Return the generator/coroutine/async-generator to the caller.
	return genResult{retVal: retVal, retDone: true, ok: true}, nil
}

// execYieldValue ports YIELD_VALUE: pops the value to yield, sends it
// on the generator's yield channel, then blocks until the next Send.
// The sent value becomes the result of the yield expression.
//
// CPython: Python/bytecodes.c:1370 YIELD_VALUE
func (e *evalState) execYieldValue(_ uint32) (genResult, error) {
	if e.genYield == nil {
		return genResult{ok: true}, fmt.Errorf("vm: YIELD_VALUE outside generator context")
	}
	val := e.popObject()
	e.genYield <- objects.GenMsg{Val: val}
	// Suspend: block until the next Send / throw.
	msg := <-e.genSend
	if msg.Err != nil {
		return genResult{ok: true}, msg.Err
	}
	// Push the sent value as the result of the yield expression, then
	// continue at the RESUME that immediately follows YIELD_VALUE.
	e.pushObject(msg.Val)
	return genResult{next: e.advance(), ok: true}, nil
}

// execSend ports the SEND opcode: sends a value into the generator or
// iterator on TOS1 and dispatches on the result.
//
// Stack before: [..., receiver, v]
// Normal yield: [..., receiver, yielded_val], advance
// StopIteration: [...], jump past END_SEND by oparg
//
// CPython: Python/bytecodes.c:1297 _SEND
func (e *evalState) execSend(oparg uint32) (genResult, error) {
	v := e.popObject()
	recvRef := e.peek(0)
	recv := recvRef.AsObject()

	switch r := recv.(type) {
	case *objects.Generator:
		val, serr := r.Send(v)
		if errors.Is(serr, objects.ErrStopIteration) {
			// Pop exhausted generator; jump past END_SEND.
			ref := e.pop()
			ref.Close()
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if serr != nil {
			return genResult{ok: true}, serr
		}
		e.pushObject(val)
		return genResult{next: e.cacheAdvance(compile.SEND), ok: true}, nil

	default:
		// Generic path: if v is None, call tp_iternext; otherwise call .send(v).
		t := recv.Type()
		var val objects.Object
		var nerr error
		if v == objects.None() {
			if t.IterNext == nil {
				return genResult{ok: true}, fmt.Errorf("TypeError: %s is not an iterator", t.Name)
			}
			val, nerr = t.IterNext(recv)
		} else {
			sendAttr, agerr := objects.GetAttr(recv, objects.NewStr("send"))
			if agerr != nil {
				return genResult{ok: true}, agerr
			}
			val, nerr = objects.Call(sendAttr, objects.NewTuple([]objects.Object{v}), nil)
		}
		if errors.Is(nerr, objects.ErrStopIteration) {
			ref := e.pop()
			ref.Close()
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if nerr != nil {
			return genResult{ok: true}, nerr
		}
		e.pushObject(val)
		return genResult{next: e.cacheAdvance(compile.SEND), ok: true}, nil
	}
}

// execGetYieldFromIter ports GET_YIELD_FROM_ITER: if TOS is already a
// generator or iterator, leave it. Otherwise call iter(TOS).
//
// CPython: Python/bytecodes.c:3091 GET_YIELD_FROM_ITER
func (e *evalState) execGetYieldFromIter() (genResult, error) {
	iterable := e.popObject()
	if _, isGen := iterable.(*objects.Generator); isGen {
		e.pushObject(iterable)
		return genResult{next: e.advance(), ok: true}, nil
	}
	t := iterable.Type()
	if t.Iter == nil {
		return genResult{ok: true}, fmt.Errorf("TypeError: '%s' object is not iterable", t.Name)
	}
	it, ierr := t.Iter(iterable)
	if ierr != nil {
		return genResult{ok: true}, ierr
	}
	e.pushObject(it)
	return genResult{next: e.advance(), ok: true}, nil
}

// execGetAwaitable ports GET_AWAITABLE: pops a value and pushes an
// awaitable iterator. Coroutines act as their own iterator (CPython
// returns the coroutine directly without the coro_wrapper indirection).
// For everything else the type's __await__ slot is consulted.
//
// CPython: Python/bytecodes.c:1274 GET_AWAITABLE
// CPython: Python/ceval.c:3640 _PyEval_GetAwaitable
// CPython: Objects/genobject.c:1067 _PyCoro_GetAwaitableIter
func (e *evalState) execGetAwaitable(_ uint32) (genResult, error) {
	iterable := e.popObject()
	iter, err := getAwaitableIter(iterable)
	if err != nil {
		return genResult{ok: true}, err
	}
	e.pushObject(iter)
	return genResult{next: e.advance(), ok: true}, nil
}

// execGetAiter ports GET_AITER: pops an iterable and pushes its async
// iterator. Equivalent to value.__aiter__(). The result must itself
// expose __anext__; otherwise the type doesn't satisfy the async-for
// protocol.
//
// CPython: Python/bytecodes.c:1230 GET_AITER
func (e *evalState) execGetAiter() (genResult, error) {
	obj := e.popObject()
	t := obj.Type()
	if t.Async == nil || t.Async.Aiter == nil {
		return genResult{ok: true}, fmt.Errorf(
			"TypeError: 'async for' requires an object with __aiter__ method, got %s", t.Name)
	}
	iter, err := t.Async.Aiter(obj)
	if err != nil {
		return genResult{ok: true}, err
	}
	it := iter.Type()
	if it.Async == nil || it.Async.Anext == nil {
		return genResult{ok: true}, fmt.Errorf(
			"TypeError: 'async for' received an object from __aiter__ that does not implement __anext__: %s",
			it.Name)
	}
	e.pushObject(iter)
	return genResult{next: e.advance(), ok: true}, nil
}

// execGetAnext ports GET_ANEXT: leaves the async iterator on the stack
// and pushes the awaitable that yields the next value. AsyncGenerator
// objects already produce an awaitable from __anext__; for any other
// async iterator the result is routed through the awaitable-iter
// helper to wrap a __await__ shape into an iterator.
//
// CPython: Python/bytecodes.c:1266 GET_ANEXT
// CPython: Python/ceval.c:3562 _PyEval_GetANext
func (e *evalState) execGetAnext() (genResult, error) {
	aiter := e.peek(0).AsObject()
	t := aiter.Type()
	if t.Async == nil || t.Async.Anext == nil {
		return genResult{ok: true}, fmt.Errorf(
			"TypeError: 'async for' requires an iterator with __anext__ method, got %s", t.Name)
	}
	next, err := t.Async.Anext(aiter)
	if err != nil {
		return genResult{ok: true}, err
	}
	// AsyncGenerator's am_anext already returns the AsyncGenASend
	// awaitable. Other shapes return a coroutine or an object whose
	// __await__ produces the awaitable; route through the same helper
	// GET_AWAITABLE uses.
	if _, ok := aiter.(*objects.AsyncGenerator); !ok {
		wrapped, werr := getAwaitableIter(next)
		if werr != nil {
			return genResult{ok: true}, fmt.Errorf(
				"TypeError: 'async for' received an invalid object from __anext__: %s",
				next.Type().Name)
		}
		next = wrapped
	}
	e.pushObject(next)
	return genResult{next: e.advance(), ok: true}, nil
}

// execEndAsyncFor ports END_ASYNC_FOR: the async-for body raised the
// exception sitting at TOS while driving the awaitable below it. If
// it was StopAsyncIteration the loop ends cleanly; anything else
// surfaces as a real raise.
//
// Stack: [..., awaitable, exc] -- []
//
// CPython: Python/bytecodes.c:1442 _END_ASYNC_FOR
func (e *evalState) execEndAsyncFor() (genResult, error) {
	excVal := e.popObject()
	_ = e.popObject() // awaitable; discarded either way

	if isStopAsyncIteration(excVal) {
		return genResult{next: e.advance(), ok: true}, nil
	}
	return genResult{ok: true}, excAsError(excVal)
}

// getAwaitableIter ports _PyCoro_GetAwaitableIter: a coroutine is its
// own awaitable iterator; anything else routes through tp_as_async's
// am_await slot. The slot's result must be an iterator and must not
// be another coroutine (PEP 492 forbids __await__ from returning a
// coroutine).
//
// CPython: Objects/genobject.c:1067 _PyCoro_GetAwaitableIter
func getAwaitableIter(o objects.Object) (objects.Object, error) {
	if _, ok := o.(*objects.Coroutine); ok {
		return o, nil
	}
	t := o.Type()
	if t.Async != nil && t.Async.Await != nil {
		res, err := t.Async.Await(o)
		if err != nil {
			return nil, err
		}
		if _, ok := res.(*objects.Coroutine); ok {
			return nil, fmt.Errorf("TypeError: __await__() returned a coroutine")
		}
		if res.Type().IterNext == nil {
			return nil, fmt.Errorf(
				"TypeError: __await__() returned non-iterator of type '%s'",
				res.Type().Name)
		}
		return res, nil
	}
	return nil, fmt.Errorf("TypeError: object %s can't be used in 'await' expression", t.Name)
}

// isStopAsyncIteration reports whether o represents a StopAsyncIteration
// exception. CPython matches via PyErr_GivenExceptionMatches on
// PyExc_StopAsyncIteration; gopy still keeps the sentinel error along
// with the type name as the bridge.
func isStopAsyncIteration(o objects.Object) bool {
	if o == nil || o == objects.None() {
		return false
	}
	if errors.Is(excAsError(o), objects.ErrStopAsyncIteration) {
		return true
	}
	if t := o.Type(); t != nil && t.Name == "StopAsyncIteration" {
		return true
	}
	return false
}

// execCleanupThrow ports CLEANUP_THROW: if the exception is StopIteration,
// extract its value; otherwise re-raise.
//
// CPython: Python/bytecodes.c:1471 CLEANUP_THROW
func (e *evalState) execCleanupThrow() (genResult, error) {
	excVal := e.popObject() // the thrown exception
	_ = e.popObject()       // last_sent_val (discard)
	_ = e.popObject()       // sub_iter (discard)

	if errors.Is(excAsError(excVal), objects.ErrStopIteration) {
		e.pushObject(objects.None())
		e.pushObject(stopIterationValue(excVal))
		return genResult{next: e.advance(), ok: true}, nil
	}
	return genResult{ok: true}, excAsError(excVal)
}

// stopIterationValue returns the .value attribute of a StopIteration
// exception, mirroring CPython's PyStopIterationObject->value access
// in CLEANUP_THROW. Falls back to None when the exception object
// doesn't carry args (e.g. it was synthesized from a flat sentinel).
//
// CPython: Python/bytecodes.c:1481 CLEANUP_THROW (the
// PyStopIterationObject ->value field)
func stopIterationValue(o objects.Object) objects.Object {
	if exc, ok := o.(objects.ExceptionInstance); ok {
		args := exc.ExceptionArgs()
		if args != nil && args.Len() > 0 {
			return args.Item(0)
		}
	}
	return objects.None()
}

// execWithExceptStart ports WITH_EXCEPT_START: calls context.__exit__
// with the current exception info and pushes the result.
//
// Stack before: [..., exit_fn, exit_self_or_null, lasti, unused, exc_val]
// Stack after:  [..., exit_fn, exit_self_or_null, lasti, unused, exc_val, exit_result]
//
// CPython: Python/bytecodes.c:3524 WITH_EXCEPT_START
func (e *evalState) execWithExceptStart() (genResult, error) {
	// Stack layout from bottom: exit_fn, exit_self, lasti, unused, exc_val (TOS).
	// peek(0)=exc_val, peek(1)=unused, peek(2)=lasti, peek(3)=exit_self, peek(4)=exit_fn
	excVal := e.peek(0).AsObject()
	// exit_fn is 4 below TOS in CPython's layout for the 5-element WITH block.
	exitFnRef := e.peek(4)
	exitFn := exitFnRef.AsObject()

	// Call exit_fn(type, val, traceback). v0.9 passes (type(exc), exc, None)
	// because we don't have traceback objects yet.
	excType := objects.None()
	if excVal != objects.None() {
		excType = excVal.Type()
	}
	result, cerr := objects.Call(exitFn, objects.NewTuple([]objects.Object{excType, excVal, objects.None()}), nil)
	if cerr != nil {
		return genResult{ok: true}, cerr
	}
	e.pushObject(result)
	return genResult{next: e.advance(), ok: true}, nil
}

// excAsError converts an exception object (which may be a string Str or
// a proper exception instance) into a Go error for re-raise.
func excAsError(o objects.Object) error {
	if o == objects.None() {
		return nil
	}
	if errors.Is(excObjectErr(o), objects.ErrStopIteration) {
		return objects.ErrStopIteration
	}
	s, _ := objects.Repr(o)
	return fmt.Errorf("%s", s)
}

// excObjectErr checks if the object represents a StopIteration exception.
func excObjectErr(o objects.Object) error {
	if o == nil || o == objects.None() {
		return nil
	}
	tp := o.Type()
	if tp.Name == "StopIteration" || tp.Name == "stop_iteration" {
		return objects.ErrStopIteration
	}
	return nil
}
