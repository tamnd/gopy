// Package _imp is the gopy port of CPython's Modules/_imp module (the
// builtin half lives in Python/import.c). It materializes the surface
// the vendored importlib._bootstrap / _bootstrap_external drive:
//
//   - source_hash(key, source)           Python/import.c:4869
//   - pyc_magic_number_token (int)       Python/import.c:4926
//   - check_hash_based_pycs (str)        Python/import.c:4920
//   - extension_suffixes()               Python/import.c:4807
//   - find_frozen / get_frozen_object    Python/import.c:4660 / 4592
//   - is_frozen / is_frozen_package      Python/import.c:4720 / 4700
//   - create_builtin / exec_builtin      Python/import.c:4488 / 4540
//   - create_dynamic / exec_dynamic      Python/import.c:4380 / 4440
//   - _fix_co_filename                   Python/import.c:4318
//
// The frozen / builtin entries bridge to gopy's own imp package (the
// frozen table and the inittab), which is the real store for those
// modules. create_dynamic / exec_dynamic raise ImportError: gopy cannot
// load CPython C extension shared objects.
//
// CPython: Python/import.c:4943 imp_module
package _imp

import (
	"bytes"
	"encoding/binary"
	"fmt"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/hash"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_imp", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_imp")
	d := m.Dict()

	if err := d.SetItem(objects.NewStr("source_hash"), objects.NewBuiltinFunction("source_hash", sourceHash)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("pyc_magic_number_token"), objects.NewInt(int64(marshal.MagicNumber))); err != nil {
		return nil, err
	}
	// check_hash_based_pycs mirrors PyConfig.check_hash_based_pycs. CPython
	// defaults to "default" which means: honor the flag bit in the .pyc
	// header. gopy currently has no override path so always reports the
	// default value.
	//
	// CPython: Python/import.c:4920 imp_module_exec
	if err := d.SetItem(objects.NewStr("check_hash_based_pycs"), objects.NewStr("default")); err != nil {
		return nil, err
	}
	// Lock helpers: gopy serializes imports through Go-side
	// synchronization so these are no-ops, matching how a single-threaded
	// CPython would see them.
	//
	// CPython: Python/import.c:4943 imp_module (lock_held / acquire_lock /
	// release_lock entries)
	if err := d.SetItem(objects.NewStr("lock_held"),
		objects.NewBuiltinFunction("lock_held", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(false), nil
		})); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("acquire_lock"),
		objects.NewBuiltinFunction("acquire_lock", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), nil
		})); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("release_lock"),
		objects.NewBuiltinFunction("release_lock", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), nil
		})); err != nil {
		return nil, err
	}
	// is_builtin(name): 1 when name is in the inittab, else 0. (-1 for a
	// loaded-builtin-on-the-frozen-path edge case never arises here.)
	//
	// CPython: Python/import.c:4720 _imp_is_builtin_impl
	if err := d.SetItem(objects.NewStr("is_builtin"),
		objects.NewBuiltinFunction("is_builtin", isBuiltin)); err != nil {
		return nil, err
	}
	// is_frozen(name): True when name is a frozen module with embedded
	// bytecode.
	//
	// CPython: Python/import.c:4740 _imp_is_frozen_impl
	if err := d.SetItem(objects.NewStr("is_frozen"),
		objects.NewBuiltinFunction("is_frozen", isFrozen)); err != nil {
		return nil, err
	}
	// extension_suffixes(): gopy cannot dynamically load CPython C
	// extension shared objects, so the list of extension suffixes is
	// empty. ExtensionFileLoader is therefore never wired to any suffix
	// in _bootstrap_external._setup.
	//
	// CPython: Python/import.c:4807 _imp_extension_suffixes_impl
	if err := d.SetItem(objects.NewStr("extension_suffixes"),
		objects.NewBuiltinFunction("extension_suffixes", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			suffixes := imp.ExtensionSuffixes()
			items := make([]objects.Object, len(suffixes))
			for i, s := range suffixes {
				items[i] = objects.NewStr(s)
			}
			return objects.NewList(items), nil
		})); err != nil {
		return nil, err
	}
	// find_frozen / get_frozen_object / is_frozen_package bridge to
	// gopy's frozen module table (imp/frozen.go).
	//
	// CPython: Python/import.c:4660 _imp_find_frozen_impl
	// CPython: Python/import.c:4592 _imp_get_frozen_object_impl
	// CPython: Python/import.c:4700 _imp_is_frozen_package_impl
	if err := d.SetItem(objects.NewStr("find_frozen"),
		objects.NewBuiltinFunction("find_frozen", findFrozen)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("get_frozen_object"),
		objects.NewBuiltinFunction("get_frozen_object", getFrozenObject)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("is_frozen_package"),
		objects.NewBuiltinFunction("is_frozen_package", isFrozenPackage)); err != nil {
		return nil, err
	}
	// create_builtin / exec_builtin bridge to the inittab. gopy's
	// initfunc builds a fully-initialized module in one step, so
	// create_builtin runs it and exec_builtin is a no-op.
	//
	// CPython: Python/import.c:4488 _imp_create_builtin
	// CPython: Python/import.c:4540 _imp_exec_builtin_impl
	if err := d.SetItem(objects.NewStr("create_builtin"),
		objects.NewBuiltinFunction("create_builtin", createBuiltin)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("exec_builtin"),
		objects.NewBuiltinFunction("exec_builtin", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewInt(0), nil
		})); err != nil {
		return nil, err
	}
	// create_dynamic / exec_dynamic: gopy cannot load CPython C
	// extension shared objects. Match CPython's failure shape with an
	// ImportError rather than silently succeeding.
	//
	// CPython: Python/import.c:4380 _imp_create_dynamic_impl
	// CPython: Python/import.c:4440 _imp_exec_dynamic_impl
	if err := d.SetItem(objects.NewStr("create_dynamic"),
		objects.NewBuiltinFunction("create_dynamic", createDynamic)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("exec_dynamic"),
		objects.NewBuiltinFunction("exec_dynamic", execDynamic)); err != nil {
		return nil, err
	}
	// _fix_co_filename(code, path): rewrite co_filename on a code object
	// (and its nested code consts) in place.
	//
	// CPython: Python/import.c:4318 _imp__fix_co_filename_impl
	if err := d.SetItem(objects.NewStr("_fix_co_filename"),
		objects.NewBuiltinFunction("_fix_co_filename", fixCoFilename)); err != nil {
		return nil, err
	}
	// _override_frozen_modules_for_tests / _override_multi_interp_extensions_check:
	// test.support.import_helper toggles these around test runs.
	// _override_frozen_modules_for_tests records the override that
	// use_frozen() consults (>0 on, <0 off, 0 default) and returns the
	// previous value, matching the C impl.
	//
	// CPython: Python/import.c:5034 _imp__override_frozen_modules_for_tests_impl
	// CPython: Python/import.c:5052 _imp__override_multi_interp_extensions_check_impl
	if err := d.SetItem(objects.NewStr("_override_frozen_modules_for_tests"),
		objects.NewBuiltinFunction("_override_frozen_modules_for_tests", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			override, err := signedIntArg(args, "_override_frozen_modules_for_tests")
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(imp.SetFrozenOverride(override))), nil
		})); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("_override_multi_interp_extensions_check"),
		objects.NewBuiltinFunction("_override_multi_interp_extensions_check", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			override, err := signedIntArg(args, "_override_multi_interp_extensions_check")
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(imp.SetMultiInterpOverride(override))), nil
		})); err != nil {
		return nil, err
	}
	return m, nil
}

// nameArg pulls a single str positional out of args for the frozen /
// builtin query functions, which all take exactly one module name.
func nameArg(fn string, args []objects.Object) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("TypeError: %s() missing required argument", fn)
	}
	u, ok := args[0].(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: %s() argument must be str, not '%T'", fn, args[0])
	}
	return u.Value(), nil
}

// signedIntArg pulls a single int positional out of args for the
// override toggles, which take one C int. A missing argument defaults
// to 0 (the "use default" override state).
func signedIntArg(args []objects.Object, fn string) (int, error) {
	if len(args) < 1 {
		return 0, nil
	}
	v, ok := args[0].(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s() argument must be int, not '%T'", fn, args[0])
	}
	n, _ := v.Int64()
	return int(n), nil
}

// isBuiltin implements _imp.is_builtin(name).
//
// CPython: Python/import.c:4720 _imp_is_builtin_impl
func isBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	name, err := nameArg("is_builtin", args)
	if err != nil {
		return nil, err
	}
	if imp.IsBuiltinName(name) {
		return objects.NewInt(1), nil
	}
	return objects.NewInt(0), nil
}

// isFrozen implements _imp.is_frozen(name): True only when the name has
// embedded bytecode (a placeholder entry with nil Code is not frozen).
//
// CPython: Python/import.c:4740 _imp_is_frozen_impl
func isFrozen(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	name, err := nameArg("is_frozen", args)
	if err != nil {
		return nil, err
	}
	if !imp.UseFrozen() {
		return objects.NewBool(false), nil
	}
	fm, ok := imp.FindFrozen(name)
	return objects.NewBool(ok && fm.HasCode()), nil
}

// isFrozenPackage implements _imp.is_frozen_package(name).
//
// CPython: Python/import.c:4700 _imp_is_frozen_package_impl
func isFrozenPackage(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	name, err := nameArg("is_frozen_package", args)
	if err != nil {
		return nil, err
	}
	fm, ok := imp.FindFrozen(name)
	if !ok || !fm.HasCode() {
		return nil, fmt.Errorf("ImportError: No such frozen object named %s", name)
	}
	return objects.NewBool(fm.IsPackage), nil
}

// findFrozen implements _imp.find_frozen(name, *, withdata=False). It
// returns a 3-tuple (data, is_package, origname) or None. gopy stores
// frozen modules as code objects, not marshalled blobs, so the data
// slot is always None (FrozenImporter.find_spec discards it and fetches
// the code later via get_frozen_object).
//
// CPython: Python/import.c:4660 _imp_find_frozen_impl
func findFrozen(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	name, err := nameArg("find_frozen", args)
	if err != nil {
		return nil, err
	}
	if !imp.UseFrozen() {
		return objects.None(), nil
	}
	fm, ok := imp.FindFrozen(name)
	if !ok || !fm.HasCode() {
		return objects.None(), nil
	}
	origname, isNone := fm.Origin()
	var origObj objects.Object = objects.None()
	if !isNone {
		origObj = objects.NewStr(origname)
	}
	return objects.NewTuple([]objects.Object{
		objects.None(),
		objects.NewBool(fm.IsPackage),
		origObj,
	}), nil
}

// getFrozenObject implements _imp.get_frozen_object(name, data=None). It
// returns the frozen module's code object.
//
// CPython: Python/import.c:4592 _imp_get_frozen_object_impl
func getFrozenObject(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	name, err := nameArg("get_frozen_object", args)
	if err != nil {
		return nil, err
	}

	// When an explicit data buffer is supplied, CPython unmarshals it
	// directly rather than consulting the frozen table; a buffer that does
	// not decode to a code object raises ImportError "... is invalid".
	if len(args) >= 2 && !objects.IsNone(args[1]) {
		data, err := toBuffer(args[1])
		if err != nil {
			return nil, fmt.Errorf("TypeError: get_frozen_object() argument 2 must be bytes, not '%T'", args[1])
		}
		return unmarshalFrozenData(args[0], data)
	}

	fm, ok := imp.FindFrozen(name)
	if !ok || !fm.HasCode() {
		return nil, fmt.Errorf("ImportError: No such frozen object named %s", name)
	}
	code, err := fm.CodeObject()
	if err != nil {
		return nil, err
	}
	if code == nil {
		return nil, fmt.Errorf("ImportError: No such frozen object named %s", name)
	}
	return code, nil
}

// unmarshalFrozenData ports unmarshal_frozen_code for the explicit-data
// path of get_frozen_object: an empty or non-code or undecodable buffer
// raises ImportError "Frozen object named %R is invalid" (a non-code
// object that decodes cleanly raises TypeError instead).
//
// CPython: Python/import.c unmarshal_frozen_code / set_frozen_error
func unmarshalFrozenData(nameObj objects.Object, data []byte) (objects.Object, error) {
	nameRepr, rerr := objects.Repr(nameObj)
	if rerr != nil {
		return nil, rerr
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ImportError: Frozen object named %s is invalid", nameRepr)
	}
	obj, err := marshal.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ImportError: Frozen object named %s is invalid", nameRepr)
	}
	code, ok := obj.(*objects.Code)
	if !ok {
		return nil, fmt.Errorf("TypeError: frozen object %s is not a code object", nameRepr)
	}
	return code, nil
}

// createDynamic implements _imp.create_dynamic(spec, file=None). gopy
// cannot load CPython C extension shared objects, so the load itself
// fails with ImportError. The spec.name / spec.origin validation that
// _Py_ext_module_loader_info_init_from_spec performs still runs first,
// so a name or origin with an embedded null raises ValueError exactly as
// CPython does before the unsupported-load failure.
//
// CPython: Python/import.c:4743 _imp_create_dynamic_impl
// CPython: Python/importdl.c:115 _Py_ext_module_loader_info_init
func createDynamic(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: create_dynamic() missing required argument 'spec'")
	}
	spec := args[0]

	nameObj, err := objects.GetAttr(spec, objects.NewStr("name"))
	if err != nil {
		return nil, err
	}
	nameStr, ok := nameObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: module name must be a string")
	}
	if err := checkEmbeddedNull(nameStr.Value()); err != nil {
		return nil, err
	}

	originObj, err := objects.GetAttr(spec, objects.NewStr("origin"))
	if err != nil {
		return nil, err
	}
	origin := ""
	if !objects.IsNone(originObj) {
		originStr, ok := originObj.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: module filename must be a string")
		}
		if err := checkEmbeddedNull(originStr.Value()); err != nil {
			return nil, err
		}
		origin = originStr.Value()
	}

	// gopy compiles its extension modules into the runtime as Go builtins
	// rather than dlopening a shared object. When the spec names a
	// registered extension, run its Init (the create+exec phases) behind the
	// PEP 489 multiple-interpreters compat check; otherwise fall back to the
	// "cannot load a C extension" ImportError.
	mod, found, err := imp.CreateExtModule(nameStr.Value(), origin)
	if err != nil {
		return nil, err
	}
	if found {
		return mod, nil
	}

	// No registered extension exposes this name. CPython reaches here after
	// dlopen finds the shared object but no PyInit_<name> symbol, raising
	// ImportError with the missing name stamped on the exception so callers
	// can read exc.name.
	//
	// CPython: Python/importdl.c:178 _PyImport_LoadDynamicModuleWithSpec
	msg := fmt.Sprintf("dynamic module does not define module export function (PyInit_%s)", nameStr.Value())
	exc := pyerrors.New(pyerrors.PyExc_ImportError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
	d := exc.EnsureAttrDict()
	_ = d.SetItem(objects.NewStr("name"), objects.NewStr(nameStr.Value()))
	_ = d.SetItem(objects.NewStr("msg"), objects.NewStr(msg))
	return nil, objects.NewRaisedError(exc, msg)
}

// execDynamic implements _imp.exec_dynamic(module). For a multi-phase
// extension it runs the def's Py_mod_exec slots through PyModule_ExecDef;
// for a single-phase extension (whose body already ran during
// create_dynamic) it is a no-op. It returns 0 on success, matching the C
// impl's int return.
//
// CPython: Python/import.c:4801 _imp_exec_dynamic_impl
// CPython: Objects/moduleobject.c:463 PyModule_ExecDef
func execDynamic(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: exec_dynamic() missing required argument 'mod'")
	}
	if err := imp.ExecExtModule(args[0]); err != nil {
		return nil, err
	}
	return objects.NewInt(0), nil
}

// checkEmbeddedNull mirrors the ValueError CPython raises when encoding a
// str that contains a NUL, the failure path the name / filename encode
// steps in _Py_ext_module_loader_info_init hit for an embedded null.
//
// CPython: Objects/unicodeobject.c PyUnicode_AsUTF8AndSize (embedded null)
func checkEmbeddedNull(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return fmt.Errorf("ValueError: embedded null character")
		}
	}
	return nil
}

// createBuiltin implements _imp.create_builtin(spec). It reads spec.name
// and runs the matching inittab initializer, which builds a fully
// initialized module (gopy has no separate exec phase for builtins).
//
// CPython: Python/import.c:4488 _imp_create_builtin
func createBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: create_builtin() missing required argument 'spec'")
	}
	nameObj, err := objects.GetAttr(args[0], objects.NewStr("name"))
	if err != nil {
		return nil, err
	}
	u, ok := nameObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: spec.name must be str, not '%T'", nameObj)
	}
	name := u.Value()
	initFn := imp.FindInitFunc(name)
	if initFn == nil {
		return nil, fmt.Errorf("ImportError: no built-in module named %s", name)
	}
	mod, err := initFn()
	if err != nil {
		return nil, err
	}
	mod.StampBuiltinModule()
	return mod, nil
}

// fixCoFilename implements _imp._fix_co_filename(code, path). It rewrites
// co_filename on the code object in place.
//
// CPython: Python/import.c:4318 _imp__fix_co_filename_impl
func fixCoFilename(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: _fix_co_filename() takes exactly 2 arguments")
	}
	code, ok := args[0].(*objects.Code)
	if !ok {
		return nil, fmt.Errorf("TypeError: _fix_co_filename() argument 1 must be code, not '%T'", args[0])
	}
	path, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: _fix_co_filename() argument 2 must be str, not '%T'", args[1])
	}
	updateCodeFilenames(code, code.Filename, path.Value())
	return objects.None(), nil
}

// updateCodeFilenames rewrites co_filename to newname on co and on every
// nested code object reachable through co_consts that still carries the
// original oldname. A code compiled with a stale dfile (the .pyc records
// it) gets re-stamped to the real source path on import, including the
// code objects of the functions it defines.
//
// CPython: Python/import.c:4291 update_code_filenames
func updateCodeFilenames(co *objects.Code, oldname, newname string) {
	if co.Filename != oldname {
		return
	}
	co.Filename = newname
	for _, c := range co.Consts {
		if nested, ok := c.(*objects.Code); ok {
			updateCodeFilenames(nested, oldname, newname)
		}
	}
}

// sourceHash mirrors _imp.source_hash(key, source). It hashes the
// source buffer with SipHash-1-3 keyed by `key` and returns the result
// as 8 little-endian bytes.
//
// CPython: Python/import.c:4869 _imp_source_hash_impl
func sourceHash(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: source_hash() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: source_hash() takes 2 positional arguments but %d were given", len(args))
	}
	keyInt, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: source_hash() key must be int, not '%T'", args[0])
	}
	key, ok := keyInt.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: source_hash() key too large for int64")
	}
	src, err := toBuffer(args[1])
	if err != nil {
		return nil, err
	}
	h := hash.KeyedHash(uint64(key), src)
	var out [8]byte
	binary.LittleEndian.PutUint64(out[:], h)
	return objects.NewBytes(out[:]), nil
}

// toBuffer extracts a []byte from a bytes-like object. CPython's
// Py_buffer protocol accepts bytes, bytearray, and any object with a
// readable buffer; the gopy port covers what importlib actually feeds
// in (bytes / bytearray / str).
func toBuffer(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.Unicode:
		s, err := objects.Str(o)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%T'", o)
}
