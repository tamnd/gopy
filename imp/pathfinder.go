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

// FindModule walks the directories that should be searched for name
// and either loads the matching source file as a module or returns
// ErrModuleNotFound. Top-level names are searched against Paths;
// dotted names are searched against the parent package's __path__,
// matching CPython's FileFinder behavior.
//
// CPython: Lib/importlib/_bootstrap_external.py:1357 FileFinder.find_spec
// CPython: Lib/importlib/_bootstrap.py:1184 _find_and_load
func (p *PathFinder) FindModule(exec Executor, name string) (*objects.Module, error) {
	if p == nil || p.Compiler == nil {
		return nil, ErrModuleNotFound
	}

	parent, tail := splitParent(name)
	search := p.Paths
	if parent != "" {
		parentMod, ok := GetModule(parent)
		if !ok {
			return nil, fmt.Errorf("%w: parent package %q is not in sys.modules", ErrModuleNotFound, parent)
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
			return loadAsPackage(exec, p.Compiler, pkgInit, pkgDir, name)
		}
		// Module case: <dir>/<tail>.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1391 suffix loop
		modFile := filepath.Join(dir, tail+".py")
		if isFile(modFile) {
			return loadAsModule(exec, p.Compiler, modFile, name, parent)
		}
	}
	return nil, fmt.Errorf("%w: No module named %q", ErrModuleNotFound, name)
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
	code, err := compiler(string(src), initFile)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: compile: %w", name, err)
	}
	if _, err := exec.ExecCode(code, mod); err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsPackage %q: exec: %w", name, err)
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
	code, err := compiler(string(src), file)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: compile: %w", name, err)
	}
	if _, err := exec.ExecCode(code, mod); err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsModule %q: exec: %w", name, err)
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
