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
)

// Coroutine mirrors PyCoroObject. Internally it shares the
// Generator's channel-based suspend/resume model.
//
// CPython: Objects/genobject.c:L1271 PyCoro_Type
type Coroutine struct {
	Header
	Name string

	YieldCh chan GenMsg
	SendCh  chan GenMsg

	started bool
	closed  bool
}

// CoroutineType is the type singleton for coroutine.
//
// CPython: Objects/genobject.c:L1271 PyCoro_Type
var CoroutineType *Type

// CoroAwaitType is the type for the iterator that __await__ returns.
//
// CPython: Objects/genobject.c:L1500 _PyCoroWrapper_Type
var CoroAwaitType *Type

func init() {
	CoroutineType = NewType("coroutine", []*Type{objectType})
	CoroutineType.Repr = coroRepr
	CoroutineType.Str = coroRepr

	CoroAwaitType = NewType("coroutine_wrapper", []*Type{objectType})
	CoroAwaitType.Iter = func(o Object) (Object, error) { return o, nil }
	CoroAwaitType.IterNext = coroAwaitNext
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

// Send drives the coroutine forward by one suspension point. The
// rules match Generator.Send: None on first call, otherwise an
// already-started coroutine receives the sent value.
//
// CPython: Objects/genobject.c:L260 gen_send_ex2
func (c *Coroutine) Send(v Object) (Object, error) {
	if c.closed {
		return nil, ErrStopIteration
	}
	if !c.started && v != None() {
		return nil, errors.New("TypeError: can't send non-None value to a just-started coroutine")
	}
	c.started = true
	c.SendCh <- GenMsg{Val: v}
	msg := <-c.YieldCh
	if msg.Err != nil {
		c.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Throw raises err inside the coroutine at its await suspension.
//
// CPython: Objects/genobject.c:L466 _gen_throw
func (c *Coroutine) Throw(err error) (Object, error) {
	if err == nil {
		return nil, errors.New("TypeError: throw() requires an exception")
	}
	if c.closed {
		return nil, err
	}
	if !c.started {
		c.closed = true
		return nil, err
	}
	c.SendCh <- GenMsg{Err: err}
	msg := <-c.YieldCh
	if msg.Err != nil {
		c.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Close throws GeneratorExit. A coroutine that yields after seeing
// GeneratorExit raises RuntimeError, mirroring "coroutine ignored
// GeneratorExit".
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
	c.SendCh <- GenMsg{Err: ErrGeneratorExit}
	msg := <-c.YieldCh
	c.closed = true
	if msg.Err == nil {
		return errors.New("RuntimeError: coroutine ignored GeneratorExit")
	}
	if errors.Is(msg.Err, ErrGeneratorExit) ||
		errors.Is(msg.Err, ErrStopIteration) {
		return nil
	}
	return msg.Err
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
