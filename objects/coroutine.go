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
	CoroutineType.Getattro = GenericGetAttr
	CoroutineType.Setattro = GenericSetAttr
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":          coroSendMethod,
		"close":         coroCloseMethod,
		"__reduce__":    genReduceReject,
		"__reduce_ex__": genReduceReject,
	} {
		SetTypeDescr(CoroutineType, name, NewMethodDescr(CoroutineType, name, fn))
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
	//
	// CPython: Objects/genobject.c:1129 coro_get_cr_await (-> _PyGen_yf)
	SetTypeDescr(CoroutineType, "cr_await", NewGetSetDescr("cr_await",
		func(o Object) (Object, error) {
			c := o.(*Coroutine)
			if c.YieldFromTarget != nil && c.started && !c.closed && c.Running.Load() == 0 {
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
		}))

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
		}))

	// tp_traverse: lets the cycle collector walk references the
	// coroutine holds (its frame, code object). Mirrors gen_traverse
	// reused on the coroutine type via the shared tp_traverse slot.
	//
	// CPython: Objects/genobject.c:1244 (PyCoro_Type tp_traverse = gen_traverse)
	CoroutineType.TpTraverse = coroTraverse

	CoroAwaitType = NewType("coroutine_wrapper", []*Type{objectType})
	CoroAwaitType.Iter = func(o Object) (Object, error) { return o, nil }
	CoroAwaitType.IterNext = coroAwaitNext
	CoroAwaitType.TpTraverse = coroAwaiterTraverse
	AddIterSlotWrappers(CoroAwaitType)
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
	// CPython: Objects/genobject.c:275 gen_send_ex2 FRAME_EXECUTING guard
	if c.Running.Load() == 1 {
		return nil, fmt.Errorf("ValueError: coroutine already executing")
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
