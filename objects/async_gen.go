// Async generator object. Ports PyAsyncGenObject from
// Objects/genobject.c. An async generator yields values via `yield`
// inside an `async def` body; consumers drive it through __anext__,
// which returns an awaitable, and that awaitable's iterator drives
// the underlying suspend/resume machinery.
//
// CPython splits the surface into three types: PyAsyncGen_Type,
// _PyAsyncGenASend_Type, and _PyAsyncGenAThrow_Type. The two
// underscored types are awaitables returned by asend() and athrow()
// respectively; aclose() also returns an athrow-style awaitable that
// raises GeneratorExit.
//
// CPython: Include/cpython/genobject.h PyAsyncGenObject
// CPython: Objects/genobject.c async generator section

package objects

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// AsyncGenerator mirrors PyAsyncGenObject.
//
// CPython: Objects/genobject.c:L1577 PyAsyncGen_Type
type AsyncGenerator struct {
	Header
	Name     string
	Qualname string

	YieldCh chan GenMsg
	SendCh  chan GenMsg

	started bool
	closed  bool

	// Running is 1 while the async generator body is actively
	// executing. CPython exposes ag_running_async as a Py_T_BOOL
	// PyMemberDef pointing at this flag.
	//
	// CPython: Objects/genobject.c:1617 ag_running PyMemberDef
	Running atomic.Int32

	// RunningAsync mirrors CPython's ag_running_async flag. It is
	// set when an asend / athrow / aclose awaitable enters its ITER
	// state and cleared when that awaitable resolves (StopAsyncIteration,
	// StopIteration, GeneratorExit, or any error). The flag distinguishes
	// "a Python-level await is in flight" from "the body byte-code is
	// running", which is what Running tracks.
	//
	// CPython: Objects/genobject.c:1617 ag_running PyMemberDef (ag_running_async)
	RunningAsync atomic.Int32

	// Code is the code object for the async generator function.
	//
	// CPython: Include/cpython/genobject.h ag_code via _gen_getcode
	Code Object

	// GiFrame is the Python-visible frame for the suspended async generator.
	// Set by the vm package and cleared on close.
	//
	// CPython: Include/cpython/genobject.h ag_iframe
	GiFrame Object

	// YieldFromTarget is the awaitable the async generator is currently
	// delegating to. Surfaced as ag_await.
	//
	// CPython: Objects/genobject.c:1608 ag_await -> coro_get_cr_await
	YieldFromTarget Object
}

// AsyncGeneratorType is the type singleton for async_generator.
var AsyncGeneratorType *Type

// AsyncGenASendType is the type for the awaitable returned by asend()
// and __anext__.
//
// CPython: Objects/genobject.c:L1879 _PyAsyncGenASend_Type
var AsyncGenASendType *Type

// AsyncGenAThrowType is the type for the awaitable returned by
// athrow() and aclose().
//
// CPython: Objects/genobject.c:L2272 _PyAsyncGenAThrow_Type
var AsyncGenAThrowType *Type

// AsyncGenWrappedValueType is the type for _PyAsyncGenWrappedValue.
// Wraps a value yielded inside an async generator body so the asend /
// __anext__ awaitable can recognize it and convert to StopIteration.
//
// CPython: Objects/genobject.c:2005 _PyAsyncGenWrappedValue_Type
var AsyncGenWrappedValueType *Type

// AsyncGenStopIterationHook builds a StopIteration error carrying the
// async-gen wrapped value. async_gen_unwrap_value at
// Objects/genobject.c:1745 calls _PyGen_SetStopIterationValue with the
// inner value; gopy reaches the same surface through this hook so
// objects/ can stay free of the errors package.
//
// CPython: Objects/genobject.c:1745 async_gen_unwrap_value
// CPython: Objects/genobject.c:652 _PyGen_SetStopIterationValue
var AsyncGenStopIterationHook func(value Object) error

// AsyncGenWrappedValue mirrors _PyAsyncGenWrappedValue. The compiler
// emits CALL_INTRINSIC_1 INTRINSIC_ASYNC_GEN_WRAP before YIELD_VALUE
// in async-generator bodies so the yielded object surfaces here, not
// as the awaitable's normal next() value.
//
// CPython: Objects/genobject.c:1463 _PyAsyncGenWrappedValue
type AsyncGenWrappedValue struct {
	Header
	Value Object
}

// NewAsyncGenWrappedValue boxes v for the async-generator yield path.
//
// CPython: Objects/genobject.c:2049 _PyAsyncGenValueWrapperNew
func NewAsyncGenWrappedValue(v Object) *AsyncGenWrappedValue {
	w := &AsyncGenWrappedValue{Value: v}
	w.init(AsyncGenWrappedValueType)
	return w
}

// asyncGenWrappedValueTraverse visits the wrapped value so the cycle
// collector can reach through the wrapper. Mirrors
// async_gen_wrapped_val_traverse.
//
// CPython: Objects/genobject.c:1997 async_gen_wrapped_val_traverse
func asyncGenWrappedValueTraverse(o Object, visit Visitor) error {
	w, ok := o.(*AsyncGenWrappedValue)
	if !ok || w.Value == nil {
		return nil
	}
	return visit(w.Value)
}

func init() {
	AsyncGeneratorType = NewType("async_generator", []*Type{objectType})
	AsyncGeneratorType.Repr = asyncGenRepr
	AsyncGeneratorType.Str = asyncGenRepr
	AsyncGeneratorType.Async = &AsyncMethods{
		Aiter: func(o Object) (Object, error) { return o, nil },
		Anext: func(o Object) (Object, error) { return o.(*AsyncGenerator).Anext(), nil },
	}
	AsyncGeneratorType.Getattro = GenericGetAttr
	AsyncGeneratorType.Setattro = GenericSetAttr

	// ag_frame: the frame object of the suspended async generator.
	//
	// CPython: Objects/genobject.c:1582 ag_getframe
	SetTypeDescr(AsyncGeneratorType, "ag_frame", NewGetSetDescr("ag_frame",
		func(o Object) (Object, error) {
			g := o.(*AsyncGenerator)
			if !g.closed && g.GiFrame != nil {
				return g.GiFrame, nil
			}
			return None(), nil
		}, nil))

	// ag_code: code object for the async generator function.
	//
	// CPython: Objects/genobject.c:1588 ag_getcode
	SetTypeDescr(AsyncGeneratorType, "ag_code", NewGetSetDescr("ag_code",
		func(o Object) (Object, error) {
			g := o.(*AsyncGenerator)
			if g.Code != nil {
				return g.Code, nil
			}
			return None(), nil
		}, nil))

	// ag_running: True while the body is async-driving (between asend
	// channel send and yield receive).
	//
	// CPython: Objects/genobject.c:1617 ag_running PyMemberDef
	SetTypeDescr(AsyncGeneratorType, "ag_running", NewGetSetDescr("ag_running",
		func(o Object) (Object, error) {
			g := o.(*AsyncGenerator)
			if g.Running.Load() == 1 {
				return True(), nil
			}
			return False(), nil
		}, nil))

	// ag_suspended: True when the async generator is suspended at yield.
	//
	// CPython: Objects/genobject.c:1594 ag_getsuspended
	SetTypeDescr(AsyncGeneratorType, "ag_suspended", NewGetSetDescr("ag_suspended",
		func(o Object) (Object, error) {
			g := o.(*AsyncGenerator)
			if g.started && !g.closed && g.Running.Load() == 0 {
				return True(), nil
			}
			return False(), nil
		}, nil))

	// ag_await: object being awaited on, or None.
	//
	// CPython: Objects/genobject.c:1608 ag_await -> coro_get_cr_await
	SetTypeDescr(AsyncGeneratorType, "ag_await", NewGetSetDescr("ag_await",
		func(o Object) (Object, error) {
			g := o.(*AsyncGenerator)
			if g.YieldFromTarget != nil && g.started && !g.closed && g.Running.Load() == 0 {
				Incref(g.YieldFromTarget)
				return g.YieldFromTarget, nil
			}
			return None(), nil
		}, nil))

	// __name__: writable name of the async generator.
	//
	// CPython: Objects/genobject.c:706 gen_get_name / gen_set_name
	SetTypeDescr(AsyncGeneratorType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) {
			return NewStr(o.(*AsyncGenerator).Name), nil
		},
		func(o Object, v Object) error {
			if v == nil {
				return fmt.Errorf("TypeError: __name__ attribute cannot be deleted")
			}
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __name__ must be a string, not %s", v.Type().Name)
			}
			o.(*AsyncGenerator).Name = s.Value()
			return nil
		}))

	// __qualname__: writable qualified name of the async generator.
	//
	// CPython: Objects/genobject.c:728 gen_get_qualname / gen_set_qualname
	SetTypeDescr(AsyncGeneratorType, "__qualname__", NewGetSetDescr("__qualname__",
		func(o Object) (Object, error) {
			g := o.(*AsyncGenerator)
			if g.Qualname != "" {
				return NewStr(g.Qualname), nil
			}
			return NewStr(g.Name), nil
		},
		func(o Object, v Object) error {
			if v == nil {
				return fmt.Errorf("TypeError: __qualname__ attribute cannot be deleted")
			}
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __qualname__ must be a string, not %s", v.Type().Name)
			}
			o.(*AsyncGenerator).Qualname = s.Value()
			return nil
		}))

	// tp_traverse: visit frame and code so the cycle collector keeps
	// suspended async generators alive through their captured frame.
	//
	// CPython: Objects/genobject.c:1477 async_gen_traverse (-> gen_traverse)
	AsyncGeneratorType.TpTraverse = asyncGenTraverse

	AsyncGenASendType = NewType("async_generator_asend",
		[]*Type{objectType})
	AsyncGenASendType.Iter = func(o Object) (Object, error) { Incref(o); return o, nil }
	AsyncGenASendType.IterNext = asyncGenASendNext
	AsyncGenASendType.TpTraverse = asyncGenASendTraverse
	// am_await: the asend awaitable is its own iterator. CPython's
	// _PyAsyncGenASend_Type.tp_as_async->am_await returns Py_NewRef(self).
	//
	// CPython: Objects/genobject.c:1957 async_gen_asend_as_async
	AsyncGenASendType.Async = &AsyncMethods{
		Await: func(o Object) (Object, error) { Incref(o); return o, nil },
	}

	AsyncGenAThrowType = NewType("async_generator_athrow",
		[]*Type{objectType})
	AsyncGenAThrowType.Iter = func(o Object) (Object, error) { Incref(o); return o, nil }
	AsyncGenAThrowType.IterNext = asyncGenAThrowNext
	AsyncGenAThrowType.TpTraverse = asyncGenAThrowTraverse
	// am_await: the athrow awaitable is its own iterator (see asend).
	//
	// CPython: Objects/genobject.c:2363 async_gen_athrow_as_async
	AsyncGenAThrowType.Async = &AsyncMethods{
		Await: func(o Object) (Object, error) { Incref(o); return o, nil },
	}

	AsyncGenWrappedValueType = NewType("async_generator_wrapped_value",
		[]*Type{objectType})
	AsyncGenWrappedValueType.TpTraverse = asyncGenWrappedValueTraverse

	AddIterSlotWrappers(AsyncGenASendType)
	AddIterSlotWrappers(AsyncGenAThrowType)

	// asend / athrow / aclose / __aiter__ / __anext__ Python methods.
	// PyMethodDef rows in Objects/genobject.c:1623 async_gen_methods.
	//
	// CPython: Objects/genobject.c:1623 async_gen_methods
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"asend":             asyncGenAsendMethod,
		"athrow":            asyncGenAthrowMethod,
		"aclose":            asyncGenAcloseMethod,
		"__aiter__":         asyncGenAiterMethod,
		"__anext__":         asyncGenAnextMethod,
		"__class_getitem__": asyncGenClassGetitemMethod,
		"__reduce__":        genReduceReject,
		"__reduce_ex__":     genReduceReject,
	} {
		SetTypeDescr(AsyncGeneratorType, name, NewMethodDescr(AsyncGeneratorType, name, fn))
	}

	// async_generator_asend.send / throw / close / __await__ rows.
	//
	// CPython: Objects/genobject.c:1903 async_gen_asend_methods
	// CPython: Objects/genobject.c:1911 async_gen_asend_as_async (am_await)
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":      asyncGenASendSendMethod,
		"throw":     asyncGenASendThrowMethod,
		"close":     asyncGenASendCloseMethod,
		"__await__": asyncGenASendAwaitMethod,
	} {
		SetTypeDescr(AsyncGenASendType, name, NewMethodDescr(AsyncGenASendType, name, fn))
	}

	// async_generator_athrow.send / throw / close / __await__ rows.
	//
	// CPython: Objects/genobject.c:2345 async_gen_athrow_methods
	// CPython: Objects/genobject.c:2358 async_gen_athrow_as_async (am_await)
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":      asyncGenAThrowSendMethod,
		"throw":     asyncGenAThrowThrowMethod,
		"close":     asyncGenAThrowCloseMethod,
		"__await__": asyncGenAThrowAwaitMethod,
	} {
		SetTypeDescr(AsyncGenAThrowType, name, NewMethodDescr(AsyncGenAThrowType, name, fn))
	}
}

// asyncGenAsendMethod implements async_generator.asend(value).
//
// CPython: Objects/genobject.c:1862 async_gen_asend
func asyncGenAsendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: asend() takes exactly one argument")
	}
	g, ok := args[0].(*AsyncGenerator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'asend' requires a 'async_generator' object")
	}
	return g.Asend(args[1]), nil
}

// asyncGenAthrowMethod implements async_generator.athrow(exc).
//
// CPython: Objects/genobject.c:1564 async_gen_athrow
func asyncGenAthrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: athrow() requires an exception")
	}
	g, ok := args[0].(*AsyncGenerator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'athrow' requires a 'async_generator' object")
	}
	// CPython: Objects/genobject.c:1567 emits a DeprecationWarning when
	// the deprecated (type, exc, tb) form is used. The actual exception
	// validation is deferred to send time (async_gen_athrow_send) so
	// running-state errors surface before "must be BaseException".
	if len(args) > 2 {
		if DeprecWarnHook != nil {
			if werr := DeprecWarnHook("the (type, exc, tb) signature of athrow() is deprecated, use the single-arg signature instead."); werr != nil {
				return nil, werr
			}
		}
		if GenThrowTripleHook == nil {
			return nil, fmt.Errorf("RuntimeError: async_generator.athrow 3-arg form not available")
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
		return g.Athrow(exc), nil
	}
	return g.AthrowRaw(args[1]), nil
}

// asyncGenAcloseMethod implements async_generator.aclose().
//
// CPython: Objects/genobject.c:2317 async_gen_aclose
func asyncGenAcloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: aclose() missing self argument")
	}
	g, ok := args[0].(*AsyncGenerator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'aclose' requires a 'async_generator' object")
	}
	return g.Aclose(), nil
}

// asyncGenASendSendMethod implements async_generator_asend.send(arg).
//
// CPython: Objects/genobject.c:1788 async_gen_asend_send
func asyncGenASendSendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument (%d given)", len(args)-1)
	}
	a, ok := args[0].(*asyncGenASend)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'async_generator_asend' object")
	}
	if a.state == asyncAwaitClosed {
		return nil, fmt.Errorf("RuntimeError: cannot reuse already awaited __anext__()/asend()")
	}
	if a.state == asyncAwaitInit {
		if a.gen.RunningAsync.Load() == 1 {
			a.state = asyncAwaitClosed
			return nil, fmt.Errorf("RuntimeError: anext(): asynchronous generator is already running")
		}
		// CPython: Objects/genobject.c:1807 — when arg is NULL or
		// None, use the captured ags_sendval; otherwise pass arg
		// through so the gen_send_ex2 FRAME_CREATED check rejects
		// non-None on a just-started async generator.
		v := args[1]
		if v == nil || v == None() {
			v = a.val
			if v == nil {
				v = None()
			}
		}
		a.state = asyncAwaitIter
		a.used = true
		a.gen.RunningAsync.Store(1)
		r, e := a.gen.Send(v)
		return asyncGenASendDriveResult(a, r, e)
	}
	r, e := a.gen.Send(args[1])
	return asyncGenASendDriveResult(a, r, e)
}

// asyncGenASendDriveResult unwraps the AsyncGenWrappedValue marker and
// updates ag_running_async + asend state to mirror CPython's
// async_gen_unwrap_value.
//
// CPython: Objects/genobject.c:1725 async_gen_unwrap_value
func asyncGenASendDriveResult(a *asyncGenASend, result Object, err error) (Object, error) {
	if err != nil {
		a.state = asyncAwaitClosed
		a.gen.RunningAsync.Store(0)
		return nil, err
	}
	if w, ok := result.(*AsyncGenWrappedValue); ok {
		a.state = asyncAwaitClosed
		a.gen.RunningAsync.Store(0)
		if AsyncGenStopIterationHook != nil {
			return nil, AsyncGenStopIterationHook(w.Value)
		}
		return nil, ErrStopIteration
	}
	return result, nil
}

// asyncGenASendThrowMethod implements async_generator_asend.throw(typ).
// The 3-arg form emits a DeprecationWarning matching gen.throw.
//
// CPython: Objects/genobject.c:1833 async_gen_asend_throw
func asyncGenASendThrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	a, ok := args[0].(*asyncGenASend)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires a 'async_generator_asend' object")
	}
	if GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: async_generator_asend.throw not available")
	}
	if a.state == asyncAwaitClosed {
		return nil, fmt.Errorf("RuntimeError: cannot reuse already awaited __anext__()/asend()")
	}
	if a.state == asyncAwaitInit {
		if a.gen.RunningAsync.Load() == 1 {
			a.state = asyncAwaitClosed
			return nil, fmt.Errorf("RuntimeError: anext(): asynchronous generator is already running")
		}
		a.state = asyncAwaitIter
		a.gen.RunningAsync.Store(1)
	}
	if len(args) > 2 {
		if DeprecWarnHook != nil {
			if werr := DeprecWarnHook("the (type, exc, tb) signature of throw() is deprecated, use the single-arg signature instead."); werr != nil {
				return nil, werr
			}
		}
		if GenThrowTripleHook == nil {
			return nil, fmt.Errorf("RuntimeError: async_generator_asend.throw 3-arg form not available")
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
		a.used = true
		r, e := a.gen.Throw(exc)
		return asyncGenASendDriveResult(a, r, e)
	}
	exc, err := GenThrowHook(args[1])
	if err != nil {
		return nil, err
	}
	a.used = true
	r, e := a.gen.Throw(exc)
	return asyncGenASendDriveResult(a, r, e)
}

// asyncGenASendCloseMethod implements async_generator_asend.close().
// CPython throws GeneratorExit; if the body yields anything in
// response it raises RuntimeError "coroutine ignored GeneratorExit".
// StopIteration / StopAsyncIteration / GeneratorExit are swallowed.
//
// CPython: Objects/genobject.c:1870 async_gen_asend_close
func asyncGenASendCloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	a, ok := args[0].(*asyncGenASend)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'async_generator_asend' object")
	}
	a.used = true
	result, terr := a.gen.Throw(ErrGeneratorExit)
	if terr != nil {
		if asyncCloseSwallow(terr) {
			return None(), nil
		}
		return nil, terr
	}
	if result != nil {
		return nil, fmt.Errorf("RuntimeError: coroutine ignored GeneratorExit")
	}
	return None(), nil
}

// asyncCloseSwallow reports whether err is one of the sentinel
// exceptions that close() should swallow: StopIteration,
// StopAsyncIteration, or GeneratorExit. CPython does the same with
// PyErr_ExceptionMatches in async_gen_asend_close /
// async_gen_athrow_close.
//
// CPython: Objects/genobject.c:1870 async_gen_asend_close
// CPython: Objects/genobject.c:2310 async_gen_athrow_close
func asyncCloseSwallow(err error) bool {
	if errors.Is(err, ErrStopIteration) ||
		errors.Is(err, ErrStopAsyncIteration) ||
		errors.Is(err, ErrGeneratorExit) {
		return true
	}
	var re *RaisedError
	if errors.As(err, &re) && re.Exc != nil {
		switch re.Exc.Type().Name {
		case "StopIteration", "StopAsyncIteration", "GeneratorExit":
			return true
		}
	}
	return false
}

// asyncGenAThrowSendMethod implements async_generator_athrow.send(arg).
//
// CPython: Objects/genobject.c:2100 async_gen_athrow_send
func asyncGenAThrowSendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument (%d given)", len(args)-1)
	}
	a, ok := args[0].(*asyncGenAThrow)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'async_generator_athrow' object")
	}
	if a.state == asyncAwaitClosed {
		return nil, fmt.Errorf("RuntimeError: cannot reuse already awaited aclose()/athrow()")
	}
	if a.state == asyncAwaitInit {
		if a.gen.RunningAsync.Load() == 1 {
			a.state = asyncAwaitClosed
			if a.isClose {
				return nil, fmt.Errorf("RuntimeError: aclose(): asynchronous generator is already running")
			}
			return nil, fmt.Errorf("RuntimeError: athrow(): asynchronous generator is already running")
		}
		a.state = asyncAwaitIter
		a.gen.RunningAsync.Store(1)
	}
	return asyncGenAThrowDrive(a, args[1])
}

// asyncGenAThrowThrowMethod implements async_generator_athrow.throw(typ).
//
// CPython: Objects/genobject.c:2233 async_gen_athrow_throw
func asyncGenAThrowThrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	a, ok := args[0].(*asyncGenAThrow)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires a 'async_generator_athrow' object")
	}
	if GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: async_generator_athrow.throw not available")
	}
	if a.state == asyncAwaitClosed {
		return nil, fmt.Errorf("RuntimeError: cannot reuse already awaited aclose()/athrow()")
	}
	if a.state == asyncAwaitInit {
		if a.gen.RunningAsync.Load() == 1 {
			a.state = asyncAwaitClosed
			if a.isClose {
				return nil, fmt.Errorf("RuntimeError: aclose(): asynchronous generator is already running")
			}
			return nil, fmt.Errorf("RuntimeError: athrow(): asynchronous generator is already running")
		}
		a.state = asyncAwaitIter
		a.gen.RunningAsync.Store(1)
	}
	if len(args) > 2 {
		if DeprecWarnHook != nil {
			if werr := DeprecWarnHook("the (type, exc, tb) signature of throw() is deprecated, use the single-arg signature instead."); werr != nil {
				return nil, werr
			}
		}
		if GenThrowTripleHook == nil {
			return nil, fmt.Errorf("RuntimeError: async_generator_athrow.throw 3-arg form not available")
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
		a.used = true
		r, e := a.gen.Throw(exc)
		return asyncGenAThrowDriveResult(a, r, e)
	}
	exc, err := GenThrowHook(args[1])
	if err != nil {
		return nil, err
	}
	a.used = true
	r, e := a.gen.Throw(exc)
	return asyncGenAThrowDriveResult(a, r, e)
}

// asyncGenAThrowDriveResult mirrors async_gen_unwrap_value for the
// athrow awaitable. Errors and AsyncGenWrappedValue both close it.
//
// CPython: Objects/genobject.c:1725 async_gen_unwrap_value
func asyncGenAThrowDriveResult(a *asyncGenAThrow, result Object, err error) (Object, error) {
	if err != nil {
		a.state = asyncAwaitClosed
		a.gen.RunningAsync.Store(0)
		return nil, err
	}
	if w, ok := result.(*AsyncGenWrappedValue); ok && !a.isClose {
		a.state = asyncAwaitClosed
		a.gen.RunningAsync.Store(0)
		if AsyncGenStopIterationHook != nil {
			return nil, AsyncGenStopIterationHook(w.Value)
		}
		return nil, ErrStopIteration
	}
	return result, nil
}

// asyncGenAThrowCloseMethod implements async_generator_athrow.close().
// Throws GeneratorExit; same sentinel semantics as asend.close.
//
// CPython: Objects/genobject.c:2310 async_gen_athrow_close
func asyncGenAThrowCloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	a, ok := args[0].(*asyncGenAThrow)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'async_generator_athrow' object")
	}
	a.used = true
	result, terr := a.gen.Throw(ErrGeneratorExit)
	if terr != nil {
		if asyncCloseSwallow(terr) {
			return None(), nil
		}
		return nil, terr
	}
	if result != nil {
		return nil, fmt.Errorf("RuntimeError: coroutine ignored GeneratorExit")
	}
	return None(), nil
}

// asyncGenASendAwaitMethod returns the asend awaitable (self) so
// `await asend_obj` drives it. CPython's am_await slot returns
// Py_NewRef(self); we mirror that via __await__.
//
// CPython: Objects/genobject.c:1911 async_gen_asend_as_async
func asyncGenASendAwaitMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __await__() missing self argument")
	}
	if _, ok := args[0].(*asyncGenASend); !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__await__' requires a 'async_generator_asend' object")
	}
	Incref(args[0])
	return args[0], nil
}

// asyncGenAThrowAwaitMethod returns the athrow awaitable (self) so
// `await athrow_obj` drives it.
//
// CPython: Objects/genobject.c:2358 async_gen_athrow_as_async
func asyncGenAThrowAwaitMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __await__() missing self argument")
	}
	if _, ok := args[0].(*asyncGenAThrow); !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__await__' requires a 'async_generator_athrow' object")
	}
	Incref(args[0])
	return args[0], nil
}

// asyncGenAThrowDrive is the shared body of send() / __next__ on the
// athrow awaitable. The first call throws the captured error or
// GeneratorExit (aclose mode); subsequent calls forward arg via Send.
//
// CPython: Objects/genobject.c:2100 async_gen_athrow_send
func asyncGenAThrowDrive(a *asyncGenAThrow, arg Object) (Object, error) {
	if a.used {
		// Subsequent send() forwards arg into the generator so awaited
		// sub-coroutines keep receiving values. CPython does the same
		// at Objects/genobject.c:2170 gen_iternext branch.
		r, e := a.gen.Send(arg)
		return asyncGenAThrowDriveResult(a, r, e)
	}
	a.used = true
	if a.isClose {
		r, e := a.gen.Throw(ErrGeneratorExit)
		if e != nil {
			if asyncCloseSwallow(e) {
				a.state = asyncAwaitClosed
				a.gen.RunningAsync.Store(0)
				return nil, ErrStopAsyncIteration
			}
			return asyncGenAThrowDriveResult(a, r, e)
		}
		if r != nil {
			return asyncGenAThrowDriveResult(a, r, nil)
		}
		a.state = asyncAwaitClosed
		a.gen.RunningAsync.Store(0)
		return nil, ErrStopAsyncIteration
	}
	if a.argExc != nil {
		if GenThrowHook == nil {
			return nil, fmt.Errorf("RuntimeError: async_generator.athrow not available")
		}
		exc, err := GenThrowHook(a.argExc)
		if err != nil {
			a.state = asyncAwaitClosed
			a.gen.RunningAsync.Store(0)
			return nil, err
		}
		r, e := a.gen.Throw(exc)
		return asyncGenAThrowDriveResult(a, r, e)
	}
	r, e := a.gen.Throw(a.err)
	return asyncGenAThrowDriveResult(a, r, e)
}

// asyncGenAiterMethod returns self.
//
// CPython: Objects/genobject.c:1855 async_gen_aiter (am_aiter slot)
func asyncGenAiterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __aiter__() missing self argument")
	}
	if _, ok := args[0].(*AsyncGenerator); !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__aiter__' requires a 'async_generator' object")
	}
	Incref(args[0])
	return args[0], nil
}

// asyncGenAnextMethod returns asend(None).
//
// CPython: Objects/genobject.c:1869 async_gen_anext
func asyncGenAnextMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __anext__() missing self argument")
	}
	g, ok := args[0].(*AsyncGenerator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__anext__' requires a 'async_generator' object")
	}
	return g.Anext(), nil
}

// asyncGenClassGetitemMethod implements async_generator.__class_getitem__.
// Returns self, matching CPython's Py_GenericAlias for the async_gen type.
//
// CPython: Objects/genobject.c Py_GenericAlias entry in async_gen_methods
func asyncGenClassGetitemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __class_getitem__() requires exactly one argument")
	}
	return NewGenericAlias(AsyncGeneratorType, args[1]), nil
}

// asyncGenASendTraverse visits the wrapped generator and the pending
// send value. Mirrors async_gen_asend_traverse.
//
// CPython: Objects/genobject.c:1933 async_gen_asend_traverse
func asyncGenASendTraverse(o Object, visit Visitor) error {
	a := o.(*asyncGenASend)
	if a.gen != nil {
		if err := visit(a.gen); err != nil {
			return err
		}
	}
	if a.val != nil {
		return visit(a.val)
	}
	return nil
}

// asyncGenAThrowTraverse visits the wrapped generator and the captured
// raw exception argument. Mirrors async_gen_athrow_traverse.
//
// CPython: Objects/genobject.c:2342 async_gen_athrow_traverse
func asyncGenAThrowTraverse(o Object, visit Visitor) error {
	a := o.(*asyncGenAThrow)
	if a.gen != nil {
		if err := visit(a.gen); err != nil {
			return err
		}
	}
	if a.argExc != nil {
		return visit(a.argExc)
	}
	return nil
}

// NewAsyncGenerator creates an async generator with the given name.
// The vm is responsible for spawning the body goroutine.
//
// CPython: Objects/genobject.c:L1546 async_gen_new
func NewAsyncGenerator(name string) *AsyncGenerator {
	g := &AsyncGenerator{
		Name:    name,
		YieldCh: make(chan GenMsg, 1),
		SendCh:  make(chan GenMsg, 1),
	}
	g.init(AsyncGeneratorType)
	return g
}

// NewAsyncGeneratorWithQualname creates an async generator carrying
// both __name__ and __qualname__. Mirrors gen_new_with_qualname when
// the CO_ASYNC_GENERATOR flag is set.
//
// CPython: Objects/genobject.c:867 gen_new_with_qualname
func NewAsyncGeneratorWithQualname(name, qualname string) *AsyncGenerator {
	g := NewAsyncGenerator(name)
	g.Qualname = qualname
	return g
}

// asyncGenTraverse mirrors gen_traverse for PyAsyncGen_Type.
//
// CPython: Objects/genobject.c:1477 async_gen_traverse (-> gen_traverse)
func asyncGenTraverse(o Object, visit Visitor) error {
	g, ok := o.(*AsyncGenerator)
	if !ok {
		return nil
	}
	if g.GiFrame != nil {
		if err := visit(g.GiFrame); err != nil {
			return err
		}
	}
	if g.Code != nil {
		if err := visit(g.Code); err != nil {
			return err
		}
	}
	return nil
}

// MarkFinished records that the async-generator body has run to
// completion so subsequent Send/Throw/Close are no-ops. Mirrors
// Generator.MarkFinished.
//
// CPython: Objects/genobject.c:225 gen_send_ex2 (gi_frame_state = FRAME_COMPLETED)
func (g *AsyncGenerator) MarkFinished() {
	g.closed = true
}

// Send drives the async generator one yield-point forward. The
// public surface is asend(), which returns an awaitable; this method
// is the inner driver the awaitable uses.
//
// CPython: Objects/genobject.c:L1879 async_gen_asend
func (g *AsyncGenerator) Send(v Object) (Object, error) {
	if g.closed {
		return nil, ErrStopAsyncIteration
	}
	if !g.started && v != None() {
		return nil, errors.New(
			"TypeError: can't send non-None value to a just-started async generator")
	}
	// CPython: Objects/genobject.c:275 gen_send_ex2 FRAME_EXECUTING guard
	if g.Running.Load() == 1 {
		return nil, fmt.Errorf("ValueError: async generator already executing")
	}
	g.started = true
	g.SendCh <- GenMsg{Val: v, CallerFrame: callerFrame()}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		if errors.Is(msg.Err, ErrStopIteration) {
			return nil, ErrStopAsyncIteration
		}
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Throw raises err inside the async generator. Used by athrow().
//
// CPython: Objects/genobject.c:L2272 async_gen_athrow
func (g *AsyncGenerator) Throw(err error) (Object, error) {
	if err == nil {
		return nil, errors.New("TypeError: throw() requires an exception")
	}
	if g.closed {
		return nil, err
	}
	if !g.started {
		g.closed = true
		return nil, err
	}
	g.SendCh <- GenMsg{Err: err, CallerFrame: callerFrame()}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		if errors.Is(msg.Err, ErrStopIteration) {
			return nil, ErrStopAsyncIteration
		}
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Close throws GeneratorExit. The "async generator ignored
// GeneratorExit" diagnostic mirrors CPython's check.
//
// CPython: Objects/genobject.c gen_close async path
func (g *AsyncGenerator) Close() error {
	if g.closed {
		return nil
	}
	if !g.started {
		g.closed = true
		return nil
	}
	if yf := g.YieldFromTarget; yf != nil {
		_ = GenCloseIter(yf)
		g.YieldFromTarget = nil
	}
	g.SendCh <- GenMsg{Err: ErrGeneratorExit, CallerFrame: callerFrame()}
	msg := <-g.YieldCh
	g.closed = true
	if msg.Err == nil {
		return errors.New("RuntimeError: async generator ignored GeneratorExit")
	}
	if errors.Is(msg.Err, ErrGeneratorExit) ||
		errors.Is(msg.Err, ErrStopIteration) {
		return nil
	}
	var re *RaisedError
	if errors.As(msg.Err, &re) && re.Exc != nil {
		switch re.Exc.Type().Name {
		case "GeneratorExit", "StopIteration", "StopAsyncIteration":
			return nil
		}
	}
	return msg.Err
}

// Asend returns an awaitable that, when driven, sends v into the
// async generator and yields the next value or raises
// StopAsyncIteration.
//
// CPython: Objects/genobject.c:L1879 async_gen_asend
func (g *AsyncGenerator) Asend(v Object) Object {
	a := &asyncGenASend{gen: g, val: v}
	a.init(AsyncGenASendType)
	return a
}

// Anext is __anext__: shorthand for asend(None).
//
// CPython: Objects/genobject.c:L1862 async_gen_anext
func (g *AsyncGenerator) Anext() Object {
	return g.Asend(None())
}

// Athrow returns an awaitable that throws err into the async
// generator and yields the result.
//
// CPython: Objects/genobject.c:L2272 async_gen_athrow
func (g *AsyncGenerator) Athrow(err error) Object {
	a := &asyncGenAThrow{gen: g, err: err}
	a.init(AsyncGenAThrowType)
	return a
}

// AthrowRaw mirrors async_gen_athrow_new but stashes the user-supplied
// exception object verbatim. Validation against BaseException is
// deferred to the first send() call so the already-running RuntimeError
// fires before "exceptions must be classes...".
//
// CPython: Objects/genobject.c:2375 async_gen_athrow_new
func (g *AsyncGenerator) AthrowRaw(exc Object) Object {
	a := &asyncGenAThrow{gen: g, argExc: exc}
	a.init(AsyncGenAThrowType)
	return a
}

// Aclose returns an awaitable that drives Close.
//
// CPython: Objects/genobject.c:L2317 async_gen_aclose
func (g *AsyncGenerator) Aclose() Object {
	a := &asyncGenAThrow{gen: g, err: ErrGeneratorExit, isClose: true}
	a.init(AsyncGenAThrowType)
	return a
}

// asyncGenASend states mirror CPython's AwaitableState enum.
//
// CPython: Objects/genobject.c AwaitableState (INIT/ITER/CLOSED)
const (
	asyncAwaitInit   = 0
	asyncAwaitIter   = 1
	asyncAwaitClosed = 2
)

type asyncGenASend struct {
	Header
	gen   *AsyncGenerator
	val   Object
	state int
	used  bool
}

// asyncGenASendNext is the IterNext slot. __next__ on the asend
// awaitable is semantically `send(None)`; route through the send
// state machine so running-async / closed errors fire correctly.
//
// CPython: Objects/genobject.c:1788 async_gen_asend_send
func asyncGenASendNext(o Object) (Object, error) {
	a := o.(*asyncGenASend)
	if a.state == asyncAwaitClosed {
		return nil, fmt.Errorf("RuntimeError: cannot reuse already awaited __anext__()/asend()")
	}
	if a.state == asyncAwaitInit {
		if a.gen.RunningAsync.Load() == 1 {
			a.state = asyncAwaitClosed
			return nil, fmt.Errorf("RuntimeError: anext(): asynchronous generator is already running")
		}
		v := a.val
		if v == nil {
			v = None()
		}
		a.state = asyncAwaitIter
		a.used = true
		a.gen.RunningAsync.Store(1)
		r, e := a.gen.Send(v)
		return asyncGenASendDriveResult(a, r, e)
	}
	r, e := a.gen.Send(None())
	return asyncGenASendDriveResult(a, r, e)
}

type asyncGenAThrow struct {
	Header
	gen     *AsyncGenerator
	err     error
	argExc  Object
	isClose bool
	state   int
	used    bool
}

// asyncGenAThrowNext is the IterNext slot. __next__ on the athrow
// awaitable is semantically `send(None)`; route through the send
// state machine so running-async / closed errors fire correctly.
//
// CPython: Objects/genobject.c:2100 async_gen_athrow_send
func asyncGenAThrowNext(o Object) (Object, error) {
	a := o.(*asyncGenAThrow)
	if a.state == asyncAwaitClosed {
		return nil, fmt.Errorf("RuntimeError: cannot reuse already awaited aclose()/athrow()")
	}
	if a.state == asyncAwaitInit {
		if a.gen.RunningAsync.Load() == 1 {
			a.state = asyncAwaitClosed
			if a.isClose {
				return nil, fmt.Errorf("RuntimeError: aclose(): asynchronous generator is already running")
			}
			return nil, fmt.Errorf("RuntimeError: athrow(): asynchronous generator is already running")
		}
		a.state = asyncAwaitIter
		a.gen.RunningAsync.Store(1)
	}
	return asyncGenAThrowDrive(a, None())
}

func asyncGenRepr(o Object) (string, error) {
	g := o.(*AsyncGenerator)
	return fmt.Sprintf("<async_generator object %s at %p>", g.Name, g), nil
}
