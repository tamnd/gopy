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
)

// AsyncGenerator mirrors PyAsyncGenObject.
//
// CPython: Objects/genobject.c:L1577 PyAsyncGen_Type
type AsyncGenerator struct {
	Header
	Name string

	YieldCh chan GenMsg
	SendCh  chan GenMsg

	started bool
	closed  bool
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

	AsyncGenASendType = NewType("async_generator_asend",
		[]*Type{objectType})
	AsyncGenASendType.Iter = func(o Object) (Object, error) { return o, nil }
	AsyncGenASendType.IterNext = asyncGenASendNext

	AsyncGenAThrowType = NewType("async_generator_athrow",
		[]*Type{objectType})
	AsyncGenAThrowType.Iter = func(o Object) (Object, error) { return o, nil }
	AsyncGenAThrowType.IterNext = asyncGenAThrowNext
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
	g.started = true
	g.SendCh <- GenMsg{Val: v}
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
	g.SendCh <- GenMsg{Err: err}
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
	g.SendCh <- GenMsg{Err: ErrGeneratorExit}
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
