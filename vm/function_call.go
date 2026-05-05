// Wires objects.FunctionType.Call so a Python-defined function can be
// invoked through objects.Call. Lives in the vm package because the
// call needs to push a new frame and drive the eval loop, which the
// objects package can't reach without an import cycle.
//
// CPython: Objects/funcobject.c function_call
package vm

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// activeThread is set by Eval so nested Function.Call can pick up the
// running thread. CPython gets this from PyThreadState_GET; the gopy
// state package doesn't expose a goroutine-local "current thread" hook
// yet, so the VM threads it through a package var. The eval loop
// already runs under the GIL, so the var is single-writer at any
// instant.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET
var activeThread *state.Thread

func init() {
	objects.FunctionType.Call = callPyFunction
}

// callPyFunction pushes a frame for the function's code, binds args
// into fast-locals, and runs the eval loop. v0.6 supports the simplest
// shape: positional args matching co.Varnames count exactly, no
// defaults, no kwargs, no *args / **kwargs.
//
// CPython: Objects/call.c _PyEval_Vector
func callPyFunction(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	fn := o.(*objects.Function)
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: %s: keyword arguments not yet supported in v0.6", fn.Name)
	}
	co := fn.Code
	if co == nil {
		return nil, fmt.Errorf("TypeError: function %q has no code", fn.Name)
	}
	expected := len(co.Varnames)
	if len(args) > expected {
		return nil, fmt.Errorf("TypeError: %s() takes %d positional arguments but %d were given",
			fn.Name, expected, len(args))
	}
	ts := activeThread
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	f := stack.Push(co, fn.Globals, nil, nil, nil)
	for i, a := range args {
		f.SetLocal(i, stackref.FromObject(a))
	}
	defer stack.Pop()
	return Eval(ts, f)
}
