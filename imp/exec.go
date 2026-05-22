// ExecCodeModule: execute a code object in a module's namespace and
// register the result in sys.modules. Mirrors PyImport_ExecCodeModule.
//
// CPython: Python/import.c:L644 PyImport_ExecCodeModule
// CPython: Python/import.c:L670 exec_code_in_module
package imp

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ExecCodeModule executes code in a fresh module named name, sets the
// standard dunder attributes, registers the module in sys.modules, and
// returns it. If name is already in sys.modules the existing module is
// reused so that partially-initialized modules see updates in-place.
//
// CPython: Python/import.c:L644 PyImport_ExecCodeModule
func ExecCodeModule(exec Executor, name string, code *objects.Code) (*objects.Module, error) {
	mod, exists := GetModule(name)
	if !exists {
		mod = objects.NewModule(name)
	}

	// Set __loader__ = None and __spec__ = None as placeholders;
	// the real import machinery fills these in when it calls us.
	// CPython: Python/import.c:L659 module_init_dunder_attrs
	d := mod.Dict()
	if err := d.SetItem(objects.NewStr("__loader__"), objects.None()); err != nil {
		return nil, fmt.Errorf("imp: ExecCodeModule %q: __loader__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__spec__"), objects.None()); err != nil {
		return nil, fmt.Errorf("imp: ExecCodeModule %q: __spec__: %w", name, err)
	}

	AddModule(name, mod)

	if _, err := exec.ExecCode(code, mod); err != nil {
		// Remove the partial module on error, matching CPython behavior.
		// CPython: Python/import.c:2710 remove_module
		RemoveModule(name)
		return nil, fmt.Errorf("imp: ExecCodeModule %q: exec: %w", name, err)
	}
	// Re-read sys.modules[name]: the module body may have replaced its
	// own entry (`sys.modules[__name__] = other`), as the decimal /
	// _pydecimal shim does. Returning the original pointer would hand
	// callers an empty husk instead of the live module.
	//
	// CPython: Python/import.c:2715 exec_code_in_module
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return nil, fmt.Errorf("imp: ExecCodeModule %q: module not found in sys.modules after exec", name)
}
