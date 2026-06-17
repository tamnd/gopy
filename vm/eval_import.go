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

	"github.com/tamnd/gopy/builtins"
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
func optionalImportFunc(builtinsMap objects.Object) (objects.Object, bool, error) {
	if builtinsMap == nil {
		return nil, false, nil
	}
	return objects.MappingGetOptionalItem(builtinsMap, objects.NewStr("__import__"))
}

// isDefaultImport reports whether fn is the built-in __import__ the
// interpreter installs, the object import_name compares against before
// taking its fast path. Identity against the builtins module's entry
// matches `import_func == tstate->interp->import_func`.
//
// CPython: Python/ceval.c:2820 import_name (fast-path identity check)
func isDefaultImport(fn objects.Object) bool {
	return builtins.DefaultImport != nil && fn == builtins.DefaultImport
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
		// A relative import requires __package__ to be a string. CPython's
		// _sanity_check raises TypeError before any resolution when level>0
		// and __package__ is set to a non-string (e.g. an object()).
		//
		// CPython: Lib/importlib/_bootstrap.py:1390 _sanity_check
		if level > 0 {
			if terr := checkPackageType(e.f.Globals); terr != nil {
				return 0, true, terr
			}
		}
		pkgname := globalName(e.f.Globals)

		// Route through the live Python importlib the way CPython's
		// import_name calls the builtin __import__ (=
		// _frozen_importlib.__import__). _bootstrap.__import__ runs
		// _find_and_load / _handle_fromlist and returns the head of a dotted
		// name for an empty fromlist, so the module pushed here is already
		// the one CPython would push. Delegating keeps a single import path
		// so a patched loader.exec_module fires and the traceback carries the
		// <frozen importlib ...> frames. Only when the bootstrap is not yet
		// installed (early startup) does the Go driver below run.
		//
		// CPython: Python/ceval.c:2898 import_name
		if mod, ok, derr := importModuleLevelObject(modname, orNone(e.f.Globals), orNone(fromlistObj), level); ok {
			if derr != nil {
				// CPython: Python/import.c:3959 import_name trims the importlib
				// machinery frames off the traceback before the calling frame is
				// recorded on the way out.
				removeImportlibFrames(e.ts)
				return 0, true, derr
			}
			e.pushObject(mod)
			return e.advance(), true, nil
		}

		exec := &vmExecutor{ts: e.ts, builtins: builtinsNS}
		mod, ierr := imp.ImportModuleLevelObject(exec, modname, pkgname, level)
		if ierr != nil {
			// Promote Go-level ErrModuleNotFound into a typed
			// ModuleNotFoundError so `try: ... except ImportError:`
			// in Python catches the miss. The unwind path otherwise
			// wraps the Go error in a bare Exception, which defeats
			// the import-machinery contract.
			//
			// CPython: Python/import.c:1759 import_name (sets ImportError)
			//
			// A failure raised while executing the module body (the imported
			// module itself ran a failing `import`, etc.) already left the
			// real exception on the thread state with its own traceback, so
			// re-synthesizing here would discard it and the inner frame it
			// points at. Only synthesize for a genuine lookup miss.
			//
			// CPython: Python/import.c:1759 import_name only sets the error
			// when PyImport_ImportModuleLevelObject returns NULL without one.
			if errors.Is(ierr, imp.ErrBlockedNone) {
				// sys.modules[name] is None: raise the halted ModuleNotFoundError
				// with name set, so `except ImportError as exc: exc.name` works.
				// A blocked sentinel is always an absolute name (level 0), so
				// modname is already the resolved key in sys.modules.
				pyerrors.SetModuleNotFoundHalted(e.ts, modname)
			} else if errors.Is(ierr, imp.ErrModuleNotFound) && !errors.Is(ierr, imp.ErrModuleExecFailed) {
				pyerrors.SetModuleNotFound(e.ts, modname)
			}
			return 0, true, ierr
		}

		// A non-empty fromlist drives _handle_fromlist: force-import any
		// submodule named in the fromlist that the package does not already
		// expose, so the IMPORT_FROM / import_all_from that follows resolves
		// it via a plain attribute read.
		//
		// CPython: Lib/importlib/_bootstrap.py:1409 _handle_fromlist
		if !isEmptyFromlist(fromlistObj) {
			if herr := e.handleFromlist(mod, fromlistObj, false); herr != nil {
				return 0, true, herr
			}
		}

		// CPython semantics: when fromlist is None/empty (plain `import
		// a.b.c`), push the TOP-LEVEL package so the name `a` is bound.
		// When fromlist is non-empty (`from a.b import c`), push the
		// deepest module so IMPORT_FROM can extract attributes.
		//
		// CPython: Python/bytecodes.c IMPORT_NAME comment "return the
		// head of the dotted name" when fromlist is empty.
		result := mod
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

	// Prefer __all__; fall back to __dict__ keys (skipping leading "_").
	// Neither read force-imports anything: _handle_fromlist already
	// pulled in the fromlist's submodules during IMPORT_NAME.
	allAttr, allFound, aerr := getOptionalAttr(e, from, "__all__")
	if aerr != nil {
		return aerr
	}
	if allFound {
		items, ierr := iterToSlice(allAttr)
		if ierr != nil {
			return ierr
		}
		all = items
	} else {
		dictAttr, dictFound, derr := getOptionalAttr(e, from, "__dict__")
		if derr != nil {
			return derr
		}
		if !dictFound {
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
		name, ok := nameObj.(*objects.Unicode)
		if !ok {
			return importStarNonStrError(from, nameObj, skipUnder)
		}
		s := name.Value()
		if skipUnder && s != "" && s[0] == '_' {
			continue
		}
		val, verr := objects.GetAttr(from, objects.NewStr(s))
		if verr != nil {
			return verr
		}
		serr := dst.SetItem(objects.NewStr(s), val)
		// CPython: Python/intrinsics.c import_all_from releases the GetAttr
		// new-ref after SetItem takes its own.
		objects.Decref(val)
		if serr != nil {
			return serr
		}
	}
	return nil
}

// importStarNonStrError builds the TypeError import_all_from raises for a
// non-string entry in __all__ (or non-string key in __dict__). When the
// module's own __name__ is not a string, the error is about __name__
// itself.
//
// CPython: Python/intrinsics.c:77 import_all_from (non-str name branch)
func importStarNonStrError(from, name objects.Object, skipUnder bool) error {
	modNameObj, err := objects.GetAttr(from, objects.NewStr("__name__"))
	if err != nil {
		return err
	}
	mn, ok := modNameObj.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: module __name__ must be a string, not %s", modNameObj.Type().Name)
	}
	key, container := "Item", "__all__"
	if skipUnder {
		key, container = "Key", "__dict__"
	}
	return fmt.Errorf("TypeError: %s in %s.%s must be str, not %s", key, mn.Value(), container, name.Type().Name)
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
// checkPackageType returns a TypeError when globals carries a __package__
// that is set (not None) but is not a string. A relative import with such a
// package is rejected before resolution.
//
// CPython: Lib/importlib/_bootstrap.py:1390 _sanity_check ("__package__ not
// set to a string")
func checkPackageType(globals objects.Object) error {
	d, ok := globals.(*objects.Dict)
	if !ok {
		return nil
	}
	v, _ := d.GetItem(objects.NewStr("__package__"))
	if v == nil || objects.IsNone(v) {
		return nil
	}
	if _, isStr := v.(*objects.Unicode); !isStr {
		return fmt.Errorf("TypeError: __package__ not set to a string")
	}
	return nil
}

func globalName(globals objects.Object) string {
	if globals == nil {
		return ""
	}
	d, ok := globals.(*objects.Dict)
	if !ok {
		return ""
	}
	// __package__ takes precedence and is returned verbatim, even when it
	// is the empty string: an empty package with a relative import is
	// exactly the "no known parent package" case resolveAbsName rejects.
	// Only a missing or None __package__ falls through to derivation.
	//
	// CPython: Lib/importlib/_bootstrap.py:1350 _calc___package__
	if v, err := d.GetItem(objects.NewStr("__package__")); err == nil && v != nil && !objects.IsNone(v) {
		if s, serr := objects.Str(v); serr == nil {
			return s
		}
	}
	// __spec__.parent is the next anchor when no explicit __package__ is set.
	//
	// CPython: Lib/importlib/_bootstrap.py:1358 _calc___package__ (spec.parent)
	if v, err := d.GetItem(objects.NewStr("__spec__")); err == nil && v != nil && !objects.IsNone(v) {
		if parent, perr := objects.GetAttr(v, objects.NewStr("parent")); perr == nil && parent != nil && !objects.IsNone(parent) {
			if s, serr := objects.Str(parent); serr == nil {
				return s
			}
		}
	}
	// Fall back to __name__. A package (one carrying __path__) anchors at
	// its own name; a plain module strips its final dotted component. For
	// __main__ this yields "" so a relative import raises.
	//
	// CPython: Lib/importlib/_bootstrap.py:1362 _calc___package__ (rpartition)
	v, err := d.GetItem(objects.NewStr("__name__"))
	if err != nil || v == nil {
		return ""
	}
	s, serr := objects.Str(v)
	if serr != nil {
		return ""
	}
	if hp, herr := d.GetItem(objects.NewStr("__path__")); herr == nil && hp != nil {
		return s
	}
	if dot := strings.LastIndex(s, "."); dot >= 0 {
		return s[:dot]
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

// errImportFromRaised is a sentinel returned by evalImportFrom after it
// has already installed a typed ImportError on the thread state. The VM
// unwind reads the thread-state exception, so the Go error only needs to
// be non-nil to signal failure.
var errImportFromRaised = errors.New("vm: import-from error raised")

// evalImportFrom ports _PyEval_ImportFrom. It fetches `name` as an
// attribute of `v`; on miss it falls back to reading "<parent>.<name>"
// straight out of sys.modules (the circular-import path), and when that
// also misses it raises the "cannot import name X from Y (location)"
// ImportError, reproducing the stdlib-shadowing and circular-import
// message variants.
//
// CPython: Python/ceval.c:3154 _PyEval_ImportFrom
func evalImportFrom(e *evalState, v objects.Object, name string) (objects.Object, error) {
	if x, found, err := getOptionalAttr(e, v, name); err != nil {
		return nil, err
	} else if found {
		return x, nil
	}

	// Issue #17636: in case this failed because of a circular relative
	// import, fall back on reading the module directly from sys.modules.
	modNameObj, found, err := getOptionalAttr(e, v, "__name__")
	if err != nil {
		return nil, err
	}
	// CPython requires PyUnicode_Check (str or subclass); a non-str
	// __name__ is treated as missing.
	var modNameStr objects.Object
	if found && objects.IsSubtype(modNameObj.Type(), objects.StrType()) {
		modNameStr = modNameObj
	}
	if modNameStr != nil {
		if s, ok := modNameStr.(*objects.Unicode); ok {
			full := s.Value() + "." + name
			if cached, ok := imp.GetModule(full); ok {
				return cached, nil
			}
		}
	}

	return nil, e.importFromError(v, name, modNameStr)
}

// importFromError builds and raises the ImportError for a failed
// `from v import name`, porting the error block of _PyEval_ImportFrom.
//
// CPython: Python/ceval.c:3185 _PyEval_ImportFrom (error label)
func (e *evalState) importFromError(v objects.Object, name string, modNameObj objects.Object) error {
	nameRepr, _ := objects.Repr(objects.NewStr(name))
	// mod_name_or_unknown is the real __name__ object when present, else a
	// fresh "<unknown module name>" str. It is the object handed to
	// PySet_Contains so an unhashable __name__ raises through.
	haveModName := modNameObj != nil
	modNameOrUnknownObj := modNameObj
	if !haveModName {
		modNameOrUnknownObj = objects.NewStr("<unknown module name>")
	}
	modRepr, _ := objects.Repr(modNameOrUnknownObj)

	// modName is the value forwarded as the ImportError `name` member: the
	// real module name when __name__ was a string, else unset.
	modName := ""
	if haveModName {
		if s, ok := modNameObj.(*objects.Unicode); ok {
			modName = s.Value()
		}
	}

	spec, specFound, serr := getOptionalAttr(e, v, "__spec__")
	if serr != nil {
		return serr
	}
	if !specFound {
		msg := fmt.Sprintf("cannot import name %s from %s (unknown location)", nameRepr, modRepr)
		pyerrors.SetImportErrorWithNameFrom(e.ts, msg, modName, "", name)
		return errImportFromRaised
	}

	origin, originFound, oerr := imp.SpecFileOrigin(spec)
	if oerr != nil {
		return oerr
	}

	shadowing, sherr := imp.ModuleIsPossiblyShadowing(originFound, origin)
	if sherr != nil {
		return sherr
	}
	shadowingStdlib := false
	if shadowing {
		c, cerr := imp.StdlibModuleNamesContains(modNameOrUnknownObj)
		if cerr != nil {
			return cerr
		}
		shadowingStdlib = c
	}

	// Fall back to __file__ for diagnostics when the spec carries no
	// location origin and v is a module.
	if !originFound {
		if mod, ok := v.(*objects.Module); ok {
			if f, ferr := mod.Dict().GetItem(objects.NewStr("__file__")); ferr == nil && f != nil {
				if fs, ok := f.(*objects.Unicode); ok {
					origin = fs.Value()
					originFound = true
				}
			}
		}
	}

	var msg string
	switch {
	case shadowingStdlib:
		originRepr, _ := objects.Repr(objects.NewStr(origin))
		msg = fmt.Sprintf("cannot import name %s from %s (consider renaming %s since it has the same name as the standard library module named %s and prevents importing that standard library module)",
			nameRepr, modRepr, originRepr, modRepr)
	default:
		initializing, ierr := imp.SpecIsInitializing(spec)
		if ierr != nil {
			return ierr
		}
		switch {
		case initializing && shadowing:
			originRepr, _ := objects.Repr(objects.NewStr(origin))
			msg = fmt.Sprintf("cannot import name %s from %s (consider renaming %s if it has the same name as a library you intended to import)",
				nameRepr, modRepr, originRepr)
		case initializing && originFound:
			msg = fmt.Sprintf("cannot import name %s from partially initialized module %s (most likely due to a circular import) (%s)",
				nameRepr, modRepr, origin)
		case initializing:
			msg = fmt.Sprintf("cannot import name %s from partially initialized module %s (most likely due to a circular import)",
				nameRepr, modRepr)
		case originFound:
			msg = fmt.Sprintf("cannot import name %s from %s (%s)", nameRepr, modRepr, origin)
		default:
			msg = fmt.Sprintf("cannot import name %s from %s (unknown location)", nameRepr, modRepr)
		}
	}

	originArg := ""
	if originFound {
		originArg = origin
	}
	pyerrors.SetImportErrorWithNameFrom(e.ts, msg, modName, originArg, name)
	return errImportFromRaised
}

// handleFromlist ports _handle_fromlist: for a package module (one that
// carries __path__), force-import each fromlist entry that is not already
// an attribute so a later attribute read resolves the submodule. A `*`
// entry recurses over module.__all__; a non-str entry raises TypeError.
//
// CPython: Lib/importlib/_bootstrap.py:1409 _handle_fromlist
func (e *evalState) handleFromlist(mod objects.Object, fromlist objects.Object, recursive bool) error {
	// _handle_fromlist runs only for packages (hasattr(module, '__path__')).
	// __import__ guards the call with the same check, and a non-module
	// cached entry never carries __path__, so it no-ops here.
	//
	// CPython: Lib/importlib/_bootstrap.py:1503 elif hasattr(module, '__path__')
	if !recursive {
		if _, present, herr := getOptionalAttr(e, mod, "__path__"); herr != nil {
			return herr
		} else if !present {
			return nil
		}
	}

	items, err := iterToSlice(fromlist)
	if err != nil {
		return err
	}
	modName := ""
	if nm, present, _ := getOptionalAttr(e, mod, "__name__"); present {
		if s, ok := nm.(*objects.Unicode); ok {
			modName = s.Value()
		}
	}

	for _, item := range items {
		x, ok := item.(*objects.Unicode)
		if !ok {
			where := "``from list''"
			if recursive {
				where = modName + ".__all__"
			}
			return fmt.Errorf("TypeError: Item in %s must be str, not %s", where, item.Type().Name)
		}
		entry := x.Value()
		switch entry {
		case "*":
			if !recursive {
				if allObj, present, _ := getOptionalAttr(e, mod, "__all__"); present && allObj != nil {
					if rerr := e.handleFromlist(mod, allObj, true); rerr != nil {
						return rerr
					}
				}
			}
		default:
			_, present, gerr := getOptionalAttr(e, mod, entry)
			if gerr != nil {
				return gerr
			}
			if present {
				continue
			}
			fromName := modName + "." + entry
			exec := &vmExecutor{ts: e.ts, builtins: callerBuiltins(e.f)}
			if _, ierr := imp.ImportModuleLevel(exec, fromName, "", 0); ierr != nil {
				// Backwards-compatibility: ignore a fromlist-triggered import
				// of a submodule that simply does not exist, but only when the
				// miss is for exactly this submodule.
				//
				// CPython: Lib/importlib/_bootstrap.py:1433 except ModuleNotFoundError
				if errors.Is(ierr, imp.ErrModuleNotFound) {
					if _, cached := imp.GetModule(fromName); !cached {
						pyerrors.Clear(e.ts)
						continue
					}
				}
				return ierr
			}
		}
	}
	return nil
}
