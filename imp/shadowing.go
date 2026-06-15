package imp

import (
	"fmt"
	"os"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// optionalAttr ports PyObject_GetOptionalAttr for the shadowing helpers:
// it returns (val, true, nil) on success and (nil, false, nil) when the
// attribute is missing (AttributeError). Any non-AttributeError failure
// propagates as the third return.
//
// CPython: Objects/object.c:1324 PyObject_GetOptionalAttr
func optionalAttr(o objects.Object, name string) (objects.Object, bool, error) {
	v, err := objects.GetAttr(o, objects.NewStr(name))
	if err == nil {
		return v, true, nil
	}
	if strings.Contains(err.Error(), "AttributeError") {
		return nil, false, nil
	}
	return nil, false, err
}

// SpecFileOrigin ports _PyModuleSpec_GetFileOrigin: returns the spec's
// origin string only when spec.has_location is truthy and spec.origin is
// a str. The bool reports whether a location origin was found.
//
// CPython: Objects/moduleobject.c:892 _PyModuleSpec_GetFileOrigin
func SpecFileOrigin(spec objects.Object) (string, bool, error) {
	hasLoc, found, err := optionalAttr(spec, "has_location")
	if err != nil || !found {
		return "", false, err
	}
	if !objects.IsTrue(hasLoc) {
		return "", false, nil
	}
	originObj, found, err := optionalAttr(spec, "origin")
	if err != nil || !found {
		return "", false, err
	}
	origin, ok := originObj.(*objects.Unicode)
	if !ok {
		return "", false, nil
	}
	return origin.Value(), true, nil
}

// SpecIsInitializing ports _PyModuleSpec_IsInitializing: spec._initializing
// is truthy.
//
// CPython: Objects/moduleobject.c:858 _PyModuleSpec_IsInitializing
func SpecIsInitializing(spec objects.Object) (bool, error) {
	v, found, err := optionalAttr(spec, "_initializing")
	if err != nil || !found {
		return false, err
	}
	return objects.IsTrue(v), nil
}

// SpecIsUninitializedSubmodule ports _PyModuleSpec_IsUninitializedSubmodule:
// name is currently mid-import as a submodule, i.e. it appears in
// spec._uninitialized_submodules. A missing list reads as "not a submodule".
//
// CPython: Objects/moduleobject.c:876 _PyModuleSpec_IsUninitializedSubmodule
func SpecIsUninitializedSubmodule(spec objects.Object, name string) (bool, error) {
	if spec == nil || objects.IsNone(spec) {
		return false, nil
	}
	v, found, err := optionalAttr(spec, "_uninitialized_submodules")
	if err != nil || !found {
		return false, err
	}
	contains, err := objects.Contains(v, objects.NewStr(name))
	if err != nil {
		return false, err
	}
	return contains, nil
}

// ModuleIsPossiblyShadowing ports _PyModule_IsPossiblyShadowing: the
// module at origin could shadow a same-named module later on the search
// path. The check is: not sys.flags.safe_path and
// dirname(origin minus a trailing /__init__.py) == (sys.path[0] or cwd).
//
// CPython: Objects/moduleobject.c:923 _PyModule_IsPossiblyShadowing
func ModuleIsPossiblyShadowing(originFound bool, origin string) (bool, error) {
	if !originFound {
		return false, nil
	}
	if safePathEnabled() {
		return false, nil
	}
	root := origin
	sep := strings.LastIndex(root, string(os.PathSeparator))
	if sep < 0 {
		return false, nil
	}
	// A package origin ends in <sep>__init__.py; step one directory up.
	if root[sep+1:] == "__init__.py" {
		root = root[:sep]
		sep = strings.LastIndex(root, string(os.PathSeparator))
		if sep < 0 {
			return false, nil
		}
	}
	root = root[:sep]

	sysPath0, ok := sysPathZero()
	if !ok {
		return false, nil
	}
	if sysPath0 == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return false, nil
		}
		sysPath0 = cwd
	}
	return sysPath0 == root, nil
}

// safePathEnabled reports whether sys.flags.safe_path is truthy.
//
// CPython: Objects/moduleobject.c:937 config->safe_path
func safePathEnabled() bool {
	sysMod, ok := GetModule("sys")
	if !ok {
		return false
	}
	flags, err := objects.GetAttr(sysMod, objects.NewStr("flags"))
	if err != nil {
		return false
	}
	sp, err := objects.GetAttr(flags, objects.NewStr("safe_path"))
	if err != nil {
		return false
	}
	return objects.IsTrue(sp)
}

// configSysPath0 holds the startup-captured leading sys.path entry, the
// equivalent of CPython's config->sys_path_0. The shadowing check uses
// this snapshot, NOT live sys.path[0], so a script that mutates sys.path
// after startup does not change shadowing detection.
//
// CPython: Python/initconfig.c config->sys_path_0
var (
	configSysPath0    string
	configSysPath0Set bool
)

// SetConfigSysPath0 records the startup leading sys.path entry. The bool
// reports whether the interpreter installed one at all (false under
// safe_path, where CPython leaves config->sys_path_0 NULL).
func SetConfigSysPath0(path string, present bool) {
	configSysPath0 = path
	configSysPath0Set = present
}

// sysPathZero returns config->sys_path_0. The bool is false when no
// leading entry was captured (e.g. safe_path).
//
// CPython: Objects/moduleobject.c:967 config->sys_path_0
func sysPathZero() (string, bool) {
	if !configSysPath0Set {
		return "", false
	}
	return configSysPath0, true
}

// StdlibModuleNamesContains reports whether modName is in
// sys.stdlib_module_names. modName is passed as the live object (not a
// Go string) so an unhashable __name__ raises through PySet_Contains
// exactly as CPython does. The lookup is silent when stdlib_module_names
// is missing or is not a set/frozenset (PyAnySet_Check guards the call).
//
// CPython: Objects/moduleobject.c:1059 PySet_Contains(stdlib_modules, mod_name)
func StdlibModuleNamesContains(modName objects.Object) (bool, error) {
	sysMod, ok := GetModule("sys")
	if !ok {
		return false, nil
	}
	namesObj, found, err := optionalAttr(sysMod, "stdlib_module_names")
	if err != nil || !found {
		return false, nil
	}
	if !anySetCheck(namesObj) {
		return false, nil
	}
	contains, err := objects.Contains(namesObj, modName)
	if err != nil {
		return false, err
	}
	return contains, nil
}

// anySetCheck ports PyAnySet_Check: the object is a set or frozenset (or
// a subclass of either).
//
// CPython: Include/cpython/setobject.h PyAnySet_Check
func anySetCheck(o objects.Object) bool {
	t := o.Type()
	return objects.IsSubtype(t, objects.SetType) || objects.IsSubtype(t, objects.FrozensetType)
}

// moduleGetattrError ports the error tail of _Py_module_getattro_impl:
// after a generic-attribute miss with no PEP 562 __getattr__, it builds
// the best-effort AttributeError, surfacing the stdlib-shadowing and
// circular-import hints. It returns a Go error whose message the objects
// layer synthesizes into the AttributeError. It is wired into
// objects.ModuleAttrErrorHook so module.go can reach the import system's
// spec helpers without an import cycle.
//
// CPython: Objects/moduleobject.c:1024 _Py_module_getattro_impl (error tail)
func moduleGetattrError(m *objects.Module, name string) error {
	d := m.Dict()

	// __name__ must be a str (or str subclass); otherwise the generic
	// "module has no attribute" message applies. CPython uses PyUnicode_Check
	// here, so a str subclass passes.
	modNameObj, _ := d.GetItem(objects.NewStr("__name__"))
	if modNameObj == nil || !objects.IsSubtype(modNameObj.Type(), objects.StrType()) {
		return fmt.Errorf("AttributeError: module has no attribute '%s'", name)
	}
	modName := unicodeContents(modNameObj)
	nameQ := quoteU(name)
	modQ := quoteU(modName)

	spec, serr := d.GetItem(objects.NewStr("__spec__"))
	if serr != nil || spec == nil || objects.IsNone(spec) {
		return fmt.Errorf("AttributeError: module %s has no attribute %s", modQ, nameQ)
	}

	origin, originFound, oerr := SpecFileOrigin(spec)
	if oerr != nil {
		return oerr
	}
	shadowing, sherr := ModuleIsPossiblyShadowing(originFound, origin)
	if sherr != nil {
		return sherr
	}
	shadowingStdlib := false
	if shadowing {
		c, cerr := StdlibModuleNamesContains(modNameObj)
		if cerr != nil {
			return cerr
		}
		shadowingStdlib = c
	}

	if shadowingStdlib {
		return fmt.Errorf("AttributeError: module %s has no attribute %s (consider renaming %s since it has the same name as the standard library module named %s and prevents importing that standard library module)",
			modQ, nameQ, quoteU(origin), modQ)
	}

	initializing, ierr := SpecIsInitializing(spec)
	if ierr != nil {
		return ierr
	}
	switch {
	case initializing && shadowing:
		return fmt.Errorf("AttributeError: module %s has no attribute %s (consider renaming %s if it has the same name as a library you intended to import)",
			modQ, nameQ, quoteU(origin))
	case initializing && originFound:
		return fmt.Errorf("AttributeError: partially initialized module %s from %s has no attribute %s (most likely due to a circular import)",
			modQ, quoteU(origin), nameQ)
	case initializing:
		return fmt.Errorf("AttributeError: partially initialized module %s has no attribute %s (most likely due to a circular import)",
			modQ, nameQ)
	}

	// Not initializing: the miss is a circular import only if the name is a
	// submodule currently mid-load (tracked on spec._uninitialized_submodules).
	//
	// CPython: Objects/moduleobject.c:1116 _PyModuleSpec_IsUninitializedSubmodule
	uninit, uerr := SpecIsUninitializedSubmodule(spec, name)
	if uerr != nil {
		return uerr
	}
	if uninit {
		return fmt.Errorf("AttributeError: cannot access submodule %s of module %s (most likely due to a circular import)",
			nameQ, modQ)
	}
	return fmt.Errorf("AttributeError: module %s has no attribute %s", modQ, nameQ)
}

// unicodeContents returns the string contents of a str (or str subclass)
// object, mirroring how CPython's %U formats a PyUnicode payload.
func unicodeContents(o objects.Object) string {
	if u, ok := o.(*objects.Unicode); ok {
		return u.Value()
	}
	if s, err := objects.Str(o); err == nil {
		return s
	}
	return ""
}

// quoteU wraps s in single quotes, matching the literal 'quotes' the
// CPython getattro format strings put around each %U substitution.
func quoteU(s string) string { return "'" + s + "'" }

// init wires the module-getattro error builder into the objects package
// so module attribute misses surface the import-system shadowing hints.
func init() {
	objects.ModuleAttrErrorHook = moduleGetattrError
}
