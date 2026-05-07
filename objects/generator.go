// Generator object. Ports PyGenObject from Objects/genobject.c.
// The yield/send protocol uses Go channels so each generator runs on
// its own goroutine and blocks at YIELD_VALUE; the caller unblocks it
// via Send. This mirrors CPython's frame-suspend approach but uses
// goroutines instead of C stack switching.
//
// CPython: Include/cpython/genobject.h PyGenObject
// CPython: Objects/genobject.c

package objects

import (
	"errors"
	"fmt"
)

// ErrGeneratorExit is the Go-level sentinel for PyExc_GeneratorExit.
// close() throws this into the body; if the body yields a value
// instead of swallowing it, the runtime raises RuntimeError per
// CPython's "generator ignored GeneratorExit" check.
//
// CPython: Objects/exceptions.c PyExc_GeneratorExit
var ErrGeneratorExit = errors.New("GeneratorExit")

// ErrStopAsyncIteration mirrors PyExc_StopAsyncIteration. An async
// generator returning normally surfaces this to its consumer.
//
// CPython: Objects/exceptions.c PyExc_StopAsyncIteration
var ErrStopAsyncIteration = errors.New("StopAsyncIteration")

// GenMsg is the channel message type for the generator yield/send protocol.
// Exported so the vm package can use it without importing this struct
// from a helper package.
type GenMsg struct {
	Val Object // yielded / sent value; nil when Err is set
	Err error  // ErrStopIteration at normal end; other errors on throw()
}

// Generator is PyGenObject: a suspended frame that produces values
// one at a time via __next__ or send(). The goroutine that runs the
// generator body communicates through YieldCh and SendCh.
//
// CPython: Objects/genobject.c:L49 PyGenObject
type Generator struct {
	Header
	Name string

	// YieldCh carries values from the generator to the caller.
	YieldCh chan GenMsg
	// SendCh carries values from the caller into the generator.
	SendCh chan GenMsg

	started bool
	closed  bool
}

// GeneratorType is the type singleton for generator.
//
// CPython: Objects/genobject.c:L898 PyGen_Type
var GeneratorType *Type

func init() {
	GeneratorType = NewType("generator", []*Type{objectType})
	GeneratorType.Repr = genRepr
	GeneratorType.Str = genRepr
	GeneratorType.Iter = func(o Object) (Object, error) { return o, nil }
	GeneratorType.IterNext = genIterNext
}

// NewGenerator creates a generator with the given name. The caller
// (RETURN_GENERATOR in the vm package) is responsible for starting the
// goroutine that drives the body and communicates via YieldCh/SendCh.
//
// CPython: Objects/genobject.c:L867 gen_new_with_qualname
func NewGenerator(name string) *Generator {
	g := &Generator{
		Name:    name,
		YieldCh: make(chan GenMsg, 1),
		SendCh:  make(chan GenMsg, 1),
	}
	g.init(GeneratorType)
	return g
}

// Send delivers v into the generator and returns the next yielded value.
// Sending None to an unstarted generator is equivalent to __next__.
// Sending a non-None to an unstarted generator raises TypeError.
//
// CPython: Objects/genobject.c:L260 gen_send_ex2
func (g *Generator) Send(v Object) (Object, error) {
	if g.closed {
		return nil, ErrStopIteration
	}
	if !g.started && v != None() {
		return nil, errors.New("TypeError: can't send non-None value to a just-started generator")
	}
	g.started = true
	g.SendCh <- GenMsg{Val: v}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Throw raises err inside the generator at its current YIELD_VALUE
// suspension point. If the generator catches it and yields, that
// value is returned; if it propagates, Throw returns the error.
//
// CPython: Objects/genobject.c:L466 _gen_throw
func (g *Generator) Throw(err error) (Object, error) {
	if err == nil {
		return nil, errors.New("TypeError: throw() requires an exception")
	}
	if g.closed {
		return nil, err
	}
	if !g.started {
		// Throwing into an unstarted generator: do not run the body,
		// just propagate the exception. Mirrors gen_send_ex when the
		// frame has not yet executed a YIELD.
		g.closed = true
		return nil, err
	}
	g.SendCh <- GenMsg{Err: err}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Close throws GeneratorExit into the generator. A body that yields
// instead of swallowing the exit raises RuntimeError; StopIteration
// and GeneratorExit are both treated as a clean exit.
//
// CPython: Objects/genobject.c:L388 gen_close
func (g *Generator) Close() error {
	return g.closeWith("generator ignored GeneratorExit")
}

func (g *Generator) closeWith(ignoredMsg string) error {
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
		// Body yielded a value rather than letting GeneratorExit
		// propagate. CPython calls this an error.
		return fmt.Errorf("RuntimeError: %s", ignoredMsg)
	}
	if errors.Is(msg.Err, ErrGeneratorExit) ||
		errors.Is(msg.Err, ErrStopIteration) {
		return nil
	}
	return msg.Err
}

func genIterNext(o Object) (Object, error) {
	return o.(*Generator).Send(None())
}

func genRepr(o Object) (string, error) {
	g := o.(*Generator)
	return fmt.Sprintf("<generator object %s at %p>", g.Name, g), nil
}
