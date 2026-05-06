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
	"github.com/tamnd/gopy/objects"
)

// tryGen handles generator / coroutine / with opcodes. Returns ok=false
// when op is not in this panel; the call shape matches dispatch.
//
//nolint:gocognit,gocyclo // generator dispatch panel; arm count is bounded by spec 1693
func (e *evalState) tryGen(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
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
	}
	return 0, nil, nil, false, false, nil
}

// execReturnGenerator ports RETURN_GENERATOR: detaches the current frame,
// creates a generator object whose goroutine will drive the body, and
// returns the generator to the caller.
//
// CPython: Python/bytecodes.c:4982 RETURN_GENERATOR
func (e *evalState) execReturnGenerator() (int, objects.Object, error, bool, bool, error) {
	name := e.f.Code.Name

	// Capture the post-RETURN_GENERATOR IP before Detach. Detach zeroes
	// the arena slot that e.f points at, so reading e.f.InstrPtr after
	// Detach would yield 0 and the generator goroutine would loop back
	// onto RETURN_GENERATOR.
	resumeIP := e.advance()

	// Detach the frame from the thread's chunk arena. The generator
	// takes ownership and will run the body lazily.
	stack := frameStackFor(e.ts)
	savedFrame := stack.Detach()
	savedFrame.InstrPtr = resumeIP
	savedTS := e.ts

	gen := objects.NewGenerator(name)

	go func() {
		// Block until the first Send() call. The first message must be
		// None (enforced by Generator.Send); we discard it here because
		// the generator body begins from the frame's IP, not a yield
		// point, so there is no stack slot waiting for the sent value.
		msg := <-gen.SendCh
		if msg.Err != nil {
			// close() before first next(): just signal StopIteration.
			gen.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
			return
		}
		// Run the generator body. yieldCh/sendCh are threaded through
		// evalState so YIELD_VALUE can reach them.
		ge := &evalState{
			ts:      savedTS,
			f:       savedFrame,
			breaker: breakerFor(savedTS),
			genYield: gen.YieldCh,
			genSend:  gen.SendCh,
		}
		_, runErr := ge.run()
		if runErr != nil && !errors.Is(runErr, objects.ErrStopIteration) {
			gen.YieldCh <- objects.GenMsg{Err: runErr}
		} else {
			gen.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
		}
	}()

	// Return the generator to the caller as a terminal return.
	return 0, gen, nil, true, true, nil
}

// execYieldValue ports YIELD_VALUE: pops the value to yield, sends it
// on the generator's yield channel, then blocks until the next Send.
// The sent value becomes the result of the yield expression.
//
// CPython: Python/bytecodes.c:1370 YIELD_VALUE
func (e *evalState) execYieldValue(_ uint32) (int, objects.Object, error, bool, bool, error) {
	if e.genYield == nil {
		return 0, nil, nil, false, true, fmt.Errorf("vm: YIELD_VALUE outside generator context")
	}
	val := e.popObject()
	e.genYield <- objects.GenMsg{Val: val}
	// Suspend: block until the next Send / throw.
	msg := <-e.genSend
	if msg.Err != nil {
		return 0, nil, nil, false, true, msg.Err
	}
	// Push the sent value as the result of the yield expression, then
	// continue at the RESUME that immediately follows YIELD_VALUE.
	e.pushObject(msg.Val)
	return e.advance(), nil, nil, false, true, nil
}

// execSend ports the SEND opcode: sends a value into the generator or
// iterator on TOS1 and dispatches on the result.
//
// Stack before: [..., receiver, v]
// Normal yield: [..., receiver, yielded_val], advance
// StopIteration: [...], jump past END_SEND by oparg
//
// CPython: Python/bytecodes.c:1297 _SEND
func (e *evalState) execSend(oparg uint32) (int, objects.Object, error, bool, bool, error) {
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
			return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
		}
		if serr != nil {
			return 0, nil, nil, false, true, serr
		}
		e.pushObject(val)
		return e.advance(), nil, nil, false, true, nil

	default:
		// Generic path: if v is None, call tp_iternext; otherwise call .send(v).
		t := recv.Type()
		var val objects.Object
		var nerr error
		if v == objects.None() {
			if t.IterNext == nil {
				return 0, nil, nil, false, true, fmt.Errorf("TypeError: %s is not an iterator", t.Name)
			}
			val, nerr = t.IterNext(recv)
		} else {
			sendAttr, agerr := objects.GetAttr(recv, objects.NewStr("send"))
			if agerr != nil {
				return 0, nil, nil, false, true, agerr
			}
			val, nerr = objects.Call(sendAttr, objects.NewTuple([]objects.Object{v}), nil)
		}
		if errors.Is(nerr, objects.ErrStopIteration) {
			ref := e.pop()
			ref.Close()
			return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
		}
		if nerr != nil {
			return 0, nil, nil, false, true, nerr
		}
		e.pushObject(val)
		return e.advance(), nil, nil, false, true, nil
	}
}

// execGetYieldFromIter ports GET_YIELD_FROM_ITER: if TOS is already a
// generator or iterator, leave it. Otherwise call iter(TOS).
//
// CPython: Python/bytecodes.c:3091 GET_YIELD_FROM_ITER
func (e *evalState) execGetYieldFromIter() (int, objects.Object, error, bool, bool, error) {
	iterable := e.popObject()
	switch iterable.(type) {
	case *objects.Generator:
		e.pushObject(iterable)
		return e.advance(), nil, nil, false, true, nil
	}
	t := iterable.Type()
	if t.Iter == nil {
		return 0, nil, nil, false, true, fmt.Errorf("TypeError: '%s' object is not iterable", t.Name)
	}
	it, ierr := t.Iter(iterable)
	if ierr != nil {
		return 0, nil, nil, false, true, ierr
	}
	e.pushObject(it)
	return e.advance(), nil, nil, false, true, nil
}

// execGetAwaitable ports GET_AWAITABLE: for now surfaces coroutines as
// not yet implemented; awaitable protocol (1687) lands in v0.10.
//
// CPython: Python/bytecodes.c:1274 GET_AWAITABLE
func (e *evalState) execGetAwaitable(_ uint32) (int, objects.Object, error, bool, bool, error) {
	return 0, nil, nil, false, true, fmt.Errorf("GET_AWAITABLE: coroutine/awaitable protocol not yet implemented (v0.9)")
}

// execGetAiter ports GET_AITER: async iterator protocol pending.
//
// CPython: Python/bytecodes.c:1230 GET_AITER
func (e *evalState) execGetAiter() (int, objects.Object, error, bool, bool, error) {
	return 0, nil, nil, false, true, fmt.Errorf("GET_AITER: async iterator protocol not yet implemented (v0.9)")
}

// execGetAnext ports GET_ANEXT: async iterator protocol pending.
//
// CPython: Python/bytecodes.c:1266 GET_ANEXT
func (e *evalState) execGetAnext() (int, objects.Object, error, bool, bool, error) {
	return 0, nil, nil, false, true, fmt.Errorf("GET_ANEXT: async iterator protocol not yet implemented (v0.9)")
}

// execEndAsyncFor ports END_ASYNC_FOR: pops async iterator and awaitable;
// re-raises unless the exception is StopAsyncIteration.
// Async protocol pending.
//
// CPython: Python/bytecodes.c END_ASYNC_FOR (_END_ASYNC_FOR)
func (e *evalState) execEndAsyncFor() (int, objects.Object, error, bool, bool, error) {
	return 0, nil, nil, false, true, fmt.Errorf("END_ASYNC_FOR: async iterator protocol not yet implemented (v0.9)")
}

// execCleanupThrow ports CLEANUP_THROW: if the exception is StopIteration,
// extract its value; otherwise re-raise.
//
// CPython: Python/bytecodes.c:1471 CLEANUP_THROW
func (e *evalState) execCleanupThrow() (int, objects.Object, error, bool, bool, error) {
	excVal := e.popObject()   // the thrown exception
	_ = e.popObject()         // last_sent_val (discard)
	_ = e.popObject()         // sub_iter (discard)

	if errors.Is(excAsError(excVal), objects.ErrStopIteration) {
		e.pushObject(objects.None())
		// StopIteration.value would be the stop value; use None for now.
		e.pushObject(objects.None())
		return e.advance(), nil, nil, false, true, nil
	}
	return 0, nil, nil, false, true, excAsError(excVal)
}

// execWithExceptStart ports WITH_EXCEPT_START: calls context.__exit__
// with the current exception info and pushes the result.
//
// Stack before: [..., exit_fn, exit_self_or_null, lasti, unused, exc_val]
// Stack after:  [..., exit_fn, exit_self_or_null, lasti, unused, exc_val, exit_result]
//
// CPython: Python/bytecodes.c:3524 WITH_EXCEPT_START
func (e *evalState) execWithExceptStart() (int, objects.Object, error, bool, bool, error) {
	// Stack layout from bottom: exit_fn, exit_self, lasti, unused, exc_val (TOS).
	// peek(0)=exc_val, peek(1)=unused, peek(2)=lasti, peek(3)=exit_self, peek(4)=exit_fn
	excVal := e.peek(0).AsObject()
	// exit_fn is 4 below TOS in CPython's layout for the 5-element WITH block.
	exitFnRef := e.peek(4)
	exitFn := exitFnRef.AsObject()

	// Call exit_fn(type, val, traceback). v0.9 passes (type(exc), exc, None)
	// because we don't have traceback objects yet.
	excType := objects.Object(objects.None())
	if excVal != objects.None() {
		excType = excVal.Type()
	}
	result, cerr := objects.Call(exitFn, objects.NewTuple([]objects.Object{excType, excVal, objects.None()}), nil)
	if cerr != nil {
		return 0, nil, nil, false, true, cerr
	}
	e.pushObject(result)
	return e.advance(), nil, nil, false, true, nil
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

