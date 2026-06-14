// ImportModuleLevel: the primary entry point for the import statement.
// Mirrors PyImport_ImportModuleLevelObject: check sys.modules, then
// try frozen modules, then built-in (inittab) modules. File-based
// loading via sys.path finders is deferred to the importlib bootstrap.
//
// CPython: Python/import.c:L1561 PyImport_ImportModuleLevelObject
// CPython: Python/import.c:L1450 PyImport_ImportModule
package imp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// ErrModuleNotFound is returned when no finder can locate the named module.
var ErrModuleNotFound = fmt.Errorf("imp: ModuleNotFoundError")

// ImportModule performs an absolute import of name. It is the
// zero-level convenience wrapper around ImportModuleLevel.
//
// CPython: Python/import.c:L1450 PyImport_ImportModule
func ImportModule(exec Executor, name string) (*objects.Module, error) {
	return ImportModuleLevel(exec, name, "", 0)
}

// ImportModuleLevel imports name relative to pkgname at the given
// level. level=0 is an absolute import; level>0 is relative.
//
// The lookup order is:
//  1. sys.modules cache
//  2. Frozen modules (FrozenModule table)
//  3. Built-in modules (Inittab)
//  4. ErrModuleNotFound
//
// File-based imports via sys.path and the importlib finder chain are
// resolved by the importlib bootstrap (imp/bootstrap.go), which
// registers a custom __import__ hook after it initializes.
//
// CPython: Python/import.c:L1561 PyImport_ImportModuleLevelObject
func ImportModuleLevel(exec Executor, name, pkgname string, level int) (*objects.Module, error) {
	absName, err := resolveAbsName(name, pkgname, level)
	if err != nil {
		return nil, err
	}

	// 1. sys.modules cache. When the entry is None the import is intentionally
	// blocked (e.g. import_helper.import_fresh_module sets sys.modules[name]=None
	// to prevent built-in modules from loading). Raise ImportError immediately
	// the same way CPython does.
	//
	// CPython: Lib/importlib/_bootstrap.py:1390 _find_and_load (None sentinel)
	// CPython: Python/import.c:L1613 sys_modules_get_dict
	if raw, present := GetModuleRaw(absName); present {
		if objects.IsNone(raw) {
			return nil, fmt.Errorf("ImportError: import of %q halted; None in sys.modules", absName)
		}
		if mod, ok := raw.(*objects.Module); ok {
			return mod, nil
		}
	}

	// 1b. Custom sys.meta_path finders. CPython's _find_spec walks
	// sys.meta_path in order; the BuiltinImporter, FrozenImporter and
	// PathFinder entries are realized by the Go steps below, so here we
	// consult only the additional finders a program (or a test) inserts.
	// A finder inserted at meta_path[0] therefore wins over the built-in
	// and frozen lookups, matching CPython's ordering.
	//
	// CPython: Lib/importlib/_bootstrap.py:912 _find_spec
	if mod, found, err := metaPathFind(exec, absName); err != nil {
		return nil, err
	} else if found {
		return mod, nil
	}

	// 2. Frozen module.
	// CPython: Python/import.c:L1632 import_find_and_load
	if fm, ok := FindFrozen(absName); ok && fm.Code != nil {
		return ExecCodeModule(exec, absName, fm.Code)
	}

	// 3. Built-in module (inittab).
	// CPython: Python/import.c:L1635 _PyImport_FindBuiltin
	if initFn := FindInitFunc(absName); initFn != nil {
		mod, err := initFn()
		if err != nil {
			return nil, fmt.Errorf("imp: init %q: %w", absName, err)
		}
		// Stamp m_module on every BuiltinFunction registered into
		// the module's dict, the same way PyModule_AddFunctions
		// passes the module name to PyCFunction_NewEx. Pickle's
		// whichmodule reads __module__ off codecs.encode and
		// friends to resolve save_global to the right import path.
		//
		// CPython: Objects/moduleobject.c:606 PyModule_AddFunctions
		mod.StampBuiltinModule()
		AddModule(absName, mod)
		// CPython's BuiltinImporter sets __spec__/__loader__ on every
		// built-in module; gopy's inittab path mirrors that so tools
		// (pyclbr, runpy, inspect) that read module.__spec__ work.
		//
		// CPython: Lib/importlib/_bootstrap.py:736 BuiltinImporter.exec_module
		AttachBuiltinSpec(exec, mod, absName)
		return mod, nil
	}

	// 4. Path-based finder (sys.path).
	// CPython: Lib/importlib/_bootstrap_external.py:1284 PathFinder.find_spec
	//
	// errFinderMiss means "no entry matched"; we fall through to the
	// final not-found below. Any other error (compile error, exec
	// error, transitive import failure) is the loader's verdict and
	// propagates as-is.
	if f := GetPathFinder(); f != nil {
		mod, err := f.FindModule(exec, absName)
		if err == nil {
			return mod, nil
		}
		if !errors.Is(err, errFinderMiss) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: No module named %q", ErrModuleNotFound, absName)
}

// metaPathFind consults the custom finders on sys.meta_path for absName.
// It skips the BuiltinImporter, FrozenImporter and PathFinder entries
// (identified by their class __name__), which gopy realizes in Go, and
// calls find_spec(name, path, None) on every other finder. The first
// finder that returns a spec drives loadFromSpec; a None return means the
// finder declined and the walk continues.
//
// CPython: Lib/importlib/_bootstrap.py:912 _find_spec
func metaPathFind(exec Executor, absName string) (*objects.Module, bool, error) {
	sysMod, ok := GetModule("sys")
	if !ok {
		return nil, false, nil
	}
	mpObj, _ := sysMod.Dict().GetItem(objects.NewStr("meta_path"))
	mp, _ := mpObj.(*objects.List)
	if mp == nil || mp.Len() == 0 {
		return nil, false, nil
	}
	// The parent package's __path__ becomes the `path` argument for a
	// submodule import, mirroring _find_and_load's parent.__path__ read.
	//
	// CPython: Lib/importlib/_bootstrap.py:1227 path = parent_module.__path__
	pathArg := objects.None()
	if parent, _ := splitParent(absName); parent != "" {
		if pm, ok := GetModule(parent); ok {
			if pp, err := pm.Dict().GetItem(objects.NewStr("__path__")); err == nil && pp != nil {
				pathArg = pp
			}
		}
	}
	nameObj := objects.NewStr(absName)
	for i := 0; i < mp.Len(); i++ {
		finder := mp.Item(i)
		if isBuiltinFinder(finder) {
			continue
		}
		findSpec, err := objects.GetAttr(finder, objects.NewStr("find_spec"))
		if err != nil {
			// A legacy finder without find_spec does not participate;
			// CPython's _find_spec skips it the same way.
			continue
		}
		spec, err := objects.Call(findSpec, objects.NewTuple([]objects.Object{nameObj, pathArg, objects.None()}), nil)
		if err != nil {
			return nil, false, err
		}
		if spec == nil || objects.IsNone(spec) {
			continue
		}
		mod, err := loadFromSpec(exec, absName, spec)
		if err != nil {
			return nil, false, err
		}
		parent, tail := splitParent(absName)
		bindOnParent(parent, tail, mod)
		return mod, true, nil
	}
	return nil, false, nil
}

// isBuiltinFinder reports whether finder is one of the three importers
// gopy realizes in Go (BuiltinImporter, FrozenImporter, PathFinder).
// Those are class objects exposing __name__; the custom finders programs
// install on meta_path are instances that do not.
//
// CPython: Lib/importlib/_bootstrap.py:736 BuiltinImporter / :976 PathFinder
func isBuiltinFinder(finder objects.Object) bool {
	nameAttr, err := objects.GetAttr(finder, objects.NewStr("__name__"))
	if err != nil {
		return false
	}
	name, ok := nameAttr.(*objects.Unicode)
	if !ok {
		return false
	}
	switch name.Value() {
	case "BuiltinImporter", "FrozenImporter", "PathFinder", "WindowsRegistryFinder":
		return true
	}
	return false
}

// resolveAbsName converts a relative import (level > 0) to an
// absolute module name using pkgname as the anchor.
//
// CPython: Python/import.c:L1572 resolve_name
func resolveAbsName(name, pkgname string, level int) (string, error) {
	if level == 0 {
		return name, nil
	}
	if pkgname == "" {
		// CPython: Python/import.c:3708 ImportError raised by resolve_name
		return "", fmt.Errorf("ImportError: attempted relative import with no known parent package")
	}
	// Walk up level-1 dots from pkgname.
	// CPython: Python/import.c:L1597 strip up to `level` components
	pkg := pkgname
	for i := 1; i < level; i++ {
		dot := strings.LastIndex(pkg, ".")
		if dot < 0 {
			// CPython: Python/import.c:3689 ImportError raised by resolve_name
			return "", fmt.Errorf("ImportError: attempted relative import beyond top-level package")
		}
		pkg = pkg[:dot]
	}
	if name == "" {
		return pkg, nil
	}
	return pkg + "." + name, nil
}
