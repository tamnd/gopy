// AnextAwaitable backs anext(iter, default). It wraps the awaitable
// produced by iter.__anext__() and converts a StopAsyncIteration into
// StopIteration(default), matching CPython's anext_awaitable type. The
// type lives here in objects so builtins/anext can mint instances
// without pulling in errors.
//
// CPython: Objects/iterobject.c:312 anextawaitableobject

package objects

import (
	"errors"
	"fmt"
)

// AnextAwaitable mirrors PyAnextAwaitable's anextawaitableobject. The
// wrapped field holds the awaitable returned by __anext__(); default_
// is the second argument to anext(iter, default) returned in place of
// the raised StopAsyncIteration.
//
// CPython: Objects/iterobject.c:312 anextawaitableobject
type AnextAwaitable struct {
	Header
	wrapped  Object
	defaultV Object
}

// AnextAwaitableType is the type singleton.
//
// CPython: Objects/iterobject.c:497 _PyAnextAwaitable_Type
var AnextAwaitableType *Type

// NewAnextAwaitable boxes awaitable and defaultValue into the wrapper
// that anext(iter, default) returns.
//
// CPython: Objects/iterobject.c:529 PyAnextAwaitable_New
func NewAnextAwaitable(awaitable, defaultValue Object) *AnextAwaitable {
	a := &AnextAwaitable{wrapped: awaitable, defaultV: defaultValue}
	a.init(AnextAwaitableType)
	return a
}

// anextAwaitableGetIter mirrors anextawaitable_getiter: turn the wrapped
// awaitable into the iterator that drives it. CPython hands the wrapped
// object (the result of __anext__()) to _PyCoro_GetAwaitableIter, which
// validates it is awaitable and returns a coroutine, generator, or
// iterator. Of those only a coroutine lacks tp_iternext; when that
// happens the coroutine's am_await slot is unwrapped and the result must
// be an iterator, else "__await__ returned a non-iterable".
//
// CPython: Objects/iterobject.c:339 anextawaitable_getiter
func (a *AnextAwaitable) anextAwaitableGetIter() (Object, error) {
	if a.wrapped == nil {
		return nil, fmt.Errorf("RuntimeError: anext_awaitable.wrapped is NULL")
	}
	if CoroGetAwaitableIterHook == nil {
		return nil, fmt.Errorf("RuntimeError: CoroGetAwaitableIterHook not installed")
	}
	awaitable, err := CoroGetAwaitableIterHook(a.wrapped)
	if err != nil {
		return nil, err
	}
	if awaitable.Type().IterNext != nil {
		return awaitable, nil
	}
	// _PyCoro_GetAwaitableIter returns a Coroutine, a Generator, or an
	// iterator. Of these, only coroutines lack tp_iternext, so unwrap
	// the coroutine's am_await slot and require an iterator.
	t := awaitable.Type()
	if t.Async == nil || t.Async.Await == nil {
		return nil, fmt.Errorf("TypeError: __await__ returned a non-iterable")
	}
	next, aerr := t.Async.Await(awaitable)
	if aerr != nil {
		return nil, aerr
	}
	if next == nil || next.Type().IterNext == nil {
		return nil, fmt.Errorf("TypeError: __await__ returned a non-iterable")
	}
	return next, nil
}

// anextAwaitableIterNext mirrors anextawaitable_iternext: drive the
// underlying iter, mapping StopAsyncIteration to StopIteration(default).
//
// CPython: Objects/iterobject.c:369 anextawaitable_iternext
func anextAwaitableIterNext(o Object) (Object, error) {
	a, ok := o.(*AnextAwaitable)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__next__' requires an 'anext_awaitable' object")
	}
	it, err := a.anextAwaitableGetIter()
	if err != nil {
		return nil, err
	}
	val, ierr := it.Type().IterNext(it)
	if ierr == nil {
		return val, nil
	}
	if errors.Is(ierr, ErrStopAsyncIteration) || isStopAsyncIteration(ierr) {
		if AsyncGenStopIterationHook != nil {
			return nil, AsyncGenStopIterationHook(a.defaultV)
		}
	}
	return nil, ierr
}

// isStopAsyncIteration reports whether err carries a Python-level
// StopAsyncIteration. Mirrors the PyErr_ExceptionMatches check inside
// anextawaitable_iternext / anextawaitable_proxy.
//
// CPython: Objects/iterobject.c:402 anextawaitable_iternext
func isStopAsyncIteration(err error) bool {
	if err == nil {
		return false
	}
	var re *RaisedError
	if errors.As(err, &re) && re.Exc != nil {
		t := re.Exc.Type()
		for t != nil {
			if t.Name == "StopAsyncIteration" {
				return true
			}
			if len(t.Bases) == 0 {
				break
			}
			t = t.Bases[0]
		}
	}
	msg := err.Error()
	if msg == "StopAsyncIteration" {
		return true
	}
	const p = "StopAsyncIteration:"
	return len(msg) >= len(p) && msg[:len(p)] == p
}

// anextAwaitableProxy mirrors anextawaitable_proxy: call a method on
// the unwrapped iterator with at most one argument, translating
// StopAsyncIteration into StopIteration(default).
//
// CPython: Objects/iterobject.c:412 anextawaitable_proxy
func anextAwaitableProxy(a *AnextAwaitable, method string, args []Object) (Object, error) {
	it, err := a.anextAwaitableGetIter()
	if err != nil {
		return nil, err
	}
	fn, gerr := GetAttr(it, NewStr(method))
	if gerr != nil {
		return nil, gerr
	}
	res, cerr := Call(fn, NewTuple(args), nil)
	if cerr == nil {
		return res, nil
	}
	if errors.Is(cerr, ErrStopAsyncIteration) || isStopAsyncIteration(cerr) {
		if AsyncGenStopIterationHook != nil {
			return nil, AsyncGenStopIterationHook(a.defaultV)
		}
	}
	return nil, cerr
}

// anextAwaitableSend implements anext_awaitable.send(arg).
//
// CPython: Objects/iterobject.c:440 anextawaitable_send
func anextAwaitableSend(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument (%d given)", len(args)-1)
	}
	a, ok := args[0].(*AnextAwaitable)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires an 'anext_awaitable' object")
	}
	return anextAwaitableProxy(a, "send", []Object{args[1]})
}

// anextAwaitableThrow implements anext_awaitable.throw(*args).
//
// CPython: Objects/iterobject.c:447 anextawaitable_throw
func anextAwaitableThrow(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	a, ok := args[0].(*AnextAwaitable)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires an 'anext_awaitable' object")
	}
	return anextAwaitableProxy(a, "throw", args[1:])
}

// anextAwaitableClose implements anext_awaitable.close(). Accepts no
// extra arguments so close(1) raises TypeError, matching CPython.
//
// CPython: Objects/iterobject.c:454 anextawaitable_close
func anextAwaitableClose(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: close() takes no arguments (%d given)", len(args)-1)
	}
	a, ok := args[0].(*AnextAwaitable)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires an 'anext_awaitable' object")
	}
	return anextAwaitableProxy(a, "close", nil)
}

// anextAwaitableTraverse visits the wrapped awaitable and default value
// so the cycle collector can reach through.
//
// CPython: Objects/iterobject.c:329 anextawaitable_traverse
func anextAwaitableTraverse(o Object, visit Visitor) error {
	a, ok := o.(*AnextAwaitable)
	if !ok {
		return nil
	}
	if a.wrapped != nil {
		if err := visit(a.wrapped); err != nil {
			return err
		}
	}
	if a.defaultV != nil {
		if err := visit(a.defaultV); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	AnextAwaitableType = NewType("anext_awaitable", []*Type{objectType})
	AnextAwaitableType.Iter = SelfIter
	AnextAwaitableType.IterNext = anextAwaitableIterNext
	AnextAwaitableType.TpTraverse = anextAwaitableTraverse
	AnextAwaitableType.Async = &AsyncMethods{
		Await: func(o Object) (Object, error) { return o, nil },
	}
	AddIterSlotWrappers(AnextAwaitableType)

	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":      anextAwaitableSend,
		"throw":     anextAwaitableThrow,
		"close":     anextAwaitableClose,
		"__await__": anextAwaitableAwait,
	} {
		SetTypeDescr(AnextAwaitableType, name, NewMethodDescr(AnextAwaitableType, name, fn))
	}
}

// anextAwaitableAwait implements anext_awaitable.__await__(), the slot
// that an `await` expression invokes. CPython sets am_await =
// PyObject_SelfIter; gopy mirrors that by returning self.
//
// CPython: Objects/iterobject.c:485 anextawaitable_as_async.am_await
func anextAwaitableAwait(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __await__() missing self argument")
	}
	a, ok := args[0].(*AnextAwaitable)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__await__' requires an 'anext_awaitable' object")
	}
	return a, nil
}
