// Port of builtin_globals_impl, builtin_locals_impl, and
// builtin_vars_impl. Both rely on the eval loop publishing the running
// frame so the builtin can read f_globals / f_locals out of it. gopy
// stores the active frame on a per-goroutine map inside vm; the
// dependency edge from builtins to vm is closed with a hook the vm
// package installs in its init().
//
// CPython: Python/bltinmodule.c:1267 builtin_globals_impl
// CPython: Python/bltinmodule.c:1933 builtin_locals_impl
// CPython: Python/bltinmodule.c:2375 builtin_vars_impl

package builtins

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// CurrentScope yields the running frame's (globals, locals). nil from
// either return means "no frame is on the stack right now"; callers
// should treat that the way builtin_globals_impl treats a NULL frame
// in CPython and return None.
type CurrentScope func() (globals, locals objects.Object)

var currentScope CurrentScope

// SetCurrentScope installs the hook used by globals() and locals() to
// fetch the running frame's scope dicts. vm.init wires this on
// startup so the dependency runs builtins -> objects, vm -> builtins
// without a cycle. Tests can override the hook directly.
//
// CPython: equivalent of _PyEval_GetFrame published through pystate
func SetCurrentScope(p CurrentScope) {
	currentScope = p
}

// Globals implements builtins.globals().
//
// CPython: Python/bltinmodule.c:1267 builtin_globals_impl
func Globals(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("TypeError: globals() takes no arguments (%d given)", len(args))
	}
	if currentScope == nil {
		return objects.None(), nil
	}
	g, _ := currentScope()
	if g == nil {
		return objects.None(), nil
	}
	return g, nil
}

// Locals implements builtins.locals().
//
// CPython: Python/bltinmodule.c:1933 builtin_locals_impl
func Locals(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("TypeError: locals() takes no arguments (%d given)", len(args))
	}
	if currentScope == nil {
		return objects.None(), nil
	}
	_, l := currentScope()
	if l == nil {
		return objects.None(), nil
	}
	return l, nil
}

// Vars implements builtins.vars(). With no argument returns locals();
// with one argument returns obj.__dict__.
//
// CPython: Python/bltinmodule.c:2375 builtin_vars_impl
func Vars(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return Locals(nil, nil)
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: vars() takes at most 1 argument (%d given)", len(args))
	}
	d, err := objects.GetAttr(args[0], objects.NewStr("__dict__"))
	if err != nil {
		return nil, fmt.Errorf("TypeError: vars() argument must have __dict__ attribute")
	}
	return d, nil
}
