// Port of builtin_aiter and builtin_anext_impl. Both dispatch through
// the type's tp_as_async slots: am_aiter for aiter(), am_anext for
// anext(). The two-argument anext(iterator, default) wraps the
// awaitable in objects.AnextAwaitable so a StopAsyncIteration during
// iteration falls back to StopIteration(default).
//
// CPython: Python/bltinmodule.c:1846 builtin_aiter
// CPython: Python/bltinmodule.c:1868 builtin_anext_impl

package builtins

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// AIter implements aiter(async_iterable). Returns the async iterator
// produced by tp_as_async->am_aiter.
//
// CPython: Python/bltinmodule.c:1846 builtin_aiter
func AIter(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: aiter expected 1 argument, got %d", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: aiter() takes no keyword arguments")
	}
	o := args[0]
	if a := o.Type().Async; a != nil && a.Aiter != nil {
		return a.Aiter(o)
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not an async iterable", o.Type().Name)
}

// ANext implements anext(aiterator[, default]). The two-argument form
// would wrap the awaitable so a StopAsyncIteration falls back to the
// default; that wrapper is not yet ported.
//
// CPython: Python/bltinmodule.c:1868 builtin_anext_impl
func ANext(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: anext expected 1 or 2 arguments, got %d", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: anext() takes no keyword arguments")
	}
	o := args[0]
	a := o.Type().Async
	if a == nil || a.Anext == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not an async iterator", o.Type().Name)
	}
	awaitable, err := a.Anext(o)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return awaitable, nil
	}
	// anext(iter, default): wrap the awaitable so a raised
	// StopAsyncIteration falls back to StopIteration(default).
	//
	// CPython: Python/bltinmodule.c:1909 builtin_anext_impl (default arg)
	// CPython: Objects/iterobject.c:529 PyAnextAwaitable_New
	return objects.NewAnextAwaitable(awaitable, args[1]), nil
}
