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
	absName, packageStr, pkgObj, rerr := resolveImportContext(frozen, name, globals, level)
	if rerr != nil {
		return nil, true, rerr
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
	// Only the still-initializing case takes this fast path. Every other
	// case (cold miss or fully loaded) falls through to _gcd_import below,
	// exactly as the C body calls import_find_and_load.
	module, accepted, ferr := acceptInitializingModule(frozen, absName)
	if ferr != nil {
		return nil, true, ferr
	}
	if !accepted {
		// CPython: Python/import.c:3718 import_find_and_load -> _gcd_import.
		// Drive the live importlib _gcd_import to load (or return the cached)
		// module, then perform the fromlist / dotted-head selection in C
		// below rather than in _bootstrap.__import__. Routing through the
		// Python __import__ here would re-run the dotted-head slice via
		// module.__name__, which raises AttributeError instead of the
		// expected KeyError when a caller stuffed a non-module into
		// sys.modules (test_malicious_relative_import, gh-134100).
		gcd, err := objects.GetAttr(frozen, objects.NewStr("_gcd_import"))
		if err != nil {
			return nil, true, err
		}
		var gcdArgs *objects.Tuple
		if level > 0 {
			gcdArgs = objects.NewTuple([]objects.Object{
				objects.NewStr(name), pkgObj, objects.NewInt(int64(level)),
			})
		} else {
			gcdArgs = objects.NewTuple([]objects.Object{objects.NewStr(name)})
		}
		module, err = objects.Call(gcd, gcdArgs, nil)
		if err != nil {
			return nil, true, err
		}
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

// resolveImportContext ports the abs_name / package resolution prologue of
// PyImport_ImportModuleLevelObject. For level>0 it runs
// _bootstrap._calc___package__ against the caller globals (carrying its
// DeprecationWarning / ImportWarning / KeyError / TypeError), derives the
// package string, and recomputes abs_name via _resolve_name; for level==0 it
// rejects an empty name with ValueError and uses name verbatim. pkgObj is the
// package object _gcd_import expects for a relative import (nil for level==0).
//
// CPython: Python/import.c:3829 PyImport_ImportModuleLevelObject (resolve)
// CPython: Lib/importlib/_bootstrap.py:1487 __import__
func resolveImportContext(frozen objects.Object, name string, globals objects.Object, level int) (absName, packageStr string, pkgObj objects.Object, err error) {
	if level > 0 {
		// globals_ = globals if globals is not None else {}; _calc___package__
		// runs the __package__/__spec__/__name__ fallback against that dict.
		g := globals
		if g == nil || objects.IsNone(g) {
			g = objects.NewDict()
		}
		calc, gerr := objects.GetAttr(frozen, objects.NewStr("_calc___package__"))
		if gerr != nil {
			return "", "", nil, gerr
		}
		pkgObj, err = objects.Call(calc, objects.NewTuple([]objects.Object{g}), nil)
		if err != nil {
			return "", "", nil, err
		}
		if u, isStr := pkgObj.(*objects.Unicode); isStr {
			packageStr = u.Value()
		}
		absName = resolveImportName(name, packageStr, level)
		return absName, packageStr, pkgObj, nil
	}
	// CPython: Python/import.c:3835 level == 0 requires a non-empty name.
	if name == "" {
		return "", "", nil, fmt.Errorf("ValueError: Empty module name")
	}
	return name, "", nil, nil
}

// importViaDelegate is the IMPORT_NAME opcode's import path. It runs the same
// import_ensure_initialized still-initializing fast path as the builtin
// __import__ (so concurrent circular imports resolve instead of dying on an
// uncaught _DeadlockError), but otherwise delegates the load to
// _frozen_importlib.__import__ rather than driving _gcd_import + the C
// dotted-head selection directly.
//
// The distinction is a refcount one, not a semantic one. IMPORT_NAME applies
// DECREF_INPUTS to the module it pushes, and the long-standing delegateImport
// route returns an owned reference whose count that decref was proven against
// (#223). Routing IMPORT_NAME through importModuleLevelObject's _gcd_import +
// headSelection path produces a module whose net refcount differs, so the
// DECREF_INPUTS drops it to a GC-clearable state and a later collection
// tp_clears its globals out from under live code. The builtin __import__ has
// no DECREF_INPUTS, so it keeps the C-faithful importModuleLevelObject body
// (whose headSelection raises the gh-134100 KeyError); IMPORT_NAME keeps the
// delegate body it was proven against, with only the circular-import fast path
// prepended.
//
// CPython: Python/import.c:3842 import_ensure_initialized (cache fast path)
func importViaDelegate(name string, globals objects.Object, fromlist objects.Object, level int) (objects.Object, bool, error) {
	frozen, ok := imp.GetModule("_frozen_importlib")
	if !ok {
		return nil, false, nil
	}
	absName, packageStr, _, rerr := resolveImportContext(frozen, name, globals, level)
	if rerr != nil {
		return nil, true, rerr
	}

	// If abs_name is already in sys.modules and still initializing (another
	// thread is mid-import on it), wait for it via _bootstrap._lock_unlock_module.
	// _lock_unlock_module CATCHES the _DeadlockError a concurrent circular
	// import raises, whereas re-entering _find_and_load's _ModuleLockManager
	// would let it propagate and kill the thread.
	//
	// CPython: Python/import.c:3842 import_ensure_initialized (cache fast path)
	raw, stillInit, werr := waitForInitializingModule(frozen, absName)
	if werr != nil {
		return nil, true, werr
	}

	// When the module is STILL initializing after the wait, another thread
	// holds the per-module lock and is itself blocked: re-entering
	// _find_and_load via the delegate would re-acquire that lock and deadlock.
	// Return the (partially initialized) cached module directly, exactly as
	// CPython's import_ensure_initialized fast path does, then run the fromlist
	// / dotted-head selection against it. This is the only path that must not
	// delegate; it is reached only under genuine concurrent circular imports.
	//
	// CPython: Python/import.c:3851 PyImport_ImportModuleLevelObject (fast path)
	if raw != nil && stillInit {
		// import_get_module returns a NEW reference (PyMapping_GetOptionalItem);
		// imp.GetModuleRaw borrows from sys.modules, so incref before the
		// selection consumes it.
		//
		// CPython: Python/import.c:238 import_get_module
		objects.Incref(raw)
		hasFrom := false
		if fromlist != nil && !objects.IsNone(fromlist) {
			t, terr := objects.IsTruthy(fromlist)
			if terr != nil {
				return nil, true, terr
			}
			hasFrom = t
		}
		if !hasFrom {
			return headSelection(name, packageStr, level, raw)
		}
		return fromlistSelection(frozen, raw, fromlist)
	}

	// Otherwise (not cached, or the wait completed and the module is fully
	// initialized) delegate. _frozen_importlib.__import__ runs _find_and_load
	// and the fromlist / dotted-head selection itself, so its return is already
	// the module CPython's import_name would push. This is the long-standing
	// refcount-proven route (#223); IMPORT_NAME's DECREF_INPUTS was proven
	// against the reference it owns.
	mod, ok2, err := delegateImport(name, globals, objects.None(), fromlist, level)
	if err != nil {
		return nil, true, err
	}
	if !ok2 {
		return nil, false, nil
	}
	return mod, true, nil
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
	raw, _, werr := waitForInitializingModule(frozen, absName)
	if werr != nil {
		return nil, false, werr
	}
	if raw == nil {
		return nil, false, nil
	}
	// import_get_module returns a NEW reference (PyMapping_GetOptionalItem);
	// imp.GetModuleRaw borrows from sys.modules (PyDict_GetItem semantics), so
	// incref before returning or the C-faithful headSelection / fromlistSelection
	// that follows in the builtin __import__ path under-counts the module and a
	// later GC tp_clears its globals out from under live code.
	//
	// CPython: Python/import.c:238 import_get_module (Py_INCREF via GetOptionalItem)
	objects.Incref(raw)
	return raw, true, nil
}

// waitForInitializingModule ports the still-initializing arm of
// import_ensure_initialized without taking ownership of the result. When
// sys.modules already holds a module for absName whose __spec__._initializing
// is True (another thread is mid-import on it), it waits via
// _bootstrap._lock_unlock_module (which CATCHES the _DeadlockError a concurrent
// circular import raises) and returns the borrowed cached module. The second
// return value reports whether the module is STILL initializing after the wait:
// True means the holding thread is itself blocked (a genuine circular-import
// deadlock the lock_unlock swallowed), so the caller must use the module
// directly instead of re-entering _find_and_load. Every other case (absent,
// None, no usable __spec__, or not initializing) returns (nil, false, nil).
//
// The returned reference is BORROWED from sys.modules. The caller increfs it
// before use.
//
// CPython: Python/import.c:244 import_ensure_initialized
func waitForInitializingModule(frozen objects.Object, absName string) (objects.Object, bool, error) {
	raw, present := imp.GetModuleRaw(absName)
	if !present || objects.IsNone(raw) {
		return nil, false, nil
	}

	// Optimization: only call _lock_unlock_module when __spec__._initializing
	// is true (set before the module is stuffed in sys.modules).
	//
	// CPython: Python/import.c:249 import_ensure_initialized (_initializing check)
	initializing, serr := specInitializing(raw)
	if serr != nil {
		return nil, false, serr
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
	// check; if so, signal nothing to wait on so the caller does a full import.
	//
	// CPython: Python/import.c:3851 mod_check != mod
	check, stillPresent := imp.GetModuleRaw(absName)
	if !stillPresent || check != raw {
		return nil, false, nil
	}
	// Re-read _initializing after the wait. If it is still True the holding
	// thread is blocked (circular-import deadlock the lock_unlock swallowed),
	// and the caller must not re-enter _find_and_load.
	stillInit, ierr := specInitializing(raw)
	if ierr != nil {
		return nil, false, ierr
	}
	return raw, stillInit, nil
}

// specInitializing reports whether raw's __spec__._initializing is True. A
// missing or None __spec__ means the module cannot be mid-import.
//
// CPython: Python/import.c:249 import_ensure_initialized (_initializing check)
func specInitializing(raw objects.Object) (bool, error) {
	spec, serr := objects.GetAttr(raw, objects.NewStr("__spec__"))
	if serr != nil {
		return false, nil //nolint:nilerr // missing __spec__ is a fall-back, not an error
	}
	if spec == nil || objects.IsNone(spec) {
		return false, nil
	}
	return imp.SpecIsInitializing(spec)
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
