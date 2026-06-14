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
	"github.com/tamnd/gopy/state"
)

// genGILResume gives the running generator body ownership of the GIL as
// it wakes from a Send. In baton mode (the driver held the lock and
// shares ts with the body) the hold is simply reassigned to this
// goroutine. In genuine mode (the driver is outside Eval, e.g. a bare
// gen.Send() from Go) the body acquires the lock for real, blocking if
// another thread holds it. No-op when no GIL is attached (unit tests).
func genGILResume(ts *state.Thread, bodyGoid uint64, driverHolds bool) {
	g := vmFor(ts).gil
	if g == nil {
		return
	}
	if driverHolds {
		g.Handoff(ts, bodyGoid)
	} else {
		g.AcquireReentrant(ts, bodyGoid)
	}
}

// genGILSuspend releases the GIL the body owns before it parks on a
// yield (or finishes). In baton mode the lock is handed back to the
// driver goroutine, which resumes from <-YieldCh already recorded as the
// owner. In genuine mode the lock is released outright so other threads,
// or a later bare Send, can take it; handing a phantom baton to a driver
// that never re-enters Eval would pin the lock forever. No-op when no GIL
// is attached.
func genGILSuspend(ts *state.Thread, bodyGoid, driverGoid uint64, driverHolds bool) {
	g := vmFor(ts).gil
	if g == nil {
		return
	}
	if driverHolds {
		g.Handoff(ts, driverGoid)
	} else {
		g.ReleaseGoid(ts, bodyGoid)
	}
}

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
		// popObject stole the owner reference, and __get__ minted a bound
		// method that owns its own reference to owner. Release the stolen
		// reference the way LOAD_SPECIAL's DECREF_INPUTS does; without it
		// every `with` / `async with` leaked the context manager once per
		// __enter__/__exit__ lookup (test_coroutines test_for_6).
		//
		// CPython: Python/bytecodes.c:3502 LOAD_SPECIAL (DECREF_INPUTS)
		objects.Decref(owner)
		if err != nil {
			return genResult{ok: true}, err
		}
		e.pushObject(bound)
		e.push(stackref.Null)
		return genResult{next: e.advance(), ok: true}, nil
	}
	// Method-like path: LookupDescriptor returned a borrowed reference, so
	// take a fresh strong reference for the attr slot (Py_NewRef) and leave
	// owner in the self_or_null slot (its stolen reference transfers there).
	//
	// CPython: Python/bytecodes.c:3502 LOAD_SPECIAL (attr = Py_NewRef(meth))
	objects.Incref(descr)
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
		c := objects.NewCoroutineWithQualname(name, qualname)
		c.Code = e.f.Code
		c.GiFrame = genFrameObj
		genFrameObj.SetOwner(c)
		savedFrame.GenOwner = c
		yieldCh, sendCh = c.YieldCh, c.SendCh
		// cr_origin: capture a traceback-style tuple of the caller chain
		// when sys.set_coroutine_origin_tracking_depth is non-zero. The
		// new coroutine's own frame is incomplete, so start the walk at
		// its previous frame (the caller).
		//
		// CPython: Objects/genobject.c:966 make_gen (cr_origin block),
		// Objects/genobject.c:1369 compute_cr_origin
		if e.ts != nil && e.ts.CoroutineOriginTrackingDepth > 0 {
			c.CrOrigin = computeCrOrigin(e.f.Previous, e.ts.CoroutineOriginTrackingDepth)
		}
		retVal = c
	case flags&compile.CoAsyncGenerator != 0:
		ag := objects.NewAsyncGeneratorWithQualname(name, qualname)
		ag.Code = e.f.Code
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
		prev, g := registerGenThread(savedTS)
		defer unregisterGenThread(prev, g)

		// Block until the first Send() call. The first message must be
		// None (enforced by Generator.Send); we discard it here because
		// the generator body begins from the frame's IP, not a yield
		// point, so there is no stack slot waiting for the sent value.
		msg := <-sendCh
		// Acquire the GIL for the running body. In baton mode the driver
		// holds the lock (parked in Send on <-YieldCh) and shares savedTS
		// with this body, so the handoff just reassigns the owning
		// goroutine; in genuine mode (a bare gen.Send() from Go outside any
		// Eval frame) the body acquires the lock for real. Either way the
		// body genuinely holds the GIL while it runs and can release it
		// around a blocking primitive (a nested thread join, lock, sleep).
		genGILResume(savedTS, g, msg.CallerHoldsGIL)
		if msg.Err != nil {
			// close() before first next(): just signal StopIteration.
			genGILSuspend(savedTS, g, msg.CallerGoid, msg.CallerHoldsGIL)
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
		if c, ok := genObj.(*objects.Coroutine); ok {
			c.Running.Store(1)
		}
		if ag, ok := genObj.(*objects.AsyncGenerator); ok {
			ag.Running.Store(1)
		}
		// Set savedFrame.Previous to the caller's frame at the moment of
		// resume. CPython does exactly this in gen_send_ex2:
		//
		//   frame->previous = tstate->current_frame;
		//   tstate->current_frame = frame;
		//
		// Generator.Send / Throw / Close stamp the caller's frame into
		// msg.CallerFrame so the resume can read it from this goroutine
		// without racing on the caller's frame stack. genCallerFrames is
		// still consulted as a fallback because yield-from forwarding may
		// route through a hook that does not populate CallerFrame.
		//
		// CPython: Objects/genobject.c:248 gen_send_ex2
		if cf, ok := msg.CallerFrame.(*frame.Frame); ok && cf != nil {
			savedFrame.Previous = cf
		} else if gen, ok2 := genObj.(*objects.Generator); ok2 {
			if cf2, ok3 := genCallerFrames.Load(gen); ok3 {
				savedFrame.Previous = cf2.(*frame.Frame)
			}
		}
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
			ts:             savedTS,
			f:              savedFrame,
			breaker:        breakerFor(savedTS),
			gil:            vmFor(savedTS).gil,
			gilTimer:       &vmFor(savedTS).gilTimer,
			genYield:       yieldCh,
			genSend:        sendCh,
			code:           savedFrame.Code.Code,
			genRunning:     genObj,
			genDriverGoid:  msg.CallerGoid,
			genDriverHolds: msg.CallerHoldsGIL,
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
		// Mark the running gen-like as closed and not running. Without
		// this the last resume left Running == 1, so
		// inspect.getgeneratorstate / getcoroutinestate / getasyncgenstate
		// would report a still-running state for an exhausted body. The
		// closed flag also tells the per-type Finalize/Close paths to
		// skip the SendCh round-trip; the body goroutine has already
		// exited, so sending would deadlock.
		//
		// CPython: Objects/genobject.c:225 gen_send_ex2
		// (gi_frame_state = FRAME_COMPLETED on return)
		switch g := genObj.(type) {
		case *objects.Generator:
			pyerrors.SetHandledFromObject(savedTS, g.CallerExc)
			g.Running.Store(0)
			g.MarkFinished()
		case *objects.Coroutine:
			g.Running.Store(0)
			g.MarkFinished()
		case *objects.AsyncGenerator:
			g.Running.Store(0)
			g.MarkFinished()
		}
		// Body has finished. Release the GIL the body owned before
		// signaling completion on yieldCh. In baton mode the lock is
		// handed back to the driver that last resumed us (it wakes from
		// <-YieldCh holding the lock, exactly as before the Send); in
		// genuine mode the lock is released outright.
		genGILSuspend(savedTS, g, ge.genDriverGoid, ge.genDriverHolds)
		switch {
		case runErr != nil && !errors.Is(runErr, objects.ErrStopIteration):
			// Preserve Python exception identity so callers can check
			// `exc is value` (e.g. _GeneratorContextManager.__exit__).
			// RERAISE returns excSentinel (a plain fmt.Errorf) that loses
			// the Python object; wrap it in RaisedError when the exception
			// is still on the thread state.
			//
			// When the body raises a Go error that propagates all the way
			// out without any frame-level handler synthesizing a typed
			// exception (e.g. an opcode arm setting pendingErr to a
			// prefixed string when there is no try/except in the body),
			// the thread state stays clear. Promote the Go error here so
			// receivers see the right TypeError / RuntimeError / etc.,
			// not a bare Exception with the prefixed message in args[0].
			//
			// CPython: Python/errors.c PyErr_NormalizeException
			var re *objects.RaisedError
			if !errors.As(runErr, &re) {
				if exc := pyerrors.Occurred(savedTS); exc != nil {
					runErr = objects.NewRaisedError(exc, runErr.Error())
				} else {
					exc := synthesizeException(runErr)
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
//
// setRunningFlag flips the gi_running / cr_running / ag_running atomic on
// whichever suspendable is driving the frame. CPython sets gi_frame_state to
// FRAME_EXECUTING on resume and back to FRAME_SUSPENDED on yield; gopy tracks
// the equivalent with the Running atomic per suspendable type.
//
// CPython: Objects/genobject.c:248 gen_send_ex2 (gi_frame_state transitions)
func setRunningFlag(running objects.Object, v int32) {
	switch r := running.(type) {
	case *objects.Generator:
		r.Running.Store(v)
	case *objects.Coroutine:
		r.Running.Store(v)
	case *objects.AsyncGenerator:
		r.Running.Store(v)
	}
}

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
	setRunningFlag(e.genRunning, 0)
	// Save this suspendable's own exc_info and restore the caller's, the
	// same swap gen_send_ex2 does for generators, coroutines, and async
	// generators alike. Without it a coroutine / async generator that
	// catches an exception and then suspends leaks its handled exception
	// into the caller's chain, so the caller's next raise picks it up as
	// __context__.
	//
	// CPython: Python/bytecodes.c:1383 YIELD_VALUE (tstate->exc_info restore)
	// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push)
	if es, ok := e.genRunning.(objects.GenExcState); ok {
		if es.ExcDepthVal() > 0 {
			es.SetExcHandled(pyerrors.HandledAsObject(e.ts))
		} else {
			es.SetExcHandled(nil)
		}
		pyerrors.SetHandledFromObject(e.ts, es.GetCallerExc())
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

	// Release the GIL before parking. In baton mode the lock is handed
	// back to the driver, which resumes from <-YieldCh holding it while
	// this body sleeps on <-genSend; in genuine mode the lock is released
	// outright. Without this the lock would stay pinned to this
	// (now-parked) goroutine and starve every other Python thread.
	genGILSuspend(e.ts, gID, e.genDriverGoid, e.genDriverHolds)
	e.genYield <- objects.GenMsg{Val: val}
	// Suspend: block until the next Send / throw.
	msg := <-e.genSend
	// Resumed: refresh the driver identity and GIL mode, then reacquire so
	// this body owns the GIL again while it runs.
	if msg.CallerGoid != 0 {
		e.genDriverGoid = msg.CallerGoid
	}
	e.genDriverHolds = msg.CallerHoldsGIL
	genGILResume(e.ts, gID, e.genDriverHolds)

	// Re-register frame and re-link f_back on resume. CPython unconditionally
	// sets frame->previous = tstate->current_frame on every resume, not just
	// the first one (so gi_frame.f_back tracks the caller correctly across
	// suspend/resume). Read it from msg.CallerFrame which Send/Throw/Close
	// populate; fall back to genCallerFrames for yield-from-only paths and
	// savedPrev for non-Generator producers (coroutine / asyncgen).
	//
	// CPython: Objects/genobject.c:248 gen_send_ex2 (previous_frame set)
	switch {
	case msg.CallerFrame != nil:
		if cf, ok := msg.CallerFrame.(*frame.Frame); ok {
			e.f.Previous = cf
		} else {
			e.f.Previous = savedPrev
		}
	case e.genRunning != nil:
		if gen, ok2 := e.genRunning.(*objects.Generator); ok2 {
			if cf, ok3 := genCallerFrames.Load(gen); ok3 {
				e.f.Previous = cf.(*frame.Frame)
			} else {
				e.f.Previous = savedPrev
			}
		} else {
			e.f.Previous = savedPrev
		}
	default:
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
	setRunningFlag(e.genRunning, 1)
	// Stash the new caller's exc_info, then re-push this suspendable's own
	// handled exception. When ExcHandled is nil (no active own except
	// block) we leave ts.HandledException as the caller's value, the
	// chain-walk fallback _PyErr_GetTopmostException uses when the slot is
	// NULL. Same restore for all three suspendable types.
	//
	// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push)
	// CPython: Python/errors.c:116 _PyErr_GetTopmostException (chain walk)
	if es, ok := e.genRunning.(objects.GenExcState); ok {
		es.SetCallerExc(pyerrors.HandledAsObject(e.ts))
		if es.GetExcHandled() != nil {
			pyerrors.SetHandledFromObject(e.ts, es.GetExcHandled())
		}
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
func (e *evalState) execSend(oparg uint32) (genResult, error) {
	v := e.popObject()
	recvRef := e.peek(0)
	recv := recvRef.AsObject()

	// Track the yield-from sub-iterator on whichever gen-like object is
	// driving this frame so gi_yieldfrom / cr_await / ag_await
	// (and throw-forwarding through _gen_throw) work correctly. CPython
	// surfaces this via _PyGen_yf walking the suspended frame's TOS;
	// gopy stores it explicitly on the running object instead.
	//
	// CPython: Objects/genobject.c:469 _gen_throw (_PyGen_yf via
	// gi_frame_state == FRAME_SUSPENDED_YIELD_FROM)
	// CPython: Objects/genobject.c:1129 coro_get_cr_await
	// CPython: Objects/genobject.c:1608 async_gen ag_await
	setRunningYieldFrom(e.genRunning, recv)

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
			setRunningYieldFrom(e.genRunning, nil)
			genCallerFrames.Delete(r)
			e.pushObject(retVal)
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if serr != nil {
			setRunningYieldFrom(e.genRunning, nil)
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
			setRunningYieldFrom(e.genRunning, nil)
			e.pushObject(retVal)
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if serr != nil {
			setRunningYieldFrom(e.genRunning, nil)
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
				setRunningYieldFrom(e.genRunning, nil)
				return genResult{ok: true}, fmt.Errorf("TypeError: %s is not an iterator", t.Name)
			}
			val, nerr = t.IterNext(recv)
			if nerr == nil {
				// tp_iternext returns borrowed; promote to owned before the VM
				// stack takes it, matching CPython's PyIter_Next convention.
				//
				// CPython: Objects/abstract.c:2840 PyIter_Next (returns new ref)
				objects.Incref(val)
			}
		} else {
			sendAttr, agerr := objects.GetAttr(recv, objects.NewStr("send"))
			if agerr != nil {
				setRunningYieldFrom(e.genRunning, nil)
				return genResult{ok: true}, agerr
			}
			val, nerr = objects.Call(sendAttr, objects.NewTuple([]objects.Object{v}), nil)
		}
		if retVal, isStop := e.stopIterRetval(nerr); isStop {
			// Same discipline as the Generator path above. Bare excSentinel
			// (fmt.Errorf("StopIteration: 42")) carries no errors.Is chain
			// back to ErrStopIteration, so e.stopIterRetval consults the
			// live tstate exception as a fallback. Required for
			// `yield from CustomIter` where __next__ raises StopIteration
			// instead of returning NULL.
			//
			// CPython: Python/bytecodes.c _SEND (StopIteration path)
			// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
			setRunningYieldFrom(e.genRunning, nil)
			// _PyGen_FetchStopIterationValue clears the error indicator once
			// it pulls the value out. A Python-level `raise StopIteration`
			// inside the sub-iterator's send() leaves the exception live on
			// the thread state; if we don't clear it, the enclosing
			// coroutine's own RETURN re-reports the stale StopIteration
			// instead of completing with its real return value.
			pyerrors.Clear(e.ts)
			e.pushObject(retVal)
			return genResult{next: e.jumpBy(int(oparg) + 1), ok: true}, nil
		}
		if nerr != nil {
			setRunningYieldFrom(e.genRunning, nil)
			return genResult{ok: true}, nerr
		}
		e.pushObject(val)
		return genResult{next: e.cacheAdvance(compile.SEND), ok: true}, nil
	}
}

// execGetYieldFromIter ports GET_YIELD_FROM_ITER: if TOS is already a
// generator or iterator, leave it. Otherwise call iter(TOS). A coroutine
// at TOS is rejected outright when the enclosing function is not itself
// a coroutine / @types.coroutine generator, matching CPython's guard
// against `yield from coro` inside a plain generator body.
//
// CPython: Python/bytecodes.c:3091 GET_YIELD_FROM_ITER
func (e *evalState) execGetYieldFromIter() (genResult, error) {
	iterable := e.popObject()
	if _, isCoro := iterable.(*objects.Coroutine); isCoro {
		flags := uint32(e.f.Code.Flags)
		if flags&(compile.CoCoroutine|compile.CoIterableCoroutine) == 0 {
			return genResult{ok: true}, fmt.Errorf(
				"TypeError: cannot 'yield from' a coroutine object in a non-coroutine generator")
		}
		e.pushObject(iterable)
		return genResult{next: e.advance(), ok: true}, nil
	}
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
		// The popped stack reference is owned here; release it before
		// surfacing the error so a failed async-for setup does not leak
		// the iterable. CPython's GET_AITER runs DECREF_INPUTS on every
		// exit path.
		//
		// CPython: Python/bytecodes.c:1230 GET_AITER (DECREF_INPUTS)
		objects.Decref(obj)
		return genResult{ok: true}, fmt.Errorf(
			"TypeError: 'async for' requires an object with __aiter__ method, got %s", t.Name)
	}
	iter, err := t.Async.Aiter(obj)
	// GET_AITER consumes the iterable regardless of the call's outcome.
	//
	// CPython: Python/bytecodes.c:1230 GET_AITER (DECREF_INPUTS)
	objects.Decref(obj)
	if err != nil {
		return genResult{ok: true}, err
	}
	it := iter.Type()
	if it.Async == nil || it.Async.Anext == nil {
		objects.Decref(iter)
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
	// The slot beneath the exception is the async iterator GET_AITER left
	// on the stack (the loop's exception table records handler depth 1, so
	// only the iterator sits below the pushed exception). popObject steals
	// the reference, so the caller now owns it and must release it; the
	// earlier `_ =` discard leaked the iterator on every async-for exit
	// (test_coroutines test_for_4 / test_for_6 getrefcount mismatch).
	//
	// CPython: Python/bytecodes.c:1442 END_ASYNC_FOR (DECREF_INPUTS)
	iter := e.popObject()

	if isStopAsyncIteration(excVal) {
		// StopAsyncIteration ends the loop cleanly: DECREF_INPUTS closes
		// both the awaitable/iterator slot and the exception.
		objects.Decref(iter)
		objects.Decref(excVal)
		return genResult{next: e.advance(), ok: true}, nil
	}
	objects.Decref(iter)
	return genResult{ok: true}, e.raiseExcFromObject(excVal)
}

// setRunningYieldFrom mirrors CPython's _PyGen_yf reflection by
// stashing the awaitable currently being delegated to on whichever
// gen-like object owns the running frame. Pass nil to clear. CPython
// derives yieldfrom from the suspended frame's TOS; gopy stores it
// explicitly on the Generator / Coroutine / AsyncGenerator so that
// gi_yieldfrom, cr_await, ag_await all read it directly.
//
// CPython: Objects/genobject.c:1129 coro_get_cr_await (calls _PyGen_yf)
// CPython: Objects/genobject.c:1608 async_gen tp_getset (ag_await)
// CPython: Objects/genobject.c:1043 _PyGen_yf
func setRunningYieldFrom(running objects.Object, target objects.Object) {
	switch r := running.(type) {
	case *objects.Generator:
		r.YieldFromTarget = target
	case *objects.Coroutine:
		r.YieldFromTarget = target
	case *objects.AsyncGenerator:
		r.YieldFromTarget = target
	}
}

// coroGetAwaitableIter ports _PyCoro_GetAwaitableIter: a coroutine (or a
// generator flagged CO_ITERABLE_COROUTINE via @types.coroutine) is its
// own awaitable iterator and is returned with a single new reference;
// anything else routes through tp_as_async's am_await slot. The slot's
// result must be an iterator and must not be another coroutine (PEP 492
// forbids __await__ from returning a coroutine).
//
// CPython: Objects/genobject.c:1067 _PyCoro_GetAwaitableIter
func coroGetAwaitableIter(o objects.Object) (objects.Object, error) {
	if _, ok := o.(*objects.Coroutine); ok {
		objects.Incref(o)
		return o, nil
	}
	if g, ok := o.(*objects.Generator); ok {
		if g.Code != nil {
			if cd, ok2 := g.Code.(*objects.Code); ok2 && uint32(cd.Flags)&compile.CoIterableCoroutine != 0 {
				objects.Incref(o)
				return o, nil
			}
		}
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
	return nil, fmt.Errorf("TypeError: '%s' object can't be awaited", t.Name)
}

// getAwaitableIter is the GET_AWAITABLE / yield-from caller of
// coroGetAwaitableIter. CPython does Py_INCREF once; the GET_AWAITABLE
// framework then does a single STACK_SHRINK (no slot decref because the
// body already did PyStackRef_CLOSE on the input). gopy's autogenerated
// GET_AWAITABLE arm drops the slot twice (explicit iterable.Close() in
// body, plus DropStack inside e.drop(1)), so on the self-return paths
// (coroutine, iterable-coro generator) we bump a second time to leave
// the surviving slot with the same refcount the receiver had on entry.
// Until the bytecodes_gen template learns to fold (X -- Y) shapes with
// explicit CLOSE into a single setPeek, the double-bump stays.
//
// CPython: Python/bytecodes.c:1274 GET_AWAITABLE (CLOSE + steal)
func getAwaitableIter(o objects.Object) (objects.Object, error) {
	out, err := coroGetAwaitableIter(o)
	if err != nil {
		return nil, err
	}
	if out == o {
		objects.Incref(o)
	}
	return out, nil
}

// formatAwaitableError ports _PyEval_FormatAwaitableError: when
// GET_AWAITABLE's oparg flags an `async with` context, the bare
// "object X can't be used in 'await' expression" message gets replaced
// with one that names the protocol method whose return value failed
// the awaitable check.
//
// CPython: Python/ceval.c:3499 _PyEval_FormatAwaitableError
func formatAwaitableError(typeName string, oparg uint32) error {
	switch oparg {
	case 1:
		return fmt.Errorf(
			"TypeError: 'async with' received an object from __aenter__ "+
				"that does not implement __await__: %s", typeName)
	case 2:
		return fmt.Errorf(
			"TypeError: 'async with' received an object from __aexit__ "+
				"that does not implement __await__: %s", typeName)
	}
	return nil
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
	return genResult{ok: true}, e.raiseExcFromObject(excVal)
}

// raiseExcFromObject installs a typed Python exception sitting on the
// VM stack onto the thread state and returns the matching excSentinel,
// mirroring how RERAISE re-raises a captured exception while preserving
// its PyObject identity. END_ASYNC_FOR / CLEANUP_THROW must use this
// helper instead of stringifying the exception, otherwise the type tag
// is lost and the receiver only sees a bare Exception("TypeError(...)").
//
// CPython: Python/bytecodes.c RERAISE (PyErr_SetRaisedException)
func (e *evalState) raiseExcFromObject(excVal objects.Object) error {
	if pyExc, ok := excVal.(*pyerrors.Exception); ok {
		pyerrors.Raise(e.ts, pyExc)
		return excSentinel(pyExc)
	}
	return excAsError(excVal)
}

// stopIterRetval inspects err for a StopIteration crossing. Returns
// (value, true) when err is either the bare ErrStopIteration sentinel
// or a RaisedError wrapping a StopIteration exception. value is the
// args[0] payload when present, else None. Used by _SEND to drop the
// await result into the END_SEND slot.
//
// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
func (e *evalState) stopIterRetval(err error) (objects.Object, bool) {
	v, ok := stopIterRetval(err)
	if ok {
		return v, true
	}
	if err == nil {
		return nil, false
	}
	// A Python-level `raise StopIteration` inside a sub-iterator's send()
	// (e.g. the duck-typed coroutine in types._GeneratorWrapper) surfaces
	// here as an excSentinelError carrying the live exception, not as a
	// RaisedError or the bare ErrStopIteration sentinel. Unwrap it so the
	// SEND opcode can pull the value out and hand it to END_SEND.
	//
	// CPython: Objects/genobject.c:1024 _PyGen_FetchStopIterationValue
	var se *excSentinelError
	if errors.As(err, &se) && se.exc != nil && isStopIterationException(se.exc) {
		val := stopIterationExcValue(se.exc)
		pyerrors.Clear(e.ts)
		return val, true
	}
	if live := pyerrors.Occurred(e.ts); live != nil {
		if isStopIterationException(live) {
			val := stopIterationExcValue(live)
			pyerrors.Clear(e.ts)
			return val, true
		}
	}
	return nil, false
}

// isStopIterationException reports whether exc's type is StopIteration
// (or a subclass), matching PyErr_ExceptionMatches(PyExc_StopIteration).
func isStopIterationException(exc *pyerrors.Exception) bool {
	if exc == nil || exc.ExcType == nil {
		return false
	}
	for cur := exc.ExcType; cur != nil; cur = primaryBase(cur) {
		if cur.Name == "StopIteration" {
			return true
		}
	}
	return false
}

// primaryBase returns the first base of t, mirroring CPython's MRO walk
// through tp_base.
func primaryBase(t *objects.Type) *objects.Type {
	if t == nil || len(t.Bases) == 0 {
		return nil
	}
	return t.Bases[0]
}

// stopIterationExcValue returns the value carried by a StopIteration:
// the dedicated StopValue slot when populated, else args[0], else None.
//
// CPython: Objects/exceptions.c:746 PyStopIterationObject.value
func stopIterationExcValue(exc *pyerrors.Exception) objects.Object {
	if exc.StopValue != nil {
		return exc.StopValue
	}
	if exc.Args != nil && exc.Args.Len() > 0 {
		return exc.Args.Item(0)
	}
	return objects.None()
}

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

// computeCrOrigin builds the cr_origin tuple captured at coroutine
// creation: walks up to depth frames from start (the coroutine's caller)
// and yields a tuple of (filename, lineno, name) per frame, in
// most-recent-first order. Returns an empty tuple when start is nil.
//
// CPython: Objects/genobject.c:1369 compute_cr_origin
func computeCrOrigin(start *frame.Frame, depth int) objects.Object {
	if start == nil || depth <= 0 {
		return objects.NewTuple(nil)
	}
	rows := make([]objects.Object, 0, depth)
	for f := start; f != nil && len(rows) < depth; f = f.Previous {
		code := f.FrameCode()
		if code == nil {
			break
		}
		line := 0
		if pos, ok := objects.CoAddr2Location(code, f.FrameLasti()); ok {
			line = pos.Line
		}
		rows = append(rows, objects.NewTuple([]objects.Object{
			objects.NewStr(code.Filename),
			objects.NewInt(int64(line)),
			objects.NewStr(code.Name),
		}))
	}
	return objects.NewTuple(rows)
}
