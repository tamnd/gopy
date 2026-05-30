// Generator, coroutine, and context-manager bytecode arms.
// Ports RETURN_GENERATOR, YIELD_VALUE, SEND, GET_YIELD_FROM_ITER,
// GET_AWAITABLE, GET_AITER, GET_ANEXT, END_ASYNC_FOR, CLEANUP_THROW,
// and WITH_EXCEPT_START from Python/bytecodes.c.
//
// CPython: Python/bytecodes.c generator / coroutine / with sections

package vm

// DEPRECATED (spec 1714): Spec 1714 phases 5+6: generator-related opcode bodies (SEND, YIELD_VALUE, GET_ITER, etc) migrate to typed op<NAME> functions.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// genResult bundles the dispatch outcome for the generator / coroutine
// / with panel. ok=false means tryGen did not match the opcode and the
// caller should fall through to the next dispatch layer. Frame return
// (RETURN_GENERATOR) parks the value on e.retVal and surfaces the
// errFrameReturn sentinel through the err return.
type genResult struct {
	next int
	ok   bool
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

	case compile.GET_AITER:
		return e.execGetAiter()

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

// specialMethod carries the dunder name and TypeError messages for LOAD_SPECIAL.
// error is used when the object does not have the required dunder.
// errorSuggestion is used when the object has the peer protocol instead
// (sync object used in async with, or vice versa).
//
// CPython: Python/ceval.c:663 _Py_SpecialMethods
type specialMethod struct {
	name            string
	error           string
	errorSuggestion string
}

// specialMethods maps the LOAD_SPECIAL oparg to method info. Order matches
// CPython's _PySpecialMethod enum.
//
// CPython: Include/internal/pycore_ceval.h:380 _PySpecialMethod
var specialMethods = [...]specialMethod{
	{
		"__enter__",
		"object does not support the context manager protocol (missed __enter__ method)",
		"object does not support the context manager protocol (missed __enter__ method) " +
			"but it supports the asynchronous context manager protocol. Did you mean to use 'async with'?",
	},
	{
		"__exit__",
		"object does not support the context manager protocol (missed __exit__ method)",
		"object does not support the context manager protocol (missed __exit__ method) " +
			"but it supports the asynchronous context manager protocol. Did you mean to use 'async with'?",
	},
	{
		"__aenter__",
		"object does not support the asynchronous context manager protocol (missed __aenter__ method)",
		"object does not support the asynchronous context manager protocol (missed __aenter__ method) " +
			"but it supports the context manager protocol. Did you mean to use 'with'?",
	},
	{
		"__aexit__",
		"object does not support the asynchronous context manager protocol (missed __aexit__ method)",
		"object does not support the asynchronous context manager protocol (missed __aexit__ method) " +
			"but it supports the context manager protocol. Did you mean to use 'with'?",
	},
}

// canSuggest returns true when the object's type has the peer protocol:
// sync __enter__/__exit__ for async ops, async __aenter__/__aexit__ for sync.
//
// CPython: Python/ceval.c:3708 _PyEval_SpecialMethodCanSuggest
func canSuggest(t *objects.Type, oparg uint32) bool {
	var peer1, peer2 string
	switch oparg {
	case 0, 1: // __enter__, __exit__ — suggest if async peer exists
		peer1, peer2 = "__aenter__", "__aexit__"
	case 2, 3: // __aenter__, __aexit__ — suggest if sync peer exists
		peer1, peer2 = "__enter__", "__exit__"
	default:
		return false
	}
	d1, _ := objects.LookupDescriptor(t, peer1)
	d2, _ := objects.LookupDescriptor(t, peer2)
	return d1 != nil && d2 != nil
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
	if int(oparg) >= len(specialMethods) {
		return genResult{ok: true}, fmt.Errorf("vm: LOAD_SPECIAL: oparg %d out of range", oparg)
	}
	sm := specialMethods[oparg]
	owner := e.popObject()
	t := owner.Type()
	descr, _ := objects.LookupDescriptor(t, sm.name)
	if descr == nil {
		// CPython: Python/bytecodes.c:3502 _LOAD_SPECIAL — raise TypeError,
		// with a suggestion when the peer protocol is available.
		// CPython: Python/ceval.c:3708 _PyEval_SpecialMethodCanSuggest
		errMsg := sm.error
		if canSuggest(t, oparg) {
			errMsg = sm.errorSuggestion
		}
		return genResult{ok: true}, fmt.Errorf("TypeError: '%s' %s", t.Name, errMsg)
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
//
//nolint:gocognit,gocyclo // single block mirrors CPython's RETURN_GENERATOR opcode + new_gen_or_coro setup
func (e *evalState) execReturnGenerator() (genResult, error) {
	name := e.f.Code.Name
	qualname := e.f.Code.Qualname
	// When the function's __name__ / __qualname__ have been mutated after
	// compilation, read the live values from the function object so the
	// generator reflects the current names.
	//
	// CPython: Objects/genobject.c:867 gen_new_with_qualname (reads from
	// PyFunction_GET_QUALNAME / PyFunction_GET_NAME which follow the live
	// function attribute, not the code object constant).
	if fn := e.f.Func; fn != nil {
		if v, err := objects.GetAttr(fn, objects.NewStr("__name__")); err == nil && v != nil {
			if s, ok := v.(*objects.Unicode); ok {
				name = s.Value()
			}
		}
		if v, err := objects.GetAttr(fn, objects.NewStr("__qualname__")); err == nil && v != nil {
			if s, ok := v.(*objects.Unicode); ok {
				qualname = s.Value()
			}
		}
	}

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
	// A suspended generator frame has no f_back: it is not on any call
	// stack. CPython clears the link when the frame is detached from the
	// running stack; we mirror that by zeroing Previous here.
	//
	// CPython: Objects/genobject.c:867 gen_new_with_qualname (frame detach)
	savedFrame.Previous = nil
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
	// Build the Python-visible frame object wrapping the saved frame.
	// Shared by gi_frame, cr_frame, ag_frame; cleared on generator close.
	//
	// CPython: Objects/genobject.c:867 gen_new_with_qualname (gi_frame init)
	genFrameObj := objects.NewFrame(savedFrame)
	switch {
	case flags&compile.CoCoroutine != 0:
		c := objects.NewCoroutine(name)
		c.GiFrame = genFrameObj
		genFrameObj.SetOwner(c)
		savedFrame.GenOwner = c
		yieldCh, sendCh = c.YieldCh, c.SendCh
		retVal = c
	case flags&compile.CoAsyncGenerator != 0:
		ag := objects.NewAsyncGenerator(name)
		ag.GiFrame = genFrameObj
		genFrameObj.SetOwner(ag)
		savedFrame.GenOwner = ag
		yieldCh, sendCh = ag.YieldCh, ag.SendCh
		retVal = ag
	default:
		g := objects.NewGenerator(name, qualname)
		g.Code = e.f.Code
		g.GiFrame = genFrameObj
		genFrameObj.SetOwner(g)
		savedFrame.GenOwner = g
		yieldCh, sendCh = g.YieldCh, g.SendCh
		retVal = g
	}

	go func() {
		// Register the generator's goroutine with the active-thread map
		// so currentThread() (used by sys.exc_info and friends) resolves
		// to savedTS. Without this, hook-driven builtins running inside
		// the generator body see a nil thread and return defaults.
		prev, g := setActiveThread(savedTS)
		defer restoreActiveThread(prev, g)

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
		// Capture the generator reference before ge.run() shadows retVal.
		genObj := retVal
		// Mark generator as actively executing so re-entrant Send() calls
		// raise ValueError instead of deadlocking on the channel.
		//
		// CPython: Objects/genobject.c:275 gi_frame_state = FRAME_EXECUTING
		if gen, ok := genObj.(*objects.Generator); ok {
			gen.Running.Store(1)
			// Save caller's handled-exception so we can restore it on the
			// first yield. The generator body inherits the caller's exception
			// initially (chain-walk fallback: gen.ExcHandled == nil means
			// fall through to caller's value), so we do NOT clear
			// ts.HandledException here.
			//
			// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push)
			gen.CallerExc = pyerrors.HandledAsObject(savedTS)
		}
		// Link savedFrame.Previous only when this generator is driven via
		// yield from by another generator (recorded in genCallerFrames by
		// execSend). Generators started by an external gen.send() call
		// leave Previous nil: CPython clears the back-link when the frame
		// is detached from the running stack so that gi_frame.f_back is
		// None for externally-driven generators, preserving the invariant
		// that f_back only connects frames within a yield-from chain.
		//
		// CPython: Objects/genobject.c:867 gen_new_with_qualname
		//   (previous_frame = NULL for a freshly-created generator)
		// CPython: Objects/genobject.c:291 gen_send_ex2
		//   (previous_frame set only for yield-from sub-generators)
		if gen, ok := genObj.(*objects.Generator); ok {
			if cf, ok2 := genCallerFrames.Load(gen); ok2 {
				savedFrame.Previous = cf.(*frame.Frame)
			}
			// else: externally driven — Previous stays nil (set at line 233)
		}
		// Coroutines and async generators driven via yield from would also
		// need a genCallerFrames lookup, but await-chaining is tracked
		// separately (see execSend Coroutine path). Leave Previous nil for
		// now: cr_frame.f_back and ag_frame.f_back are None by default.
		// Register the generator frame for sys._getframe() so that
		// currentInterpreterFrame() returns it while this goroutine runs.
		// Record the frame-stack depth at this entry point so
		// currentInterpreterFrame can tell when callPyFunction has pushed
		// additional frames on top (in which case the stack top, not the
		// saved frame, is the innermost executing frame).
		//
		// CPython: _PyEval_EvalFrameDefault tstate->current_frame is always
		// the innermost frame; gopy approximates that via depth tracking.
		genEntryDepths.Store(g, frameStackFor(savedTS).Depth())
		activeEvalFrames.Store(g, savedFrame)
		defer genEntryDepths.Delete(g)
		defer activeEvalFrames.Delete(g)
		// Expose this generator's own frame so genThrowForwardHook can
		// install it as the "current frame" when forwarding a throw to a
		// custom (non-Generator) yield-from iterator. Mirrors CPython's
		// tstate->current_frame = frame; ... tstate->current_frame = prev
		// in _gen_throw for the custom-iterator path.
		//
		// CPython: Objects/genobject.c:523 _gen_throw
		if gen, ok := genObj.(*objects.Generator); ok {
			genOwnFrames.Store(gen, savedFrame)
			defer genOwnFrames.Delete(gen)
		}
		// Run the generator body. yieldCh/sendCh are threaded through
		// evalState so YIELD_VALUE can reach them.
		ge := &evalState{
			ts:         savedTS,
			f:          savedFrame,
			breaker:    breakerFor(savedTS),
			genYield:   yieldCh,
			genSend:    sendCh,
			code:       savedFrame.Code.Code,
			genRunning: genObj,
		}
		retVal, runErr := ge.run()
		// Clear f_back on exhaustion: a finished generator frame is detached
		// from all call stacks. CPython does the same when the frame's
		// reference count drops to zero; we mirror it eagerly so that
		// gi_frame.f_back is None as soon as the body returns.
		//
		// CPython: Objects/genobject.c:254 gen_send_ex2 (frame detach on return)
		savedFrame.Previous = nil
		// Release every live stackref the body owned (locals, cells, frees,
		// stack). A finished generator should not keep its arguments alive,
		// so test_close_clears_frame / test_close_releases_frame_locals can
		// see referenced objects collected as soon as the body returns or
		// raises. gi_code stays valid because the Generator object holds
		// the code pointer separately from the frame.
		//
		// CPython: Python/frame.c:108 _PyFrame_ClearExceptCode
		savedFrame.FrameClearLocals()
		// Generator body has finished. Restore caller's exception state so
		// the caller's sys.exception() is not contaminated by the generator's
		// last except block. CallerExc is always current because execYieldValue
		// refreshes it on every resume.
		//
		// CPython: Objects/genobject.c gen_send_ex2 (exc_info pop after return)
		if gen, ok := genObj.(*objects.Generator); ok {
			pyerrors.SetHandledFromObject(savedTS, gen.CallerExc)
			// Mark the generator as closed and not running. Without this the
			// last resume left Running == 1, so inspect.getgeneratorstate
			// would report GEN_RUNNING for an exhausted generator. Closed
			// also tells genFinalize to skip the SendCh round-trip; the
			// goroutine has already exited, so sending would deadlock.
			//
			// CPython: Objects/genobject.c:225 gen_send_ex2
			// (gi_frame_state = FRAME_COMPLETED on return)
			gen.Running.Store(0)
			gen.MarkFinished()
		}
		switch {
		case runErr != nil && !errors.Is(runErr, objects.ErrStopIteration):
			// Preserve Python exception identity so callers can check
			// `exc is value` (e.g. _GeneratorContextManager.__exit__).
			// RERAISE returns excSentinel (a plain fmt.Errorf) that loses
			// the Python object; wrap it in RaisedError when the exception
			// is still on the thread state.
			if exc := pyerrors.Occurred(savedTS); exc != nil {
				var re *objects.RaisedError
				if !errors.As(runErr, &re) {
					runErr = objects.NewRaisedError(exc, runErr.Error())
				}
			}
			yieldCh <- objects.GenMsg{Err: runErr}
		case retVal != nil && retVal != objects.None():
			// Body returned a value (PEP 380, PEP 492). CPython wraps
			// it in StopIteration(value) so `except StopIteration as
			// e: e.value` sees the return. Carry the typed Exception
			// across the channel through RaisedError so the receiver's
			// Eval frame finds the original instance on the thread
			// state (preserves `.value` identity).
			//
			// CPython: Objects/genobject.c:225 gen_send_ex2
			exc := pyerrors.New(pyerrors.PyExc_StopIteration,
				objects.NewTuple([]objects.Object{retVal}))
			yieldCh <- objects.GenMsg{Err: objects.NewRaisedError(exc, "StopIteration")}
		default:
			yieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
		}
	}()

	// Return the generator/coroutine/async-generator to the caller.
	// CPython: Python/bytecodes.c:4982 RETURN_GENERATOR ends with
	// goto exit_frame after stashing retval; gopy mirrors that by
	// parking retval on e.retVal and surfacing errFrameReturn so the
	// loop driver pops out of run().
	e.retVal = retVal
	return genResult{ok: true}, errFrameReturn
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
	// Swap exception states on yield: mirror CPython's YIELD_VALUE restoring
	// tstate->exc_info to the caller's chain. ExcDepth tracks how many of
	// the generator's own PUSH_EXC_INFO calls are still active. If ExcDepth
	// is zero the generator's gi_exc_state.exc_value would be NULL in CPython
	// (all own except blocks have been exited or the body never entered one),
	// so we record nil to enable the chain-walk fallback on the next resume.
	//
	// CPython: Python/bytecodes.c:1383 YIELD_VALUE (tstate->exc_info restore)
	// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push)
	if gen, ok := e.genRunning.(*objects.Generator); ok {
		gen.Running.Store(0)
		if gen.ExcDepth > 0 {
			gen.ExcHandled = pyerrors.HandledAsObject(e.ts)
		} else {
			gen.ExcHandled = nil
		}
		pyerrors.SetHandledFromObject(e.ts, gen.CallerExc)
	}
	// Deregister frame and clear f_back before yielding so that
	// suspended generators are invisible to sys._getframe() from the
	// caller's side, matching CPython (only running frames are visible).
	//
	// CPython: Objects/genobject.c gen_send_ex2 clears previous_frame on yield
	gID := goid()
	activeEvalFrames.Delete(gID)
	savedPrev := e.f.Previous
	e.f.Previous = nil

	e.genYield <- objects.GenMsg{Val: val}
	// Suspend: block until the next Send / throw.
	msg := <-e.genSend

	// Re-register frame and re-link f_back on resume.
	// CPython: Objects/genobject.c gen_send_ex2 sets previous_frame before
	// re-entering the body.
	if gen, ok2 := e.genRunning.(*objects.Generator); ok2 {
		if cf, ok3 := genCallerFrames.Load(gen); ok3 {
			e.f.Previous = cf.(*frame.Frame)
		} else {
			e.f.Previous = savedPrev
		}
	} else {
		e.f.Previous = savedPrev
	}
	// Refresh the entry depth and re-register the generator frame. The
	// entry depth is the frame-stack depth at this exact resume point;
	// any subsequent callPyFunction call will push above it.
	genEntryDepths.Store(gID, frameStackFor(e.ts).Depth())
	activeEvalFrames.Store(gID, e.f)

	// On resume: save the new caller state into CallerExc, then restore
	// the generator's own exception state. When ExcHandled is nil (generator
	// never entered its own except block, or exited all of them), do not
	// override ts.HandledException — the generator sees the caller's current
	// exception (chain-walk fallback). This mirrors gen_send_ex2 re-pushing
	// gi_exc_state and _PyErr_GetTopmostException walking to previous_item
	// when gen's slot is NULL.
	//
	// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push)
	// CPython: Python/errors.c:116 _PyErr_GetTopmostException (chain walk)
	if gen, ok := e.genRunning.(*objects.Generator); ok {
		gen.Running.Store(1)
		gen.CallerExc = pyerrors.HandledAsObject(e.ts)
		if gen.ExcHandled != nil {
			pyerrors.SetHandledFromObject(e.ts, gen.ExcHandled)
		}
		// else: ExcHandled == nil → chain-walk fallback: ts.HandledException
		// stays as gen.CallerExc, which is what the generator inherits.
	}
	if msg.Err != nil {
		// A throw() that carried a Python exception object travels as
		// objects.RaisedError. Install it on the thread state so
		// handleException's Occurred(ts) check finds the original
		// PyObject, preserving identity for `exc is value` checks.
		//
		// CPython: Objects/genobject.c:586 _gen_throw (PyErr_Restore
		// before gen_send_ex)
		var re *objects.RaisedError
		if errors.As(msg.Err, &re) {
			if exc, ok2 := re.Exc.(*pyerrors.Exception); ok2 {
				// Clear any stale exception left by the sub-generator's
				// goroutine before re-raising so that pyerrors.Raise can
				// chain __context__ correctly. Without the clear, prev ==
				// exc (same pointer) and the condition prev != exc fails.
				//
				// CPython: Objects/genobject.c gen_send_ex2 PyErr_Restore
				// always re-installs the exception on a fresh slot; the
				// prior exception was normalised and cleared before
				// gen_send_ex entered the body.
				pyerrors.Clear(e.ts)
				pyerrors.Raise(e.ts, exc)
			}
		}
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
// Stack before:      [..., receiver, v]
// Normal yield:      [..., receiver, yielded_val], re-execute SEND
// StopIteration:     [..., receiver, retval], jump to END_SEND
//
// The StopIteration path leaves receiver on the stack so END_SEND
// can pop it together with retval in one place, matching CPython's
// stack discipline. The previous code popped receiver here, which
// caused END_SEND to underflow the stack and corrupt the outer
// for-loop's iterator.
//
// CPython: Python/bytecodes.c:1297 _SEND
//
//nolint:gocognit,gocyclo // mirrors _SEND opcode: gen/coro/async-gen routing, fastpath, blocking SendCh, exception handoff
func (e *evalState) execSend(oparg uint32) (genResult, error) {
	v := e.popObject()
	recvRef := e.peek(0)
	recv := recvRef.AsObject()

	// Track the yield-from sub-iterator on the outer generator so
	// gi_yieldfrom and throw-forwarding (_gen_throw) work correctly.
	//
	// CPython: Objects/genobject.c:469 _gen_throw (_PyGen_yf via
	// gi_frame_state == FRAME_SUSPENDED_YIELD_FROM)
	outerGen, _ := e.genRunning.(*objects.Generator)
	if outerGen != nil {
		outerGen.YieldFromTarget = recv
	}

	switch r := recv.(type) {
	case *objects.Generator:
		// Link r's frame back to this generator's frame so sys._getframe()
		// chains correctly inside the sub-generator body.
		//
		// CPython: Objects/genobject.c gen_send_ex2 (sets previous_frame)
		genCallerFrames.Store(r, e.f)
		val, serr := r.Send(v)
		if retVal, isStop := stopIterRetval(serr); isStop {
			// Leave receiver on stack; push the StopIteration return
			// value (args[0] or None). END_SEND will pop both.
			//
			// CPython: Python/bytecodes.c _SEND (StopIteration path)
			// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
			if outerGen != nil {
				outerGen.YieldFromTarget = nil
			}
			genCallerFrames.Delete(r)
			e.pushObject(retVal)
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if serr != nil {
			if outerGen != nil {
				outerGen.YieldFromTarget = nil
			}
			genCallerFrames.Delete(r)
			return genResult{ok: true}, serr
		}
		e.pushObject(val)
		return genResult{next: e.cacheAdvance(compile.SEND), ok: true}, nil

	case *objects.Coroutine:
		val, serr := r.Send(v)
		if retVal, isStop := stopIterRetval(serr); isStop {
			// _PyGen_FetchStopIterationValue: pull args[0] (or None)
			// from the StopIteration so END_SEND can hand it back as
			// the value of the await expression.
			//
			// CPython: Python/bytecodes.c _SEND (StopIteration path)
			// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
			if outerGen != nil {
				outerGen.YieldFromTarget = nil
			}
			e.pushObject(retVal)
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if serr != nil {
			if outerGen != nil {
				outerGen.YieldFromTarget = nil
			}
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
				if outerGen != nil {
					outerGen.YieldFromTarget = nil
				}
				return genResult{ok: true}, fmt.Errorf("TypeError: %s is not an iterator", t.Name)
			}
			val, nerr = t.IterNext(recv)
		} else {
			sendAttr, agerr := objects.GetAttr(recv, objects.NewStr("send"))
			if agerr != nil {
				if outerGen != nil {
					outerGen.YieldFromTarget = nil
				}
				return genResult{ok: true}, agerr
			}
			val, nerr = objects.Call(sendAttr, objects.NewTuple([]objects.Object{v}), nil)
		}
		if retVal, isStop := stopIterRetval(nerr); isStop {
			// Same discipline as the Generator path above.
			//
			// CPython: Python/bytecodes.c _SEND (StopIteration path)
			// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
			if outerGen != nil {
				outerGen.YieldFromTarget = nil
			}
			e.pushObject(retVal)
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if nerr != nil {
			if outerGen != nil {
				outerGen.YieldFromTarget = nil
			}
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

// stopIterRetval inspects err for a StopIteration crossing. Returns
// (value, true) when err is either the bare ErrStopIteration sentinel
// or a RaisedError wrapping a StopIteration exception. value is the
// args[0] payload when present, else None. Used by _SEND to drop the
// await result into the END_SEND slot.
//
// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
func stopIterRetval(err error) (objects.Object, bool) {
	if err == nil {
		return nil, false
	}
	if errors.Is(err, objects.ErrStopIteration) {
		var re *objects.RaisedError
		if errors.As(err, &re) {
			if exc, ok := re.Exc.(objects.ExceptionInstance); ok {
				args := exc.ExceptionArgs()
				if args != nil && args.Len() > 0 {
					return args.Item(0), true
				}
			}
		}
		return objects.None(), true
	}
	var re *objects.RaisedError
	if errors.As(err, &re) {
		if exc, ok := re.Exc.(*pyerrors.Exception); ok {
			if exc.ExcType != nil && exc.ExcType.Name == "StopIteration" {
				if exc.Args != nil && exc.Args.Len() > 0 {
					return exc.Args.Item(0), true
				}
				return objects.None(), true
			}
		}
	}
	return nil, false
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
	// exit_self lives one slot above exit_fn; when the LOAD_SPECIAL that
	// produced the pair pushed an unbound descriptor (no DescrGet, the
	// builtin-function case), this slot holds the owner and must be
	// prepended to the positional args so the call sees self.
	exitSelfRef := e.peek(3)
	var exitSelf objects.Object
	if !exitSelfRef.IsNull() {
		exitSelf = exitSelfRef.AsObject()
	}

	// Call exit_fn(type, val, traceback).
	excType := objects.None()
	excTB := objects.None()
	if excVal != objects.None() {
		excType = excVal.Type()
		if exc, ok := excVal.(*pyerrors.Exception); ok && exc.TB != nil {
			excTB = exc.TB
		}
	}
	var callArgs []objects.Object
	if exitSelf != nil {
		callArgs = []objects.Object{exitSelf, excType, excVal, excTB}
	} else {
		callArgs = []objects.Object{excType, excVal, excTB}
	}
	result, cerr := objects.Call(exitFn, objects.NewTuple(callArgs), nil)
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
