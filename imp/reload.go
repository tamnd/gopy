// ReloadModule: re-execute a module against its existing namespace so
// importlib.reload(m) keeps the same module object alive while picking
// up new definitions. Mirrors PyImport_ReloadModule, which delegates to
// importlib.reload; gopy has no working importlib bootstrap yet, so we
// inline the bits of the Python-level reload that matter for the
// frozen and built-in module cases.
//
// CPython: Python/import.c:3983 PyImport_ReloadModule
// CPython: Lib/importlib/__init__.py:94 reload
package imp

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ReloadModule re-executes mod's underlying source against its existing
// __dict__. The module pointer is preserved so callers that already
// hold a reference observe the new bindings in place.
//
// The lookup mirrors ImportModuleLevel's order without the sys.modules
// short-circuit: frozen modules first, then the inittab. Source-only
// modules cannot be reloaded yet because gopy does not retain the
// originating Code on the module.
//
// CPython: Python/import.c:3983 PyImport_ReloadModule
// CPython: Lib/importlib/__init__.py:94 reload
func ReloadModule(exec Executor, mod *objects.Module) (*objects.Module, error) {
	if mod == nil {
		return nil, fmt.Errorf("imp: ReloadModule: module is nil")
	}

	// importlib.reload reads __spec__.name first, then falls back to
	// __name__. gopy modules don't carry a spec yet, so use __name__
	// directly.
	// CPython: Lib/importlib/__init__.py:101 spec.name fallback to __name__
	nameObj, err := mod.Dict().GetItem(objects.NewStr("__name__"))
	if err != nil || nameObj == nil {
		return nil, fmt.Errorf("TypeError: reload() argument must be a module")
	}
	nameStr, ok := nameObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: reload() argument must be a module")
	}
	name := nameStr.Value()

	// CPython: Lib/importlib/__init__.py:108 sys.modules[name] is module
	cached, present := GetModule(name)
	if !present || cached != mod {
		return nil, fmt.Errorf("ImportError: module %q not in sys.modules", name)
	}

	// CPython: Lib/importlib/__init__.py:126 _bootstrap._find_spec then _exec
	if fm, ok := FindFrozen(name); ok && fm.Code != nil {
		return ExecCodeModule(exec, name, fm.Code)
	}

	if initFn := FindInitFunc(name); initFn != nil {
		fresh, err := initFn()
		if err != nil {
			return nil, fmt.Errorf("imp: ReloadModule %q: init: %w", name, err)
		}
		// CPython: Python/import.c:1853 reload_singlephase_extension copies
		// the freshly initialized module's dict back into the cached module.
		copyModuleDict(fresh, mod)
		return mod, nil
	}

	return nil, fmt.Errorf("%w: cannot reload %q (no frozen or built-in source)", ErrModuleNotFound, name)
}

// copyModuleDict folds src.__dict__ into dst.__dict__, overwriting any
// keys present in src. Mirrors the m_copy fold used by
// reload_singlephase_extension.
//
// CPython: Python/import.c:1853 reload_singlephase_extension
func copyModuleDict(src, dst *objects.Module) {
	keys := src.Dict().Keys()
	for _, k := range keys {
		v, err := src.Dict().GetItem(k)
		if err != nil {
			continue
		}
		_ = dst.Dict().SetItem(k, v)
	}
}
