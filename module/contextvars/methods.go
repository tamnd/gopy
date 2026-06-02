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
	// Register name (read-only getset) plus the get/set/reset methods
	// and let the inherited PyObject_GenericGetAttr slot resolve them.
	// A NULL setter on the name getset makes `cv.name = x` raise
	// AttributeError, matching CPython's tp_getset.
	//
	// CPython: Python/context.c:1083 PyContextVar_members
	// CPython: Python/context.c:1088 PyContextVar_methods
	objects.SetTypeDescr(ContextVarType, "name",
		objects.NewGetSetDescr("name", contextVarGetName, nil))
	objects.SetTypeDescr(ContextVarType, "get",
		objects.NewMethodDescr(ContextVarType, "get", contextVarGetDescr))
	objects.SetTypeDescr(ContextVarType, "set",
		objects.NewMethodDescr(ContextVarType, "set", contextVarSetDescr))
	objects.SetTypeDescr(ContextVarType, "reset",
		objects.NewMethodDescr(ContextVarType, "reset", contextVarResetDescr))
	ContextVarType.Repr = contextVarRepr
	ContextVarType.Str = contextVarRepr
	// Route attribute assignment through the generic setattr so the
	// read-only `name` getset raises AttributeError rather than the
	// "object has no attributes" TypeError a nil tp_setattro yields.
	//
	// CPython: Python/context.c:1110 inherits object's tp_setattro
	ContextVarType.Setattro = objects.GenericSetAttr
	// ContextVar omits Py_TPFLAGS_BASETYPE, so `class C(ContextVar)`
	// raises "not an acceptable base type".
	//
	// CPython: Python/context.c:1110 .tp_flags (no Py_TPFLAGS_BASETYPE)
	ContextVarType.TpFlags &^= objects.TpFlagBasetype
}

// contextVarSelf pulls the ContextVar receiver from args[0] for a
// method descriptor, which binds self as the first positional argument.
func contextVarSelf(name string, args []objects.Object) (*ContextVar, []objects.Object, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' of 'ContextVar' object needs an argument", name)
	}
	cv, ok := args[0].(*ContextVar)
	if !ok {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'ContextVar' object", name)
	}
	return cv, args[1:], nil
}

func contextVarGetName(o objects.Object) (objects.Object, error) {
	cv, ok := o.(*ContextVar)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'name' requires a 'ContextVar' object")
	}
	return objects.NewStr(cv.name), nil
}

func contextVarGetDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	cv, rest, err := contextVarSelf("get", args)
	if err != nil {
		return nil, err
	}
	return contextVarGet(cv, rest, kwargs)
}

func contextVarSetDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	cv, rest, err := contextVarSelf("set", args)
	if err != nil {
		return nil, err
	}
	return contextVarSet(cv, rest, kwargs)
}

func contextVarResetDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	cv, rest, err := contextVarSelf("reset", args)
	if err != nil {
		return nil, err
	}
	return contextVarReset(cv, rest, kwargs)
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
