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
}

// builtinLookup retrieves the named object from the builtins module.
//
// CPython: Python/bltinmodule.c:_PyEval_GetBuiltin
func builtinLookup(name string) (objects.Object, error) {
	mod, ok := imp.GetModule("builtins")
	if !ok {
		return nil, fmt.Errorf("AttributeError: builtins module not loaded")
	}
	v, err := mod.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		return nil, fmt.Errorf("AttributeError: builtins: %q not found: %w", name, err)
	}
	if v == nil {
		return nil, fmt.Errorf("AttributeError: builtins: %q", name)
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
