package sys

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// asyncgenHooksType is the named tuple returned by
// sys.get_asyncgen_hooks(). CPython builds it once as a structseq with
// the two fields firstiter and finalizer.
//
// CPython: Python/sysmodule.c:1430 asyncgen_hooks_desc
var asyncgenHooksType = objects.NewStructSeqType("asyncgen_hooks", []objects.StructSeqField{
	{Name: "firstiter"},
	{Name: "finalizer"},
})

// hookValue casts a per-thread hook slot back to an Object, returning
// None when no hook is installed. The slots are stored as any in
// state.Thread so the state package stays objects-free.
func hookValue(v any) objects.Object {
	if v == nil {
		return objects.None()
	}
	if o, ok := v.(objects.Object); ok {
		return o
	}
	return objects.None()
}

// getAsyncgenHooks ports sys.get_asyncgen_hooks() -> (firstiter,
// finalizer). Reads the running thread's hook slots.
//
// CPython: Python/sysmodule.c:1480 sys_get_asyncgen_hooks_impl
func getAsyncgenHooks(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ts := resolveThread()
	if ts == nil {
		return objects.NewStructSeq(asyncgenHooksType, []objects.Object{objects.None(), objects.None()}), nil
	}
	return objects.NewStructSeq(asyncgenHooksType, []objects.Object{
		hookValue(ts.AsyncGenFirstIter),
		hookValue(ts.AsyncGenFinalizer),
	}), nil
}

// setAsyncgenHooks ports sys.set_asyncgen_hooks(firstiter=None,
// finalizer=None). Each argument must be callable or None; None clears
// the slot. CPython validates callability before storing either hook.
//
// CPython: Python/sysmodule.c:1443 sys_set_asyncgen_hooks_impl
func setAsyncgenHooks(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: set_asyncgen_hooks() takes at most 2 arguments (%d given)", len(args))
	}
	var firstiter, finalizer objects.Object
	if len(args) >= 1 {
		firstiter = args[0]
	}
	if len(args) >= 2 {
		finalizer = args[1]
	}
	for k, v := range kwargs {
		switch k {
		case "firstiter":
			firstiter = v
		case "finalizer":
			finalizer = v
		default:
			return nil, fmt.Errorf("TypeError: set_asyncgen_hooks() got an unexpected keyword argument '%s'", k)
		}
	}
	ts := resolveThread()
	if ts == nil {
		return objects.None(), nil
	}
	if firstiter != nil && firstiter != objects.None() {
		if !objects.Callable(firstiter) {
			return nil, fmt.Errorf("TypeError: callable firstiter expected, got %s", firstiter.Type().Name)
		}
		ts.AsyncGenFirstIter = firstiter
	} else if firstiter == objects.None() {
		ts.AsyncGenFirstIter = nil
	}
	if finalizer != nil && finalizer != objects.None() {
		if !objects.Callable(finalizer) {
			return nil, fmt.Errorf("TypeError: callable finalizer expected, got %s", finalizer.Type().Name)
		}
		ts.AsyncGenFinalizer = finalizer
	} else if finalizer == objects.None() {
		ts.AsyncGenFinalizer = nil
	}
	return objects.None(), nil
}
