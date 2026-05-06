// init_importlib sequence. Mirrors the two-phase importlib bootstrap
// in pylifecycle.c: first the pure-Python _frozen_importlib is
// installed, then _frozen_importlib_external extends it.
//
// In gopy the frozen code objects start as nil placeholders (see
// frozen_bootstrap.go). InitImportlib returns ErrBootstrapNotReady
// when the code objects have not been embedded yet; callers in the
// early lifecycle can treat this as a soft error and proceed with a
// reduced import system.
//
// CPython: Python/pylifecycle.c:L987 init_importlib
// CPython: Python/pylifecycle.c:L1041 init_importlib_external
package imp

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ErrBootstrapNotReady is returned when the frozen importlib code
// objects have not been embedded yet (placeholder state).
var ErrBootstrapNotReady = errors.New("imp: bootstrap: frozen importlib code objects are not yet embedded")

// Executor is the minimal interface the bootstrap needs from the eval
// loop: execute a code object in the given module's namespace and
// return its result.
//
// CPython: Python/ceval.c:L753 _PyEval_EvalCode (simplified)
type Executor interface {
	ExecCode(code *objects.Code, mod *objects.Module) (objects.Object, error)
}

// InitImportlib runs phase 1 of the importlib bootstrap: executes the
// _frozen_importlib code object and installs the resulting module into
// sys.modules. It then calls the module's _install(sys_module) hook.
//
// Returns ErrBootstrapNotReady when the frozen code object has not
// been embedded yet.
//
// CPython: Python/pylifecycle.c:L987 init_importlib
func InitImportlib(exec Executor, sysmod *objects.Module) error {
	fm, ok := FindFrozen("_frozen_importlib")
	if !ok {
		return fmt.Errorf("imp: _frozen_importlib not in frozen table")
	}
	if fm.Code == nil {
		return ErrBootstrapNotReady
	}

	mod := objects.NewModule("_frozen_importlib")
	if _, err := exec.ExecCode(fm.Code, mod); err != nil {
		return fmt.Errorf("imp: execute _frozen_importlib: %w", err)
	}
	AddModule("_frozen_importlib", mod)

	// Call _install(sys_module) to wire up the importlib internals.
	// CPython: Python/pylifecycle.c:L1003 call _install
	if err := callInstall(mod, sysmod); err != nil {
		return fmt.Errorf("imp: _frozen_importlib._install: %w", err)
	}
	return nil
}

// InitImportlibExternal runs phase 2 of the importlib bootstrap:
// executes _frozen_importlib_external and calls its _install hook with
// the _frozen_importlib module.
//
// CPython: Python/pylifecycle.c:L1041 init_importlib_external
func InitImportlibExternal(exec Executor) error {
	bootstrap, ok := GetModule("_frozen_importlib")
	if !ok {
		return fmt.Errorf("imp: _frozen_importlib must be initialized before _external")
	}

	fm, ok := FindFrozen("_frozen_importlib_external")
	if !ok {
		return fmt.Errorf("imp: _frozen_importlib_external not in frozen table")
	}
	if fm.Code == nil {
		return ErrBootstrapNotReady
	}

	mod := objects.NewModule("_frozen_importlib_external")
	if _, err := exec.ExecCode(fm.Code, mod); err != nil {
		return fmt.Errorf("imp: execute _frozen_importlib_external: %w", err)
	}
	AddModule("_frozen_importlib_external", mod)

	// Call _install(_frozen_importlib) to extend the bootstrap.
	// CPython: Python/pylifecycle.c:L1057 call _install
	if err := callInstall(mod, bootstrap); err != nil {
		return fmt.Errorf("imp: _frozen_importlib_external._install: %w", err)
	}
	return nil
}

// callInstall calls mod._install(arg) by looking up the "_install"
// attribute and invoking it. This is a best-effort call; if the
// attribute is absent the call is silently skipped (the code object
// may not yet expose it in placeholder state).
//
// CPython: Python/pylifecycle.c:L1003 PyObject_CallOneArg(_install, arg)
func callInstall(mod *objects.Module, arg *objects.Module) error {
	// Intentionally discard the error: absent _install is expected
	// when the frozen module is in placeholder state.
	fn, _ := objects.GetAttr(mod, objects.NewStr("_install"))
	if fn == nil {
		return nil
	}
	typ := fn.Type()
	if typ.Call == nil {
		return fmt.Errorf("imp: _install is not callable")
	}
	_, callErr := typ.Call(fn, []objects.Object{arg}, nil)
	return callErr
}
