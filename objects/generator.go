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
	"sync/atomic"
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

// RaisedError is the Go-level wrapper for a Python exception object
// crossing the generator yield/send protocol or any other channel that
// only carries Go errors. The vm side recognizes this wrapper and
// installs Exc on the thread state via pyerrors.Raise before unwinding,
// so a `raise` re-raised inside the generator preserves the original
// PyObject identity (`exc is value` checks in contextlib's __exit__).
//
// CPython: equivalent of PyErr_Restore passing the exception PyObject
// directly across the generator boundary.
type RaisedError struct {
	Exc Object // The Python exception instance
	Msg string // Formatted message for Error()
}

// Error implements the error interface. The text mirrors excSentinel
// so any caller that pins err.Error() keeps working.
func (r *RaisedError) Error() string {
	if r == nil {
		return "Exception"
	}
	return r.Msg
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

	// Running is 1 while the generator goroutine is actively executing
	// the body (between reading from SendCh and writing to YieldCh).
	// Mirrors CPython's gi_frame_state == FRAME_EXECUTING check that
	// prevents re-entrant calls from deadlocking.
	//
	// CPython: Objects/genobject.c:275 gen_send_ex2 FRAME_EXECUTING check
	Running atomic.Int32
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
	GeneratorType.Getattro = GenericGetAttr
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":  genSendMethod,
		"throw": genThrowMethod,
		"close": genCloseMethod,
	} {
		SetTypeDescr(GeneratorType, name, NewMethodDescr(GeneratorType, name, fn))
	}
	// gi_running: 1 when the generator body is executing, 0 otherwise.
	//
	// CPython: Objects/genobject.c gi_running member (PyMemberDef)
	SetTypeDescr(GeneratorType, "gi_running", NewGetSetDescr("gi_running",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			return NewInt(int64(g.Running.Load())), nil
		}, nil))
	// gi_frame: the frame object of the suspended generator. Returns None
	// when the generator is exhausted or not yet started.
	//
	// CPython: Objects/genobject.c gi_frame member
	SetTypeDescr(GeneratorType, "gi_frame", NewGetSetDescr("gi_frame",
		func(o Object) (Object, error) {
			return None(), nil
		}, nil))
	AddIterSlotWrappers(GeneratorType)
}

func genSendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument")
	}
	g, ok := args[0].(*Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'generator' object")
	}
	return g.Send(args[1])
}

// GenThrowHook converts a Python exception object to a Go error for
// generator.throw(). Installed by the vm package to break the import cycle.
// The returned error is what gets sent into the generator goroutine;
// callers can pass it to Generator.Throw directly.
var GenThrowHook func(Object) (error, error)

// NewRaisedError wraps a Python exception object as a Go error. The
// caller is responsible for ensuring exc is a real exception instance
// (not a class). msg should be the formatted "Type: message" string.
func NewRaisedError(exc Object, msg string) *RaisedError {
	return &RaisedError{Exc: exc, Msg: msg}
}

func genThrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	g, ok := args[0].(*Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires a 'generator' object")
	}
	if GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: generator.throw not available")
	}
	exc, err := GenThrowHook(args[1])
	if err != nil {
		return nil, err
	}
	return g.Throw(exc)
}

func genCloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	g, ok := args[0].(*Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'generator' object")
	}
	if err := g.Close(); err != nil {
		return nil, err
	}
	return None(), nil
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
	// Detect re-entrant calls: if the generator body is currently executing
	// (e.g., the body calls next() on itself), raise ValueError immediately
	// rather than deadlocking on the channel. Mirrors CPython's
	// gi_frame_state == FRAME_EXECUTING guard.
	//
	// CPython: Objects/genobject.c:275 gen_send_ex2
	if g.Running.Load() == 1 {
		return nil, fmt.Errorf("ValueError: generator already executing")
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
