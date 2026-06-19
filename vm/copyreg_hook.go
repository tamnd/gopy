// vm-side wiring for the object.__reduce_ex__ pipeline. objects/ can
// not import the import package without creating a cycle, so the
// pickle reducer in objects looks up copyreg through the
// CopyregLookup hook installed below.
//
// CPython: Objects/typeobject.c:7747 _common_reduce (import_copyreg)

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func init() {
	objects.CopyregLookup = copyregLookup
	objects.BuiltinLookup = builtinLookup
	objects.CurrentBuiltinsHook = currentBuiltins
	objects.ImportModuleHook = importModuleByName
	objects.ModuleReprHook = moduleReprViaImportlib
}

// moduleReprViaImportlib renders a module's repr by calling
// importlib._bootstrap._module_repr, the same delegation CPython's C
// module_repr performs through _PyImport_ImportlibModuleRepr. Any
// failure to reach importlib falls back to the minimal Go rendering so
// repr() never raises.
//
// CPython: Python/import.c:3346 _PyImport_ImportlibModuleRepr
func moduleReprViaImportlib(m objects.Object) (string, error) {
	bootstrap, ok := imp.GetModule("importlib._bootstrap")
	if !ok || bootstrap == nil {
		mod, err := importModuleByName("importlib._bootstrap")
		if err != nil || mod == nil {
			return objects.ModuleReprFallback(m)
		}
		var modOk bool
		if bootstrap, modOk = mod.(*objects.Module); !modOk {
			return objects.ModuleReprFallback(m)
		}
	}
	// importlib._bootstrap caches the _bootstrap_external module in a
	// module global, normally wired by _install_external_importers during
	// the frozen bootstrap. gopy resolves imports Go-side and never runs
	// that hook, so _module_repr_from_spec's isinstance(loader,
	// NamespaceLoader) check would always miss. Wire the global the way
	// _install_external_importers does so the namespace-package repr (and
	// the other consumers of the cached module) behave like CPython.
	//
	// CPython: Lib/importlib/_bootstrap.py:1565 _install_external_importers
	if cur, _ := bootstrap.Dict().GetItem(objects.NewStr("_bootstrap_external")); cur == nil || cur == objects.None() {
		ext, err := importModuleByName("importlib._bootstrap_external")
		if err == nil && ext != nil {
			_ = bootstrap.Dict().SetItem(objects.NewStr("_bootstrap_external"), ext)
		}
	}
	fn, err := bootstrap.Dict().GetItem(objects.NewStr("_module_repr"))
	if err != nil || fn == nil {
		return objects.ModuleReprFallback(m)
	}
	res, err := objects.Call(fn, objects.NewTuple([]objects.Object{m}), nil)
	if err != nil {
		return objects.ModuleReprFallback(m)
	}
	return objects.Str(res)
}

// importModuleByName imports an absolute module name, returning the
// cached module when sys.modules already holds it and otherwise driving
// a fresh import through the path/finder machinery. The _pickle decoder
// reaches for this when resolving GLOBAL references and when loading
// _compat_pickle for the proto < 3 name mapping.
//
// CPython: Python/import.c:1450 PyImport_ImportModule
func importModuleByName(name string) (objects.Object, error) {
	if mod, ok := imp.GetModule(name); ok && mod != nil {
		return mod, nil
	}
	// PyImport_ImportModule drives the live importlib _gcd_import, which
	// recurses parent packages and resolves namespace packages (directories
	// with no __init__.py). The Go ImportModule driver below resolves only
	// the named module against sys.path and does not import the parents, so
	// it misses a deeply dotted namespace submodule. Prefer _gcd_import once
	// _frozen_importlib is installed; fall back to the Go driver during early
	// bootstrap, before importlib is live.
	//
	// CPython: Python/import.c:1450 PyImport_ImportModule (_gcd_import)
	if frozen, ok := imp.GetModule("_frozen_importlib"); ok && frozen != nil {
		gcd, err := objects.GetAttr(frozen, objects.NewStr("_gcd_import"))
		if err == nil && gcd != nil {
			return objects.Call(gcd, objects.NewTuple([]objects.Object{objects.NewStr(name)}), nil)
		}
	}
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	exec := &vmExecutor{ts: ts}
	mod, err := imp.ImportModule(exec, name)
	if err != nil {
		return nil, err
	}
	return mod, nil
}

// currentBuiltins returns the builtins namespace a function built under
// the running frame should inherit when its globals lack an explicit
// __builtins__ key: the frame's f_builtins, else the builtins module
// dict.
//
// CPython: Objects/dictobject.c _PyDict_LoadBuiltinsFromGlobals
// (PyEval_GetBuiltins fallback)
func currentBuiltins() objects.Object {
	if ts := currentThread(); ts != nil {
		if f := frameStackFor(ts).Top(); f != nil && frameHasExplicitBuiltins(f) {
			if b := callerBuiltins(f); b != nil {
				return b
			}
		}
	}
	if mod, ok := imp.GetModule("builtins"); ok && mod != nil {
		return mod.Dict()
	}
	return nil
}

// builtinLookup retrieves the named object the way _PyEval_GetBuiltin
// does: from the running frame's f_builtins, not the builtins module.
// A namespace that installs its own __builtins__ governs what iter /
// reversed / getattr a reducer running under it resolves to, and a
// missing name is AttributeError(name) (PyErr_SetObject). When the
// frame uses gopy's implicit fallback (globals standing in for an
// absent __builtins__) or there is no running frame, fall back to the
// builtins module so internal reducers keep working.
//
// CPython: Python/bltinmodule.c:3083 _PyEval_GetBuiltin
// CPython: Python/ceval.c PyEval_GetBuiltins (current frame f_builtins)
func builtinLookup(name string) (objects.Object, error) {
	var builtins objects.Object
	if ts := currentThread(); ts != nil {
		if f := frameStackFor(ts).Top(); f != nil && frameHasExplicitBuiltins(f) {
			builtins = callerBuiltins(f)
		}
	}
	if builtins == nil {
		mod, ok := imp.GetModule("builtins")
		if !ok {
			return nil, fmt.Errorf("AttributeError: %s", name)
		}
		builtins = mod.Dict()
	}
	v, found, err := objects.MappingGetOptionalItem(builtins, objects.NewStr(name))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("AttributeError: %s", name)
	}
	return v, nil
}

// copyregLookup retrieves the named attribute from the copyreg module.
// copyreg is a pure-Python module that lives on sys.path; we drive it
// through ImportModule when it has not been loaded yet so the reducer
// can fire even from tests that never imported pickle first.
//
// CPython: Python/import.c:1450 PyImport_ImportModule (via import_copyreg)
func copyregLookup(name string) (objects.Object, error) {
	mod, ok := imp.GetModule("copyreg")
	if !ok {
		ts := currentThread()
		if ts == nil {
			ts = state.NewThread()
		}
		exec := &vmExecutor{ts: ts}
		var err error
		mod, err = imp.ImportModule(exec, "copyreg")
		if err != nil {
			return nil, fmt.Errorf("ImportError: copyreg required for pickle: %w", err)
		}
	}
	v, err := mod.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		return nil, fmt.Errorf("AttributeError: copyreg.%s: %w", name, err)
	}
	if v == nil {
		return nil, fmt.Errorf("AttributeError: copyreg.%s not found", name)
	}
	return v, nil
}
