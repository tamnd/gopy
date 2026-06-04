// IMPORT_NAME and IMPORT_FROM bytecode arms. Added to the hand-written
// panel rather than the generated arms because the import machinery
// depends on the imp package which is only available after v0.8.
//
// CPython: Python/bytecodes.c IMPORT_NAME / IMPORT_FROM
package vm

// DEPRECATED (spec 1714): Spec 1714 phase 5: IMPORT_NAME / IMPORT_FROM bodies migrate to typed op<NAME> functions invoked from vm/eval_dispatch_gen.go.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
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

// optionalImportFunc reads __import__ out of the frame's builtins
// mapping. Mirrors the PyMapping_GetOptionalItemString call import_name
// opens with: (func, true, nil) when present, (nil, false, nil) when
// the mapping lacks the key, and (nil, false, err) for a real failure.
//
// CPython: Python/ceval.c:2805 PyMapping_GetOptionalItemString(f_builtins, "__import__")
func optionalImportFunc(builtins objects.Object) (objects.Object, bool, error) {
	if builtins == nil {
		return nil, false, nil
	}
	return objects.MappingGetOptionalItem(builtins, objects.NewStr("__import__"))
}

// isDefaultImport reports whether fn is the built-in __import__ the
// interpreter installs, the object import_name compares against before
// taking its fast path. Identity against the builtins module's entry
// matches `import_func == tstate->interp->import_func`.
//
// CPython: Python/ceval.c:2820 import_name (fast-path identity check)
func isDefaultImport(fn objects.Object) bool {
	bm, ok := imp.GetModule("builtins")
	if !ok || bm == nil {
		return false
	}
	def, err := bm.Dict().GetItem(objects.NewStr("__import__"))
	if err != nil || def == nil {
		return false
	}
	return fn == def
}

// frameHasExplicitBuiltins reports whether the frame's globals carry an
// explicit __builtins__ binding. gopy falls back to using globals as the
// builtins mapping when none was set, so a missing __import__ in that
// fallback must not be mistaken for a namespace that deliberately
// dropped __import__.
func frameHasExplicitBuiltins(f *frame.Frame) bool {
	if f == nil {
		return false
	}
	d, ok := f.Globals.(*objects.Dict)
	if !ok {
		return false
	}
	v, _ := d.GetItem(objects.NewStr("__builtins__"))
	return v != nil
}

// orNone returns o, or None when o is nil, so a Python callable never
// receives a Go nil in its argument tuple.
func orNone(o objects.Object) objects.Object {
	if o == nil {
		return objects.None()
	}
	return o
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
	b := e.builtins
	if b == nil {
		// No builtins were inherited from the importing frame (e.g. the
		// top-level -c entry point where globals IS the builtins dict).
		// Fall back to the "builtins" module that was registered by
		// builtins.Init so that class definitions in imported modules can
		// resolve __build_class__.
		//
		// CPython: Python/import.c:L657 module_init_dunder_attrs always
		// sets __builtins__ via interp->builtins_module.
		if bm, ok := imp.GetModule("builtins"); ok {
			b = bm.Dict()
		}
	}
	if b != nil {
		if err := mod.Dict().SetItem(objects.NewStr("__builtins__"), b); err != nil {
			return nil, err
		}
	}
	mod.Initializing = true
	result, err := EvalCode(e.ts, code, mod.Dict(), nil)
	mod.Initializing = false
	return result, err
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

		// import_name resolves __import__ out of f_builtins and calls it.
		// A namespace that installs its own __builtins__ can override or
		// drop __import__; an explicit builtins missing the key is the
		// "__import__ not found" ImportError, and a custom callable is
		// invoked with the full (name, globals, locals, fromlist, level)
		// tuple rather than the fast path.
		//
		// CPython: Python/ceval.c:2799 import_name
		builtinsNS := callerBuiltins(e.f)
		importFunc, foundImport, ferr := optionalImportFunc(builtinsNS)
		if ferr != nil {
			return 0, true, ferr
		}
		if foundImport && !isDefaultImport(importFunc) {
			locals := e.f.Locals
			if locals == nil {
				locals = e.f.Globals
			}
			res, cerr := objects.Call(importFunc, objects.NewTuple([]objects.Object{
				objects.NewStr(modname),
				orNone(e.f.Globals),
				orNone(locals),
				orNone(fromlistObj),
				orNone(levelObj),
			}), nil)
			if cerr != nil {
				return 0, true, cerr
			}
			e.pushObject(res)
			return e.advance(), true, nil
		}
		if !foundImport && frameHasExplicitBuiltins(e.f) {
			return 0, true, fmt.Errorf("ImportError: __import__ not found")
		}

		level := importLevel(levelObj)
		pkgname := globalName(e.f.Globals)

		exec := &vmExecutor{ts: e.ts, builtins: builtinsNS}
		mod, ierr := imp.ImportModuleLevel(exec, modname, pkgname, level)
		if ierr != nil {
			// Promote Go-level ErrModuleNotFound into a typed
			// ModuleNotFoundError so `try: ... except ImportError:`
			// in Python catches the miss. The unwind path otherwise
			// wraps the Go error in a bare Exception, which defeats
			// the import-machinery contract.
			//
			// CPython: Python/import.c:1759 import_name (sets ImportError)
			if errors.Is(ierr, imp.ErrModuleNotFound) {
				pyerrors.SetString(e.ts, pyerrors.PyExc_ModuleNotFoundError,
					fmt.Sprintf("No module named %q", modname))
			}
			return 0, true, ierr
		}

		// CPython semantics: when fromlist is None/empty (plain `import
		// a.b.c`), push the TOP-LEVEL package so the name `a` is bound.
		// When fromlist is non-empty (`from a.b import c`), push the
		// deepest module so IMPORT_FROM can extract attributes.
		//
		// CPython: Python/bytecodes.c IMPORT_NAME comment "return the
		// head of the dotted name" when fromlist is empty.
		result := objects.Object(mod)
		if isEmptyFromlist(fromlistObj) && strings.Contains(modname, ".") {
			top := strings.SplitN(modname, ".", 2)[0]
			if tm, ok := imp.GetModule(top); ok {
				result = tm
			}
		}
		e.pushObject(result)
		return e.advance(), true, nil

	case compile.IMPORT_FROM:
		// TOS remains the module (not popped); push the attribute.
		// oparg = index into co.Names.
		// CPython: Python/bytecodes.c IMPORT_FROM dispatches to
		// _PyEval_ImportFrom (Python/ceval.c:3154).
		mod := e.popObject()

		co := e.f.Code
		if int(oparg) >= len(co.Names) {
			return 0, true, fmt.Errorf("vm: IMPORT_FROM: name index %d out of range", oparg)
		}
		attrname := co.Names[oparg]

		attr, aerr := evalImportFrom(e, mod, attrname)
		if aerr != nil {
			return 0, true, aerr
		}
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

// isEmptyFromlist reports whether fromlist is None, the empty tuple, or
// the empty list. This mirrors CPython's check in import_name:
// "if fromlist is NULL or fromlist is empty tuple, head is returned".
//
// CPython: Python/bytecodes.c IMPORT_NAME (fromlist emptiness check)
func isEmptyFromlist(o objects.Object) bool {
	if o == nil || objects.IsNone(o) {
		return true
	}
	if t, ok := o.(*objects.Tuple); ok {
		return t.Len() == 0
	}
	if l, ok := o.(*objects.List); ok {
		return l.Len() == 0
	}
	return false
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

// globalName extracts the relative-import anchor from the globals
// dict. CPython prefers __package__ (the dotted package containing
// the importing module) and falls back to __name__ when __package__
// is missing or None. Reading __name__ alone breaks `from . import
// foo` inside a submodule because __name__ there is the dotted
// module path while __package__ correctly points at the parent.
//
// CPython: Python/import.c:1665 import_name (read __package__ first)
func globalName(globals objects.Object) string {
	if globals == nil {
		return ""
	}
	d, ok := globals.(*objects.Dict)
	if !ok {
		return ""
	}
	if v, err := d.GetItem(objects.NewStr("__package__")); err == nil && v != nil && !objects.IsNone(v) {
		if s, serr := objects.Str(v); serr == nil && s != "" {
			return s
		}
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

// getOptionalAttr ports CPython's PyObject_GetOptionalAttr. It calls
// GetAttr and on a missing-attribute outcome (AttributeError) returns
// (nil, false, nil) with the thread-state exception cleared. Any other
// error propagates as (nil, false, err). On success returns
// (val, true, nil).
//
// CPython: Objects/object.c:1324 PyObject_GetOptionalAttr
func getOptionalAttr(e *evalState, o objects.Object, name string) (objects.Object, bool, error) {
	v, err := objects.GetAttr(o, objects.NewStr(name))
	if err == nil {
		return v, true, nil
	}
	// AttributeError set into ts by a user-defined __getattr__ wins
	// over the Go error string; either way, treat AttributeError as
	// "not found" and let other exception types propagate.
	if exc := pyerrors.Occurred(e.ts); exc != nil {
		if pyerrors.Match(exc, pyerrors.PyExc_AttributeError) {
			pyerrors.Clear(e.ts)
			return nil, false, nil
		}
		return nil, false, err
	}
	if isAttributeErrorMsg(err) {
		return nil, false, nil
	}
	return nil, false, err
}

// isAttributeErrorMsg detects the legacy "AttributeError: ..." Go error
// shape still produced by callsites that have not been routed through
// pyerrors.SetString. Mirrors the prefix table used by
// synthesizeException.
func isAttributeErrorMsg(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, "vm: "); ok {
		msg = rest
	}
	return strings.HasPrefix(msg, "AttributeError:")
}

// evalImportFrom ports _PyEval_ImportFrom. It tries to fetch `name` as
// an attribute of `v`; on miss it consults sys.modules under
// "<parent>.<name>" using the parent's __name__. As a gopy-specific
// extension (we lack importlib's _handle_fromlist plumbing), it
// force-imports the submodule when sys.modules has not cached it yet.
//
// CPython: Python/ceval.c:3154 _PyEval_ImportFrom
func evalImportFrom(e *evalState, v objects.Object, name string) (objects.Object, error) {
	if x, found, err := getOptionalAttr(e, v, name); err != nil {
		return nil, err
	} else if found {
		return x, nil
	}

	// Issue #17636 fallback: read parent.__name__ and look up
	// "<parent>.<name>" in sys.modules.
	modNameObj, found, err := getOptionalAttr(e, v, "__name__")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("vm: ImportError: cannot import name %q from <unknown module name>", name)
	}
	parentName, serr := objects.Str(modNameObj)
	if serr != nil {
		return nil, fmt.Errorf("vm: ImportError: cannot import name %q from <unknown module name>", name)
	}
	full := parentName + "." + name

	if cached, ok := imp.GetModule(full); ok {
		return cached, nil
	}
	// gopy extension: no _handle_fromlist runs during IMPORT_NAME, so
	// the submodule may never have entered sys.modules. Force-import
	// it here. CPython's _handle_fromlist (Lib/importlib/_bootstrap.py)
	// performs the same _call_with_frames_removed(import_, ...) per
	// fromlist entry.
	exec := &vmExecutor{ts: e.ts, builtins: callerBuiltins(e.f)}
	sub, ierr := imp.ImportModuleLevel(exec, full, "", 0)
	if ierr != nil {
		return nil, fmt.Errorf("vm: ImportError: cannot import name %q from %q: %w", name, parentName, ierr)
	}
	return sub, nil
}
