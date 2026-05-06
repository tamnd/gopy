// vm-side wiring for builtins.globals() and builtins.locals(). The
// builtin needs to read the running frame, but builtins doesn't import
// vm or frame. This file installs a hook from the vm side so the
// dependency arrow points the right way: builtins -> objects, vm ->
// builtins.

package vm

import (
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func init() {
	builtins.SetCurrentScope(currentScope)
	builtins.SetImporter(currentImporter)
}

// currentScope yields the running frame's (globals, locals). The
// locals branch matches PyEval_GetFrameLocals: if the frame has an
// explicit f_locals dict (set by EvalCode for module / class bodies)
// it is returned as-is; otherwise the fast locals are materialized
// into a fresh dict via FrameFastToLocals.
//
// CPython: Python/bltinmodule.c:1267 builtin_globals_impl
// CPython: Python/bltinmodule.c:1933 builtin_locals_impl
func currentScope() (objects.Object, objects.Object) {
	ts := currentThread()
	if ts == nil {
		return nil, nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, nil
	}
	if f.Locals != nil {
		return f.Globals, f.Locals
	}
	d, err := objects.FrameFastToLocals(objects.NewFrame(f))
	if err != nil {
		return f.Globals, nil
	}
	return f.Globals, d
}

// currentImporter is the hook builtins.__import__ delegates to. It
// reuses vmExecutor so the import can run frozen / built-in module
// init code, then forwards to imp.ImportModuleLevel. fromlist is
// accepted for signature parity; the existing IMPORT_NAME arm
// likewise drops it pending fromlist-driven submodule discovery.
//
// CPython: Python/import.c:1561 PyImport_ImportModuleLevelObject
func currentImporter(name, pkgname string, level int, _ []string) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	exec := &vmExecutor{ts: ts}
	mod, err := imp.ImportModuleLevel(exec, name, pkgname, level)
	if err != nil {
		return nil, err
	}
	return mod, nil
}
