// Path-based finder. Mirrors the trio of CPython classes that locate
// a module on disk:
//
//	PathFinder         - meta-path entry that walks sys.path
//	FileFinder         - per-directory finder
//	SourceFileLoader   - loads + compiles .py files
//
// The CPython chain sits inside _frozen_importlib_external; the
// classes are full Python objects with caches, namespace-package
// support, and a path_importer_cache hook. gopy ports the slice that
// import statements actually exercise: walk a list of directories,
// look for `<tail>.py` or `<tail>/__init__.py`, hand the file to the
// existing LoadSourceFile path. Caching, .pyc fallback, namespace
// packages, and the path_hooks plumbing land later.
//
// CPython: Lib/importlib/_bootstrap_external.py:1196 PathFinder
// CPython: Lib/importlib/_bootstrap_external.py:1322 FileFinder
// CPython: Lib/importlib/_bootstrap_external.py:962 SourceFileLoader
package imp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tamnd/gopy/objects"
)

// PathFinder is the gopy port of importlib's PathFinder meta-path
// hook. It carries the directory list to search (the equivalent of
// sys.path) plus the SourceCompiler the resulting loader should use.
//
// CPython: Lib/importlib/_bootstrap_external.py:1196 PathFinder
type PathFinder struct {
	// Paths is the ordered directory list. Entries are absolute or
	// relative to the process cwd; empty entries are treated as ".".
	// CPython: Lib/importlib/_bootstrap_external.py:1290 path = sys.path
	Paths []string

	// Compiler is the SourceCompiler used to turn .py source into a
	// code object. It is injected by the runtime to dodge a parser
	// cycle on imp; see loader.go for the same hook on LoadSource.
	// CPython: Python/pythonrun.c:1102 Py_CompileStringExFlags
	Compiler SourceCompiler
}

// errFinderMiss is the sentinel FindModule returns when no path
// finder entry matched the requested module. It wraps
// ErrModuleNotFound so callers that only care about the broad
// "module not found" category keep matching, while the import
// driver can use errors.Is(err, errFinderMiss) to distinguish a
// finder miss from a real loader error whose own chain happens to
// mention ModuleNotFound (e.g. a transitive import that failed).
//
// CPython: Lib/importlib/_bootstrap.py:1184 _find_and_load uses None
// from find_spec for the same purpose.
var errFinderMiss = fmt.Errorf("%w: finder miss", ErrModuleNotFound)

// FindModule walks the directories that should be searched for name
// and either loads the matching source file as a module, returns
// errFinderMiss when no entry matched, or returns the loader's error
// when the file was found but compile/exec failed. Top-level names
// are searched against Paths; dotted names are searched against the
// parent package's __path__, matching CPython's FileFinder behavior.
//
// CPython: Lib/importlib/_bootstrap_external.py:1357 FileFinder.find_spec
// CPython: Lib/importlib/_bootstrap.py:1184 _find_and_load
func (p *PathFinder) FindModule(exec Executor, name string) (*objects.Module, error) {
	if p == nil || p.Compiler == nil {
		return nil, errFinderMiss
	}

	parent, tail := splitParent(name)
	search := p.Paths
	if parent != "" {
		parentMod, ok := GetModule(parent)
		if !ok {
			// Parent package not yet loaded; import it first, mirroring
			// CPython's _find_and_load which calls _find_and_load_unlocked
			// on the parent before loading the child.
			//
			// CPython: Lib/importlib/_bootstrap.py:1227 _find_and_load
			pm, err := ImportModuleLevel(exec, parent, "", 0)
			if err != nil {
				return nil, fmt.Errorf("%w: parent package %q: %w", errFinderMiss, parent, err)
			}
			parentMod = pm
		}
		paths, err := readPackagePath(parentMod)
		if err != nil {
			return nil, err
		}
		search = paths
	}

	for _, entry := range search {
		dir := entry
		if dir == "" {
			dir = "."
		}
		// Package case: <dir>/<tail>/__init__.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1378 cache_module in cache
		pkgDir := filepath.Join(dir, tail)
		pkgInit := filepath.Join(pkgDir, "__init__.py")
		if isFile(pkgInit) {
			mod, err := loadAsPackage(exec, p.Compiler, pkgInit, pkgDir, name)
			if err != nil {
				return nil, err
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
		// Module case: <dir>/<tail>.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1391 suffix loop
		modFile := filepath.Join(dir, tail+".py")
		if isFile(modFile) {
			mod, err := loadAsModule(exec, p.Compiler, modFile, name, parent)
			if err != nil {
				return nil, err
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errFinderMiss, name)
}

// bindOnParent installs child as an attribute on the parent package's
// module dict. Mirrors the setattr step _find_and_load_unlocked runs
// after a successful submodule load so `import a.b` makes `a.b`
// resolve as an attribute on `a`. Errors are swallowed to match
// CPython, which also catches AttributeError around the setattr.
//
// CPython: Lib/importlib/_bootstrap.py:1234 setattr(parent_module, child, module)
func bindOnParent(parent, tail string, child *objects.Module) {
	if parent == "" {
		return
	}
	pm, ok := GetModule(parent)
	if !ok {
		return
	}
	_ = pm.Dict().SetItem(objects.NewStr(tail), child)
}

// splitParent splits a dotted module name into (parent, tail).
// "unittest.result" -> ("unittest", "result"); "unittest" -> ("", "unittest").
//
// CPython: Lib/importlib/_bootstrap.py:1208 fullname.rpartition('.')
func splitParent(name string) (parent, tail string) {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return "", name
	}
	return name[:dot], name[dot+1:]
}

// readPackagePath returns the parent package's __path__ as a slice
// of directory strings. Mirrors how _bootstrap._find_and_load reads
// `parent.__path__` before invoking the path finders.
//
// CPython: Lib/importlib/_bootstrap.py:1227 path = parent_module.__path__
func readPackagePath(mod *objects.Module) ([]string, error) {
	name := moduleName(mod)
	pathObj, err := mod.Dict().GetItem(objects.NewStr("__path__"))
	if err != nil || pathObj == nil {
		return nil, fmt.Errorf("%w: package %q has no __path__", ErrModuleNotFound, name)
	}
	out := []string{}
	switch v := pathObj.(type) {
	case *objects.List:
		for i := 0; i < v.Len(); i++ {
			s, ok := v.Item(i).(*objects.Unicode)
			if !ok {
				continue
			}
			out = append(out, s.Value())
		}
	case *objects.Tuple:
		for i := 0; i < v.Len(); i++ {
			s, ok := v.Item(i).(*objects.Unicode)
			if !ok {
				continue
			}
			out = append(out, s.Value())
		}
	default:
		return nil, fmt.Errorf("%w: package %q __path__ is %T, want list/tuple", ErrModuleNotFound, name, pathObj)
	}
	return out, nil
}

// moduleName reads __name__ from the module dict. Cheaper than
// adding a Module.Name accessor in objects/ for one caller.
func moduleName(mod *objects.Module) string {
	v, err := mod.Dict().GetItem(objects.NewStr("__name__"))
	if err != nil || v == nil {
		return "<unnamed>"
	}
	if s, ok := v.(*objects.Unicode); ok {
		return s.Value()
	}
	return "<unnamed>"
}

// loadAsPackage is LoadSourceFile plus the package dunder slice
// (`__file__`, `__path__`, `__package__`). __path__ is set before
// the body runs because `__init__.py` typically does
// `from .submod import x`, which immediately consults the parent's
// __path__.
//
// CPython: Lib/importlib/_bootstrap_external.py:962 SourceFileLoader.exec_module
// CPython: Lib/importlib/_bootstrap.py:516 _init_module_attrs
func loadAsPackage(exec Executor, compiler SourceCompiler, initFile, pkgDir, name string) (*objects.Module, error) {
	mod, exists := GetModule(name)
	if !exists {
		mod = objects.NewModule(name)
	}
	d := mod.Dict()
	if err := d.SetItem(objects.NewStr("__file__"), objects.NewStr(initFile)); err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: __file__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__path__"),
		objects.NewList([]objects.Object{objects.NewStr(pkgDir)})); err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: __path__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__package__"), objects.NewStr(name)); err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: __package__: %w", name, err)
	}
	AddModule(name, mod)

	src, err := os.ReadFile(initFile) //nolint:gosec // initFile is filepath.Join of a trusted PathFinder.Paths entry.
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: %w", name, err)
	}
	code, err := compiler(src, initFile)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: compile: %w", name, err)
	}
	if _, err := exec.ExecCode(code, mod); err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsPackage %q: exec: %w", name, err)
	}
	// CPython: Python/import.c:2715 exec_code_in_module re-reads
	// sys.modules so an `__init__.py` that reassigns its own entry
	// (rare for packages, but the same shape as decimal/_pydecimal).
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return mod, nil
}

// loadAsModule is the flat-file equivalent: load source, set
// __file__ and __package__ (which is the parent dotted name, or ""
// for top-level), then exec.
//
// CPython: Lib/importlib/_bootstrap.py:516 _init_module_attrs
func loadAsModule(exec Executor, compiler SourceCompiler, file, name, parent string) (*objects.Module, error) {
	mod, exists := GetModule(name)
	if !exists {
		mod = objects.NewModule(name)
	}
	d := mod.Dict()
	if err := d.SetItem(objects.NewStr("__file__"), objects.NewStr(file)); err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: __file__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__package__"), objects.NewStr(parent)); err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: __package__: %w", name, err)
	}
	AddModule(name, mod)

	src, err := os.ReadFile(file) //nolint:gosec // file is filepath.Join of a trusted PathFinder.Paths entry.
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: %w", name, err)
	}
	code, err := compiler(src, file)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: compile: %w", name, err)
	}
	if _, err := exec.ExecCode(code, mod); err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsModule %q: exec: %w", name, err)
	}
	// CPython: Python/import.c:2715 exec_code_in_module re-reads
	// sys.modules so a module body that reassigns its own entry
	// (`sys.modules[__name__] = other`, e.g. decimal/_pydecimal) wins.
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return mod, nil
}

// isFile reports whether path exists and is a regular file. It is the
// gopy stand-in for importlib's _path_isfile helper.
//
// CPython: Lib/importlib/_bootstrap_external.py:159 _path_isfile
func isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

var (
	pathFinderMu sync.RWMutex
	pathFinder   *PathFinder
)

// SetPathFinder installs the package-level PathFinder consulted by
// ImportModuleLevel after the inittab miss. Callers (lifecycle,
// cmd/gopy, tests) build a PathFinder with the desired Paths and
// Compiler and pass it here once at startup.
//
// Passing nil clears the finder, which is useful for tests that want
// to confirm an import fails when path-based lookup is disabled.
func SetPathFinder(f *PathFinder) {
	pathFinderMu.Lock()
	pathFinder = f
	pathFinderMu.Unlock()
}

// GetPathFinder returns the currently installed PathFinder, or nil.
func GetPathFinder() *PathFinder {
	pathFinderMu.RLock()
	f := pathFinder
	pathFinderMu.RUnlock()
	return f
}
