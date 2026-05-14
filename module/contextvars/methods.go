// ContextVar method bindings: get, set, reset, and the name member.
// Ports PyContextVar_methods + PyContextVar_members from
// Python/context.c so Python code can call cv.get(), cv.set(value),
// cv.reset(token), and read cv.name.
//
// CPython: Python/context.c:1083 PyContextVar_members
// CPython: Python/context.c:1088 PyContextVar_methods

package contextvars

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// CurrentThreadHook is the vm-installed callback that returns the
// running thread. ContextVar.get / .set / .reset need a *state.Thread
// to read the per-thread context; vm sets this on init so the
// contextvars package does not have to import vm.
var CurrentThreadHook func() *state.Thread

func init() {
	ContextVarType.Getattro = contextVarGetattr
}

func contextVarGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	cv, ok := o.(*ContextVar)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected ContextVar")
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "name":
		return objects.NewStr(cv.name), nil
	case "get":
		return objects.NewBuiltinFunction("get", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return contextVarGet(cv, args, kwargs)
		}), nil
	case "set":
		return objects.NewBuiltinFunction("set", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return contextVarSet(cv, args, kwargs)
		}), nil
	case "reset":
		return objects.NewBuiltinFunction("reset", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return contextVarReset(cv, args, kwargs)
		}), nil
	}
	return nil, fmt.Errorf("AttributeError: 'ContextVar' object has no attribute '%s'", n.Value())
}

// currentTS returns the running thread or errors out if vm has not
// installed the hook.
func currentTS() (*state.Thread, error) {
	if CurrentThreadHook == nil {
		return nil, fmt.Errorf("RuntimeError: ContextVar requires a thread state; vm did not install CurrentThreadHook")
	}
	ts := CurrentThreadHook()
	if ts == nil {
		return nil, fmt.Errorf("RuntimeError: no current thread state")
	}
	return ts, nil
}

// contextVarGet ports ContextVar.get([default]).
//
// CPython: Python/context.c:946 _contextvars_ContextVar_get_impl
func contextVarGet(cv *ContextVar, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: ContextVar.get() takes no keyword arguments")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: ContextVar.get() takes at most 1 positional argument (%d given)", len(args))
	}
	ts, err := currentTS()
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return cv.GetWithDefault(ts, args[0])
	}
	return cv.Get(ts)
}

// contextVarSet ports ContextVar.set(value).
//
// CPython: Python/context.c:1003 _contextvars_ContextVar_set_impl
func contextVarSet(cv *ContextVar, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 || len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ContextVar.set() takes exactly 1 positional argument")
	}
	ts, err := currentTS()
	if err != nil {
		return nil, err
	}
	return cv.Set(ts, args[0])
}

// contextVarReset ports ContextVar.reset(token).
//
// CPython: Python/context.c:1065 _contextvars_ContextVar_reset_impl
func contextVarReset(cv *ContextVar, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 || len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ContextVar.reset() takes exactly 1 positional argument")
	}
	tok, ok := args[0].(*Token)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected an instance of Token, got %s", args[0].Type().Name)
	}
	ts, err := currentTS()
	if err != nil {
		return nil, err
	}
	if err := cv.Reset(ts, tok); err != nil {
		return nil, err
	}
	return objects.None(), nil
}
