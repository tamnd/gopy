// Package atexit is the gopy port of CPython's atexit module.
// Callbacks registered through atexit.register run in LIFO order at
// interpreter shutdown (or when _run_exitfuncs is called explicitly).
//
// CPython: Modules/atexitmodule.c atexitmodule
package atexit

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("atexit", buildModule)
}

// callback captures one (func, args, kwargs) tuple plus a stable id
// so unregister can identify it. Mirrors atexit_callback.
//
// CPython: Modules/atexitmodule.c atexit_callback
type callback struct {
	fn     objects.Object
	args   []objects.Object
	kwargs map[string]objects.Object
}

var (
	mu        sync.Mutex
	callbacks []callback
)

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("atexit")
	d := m.Dict()
	entries := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"register", atexitRegister},
		{"unregister", atexitUnregister},
		{"_run_exitfuncs", atexitRunExitfuncs},
		{"_clear", atexitClear},
		{"_ncallbacks", atexitNcallbacks},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), objects.NewBuiltinFunction(e.name, e.fn)); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// atexitRegister installs func to be called at shutdown. Returns func
// so the call site can keep using it: atexit.register(f) == f.
//
// CPython: Modules/atexitmodule.c:171 atexit_register
func atexitRegister(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: register() takes at least 1 argument (0 given)")
	}
	fn := args[0]
	if fn.Type().Call == nil {
		return nil, fmt.Errorf("TypeError: the first argument must be callable")
	}
	mu.Lock()
	callbacks = append(callbacks, callback{fn: fn, args: args[1:], kwargs: kwargs})
	mu.Unlock()
	return fn, nil
}

// atexitUnregister removes every registered callback whose function
// compares equal to the supplied callable. CPython uses
// PyObject_RichCompareBool(Py_EQ); gopy approximates with pointer
// identity, which matches the common register/unregister pattern.
//
// CPython: Modules/atexitmodule.c:307 atexit_unregister
func atexitUnregister(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: unregister() takes exactly one argument (%d given)", len(args))
	}
	target := args[0]
	mu.Lock()
	kept := callbacks[:0]
	for _, c := range callbacks {
		if c.fn != target {
			kept = append(kept, c)
		}
	}
	callbacks = kept
	mu.Unlock()
	return objects.None(), nil
}

// atexitRunExitfuncs invokes every registered callback in LIFO order
// and clears the registry. Exceptions are reported via sys.excepthook
// in CPython; the gopy port mirrors that by ignoring per-callback
// errors so a later callback isn't skipped.
//
// CPython: Modules/atexitmodule.c:102 atexit_callfuncs
func atexitRunExitfuncs(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	pending := callbacks
	callbacks = nil
	mu.Unlock()
	for i := len(pending) - 1; i >= 0; i-- {
		c := pending[i]
		tp := c.fn.Type()
		if tp.Call == nil {
			continue
		}
		_, _ = tp.Call(c.fn, c.args, c.kwargs)
	}
	return objects.None(), nil
}

// atexitClear drops every registered callback without running any.
//
// CPython: Modules/atexitmodule.c:237 atexit_clear
func atexitClear(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	callbacks = nil
	mu.Unlock()
	return objects.None(), nil
}

// atexitNcallbacks returns the count of currently-registered callbacks.
//
// CPython: Modules/atexitmodule.c:250 atexit_ncallbacks
func atexitNcallbacks(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	n := len(callbacks)
	mu.Unlock()
	return objects.NewInt(int64(n)), nil
}
