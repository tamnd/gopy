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
	AsyncGenASendType.Iter = func(o Object) (Object, error) { return o, nil }
	AsyncGenASendType.IterNext = asyncGenASendNext
	AsyncGenASendType.TpTraverse = asyncGenASendTraverse

	AsyncGenAThrowType = NewType("async_generator_athrow",
		[]*Type{objectType})
	AsyncGenAThrowType.Iter = func(o Object) (Object, error) { return o, nil }
	AsyncGenAThrowType.IterNext = asyncGenAThrowNext
	AsyncGenAThrowType.TpTraverse = asyncGenAThrowTraverse

	AddIterSlotWrappers(AsyncGenASendType)
	AddIterSlotWrappers(AsyncGenAThrowType)
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

// asyncGenAThrowTraverse visits the wrapped generator. The pending
// error is a Go error rather than a Python object, so it is not
// reachable through the cycle GC. Mirrors async_gen_athrow_traverse.
//
// CPython: Objects/genobject.c:2342 async_gen_athrow_traverse
func asyncGenAThrowTraverse(o Object, visit Visitor) error {
	a := o.(*asyncGenAThrow)
	if a.gen == nil {
		return nil
	}
	return visit(a.gen)
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

// Aclose returns an awaitable that drives Close.
//
// CPython: Objects/genobject.c:L2317 async_gen_aclose
func (g *AsyncGenerator) Aclose() Object {
	a := &asyncGenAThrow{gen: g, err: ErrGeneratorExit, isClose: true}
	a.init(AsyncGenAThrowType)
	return a
}

type asyncGenASend struct {
	Header
	gen  *AsyncGenerator
	val  Object
	used bool
}

func asyncGenASendNext(o Object) (Object, error) {
	a := o.(*asyncGenASend)
	v := a.val
	if a.used {
		// CPython forwards None on subsequent next() calls of the
		// same wrapper because the originally-sent value has already
		// been consumed.
		v = None()
	}
	a.used = true
	return a.gen.Send(v)
}

type asyncGenAThrow struct {
	Header
	gen     *AsyncGenerator
	err     error
	isClose bool
	used    bool
}

func asyncGenAThrowNext(o Object) (Object, error) {
	a := o.(*asyncGenAThrow)
	if a.used {
		return nil, ErrStopAsyncIteration
	}
	a.used = true
	if a.isClose {
		// Close converts the result into StopAsyncIteration on
		// success so the awaiting consumer sees a clean end.
		if cerr := a.gen.Close(); cerr != nil {
			return nil, cerr
		}
		return nil, ErrStopAsyncIteration
	}
	return a.gen.Throw(a.err)
}

func asyncGenRepr(o Object) (string, error) {
	g := o.(*AsyncGenerator)
	return fmt.Sprintf("<async_generator object %s at %p>", g.Name, g), nil
}
