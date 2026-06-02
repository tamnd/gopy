// Coroutine object. Ports PyCoroObject from Objects/genobject.c.
// A coroutine is a suspended frame that produces values via await,
// not via direct iteration. The send/throw/close protocol is the
// same as Generator; the difference is that __iter__ is not legal,
// and __await__ returns a low-level iterator that drives send().
//
// CPython: Include/cpython/genobject.h PyCoroObject
// CPython: Objects/genobject.c coroutine section

package objects

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// Coroutine mirrors PyCoroObject. Internally it shares the
// Generator's channel-based suspend/resume model.
//
// CPython: Objects/genobject.c:L1271 PyCoro_Type
type Coroutine struct {
	Header
	Name     string
	Qualname string

	YieldCh chan GenMsg
	SendCh  chan GenMsg

	started bool
	closed  bool

	// Running is 1 while the coroutine body is actively executing.
	// Mirrors CPython's cr_frame_state == FRAME_EXECUTING check.
	//
	// CPython: Objects/genobject.c:1149 cr_getrunning
	Running atomic.Int32

	// cr_exc_state emulation. CPython gives coroutines the same
	// _PyErr_StackItem slot generators get (cr_exc_state), and the shared
	// gen_send_ex2 saves/restores it across every yield so a coroutine
	// that catches an exception and then suspends does not leak its
	// handled exception into the caller's exc_info. See Generator for the
	// field roles.
	//
	// CPython: Include/cpython/genobject.h cr_exc_state (_PyErr_StackItem)
	// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push/pop)
	ExcHandled Object
	CallerExc  Object
	ExcDepth   int

	// Code is the code object for the coroutine function.
	//
	// CPython: Include/cpython/genobject.h cr_code via _gen_getcode
	Code Object

	// GiFrame is the Python-visible frame for the suspended coroutine.
	// Set by the vm package and cleared on close.
	//
	// CPython: Include/cpython/genobject.h cr_iframe
	GiFrame Object

	// YieldFromTarget is the awaitable currently delegated by `await`.
	// CPython surfaces it as cr_await via coro_get_cr_await -> _PyGen_yf.
	//
	// CPython: Objects/genobject.c:1129 coro_get_cr_await
	YieldFromTarget Object

	// CrOrigin is the optional traceback-style tuple captured at
	// coroutine creation when sys.set_coroutine_origin_tracking_depth
	// is non-zero. Read-only attribute.
	//
	// CPython: Objects/genobject.c:1184 cr_origin (PyMemberDef)
	CrOrigin Object
}

// GCRoot pins a running coroutine as a cycle-collector root: its body
// executes on its own goroutine, whose stack holds the live reference
// the refcount collector cannot see.
//
// CPython: Python/gc.c:1208 gc_collect_main (executing frame stays rooted)
func (c *Coroutine) GCRoot() bool { return c.Running.Load() == 1 }

func (c *Coroutine) GetExcHandled() Object  { return c.ExcHandled }
func (c *Coroutine) SetExcHandled(o Object) { c.ExcHandled = o }
func (c *Coroutine) GetCallerExc() Object   { return c.CallerExc }
func (c *Coroutine) SetCallerExc(o Object)  { c.CallerExc = o }
func (c *Coroutine) ExcDepthVal() int       { return c.ExcDepth }
func (c *Coroutine) IncExcDepth()           { c.ExcDepth++ }
func (c *Coroutine) DecExcDepth() {
	if c.ExcDepth > 0 {
		c.ExcDepth--
	}
}

// CoroutineType is the type singleton for coroutine.
//
// CPython: Objects/genobject.c:L1271 PyCoro_Type
var CoroutineType *Type

// CoroAwaitType is the type for the iterator that __await__ returns.
//
// CPython: Objects/genobject.c:L1500 _PyCoroWrapper_Type
var CoroAwaitType *Type

//nolint:gocyclo // coroutine type-registration table: flat sequence of attribute/descriptor installs
func init() {
	CoroutineType = NewType("coroutine", []*Type{objectType})
	CoroutineType.Repr = coroRepr
	CoroutineType.Str = coroRepr
	CoroutineType.Getattro = GenericGetAttr
	CoroutineType.Setattro = GenericSetAttr
	// Coroutines are hashable by identity: CPython leaves tp_hash unset
	// so it inherits object's _Py_HashPointer through inherit_slots.
	// asyncio.gather keys a dict on the raw coroutine, so the slot must
	// be present.
	//
	// CPython: Objects/genobject.c PyCoro_Type (tp_hash inherited from object)
	CoroutineType.Hash = IdentityHash
	// am_await: a coroutine yields the coroutine_wrapper that drives it,
	// exactly as coro_await does. _PyCoro_GetAwaitableIter returns the
	// coroutine itself (it lacks tp_iternext), so callers that need an
	// iterator (anextawaitable_getiter) unwrap this slot to reach the
	// wrapper. Without the slot that unwrap reports "__await__ returned a
	// non-iterable".
	//
	// CPython: Objects/genobject.c:1486 coro_await (coro_as_async.am_await)
	CoroutineType.Async = &AsyncMethods{Await: coroAmAwait}
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":          coroSendMethod,
		"throw":         coroThrowMethod,
		"close":         coroCloseMethod,
		"__await__":     coroAwaitMethod,
		"__reduce__":    genReduceReject,
		"__reduce_ex__": genReduceReject,
	} {
		SetTypeDescr(CoroutineType, name, NewMethodDescr(CoroutineType, name, fn))
	}
	// Docstrings for the method descriptors. CPython's PyMethodDef rows
	// carry the strings inline; gopy attaches them via WithDoc after the
	// loop so introspection (inspect, help, types.CoroutineType.send.__doc__)
	// returns the same text.
	//
	// CPython: Objects/genobject.c:1188 coro_send_doc
	// CPython: Objects/genobject.c:1192 coro_throw_doc
	// CPython: Objects/genobject.c:1202 coro_close_doc
	if d, ok := typeDescrTable[CoroutineType]["send"].(*MethodDescr); ok {
		d.WithDoc("send(arg) -> send 'arg' into coroutine,\nreturn next iterated value or raise StopIteration.")
	}
	if d, ok := typeDescrTable[CoroutineType]["throw"].(*MethodDescr); ok {
		d.WithDoc("throw(value)\nthrow(type[,value[,traceback]])\n\nRaise exception in coroutine, return next iterated value or raise\nStopIteration.\nthe (type, val, tb) signature is deprecated, \nand may be removed in a future version of Python.")
	}
	if d, ok := typeDescrTable[CoroutineType]["close"].(*MethodDescr); ok {
		d.WithDoc("close() -> raise GeneratorExit inside coroutine.")
	}

	// cr_frame: the frame object of the suspended coroutine.
	//
	// CPython: Objects/genobject.c:1158 cr_getframe (-> _gen_getframe)
	SetTypeDescr(CoroutineType, "cr_frame", NewGetSetDescr("cr_frame",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if !c.closed && c.GiFrame != nil {
				return c.GiFrame, nil
			}
			return None(), nil
		}, nil))

	// cr_code: the code object for the coroutine function.
	//
	// CPython: Objects/genobject.c:1164 cr_getcode (-> _gen_getcode)
	SetTypeDescr(CoroutineType, "cr_code", NewGetSetDescr("cr_code",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.Code != nil {
				return c.Code, nil
			}
			return None(), nil
		}, nil))

	// cr_running: True when the coroutine body is currently executing.
	//
	// CPython: Objects/genobject.c:1149 cr_getrunning
	SetTypeDescr(CoroutineType, "cr_running", NewGetSetDescr("cr_running",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.Running.Load() == 1 {
				return True(), nil
			}
			return False(), nil
		}, nil))

	// cr_suspended: True when the coroutine is suspended at an await.
	//
	// CPython: Objects/genobject.c:1138 cr_getsuspended
	SetTypeDescr(CoroutineType, "cr_suspended", NewGetSetDescr("cr_suspended",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.started && !c.closed && c.Running.Load() == 0 {
				return True(), nil
			}
			return False(), nil
		}, nil))

	// cr_await: the awaitable currently driven by `await`, or None.
	// CPython's coro_get_cr_await returns a new reference (Py_NewRef of
	// the yf result); we mirror that with Incref so LOAD_ATTR receives
	// an owned reference and the subsequent stackref Close does not
	// underflow the target's refcount.
	//
	// CPython: Objects/genobject.c:1129 coro_get_cr_await (-> _PyGen_yf)
	SetTypeDescr(CoroutineType, "cr_await", NewGetSetDescr("cr_await",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.YieldFromTarget != nil && c.started && !c.closed && c.Running.Load() == 0 {
				Incref(c.YieldFromTarget)
				return c.YieldFromTarget, nil
			}
			return None(), nil
		}, nil))

	// cr_origin: read-only origin traceback tuple, populated only when
	// sys.set_coroutine_origin_tracking_depth was called with a non-zero
	// depth before the coroutine was created.
	//
	// CPython: Objects/genobject.c:1184 cr_origin (PyMemberDef)
	SetTypeDescr(CoroutineType, "cr_origin", NewGetSetDescr("cr_origin",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.CrOrigin != nil {
				return c.CrOrigin, nil
			}
			return None(), nil
		}, nil))

	// __name__: writable string name of the coroutine.
	//
	// CPython: Objects/genobject.c:706 gen_get_name / gen_set_name
	SetTypeDescr(CoroutineType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) {
			return NewStr(o.(*Coroutine).Name), nil
		},
		func(o Object, v Object) error {
			if v == nil {
				return fmt.Errorf("TypeError: __name__ attribute cannot be deleted")
			}
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __name__ must be a string, not %s", v.Type().Name)
			}
			o.(*Coroutine).Name = s.Value()
			return nil
		}).WithDoc("name of the coroutine"))

	// __qualname__: writable qualified name of the coroutine.
	//
	// CPython: Objects/genobject.c:728 gen_get_qualname / gen_set_qualname
	SetTypeDescr(CoroutineType, "__qualname__", NewGetSetDescr("__qualname__",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.Qualname != "" {
				return NewStr(c.Qualname), nil
			}
			return NewStr(c.Name), nil
		},
		func(o Object, v Object) error {
			if v == nil {
				return fmt.Errorf("TypeError: __qualname__ attribute cannot be deleted")
			}
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __qualname__ must be a string, not %s", v.Type().Name)
			}
			o.(*Coroutine).Qualname = s.Value()
			return nil
		}).WithDoc("qualified name of the coroutine"))

	// tp_traverse: lets the cycle collector walk references the
	// coroutine holds (its frame, code object). Mirrors gen_traverse
	// reused on the coroutine type via the shared tp_traverse slot.
	//
	// CPython: Objects/genobject.c:1244 (PyCoro_Type tp_traverse = gen_traverse)
	CoroutineType.TpTraverse = coroTraverse

	// tp_finalize: when an un-awaited coroutine becomes unreachable, the
	// cycle collector invokes this slot. A coroutine that has never been
	// resumed (started == false) emits a RuntimeWarning naming the
	// qualname; one suspended mid-await is closed so its finally clauses
	// run. Mirrors _PyGen_Finalize's CO_COROUTINE branches.
	//
	// CPython: Objects/genobject.c:87 _PyGen_Finalize (PyCoro_CheckExact)
	CoroutineType.Finalize = coroFinalize

	CoroAwaitType = NewType("coroutine_wrapper", []*Type{objectType})
	CoroAwaitType.Iter = SelfIter
	CoroAwaitType.IterNext = coroAwaitNext
	CoroAwaitType.TpTraverse = coroAwaiterTraverse
	AddIterSlotWrappers(CoroAwaitType)

	// CPython's coroutine_wrapper exposes send/throw/close so callers
	// driving a coroutine via its __await__ iterator can resume it the
	// same way they would a generator.
	//
	// CPython: Objects/genobject.c:1466 coro_wrapper_methods
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":          coroWrapperSendMethod,
		"throw":         coroWrapperThrowMethod,
		"close":         coroWrapperCloseMethod,
		"__reduce__":    genReduceReject,
		"__reduce_ex__": genReduceReject,
	} {
		SetTypeDescr(CoroAwaitType, name, NewMethodDescr(CoroAwaitType, name, fn))
	}
}

// coroWrapperSendMethod implements coroutine_wrapper.send(value): forwards
// to the wrapped coroutine's Send.
//
// CPython: Objects/genobject.c:1457 coro_wrapper_send
func coroWrapperSendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument")
	}
	w, ok := args[0].(*coroAwaiter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'coroutine_wrapper' object")
	}
	return w.coro.Send(args[1])
}

// coroWrapperThrowMethod implements coroutine_wrapper.throw.
//
// CPython: Objects/genobject.c:1461 coro_wrapper_throw
func coroWrapperThrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	w, ok := args[0].(*coroAwaiter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires a 'coroutine_wrapper' object")
	}
	if GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: coroutine.throw not available")
	}
	if len(args) > 2 {
		if DeprecWarnHook != nil {
			if werr := DeprecWarnHook("the (type, exc, tb) signature of throw() is deprecated, use the single-arg signature instead."); werr != nil {
				return nil, werr
			}
		}
		if GenThrowTripleHook == nil {
			return nil, fmt.Errorf("RuntimeError: coroutine.throw 3-arg form not available")
		}
		var val, tb Object
		if len(args) > 2 {
			val = args[2]
		}
		if len(args) > 3 {
			tb = args[3]
		}
		exc, err := GenThrowTripleHook(args[1], val, tb)
		if err != nil {
			return nil, err
		}
		return w.coro.Throw(exc)
	}
	exc, err := GenThrowHook(args[1])
	if err != nil {
		return nil, err
	}
	return w.coro.Throw(exc)
}

// coroWrapperCloseMethod implements coroutine_wrapper.close.
//
// CPython: Objects/genobject.c:1464 coro_wrapper_close
func coroWrapperCloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	w, ok := args[0].(*coroAwaiter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'coroutine_wrapper' object")
	}
	if err := w.coro.Close(); err != nil {
		return nil, err
	}
	return None(), nil
}

// coroFinalize is the tp_finalize slot for coroutine objects. Mirrors
// the PyCoro_CheckExact arms of _PyGen_Finalize: a coroutine in the
// FRAME_CREATED state (never sent into) emits a RuntimeWarning naming
// the qualname; any other suspended coroutine is closed so its finally
// blocks run.
//
// CPython: Objects/genobject.c:87 _PyGen_Finalize (CO_COROUTINE branches)
func coroFinalize(o Object) {
	c, ok := o.(*Coroutine)
	if !ok {
		return
	}
	if c.closed {
		return
	}
	// A running coroutine cannot be finalized for the same reason as
	// the Generator equivalent: closeWith would deadlock if the body
	// is currently executing. genFinalize documents the issue in
	// detail; the protection here is identical.
	if c.Running.Load() == 1 {
		return
	}
	var saved any
	if h := SaveCurrentExceptionHook; h != nil {
		saved = h()
	}
	if !c.started {
		if h := WarnUnawaitedCoroutineHook; h != nil {
			h(c)
		}
	} else {
		cRepr, _ := Repr(c)
		if cerr := c.Close(); cerr != nil {
			if h := WriteUnraisableHook; h != nil {
				h(c, "Exception ignored while closing coroutine "+cRepr, cerr)
			}
		}
	}
	if h := RestoreCurrentExceptionHook; h != nil {
		h(saved)
	}
}

// coroTraverse mirrors gen_traverse for PyCoro_Type. cr_origin is
// intentionally skipped (tuples/str/int only, no cycle participation).
//
// CPython: Objects/genobject.c:61 gen_traverse (shared by PyCoro_Type)
func coroTraverse(o Object, visit Visitor) error {
	c, ok := o.(*Coroutine)
	if !ok {
		return nil
	}
	if c.GiFrame != nil {
		if err := visit(c.GiFrame); err != nil {
			return err
		}
	}
	if c.Code != nil {
		if err := visit(c.Code); err != nil {
			return err
		}
	}
	return nil
}

// coroAwaiterTraverse visits the wrapped coroutine. Mirrors
// coro_wrapper_traverse.
//
// CPython: Objects/genobject.c:1480 coro_wrapper_traverse
func coroAwaiterTraverse(o Object, visit Visitor) error {
	w := o.(*coroAwaiter)
	if w.coro == nil {
		return nil
	}
	return visit(w.coro)
}

// NewCoroutine creates a coroutine. Like Generator the caller is
// responsible for spawning the goroutine that drives the body.
//
// CPython: Objects/genobject.c:L1219 coro_new
func NewCoroutine(name string) *Coroutine {
	c := &Coroutine{
		Name:    name,
		YieldCh: make(chan GenMsg, 1),
		SendCh:  make(chan GenMsg, 1),
	}
	c.init(CoroutineType)
	return c
}

// NewCoroutineWithQualname creates a coroutine carrying both __name__
// and __qualname__. Mirrors the gen_new_with_qualname coroutine flag
// path in CPython.
//
// CPython: Objects/genobject.c:867 gen_new_with_qualname
func NewCoroutineWithQualname(name, qualname string) *Coroutine {
	c := NewCoroutine(name)
	c.Qualname = qualname
	return c
}

// IsSuspendedYieldFrom reports whether the coroutine is currently
// suspended yielding from a sub-awaitable, matching CPython's
// _PyGen_yf(coro) != NULL on a PyCoro_CheckExact instance, i.e. frame
// state FRAME_SUSPENDED_YIELD_FROM. The same predicate backs cr_await.
//
// CPython: Objects/genobject.c:374 _PyGen_yf (FRAME_SUSPENDED_YIELD_FROM)
func (c *Coroutine) IsSuspendedYieldFrom() bool {
	return c.YieldFromTarget != nil && c.started && !c.closed && c.Running.Load() == 0
}

// MarkFinished records that the coroutine body has run to completion
// so subsequent Send/Throw/Close are no-ops and inspect.getcoroutinestate
// reports CORO_CLOSED. Mirrors Generator.MarkFinished.
//
// CPython: Objects/genobject.c:225 gen_send_ex2 (gi_frame_state = FRAME_COMPLETED)
func (c *Coroutine) MarkFinished() {
	c.closed = true
}

// Send drives the coroutine forward by one suspension point. The
// rules match Generator.Send: None on first call, otherwise an
// already-started coroutine receives the sent value.
//
// CPython: Objects/genobject.c:L260 gen_send_ex2
func (c *Coroutine) Send(v Object) (Object, error) {
	if c.closed {
		return nil, errReuseAwaited()
	}
	if !c.started && v != None() {
		return nil, errors.New("TypeError: can't send non-None value to a just-started coroutine")
	}
	// CPython: Objects/genobject.c:275 gen_send_ex2 FRAME_EXECUTING guard
	if c.Running.Load() == 1 {
		return nil, fmt.Errorf("ValueError: coroutine already executing")
	}
	c.started = true
	c.SendCh <- GenMsg{Val: v, CallerFrame: callerFrame()}
	msg := <-c.YieldCh
	if msg.Err != nil {
		c.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// errReuseAwaited mirrors CPython's "cannot reuse already awaited
// coroutine" RuntimeError raised by gen_send_ex2 when a coroutine in
// FRAME_COMPLETED state is sent/thrown into. Mirrors the behavior of
// gen_send_ex2 setting PyExc_RuntimeError for the coroutine variant of
// the FRAME_COMPLETED branch (vs StopIteration for plain generators).
//
// CPython: Objects/genobject.c:230 gen_send_ex2 FRAME_COMPLETED
func errReuseAwaited() error {
	return errors.New("RuntimeError: cannot reuse already awaited coroutine")
}

// Throw raises err inside the coroutine at its await suspension.
//
// CPython: Objects/genobject.c:L466 _gen_throw
func (c *Coroutine) Throw(err error) (Object, error) {
	return c.throwWithCaller(err, callerFrame())
}

// ownFrame returns the coroutine body's own interpreter frame so a
// forwarded throw can stamp it as the awaited sub-target's f_back,
// matching the chain the await/send path builds. See Generator.ownFrame.
//
// CPython: Objects/genobject.c:489 _gen_throw (tstate->current_frame = frame)
func (c *Coroutine) ownFrame() InterpreterFrame {
	if f, ok := c.GiFrame.(*Frame); ok && f != nil {
		return f.Interp()
	}
	return nil
}

// throwWithCaller is the body of Throw with the resume frame threaded
// explicitly so a forwarded throw keeps cr_frame.f_back identical to the
// await/send path.
//
// CPython: Objects/genobject.c:466 _gen_throw
func (c *Coroutine) throwWithCaller(err error, caller InterpreterFrame) (Object, error) {
	if err == nil {
		return nil, errors.New("TypeError: throw() requires an exception")
	}
	if c.closed {
		return nil, errReuseAwaited()
	}
	if !c.started {
		c.closed = true
		return nil, err
	}
	// Forward to the await sub-target if present so the exception
	// propagates through the await chain. This mirrors _gen_throw's
	// PyGen_yf branch which recursively throws into the awaited
	// generator/coroutine before resuming the outer body.
	//
	// CPython: Objects/genobject.c:469 _gen_throw (_PyGen_yf branch)
	if yf := c.YieldFromTarget; yf != nil {
		forwarded := true
		var fval Object
		var ferr error
		// Link this coroutine's frame into the running call chain for the
		// duration of the forwarded throw, then unlink it. This mirrors
		// _gen_throw bracketing frame->previous = prev / current_frame =
		// frame around the recursion so the resumed leaf sees the full
		// await chain in its f_back, then restores the suspended state.
		//
		// CPython: Objects/genobject.c:493 _gen_throw (frame linking)
		my := c.ownFrame()
		if my != nil {
			my.FrameSetBack(caller)
		}
		switch v := yf.(type) {
		case *Generator:
			fval, ferr = v.throwWithCaller(err, my)
		case *Coroutine:
			fval, ferr = v.throwWithCaller(err, my)
		default:
			if GenThrowForwardHook != nil {
				fval, ferr = GenThrowForwardHook(nil, yf, err)
			} else {
				forwarded = false
			}
		}
		if my != nil {
			my.FrameSetBack(nil)
		}
		if forwarded {
			return c.forwardThrowResult(fval, ferr, caller)
		}
	}
	c.SendCh <- GenMsg{Err: err, CallerFrame: caller}
	msg := <-c.YieldCh
	if msg.Err != nil {
		c.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// forwardThrowResult handles the result of forwarding a throw to an
// await sub-target. Mirrors the retval / StopIteration / other-exception
// branches in _gen_throw after the yf.throw() call.
//
// CPython: Objects/genobject.c:511 _gen_throw (retval / StopIteration branches)
func (c *Coroutine) forwardThrowResult(fval Object, ferr error, caller InterpreterFrame) (Object, error) {
	if ferr == nil {
		return fval, nil
	}
	if stopVal, isStop := genStopIterVal(ferr); isStop {
		c.YieldFromTarget = nil
		c.SendCh <- GenMsg{Val: stopVal, CallerFrame: caller}
		msg := <-c.YieldCh
		if msg.Err != nil {
			c.closed = true
			return nil, msg.Err
		}
		return msg.Val, nil
	}
	c.SendCh <- GenMsg{Err: ferr, CallerFrame: caller}
	msg := <-c.YieldCh
	if msg.Err != nil {
		c.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Close throws GeneratorExit. A coroutine that yields after seeing
// GeneratorExit raises RuntimeError, mirroring "coroutine ignored
// GeneratorExit". When the coroutine is parked at an `await`, the
// sub-iterator is closed first via gen_close_iter so its finally
// clauses fire before the body sees the exit.
//
// CPython: Objects/genobject.c:L388 gen_close (coroutine variant)
func (c *Coroutine) Close() error {
	if c.closed {
		return nil
	}
	if !c.started {
		c.closed = true
		return nil
	}
	if yf := c.YieldFromTarget; yf != nil {
		_ = GenCloseIter(yf)
		c.YieldFromTarget = nil
	}
	c.SendCh <- GenMsg{Err: ErrGeneratorExit, CallerFrame: callerFrame()}
	msg := <-c.YieldCh
	c.closed = true
	if msg.Err == nil {
		return errors.New("RuntimeError: coroutine ignored GeneratorExit")
	}
	if errors.Is(msg.Err, ErrGeneratorExit) ||
		errors.Is(msg.Err, ErrStopIteration) {
		clearAfterCloseSwallow()
		return nil
	}
	var re *RaisedError
	if errors.As(msg.Err, &re) && re.Exc != nil {
		switch re.Exc.Type().Name {
		case "GeneratorExit", "StopIteration":
			clearAfterCloseSwallow()
			return nil
		}
	}
	return msg.Err
}

// clearAfterCloseSwallow mirrors the PyErr_Clear in gen_close that drops
// the swallowed GeneratorExit / StopIteration off the thread state. Without
// it the stale exception lingers and wrapCallError re-surfaces it as the
// type of the next send()/throw() that returns a plain Go error.
//
// CPython: Objects/genobject.c:443 gen_close (PyErr_Clear), Python/errors.c:488 _PyErr_Clear
func clearAfterCloseSwallow() {
	if ClearCurrentExceptionHook != nil {
		ClearCurrentExceptionHook()
	}
}

func coroSendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument")
	}
	c, ok := args[0].(*Coroutine)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'coroutine' object")
	}
	return c.Send(args[1])
}

func coroCloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	c, ok := args[0].(*Coroutine)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'coroutine' object")
	}
	if err := c.Close(); err != nil {
		return nil, err
	}
	return None(), nil
}

// coroThrowMethod dispatches coroutine.throw(exc) / throw(typ, val, tb).
//
// CPython: Objects/genobject.c:599 gen_throw (shared with coroutines)
func coroThrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	c, ok := args[0].(*Coroutine)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires a 'coroutine' object")
	}
	if GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: coroutine.throw not available")
	}
	if len(args) > 2 {
		if DeprecWarnHook != nil {
			if werr := DeprecWarnHook("the (type, exc, tb) signature of throw() is deprecated, use the single-arg signature instead."); werr != nil {
				return nil, werr
			}
		}
		if GenThrowTripleHook == nil {
			return nil, fmt.Errorf("RuntimeError: coroutine.throw 3-arg form not available")
		}
		var val, tb Object
		if len(args) > 2 {
			val = args[2]
		}
		if len(args) > 3 {
			tb = args[3]
		}
		exc, err := GenThrowTripleHook(args[1], val, tb)
		if err != nil {
			return nil, err
		}
		return c.Throw(exc)
	}
	exc, err := GenThrowHook(args[1])
	if err != nil {
		return nil, err
	}
	return c.Throw(exc)
}

// coroAwaitMethod dispatches coroutine.__await__(). CPython exposes the
// am_await slot as a method-wrapper via slotdefs; gopy registers an
// explicit method so attribute access surfaces the awaiter.
//
// CPython: Objects/genobject.c:1486 coro_await
func coroAwaitMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __await__() missing self argument")
	}
	c, ok := args[0].(*Coroutine)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__await__' requires a 'coroutine' object")
	}
	return c.Await(), nil
}

// coroAmAwait is the am_await slot for coroutines. CPython's
// coro_as_async.am_await is coro_await, which returns the
// coroutine_wrapper. _PyCoro_GetAwaitableIter returns the coroutine
// itself, so anextawaitable_getiter unwraps this slot to reach an
// iterator.
//
// CPython: Objects/genobject.c:1486 coro_await
func coroAmAwait(o Object) (Object, error) {
	c, ok := o.(*Coroutine)
	if !ok {
		return nil, fmt.Errorf("TypeError: __await__() requires a 'coroutine' object")
	}
	return c.Await(), nil
}

// Await returns the iterator that drives this coroutine. CPython
// returns a thin wrapper whose __next__ forwards to send(None).
//
// CPython: Objects/genobject.c:L1486 coro_await
func (c *Coroutine) Await() Object {
	w := &coroAwaiter{coro: c}
	w.init(CoroAwaitType)
	return w
}

type coroAwaiter struct {
	Header
	coro *Coroutine
}

func coroAwaitNext(o Object) (Object, error) {
	return o.(*coroAwaiter).coro.Send(None())
}

func coroRepr(o Object) (string, error) {
	c := o.(*Coroutine)
	return fmt.Sprintf("<coroutine object %s at %p>", c.Name, c), nil
}
