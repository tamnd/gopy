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

// FindModule walks Paths looking for a source file that matches name.
// Returns an executed module or ErrModuleNotFound when no match
// surfaces. The lookup mirrors FileFinder.find_spec's per-directory
// scan: prefer a package (a directory containing __init__.py), then
// fall back to a flat <tail>.py source file.
//
// CPython: Lib/importlib/_bootstrap_external.py:1357 FileFinder.find_spec
func (p *PathFinder) FindModule(exec Executor, name string) (*objects.Module, error) {
	if p == nil || p.Compiler == nil {
		return nil, ErrModuleNotFound
	}
	tail := name
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		tail = name[dot+1:]
	}
	for _, entry := range p.Paths {
		dir := entry
		if dir == "" {
			dir = "."
		}
		// Package case: <dir>/<tail>/__init__.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1378 cache_module in cache
		pkgInit := filepath.Join(dir, tail, "__init__.py")
		if isFile(pkgInit) {
			return LoadSourceFile(exec, p.Compiler, pkgInit, name)
		}
		// Module case: <dir>/<tail>.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1391 suffix loop
		modFile := filepath.Join(dir, tail+".py")
		if isFile(modFile) {
			return LoadSourceFile(exec, p.Compiler, modFile, name)
		}
	}
	return nil, fmt.Errorf("%w: No module named %q", ErrModuleNotFound, name)
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
