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

	// 1. sys.modules cache.
	// CPython: Python/import.c:L1613 sys_modules_get_dict
	if mod, ok := GetModule(absName); ok {
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
		AddModule(absName, mod)
		return mod, nil
	}

	// 4. Path-based finder (sys.path).
	// CPython: Lib/importlib/_bootstrap_external.py:1284 PathFinder.find_spec
	if f := GetPathFinder(); f != nil {
		mod, err := f.FindModule(exec, absName)
		if err == nil {
			return mod, nil
		}
		if !errors.Is(err, ErrModuleNotFound) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: No module named %q", ErrModuleNotFound, absName)
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
		return "", fmt.Errorf("imp: attempted relative import with no known parent package")
	}
	// Walk up level-1 dots from pkgname.
	// CPython: Python/import.c:L1597 strip up to `level` components
	pkg := pkgname
	for i := 1; i < level; i++ {
		dot := strings.LastIndex(pkg, ".")
		if dot < 0 {
			return "", fmt.Errorf("imp: attempted relative import beyond top-level package")
		}
		pkg = pkg[:dot]
	}
	if name == "" {
		return pkg, nil
	}
	return pkg + "." + name, nil
}
