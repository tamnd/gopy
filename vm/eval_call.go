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
// into fast-locals, and runs the eval loop. Supports positional and
// keyword arguments; positional defaults fill in the tail when the
// caller provided fewer args than parameters. *args / **kwargs are
// not yet supported.
//
// CPython: Objects/call.c _PyEval_Vector
func callPyFunction(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	fn := o.(*objects.Function)
	co := fn.Code
	if co == nil {
		return nil, fmt.Errorf("TypeError: function %q has no code", fn.Name)
	}
	npos := co.Argcount
	if npos == 0 && len(co.Varnames) > 0 {
		// v0.6 compiler doesn't always set Argcount yet; fall back to
		// "all varnames are positional" until that wires up.
		npos = len(co.Varnames)
	}
	if len(args) > npos {
		return nil, fmt.Errorf("TypeError: %s() takes %d positional arguments but %d were given",
			fn.Name, npos, len(args))
	}
	ts := activeThread
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	f := stack.Push(co, fn.Globals, nil, fn, nil)
	defer stack.Pop()

	// Positional bind.
	for i, a := range args {
		f.SetLocal(i, stackref.FromObject(a))
	}
	// Keyword bind: scan Varnames for a match, error on unknown name.
	for k, v := range kwargs {
		idx := -1
		for i, name := range co.Varnames {
			if name == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("TypeError: %s() got an unexpected keyword argument %q", fn.Name, k)
		}
		if !f.LocalAt(idx).IsNull() {
			return nil, fmt.Errorf("TypeError: %s() got multiple values for argument %q", fn.Name, k)
		}
		f.SetLocal(idx, stackref.FromObject(v))
	}
	// Defaults fill any unbound positional tail.
	if fn.Defaults != nil {
		nDefaults := fn.Defaults.Len()
		for i := 0; i < nDefaults; i++ {
			slot := npos - nDefaults + i
			if slot < 0 {
				continue
			}
			if f.LocalAt(slot).IsNull() {
				f.SetLocal(slot, stackref.FromObject(fn.Defaults.Item(i)))
			}
		}
	}
	// Verify every positional slot is bound.
	for i := 0; i < npos; i++ {
		if f.LocalAt(i).IsNull() {
			return nil, fmt.Errorf("TypeError: %s() missing required argument %q", fn.Name, co.Varnames[i])
		}
	}
	return Eval(ts, f)
}
