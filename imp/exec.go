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
		// CPython: Python/import.c:L685 remove on exec failure
		RemoveModule(name)
		return nil, fmt.Errorf("imp: ExecCodeModule %q: exec: %w", name, err)
	}
	return mod, nil
}
