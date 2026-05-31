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
