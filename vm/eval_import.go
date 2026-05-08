// IMPORT_NAME and IMPORT_FROM bytecode arms. Added to the hand-written
// panel rather than the generated arms because the import machinery
// depends on the imp package which is only available after v0.8.
//
// CPython: Python/bytecodes.c IMPORT_NAME / IMPORT_FROM
package vm

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// callerBuiltins returns the __builtins__ the importing frame should
// hand to the imported module. Mirrors _PyEval_BuildFrame's
// inheritance chain: prefer the frame's own builtins; if absent (the
// top-level -c case where globals == builtins-dict) fall back to
// globals itself.
//
// CPython: Python/ceval.c:1849 _PyEval_BuildFrame
func callerBuiltins(f *frame.Frame) objects.Object {
	if f == nil {
		return nil
	}
	if f.Builtins != nil {
		return f.Builtins
	}
	return f.Globals
}

// vmExecutor implements imp.Executor using the current thread's eval loop.
// It is created per-import inside the IMPORT_NAME arm. builtins is the
// __builtins__ binding the importing frame is running under; it is
// stamped onto the imported module's dict so LOAD_GLOBAL inside the
// imported module can reach print, getattr, and friends.
//
// CPython: Python/import.c:L657 module_init_dunder_attrs sets
// __builtins__ alongside __name__ and __file__.
type vmExecutor struct {
	ts       *state.Thread
	builtins objects.Object
}

// ExecCode executes code in mod's namespace using the current thread.
// Before running the body, stamp __builtins__ onto the module dict if
// the importing frame had one. CPython does the same in
// module_init_dunder_attrs.
//
// CPython: Python/ceval.c:L753 _PyEval_EvalCode (simplified)
// CPython: Python/import.c:L657 module_init_dunder_attrs
func (e *vmExecutor) ExecCode(code *objects.Code, mod *objects.Module) (objects.Object, error) {
	if e.builtins != nil {
		if err := mod.Dict().SetItem(objects.NewStr("__builtins__"), e.builtins); err != nil {
			return nil, err
		}
	}
	return EvalCode(e.ts, code, mod.Dict(), nil)
}

// tryImport handles IMPORT_NAME and IMPORT_FROM. It is consulted by
// dispatch before falling back to the generated arms.
//
// CPython: Python/bytecodes.c IMPORT_NAME / IMPORT_FROM
func (e *evalState) tryImport(op compile.Opcode, oparg uint32) (next int, ok bool, err error) {
	switch op {
	case compile.IMPORT_NAME:
		// Stack: TOS = fromlist, TOS1 = level (int).
		// oparg = index into co.Names.
		// CPython: Python/bytecodes.c IMPORT_NAME
		fromlistObj := e.popObject()
		levelObj := e.popObject()

		co := e.f.Code
		if int(oparg) >= len(co.Names) {
			return 0, true, fmt.Errorf("vm: IMPORT_NAME: name index %d out of range", oparg)
		}
		modname := co.Names[oparg]

		level := importLevel(levelObj)
		pkgname := globalName(e.f.Globals)

		exec := &vmExecutor{ts: e.ts, builtins: callerBuiltins(e.f)}
		mod, ierr := imp.ImportModuleLevel(exec, modname, pkgname, level)
		if ierr != nil {
			return 0, true, ierr
		}

		// For submodule imports (fromlist non-empty), return the top-level.
		// For "from x import y", fromlist=("y",) but we push the module and
		// let IMPORT_FROM extract the attribute.
		_ = fromlistObj
		e.pushObject(mod)
		return e.advance(), true, nil

	case compile.IMPORT_FROM:
		// TOS remains the module (not popped); push the attribute.
		// oparg = index into co.Names.
		// CPython: Python/bytecodes.c IMPORT_FROM
		mod := e.popObject()

		co := e.f.Code
		if int(oparg) >= len(co.Names) {
			return 0, true, fmt.Errorf("vm: IMPORT_FROM: name index %d out of range", oparg)
		}
		attrname := co.Names[oparg]

		attr, gerr := objects.GetAttr(mod, objects.NewStr(attrname))
		if gerr != nil {
			return 0, true, fmt.Errorf("vm: ImportError: cannot import name %q: %w", attrname, gerr)
		}

		// IMPORT_FROM leaves the module on the stack under the attribute.
		e.pushObject(mod)
		e.pushObject(attr)
		return e.advance(), true, nil
	}
	return 0, false, nil
}

// importStar implements `from x import *`. It reads __all__ from the module
// if present; otherwise it reads __dict__ and skips names that start with "_".
// All found names are stored into the current frame's locals (or globals for
// module-scope code).
//
// CPython: Python/intrinsics.c:124 import_star
// CPython: Python/intrinsics.c:40 import_all_from
func (e *evalState) importStar(from objects.Object) error {
	locals := e.f.Locals
	if locals == nil {
		locals = e.f.Globals
	}
	if locals == nil {
		return fmt.Errorf("ImportError: no locals found during 'import *'")
	}
	dst, ok := locals.(*objects.Dict)
	if !ok {
		return fmt.Errorf("ImportError: 'import *' locals must be a dict, got %T", locals)
	}

	var all []objects.Object
	skipUnder := false

	// Check for __all__.
	allAttr, aerr := objects.GetAttr(from, objects.NewStr("__all__"))
	if aerr == nil && allAttr != nil {
		items, ierr := iterToSlice(allAttr)
		if ierr != nil {
			return ierr
		}
		all = items
	} else {
		// Fall back to __dict__ keys, skipping names starting with "_".
		dictAttr, derr := objects.GetAttr(from, objects.NewStr("__dict__"))
		if derr != nil || dictAttr == nil {
			return fmt.Errorf("ImportError: from-import-* object has no __dict__ and no __all__")
		}
		items, ierr := iterToSlice(dictAttr)
		if ierr != nil {
			return ierr
		}
		all = items
		skipUnder = true
	}

	for _, nameObj := range all {
		name, nerr := objects.Str(nameObj)
		if nerr != nil {
			return fmt.Errorf("TypeError: 'import *' name must be str")
		}
		if skipUnder && name != "" && name[0] == '_' {
			continue
		}
		val, verr := objects.GetAttr(from, objects.NewStr(name))
		if verr != nil {
			return verr
		}
		if serr := dst.SetItem(objects.NewStr(name), val); serr != nil {
			return serr
		}
	}
	return nil
}

// importLevel extracts the integer import level from a Python int object.
// Level 0 = absolute, 1+ = relative.
//
// CPython: Python/bytecodes.c IMPORT_NAME (level = PEEK(2))
func importLevel(obj objects.Object) int {
	if obj == nil {
		return 0
	}
	iv, ok := obj.(*objects.Int)
	if !ok {
		return 0
	}
	v, exact := iv.Int64()
	if !exact || v < 0 {
		return 0
	}
	return int(v)
}

// globalName extracts __name__ from the globals dict for use as the
// anchor in relative imports.
//
// CPython: Python/bytecodes.c IMPORT_NAME (GLOBALS()["__name__"])
func globalName(globals objects.Object) string {
	if globals == nil {
		return ""
	}
	d, ok := globals.(*objects.Dict)
	if !ok {
		return ""
	}
	v, err := d.GetItem(objects.NewStr("__name__"))
	if err != nil || v == nil {
		return ""
	}
	if tp := v.Type(); tp.Str != nil {
		s, serr := tp.Str(v)
		if serr == nil {
			return s
		}
	}
	return ""
}
