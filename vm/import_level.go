package vm

import (
	"fmt"
	"strings"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// importModuleLevelObject ports PyImport_ImportModuleLevelObject, the C
// body behind the builtin __import__. CPython describes it as
// "importlib.__import__() & _gcd_import(), ported to C for added
// performance": it resolves the absolute name, drives the live importlib
// _gcd_import / _find_and_load to load it, then performs the fromlist /
// dotted-head selection in C rather than in _bootstrap.__import__.
//
// Routing the builtin through this port (instead of calling
// _frozen_importlib.__import__ wholesale) matters for the dotted-head
// selection: the C code slices the standalone abs_name and re-reads
// sys.modules, raising KeyError("%R not in sys.modules as expected") when
// the entry is missing. The Python mirror instead reads module.__name__,
// which raises AttributeError when a caller has stuffed a non-module
// object into sys.modules (the test_malicious_relative_import regression
// guard, gh-134100).
//
// The returned ok is false when _frozen_importlib is not installed yet
// (early bootstrap), so currentImporter falls back to the Go driver.
//
// CPython: Python/import.c:3798 PyImport_ImportModuleLevelObject
func importModuleLevelObject(name string, globals objects.Object, fromlist objects.Object, level int) (objects.Object, bool, error) {
	frozen, ok := imp.GetModule("_frozen_importlib")
	if !ok {
		return nil, false, nil
	}

	// CPython: Python/import.c:3829 resolve_name / abs_name selection.
	// _gcd_import runs _sanity_check + _resolve_name itself, so we hand it
	// the original (name, package, level); we recompute abs_name here both
	// to slice the dotted head below and to drive the import_get_module /
	// import_ensure_initialized cache fast path the C body runs before ever
	// calling _find_and_load.
	var packageStr string
	var absName string
	if level > 0 {
		// _bootstrap.__import__: globals_ = globals if globals is not None
		// else {}. _calc___package__ runs the __package__/__spec__/__name__
		// fallback (and its DeprecationWarning / ImportWarning / KeyError /
		// TypeError) against that dict.
		//
		// CPython: Lib/importlib/_bootstrap.py:1487 __import__
		g := globals
		if g == nil || objects.IsNone(g) {
			g = objects.NewDict()
		}
		calc, err := objects.GetAttr(frozen, objects.NewStr("_calc___package__"))
		if err != nil {
			return nil, true, err
		}
		pkgObj, err := objects.Call(calc, objects.NewTuple([]objects.Object{g}), nil)
		if err != nil {
			return nil, true, err
		}
		if u, isStr := pkgObj.(*objects.Unicode); isStr {
			packageStr = u.Value()
		}
		absName = resolveImportName(name, packageStr, level)
	} else {
		// CPython: Python/import.c:3835 level == 0 requires a non-empty name.
		if name == "" {
			return nil, true, fmt.Errorf("ValueError: Empty module name")
		}
		absName = name
	}

	// CPython: Python/import.c:3842 import_get_module + import_ensure_initialized.
	// When abs_name is already in sys.modules AND still initializing (another
	// thread is mid-import on it), the C body waits for it via
	// _bootstrap._lock_unlock_module instead of re-entering _find_and_load.
	// That matters for concurrent circular imports: _lock_unlock_module
	// CATCHES the _DeadlockError the per-module lock raises, whereas the
	// _ModuleLockManager context inside _find_and_load lets it propagate and
	// kill the importing thread. Skipping this fast path is the difference
	// between test_threaded_import.test_circular_imports resolving (both
	// threads finish) and one thread dying on an uncaught _DeadlockError.
	//
	// Only the still-initializing case takes this branch. For the fully
	// loaded cache hit and the cold miss, _find_and_load's own optimization
	// (Lib/importlib/_bootstrap.py:1364 "module is _NEEDS_LOADING or
	// _initializing") already returns the module without entering the lock,
	// so delegating the whole import to the live __import__ is observably
	// identical and keeps the common path's refcount discipline unchanged.
	module, accepted, ferr := acceptInitializingModule(frozen, absName)
	if ferr != nil {
		return nil, true, ferr
	}
	if !accepted {
		// CPython: Python/import.c:3718 import_find_and_load -> the builtin
		// __import__ runs _find_and_load / _handle_fromlist and the dotted-head
		// selection. __import__ ignores its locals argument, so pass None.
		return delegateImport(name, orNone(globals), objects.None(), orNone(fromlist), level)
	}

	// CPython: Python/import.c:3881 has_from = PyObject_IsTrue(fromlist).
	hasFrom := false
	if fromlist != nil && !objects.IsNone(fromlist) {
		t, err := objects.IsTruthy(fromlist)
		if err != nil {
			return nil, true, err
		}
		hasFrom = t
	}
	if !hasFrom {
		return headSelection(name, packageStr, level, module)
	}
	return fromlistSelection(frozen, module, fromlist)
}

// acceptInitializingModule ports the still-initializing arm of
// import_ensure_initialized. It returns (module, true, nil) only when
// sys.modules already holds a module for absName whose __spec__._initializing
// is True: another thread is mid-import on it. In that case it waits via
// _bootstrap._lock_unlock_module, which CATCHES the _DeadlockError a
// concurrent circular import raises, and the caller accepts the (possibly
// partially initialized) cached module instead of re-entering _find_and_load
// (whose _ModuleLockManager would let that _DeadlockError propagate and kill
// the thread).
//
// Every other case (absent, None, or a fully initialized cache hit) returns
// (nil, false, nil): the caller delegates to the live __import__, whose own
// _find_and_load optimization returns the module without locking. Restricting
// the special handling to the initializing case keeps the common import path
// byte-for-byte identical to the long-standing delegateImport route.
//
// CPython: Python/import.c:244 import_ensure_initialized
// CPython: Python/import.c:3842 PyImport_ImportModuleLevelObject (cache check)
func acceptInitializingModule(frozen objects.Object, absName string) (objects.Object, bool, error) {
	raw, present := imp.GetModuleRaw(absName)
	if !present || objects.IsNone(raw) {
		return nil, false, nil
	}

	// Optimization: only call _lock_unlock_module when __spec__._initializing
	// is true (set before the module is stuffed in sys.modules).
	//
	// CPython: Python/import.c:249 import_ensure_initialized (_initializing check)
	spec, serr := objects.GetAttr(raw, objects.NewStr("__spec__"))
	if serr != nil || spec == nil || objects.IsNone(spec) {
		return nil, false, nil
	}
	initializing, ierr := imp.SpecIsInitializing(spec)
	if ierr != nil {
		return nil, false, ierr
	}
	if !initializing {
		return nil, false, nil
	}

	// Wait until the module is done importing. _lock_unlock_module acquires
	// then releases the per-module lock and CATCHES the _DeadlockError a
	// concurrent circular import raises, accepting a partially initialized
	// module rather than propagating the error.
	//
	// CPython: Python/import.c:267 _bootstrap._lock_unlock_module
	lockUnlock, lerr := objects.GetAttr(frozen, objects.NewStr("_lock_unlock_module"))
	if lerr != nil {
		return nil, false, lerr
	}
	if _, cerr := objects.Call(lockUnlock, objects.NewTuple([]objects.Object{objects.NewStr(absName)}), nil); cerr != nil {
		return nil, false, cerr
	}

	// Verify the module is still in sys.modules. Another thread may have
	// removed it (import failure) between the lookup and the initializing
	// check; if so, fall back to a full import to preserve normal semantics.
	//
	// CPython: Python/import.c:3851 mod_check != mod
	check, stillPresent := imp.GetModuleRaw(absName)
	if !stillPresent || check != raw {
		return nil, false, nil
	}
	// import_get_module returns a NEW reference (PyMapping_GetOptionalItem);
	// imp.GetModuleRaw borrows from sys.modules (PyDict_GetItem semantics), so
	// incref before returning or the IMPORT_NAME DECREF_INPUTS under-counts the
	// module and a later GC tp_clears its globals out from under live code.
	//
	// CPython: Python/import.c:238 import_get_module (Py_INCREF via GetOptionalItem)
	objects.Incref(raw)
	return raw, true, nil
}

// headSelection mirrors the !has_from branch of
// PyImport_ImportModuleLevelObject: an absolute dotted import returns the
// top-level package, while a relative dotted import re-reads the
// already-loaded head from sys.modules by slicing the resolved abs_name.
//
// CPython: Python/import.c:3887 (!has_from branch)
func headSelection(name, packageStr string, level int, module objects.Object) (objects.Object, bool, error) {
	if level != 0 && name == "" {
		// CPython: Python/import.c:3895 (elif !name: final_mod = mod).
		return module, true, nil
	}
	runes := []rune(name)
	dot := indexRune(runes, '.')
	if dot < 0 {
		// CPython: Python/import.c:3897 (no dot, simple exit).
		return module, true, nil
	}
	if level == 0 {
		// CPython: Python/import.c:3903 re-import the front absolutely.
		front := string(runes[:dot])
		return importModuleLevelObject(front, objects.None(), nil, 0)
	}
	// CPython: Python/import.c:3912 slice abs_name to its first `dot`
	// components and re-read sys.modules. abs_name is the standalone
	// resolved name, not module.__name__, so a non-module sys.modules entry
	// surfaces as the KeyError below rather than an AttributeError.
	absName := resolveImportName(name, packageStr, level)
	absRunes := []rune(absName)
	cutOff := len(runes) - dot
	toReturn := string(absRunes[:len(absRunes)-cutOff])
	mod, err := imp.SysModules().GetItem(objects.NewStr(toReturn))
	if err == nil && mod != nil {
		// GetItem borrows from sys.modules; import_get_module hands back a
		// new reference, so incref before returning.
		//
		// CPython: Python/import.c:3917 import_get_module(to_return)
		objects.Incref(mod)
	}
	if err != nil || mod == nil {
		// CPython: Python/import.c:3924 KeyError "%R not in sys.modules
		// as expected".
		r, rerr := objects.Repr(objects.NewStr(toReturn))
		if rerr != nil {
			r = "'" + toReturn + "'"
		}
		msg := fmt.Sprintf("%s not in sys.modules as expected", r)
		exc := pyerrors.New(pyerrors.PyExc_KeyError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		return nil, true, objects.NewRaisedError(exc, "KeyError: "+msg)
	}
	return mod, true, nil
}

// fromlistSelection mirrors the has_from branch: when the loaded module is
// a package (carries __path__) defer to importlib._handle_fromlist to
// force-import each requested submodule, otherwise return the module
// untouched.
//
// CPython: Python/import.c:3939 (has_from branch)
func fromlistSelection(frozen objects.Object, module objects.Object, fromlist objects.Object) (objects.Object, bool, error) {
	hasPath, err := objects.HasAttrString(module, "__path__")
	if err != nil {
		return nil, true, err
	}
	if !hasPath {
		return module, true, nil
	}
	// _bootstrap.__import__ passes _gcd_import as the import callable so
	// _handle_fromlist loads `pkg.sub` by absolute name. fromlist reaches
	// _handle_fromlist untouched: it iterates the object and raises the
	// "Item in ``from list'' must be str" TypeError for a non-str entry,
	// matching the C body which never pre-validates the elements.
	//
	// CPython: Lib/importlib/_bootstrap.py:1505 _handle_fromlist(module,
	// fromlist, _gcd_import)
	gcd, err := objects.GetAttr(frozen, objects.NewStr("_gcd_import"))
	if err != nil {
		return nil, true, err
	}
	handle, err := objects.GetAttr(frozen, objects.NewStr("_handle_fromlist"))
	if err != nil {
		return nil, true, err
	}
	res, err := objects.Call(handle, objects.NewTuple([]objects.Object{
		module, fromlist, gcd,
	}), nil)
	if err != nil {
		return nil, true, err
	}
	return res, true, nil
}

// resolveImportName mirrors _bootstrap._resolve_name: strip the trailing
// (level-1) dotted components from package, then re-attach name. It
// recomputes the abs_name that _gcd_import derived internally so
// headSelection can slice it.
//
// CPython: Lib/importlib/_bootstrap.py _resolve_name
func resolveImportName(name, pkg string, level int) string {
	for i := 1; i < level; i++ {
		dot := strings.LastIndexByte(pkg, '.')
		if dot < 0 {
			break
		}
		pkg = pkg[:dot]
	}
	if name == "" {
		return pkg
	}
	return pkg + "." + name
}

// indexRune returns the index of the first occurrence of r in runes, or
// -1. Matches PyUnicode_FindChar(..., direction=1) on code points.
func indexRune(runes []rune, r rune) int {
	for i, c := range runes {
		if c == r {
			return i
		}
	}
	return -1
}
