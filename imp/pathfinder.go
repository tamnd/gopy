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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tamnd/gopy/marshal"
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
	// When LivePathHook is installed, the hook wins so user-code
	// mutations of sys.path are honored.
	// CPython: Lib/importlib/_bootstrap_external.py:1290 path = sys.path
	Paths []string

	// Compiler is the SourceCompiler used to turn .py source into a
	// code object. It is injected by the runtime to dodge a parser
	// cycle on imp; see loader.go for the same hook on LoadSource.
	// CPython: Python/pythonrun.c:1102 Py_CompileStringExFlags
	Compiler SourceCompiler
}

// LivePathHook returns the live sys.path entries. module/sys installs
// it on init so PathFinder honors sys.path.insert / append after
// startup. nil means "use the static Paths snapshot", which is the
// pre-existing behavior tests rely on.
//
// CPython: Lib/importlib/_bootstrap_external.py:1290 path = sys.path
var LivePathHook func() []string

// SetLivePathHook installs (or clears) the hook used by FindModule to
// fetch the live sys.path. nil restores the static-snapshot behavior.
func SetLivePathHook(f func() []string) {
	LivePathHook = f
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
	if parent == "" && LivePathHook != nil {
		if live := LivePathHook(); live != nil {
			search = live
		}
	}
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
				// A parent that was located but raised while executing its
				// __init__ must surface that exception verbatim (CPython
				// propagates it from _find_and_load), so do not relabel it
				// as a finder miss. Only a genuine parent-not-found is a
				// miss the child lookup can recover from.
				//
				// CPython: Lib/importlib/_bootstrap.py:1227 _find_and_load
				if errors.Is(err, ErrModuleExecFailed) || !errors.Is(err, ErrModuleNotFound) {
					return nil, err
				}
				return nil, fmt.Errorf("%w: parent package %q: %w", errFinderMiss, parent, err)
			}
			parentMod = pm
		}
		// Importing the parent package may have imported this child as a side
		// effect (e.g. the parent's __init__ ran `from .child import ...`),
		// caching it in sys.modules and possibly rebinding the parent's
		// attribute to something other than the submodule. In that case CPython
		// returns the already-cached child and never reloads or re-binds it, so
		// the parent's rebinding survives.
		//
		// CPython: Lib/importlib/_bootstrap.py:1290 _find_and_load_unlocked
		if cached, ok := GetModule(name); ok {
			return cached, nil
		}
		paths, err := readPackagePath(parentMod)
		if err != nil {
			return nil, err
		}
		search = paths
	}

	// PEP 420: a directory matching the tail with no __init__.py and no
	// flat-file match is a namespace portion. CPython's PathFinder
	// accumulates portions across every path entry and, only after no
	// regular module is found anywhere, builds a namespace package whose
	// __path__ is the collected portions.
	//
	// CPython: Lib/importlib/_bootstrap_external.py:1430 FileFinder.find_spec
	// (namespace portion path) / Lib/importlib/_bootstrap.py:1167 PathFinder
	var namespacePortions []string
	for _, entry := range search {
		dir := entry
		if dir == "" {
			dir = "."
		}
		// A sys.path entry that is not a directory (a zip archive, or a
		// path that points inside one) is handled by a custom importer
		// registered on sys.path_hooks, exactly as CPython's PathFinder
		// routes such entries through zipimport.zipimporter. Only consult
		// the hooks for non-directories so the directory scan below stays
		// the fast path for the common case.
		//
		// CPython: Lib/importlib/_bootstrap_external.py:1236 _path_importer_cache
		if !isDir(dir) {
			spec, handled, herr := pathHookSpec(exec, entry, name)
			if herr != nil {
				return nil, herr
			}
			if !handled {
				continue
			}
			// A namespace spec from the importer (loader None, search
			// locations set) is a PEP 420 portion: collect it and keep
			// scanning, exactly as CPython's PathFinder extends
			// namespace_path instead of returning. A spec with a real
			// loader is a concrete module, so load and return it.
			//
			// CPython: Lib/importlib/_bootstrap_external.py:1284 PathFinder._get_spec
			if portions, isNS := namespacePortionsOf(spec); isNS {
				namespacePortions = append(namespacePortions, portions...)
				continue
			}
			mod, lerr := loadFromSpec(exec, name, spec)
			if lerr != nil {
				return nil, lerr
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
		// Package case: <dir>/<tail>/__init__.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1378 cache_module in cache
		pkgDir := filepath.Join(dir, tail)
		pkgInit := filepath.Join(pkgDir, "__init__.py")
		if isFile(pkgInit) && caseOK(pkgDir) {
			mod, err := loadAsPackage(exec, p.Compiler, pkgInit, pkgDir, name)
			if err != nil {
				return nil, err
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
		// Sourceless package: <dir>/<tail>/__init__.pyc. CPython's
		// FileFinder walks its loader suffixes for __init__, so the
		// bytecode loader is tried after the source loader.
		// CPython: Lib/importlib/_bootstrap_external.py:1424 __init__ suffix loop
		pkgInitPyc := filepath.Join(pkgDir, "__init__.pyc")
		if isFile(pkgInitPyc) && caseOK(pkgDir) {
			mod, err := loadAsPackageBytecode(exec, pkgInitPyc, pkgDir, name)
			if err != nil {
				return nil, err
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
		// Module case: <dir>/<tail>.py.
		// CPython: Lib/importlib/_bootstrap_external.py:1391 suffix loop
		modFile := filepath.Join(dir, tail+".py")
		if isFile(modFile) && caseOK(modFile) {
			mod, err := loadAsModule(exec, p.Compiler, modFile, name, parent)
			if err != nil {
				return nil, err
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
		// Sourceless module: <dir>/<tail>.pyc, loaded by the bytecode
		// loader once the source suffix has missed.
		// CPython: Lib/importlib/_bootstrap_external.py:1215 SourcelessFileLoader
		modPyc := filepath.Join(dir, tail+".pyc")
		if isFile(modPyc) && caseOK(modPyc) {
			mod, err := loadAsModuleBytecode(exec, modPyc, name, parent)
			if err != nil {
				return nil, err
			}
			bindOnParent(parent, tail, mod)
			return mod, nil
		}
		if isDir(pkgDir) && caseOK(pkgDir) {
			namespacePortions = append(namespacePortions, pkgDir)
		}
	}
	if len(namespacePortions) > 0 {
		mod := loadAsNamespace(exec, name, parent, namespacePortions)
		bindOnParent(parent, tail, mod)
		return mod, nil
	}
	return nil, fmt.Errorf("%w: %s", errFinderMiss, name)
}

// pathHookSpec consults sys.path_hooks for a custom importer able to load
// modules out of entry (zipimport.zipimporter for a .zip archive) and asks
// that importer for name's spec.
//
// handled is false when no hook claims entry, or when the importer claims
// entry but has no spec for name, so FindModule keeps scanning the
// remaining path entries. herr carries a find_spec failure that must
// propagate. The spec is returned unloaded so FindModule can tell a
// concrete module apart from a PEP 420 namespace portion.
//
// CPython: Lib/importlib/_bootstrap_external.py:1284 PathFinder._get_spec
func pathHookSpec(exec Executor, entry, name string) (spec objects.Object, handled bool, herr error) {
	_ = exec
	importer, ok := pathHookImporter(entry)
	if !ok {
		return nil, false, nil
	}
	findSpec, err := objects.GetAttr(importer, objects.NewStr("find_spec"))
	if err != nil {
		return nil, false, nil
	}
	s, err := objects.Call(findSpec, objects.NewTuple([]objects.Object{objects.NewStr(name)}), nil)
	if err != nil {
		return nil, true, err
	}
	if s == nil || objects.IsNone(s) {
		// The archive exists but does not contain name: a miss, not an
		// error. CPython's PathFinder moves on to the next path entry.
		return nil, false, nil
	}
	return s, true, nil
}

// namespacePortionsOf reports whether spec is a PEP 420 namespace spec
// (loader None) and, if so, returns its submodule_search_locations as a
// slice of strings. A spec with a real loader is a concrete module and
// returns isNS false.
//
// CPython: Lib/importlib/_bootstrap_external.py:1284 PathFinder._get_spec
// (spec.submodule_search_locations / namespace_path extension)
func namespacePortionsOf(spec objects.Object) (portions []string, isNS bool) {
	loader, err := objects.GetAttr(spec, objects.NewStr("loader"))
	if err != nil || !objects.IsNone(loader) {
		return nil, false
	}
	ssl, err := objects.GetAttr(spec, objects.NewStr("submodule_search_locations"))
	if err != nil || ssl == nil || objects.IsNone(ssl) {
		return nil, false
	}
	switch v := ssl.(type) {
	case *objects.List:
		for i := 0; i < v.Len(); i++ {
			if s, ok := v.Item(i).(*objects.Unicode); ok {
				portions = append(portions, s.Value())
			}
		}
	case *objects.Tuple:
		for i := 0; i < v.Len(); i++ {
			if s, ok := v.Item(i).(*objects.Unicode); ok {
				portions = append(portions, s.Value())
			}
		}
	}
	return portions, true
}

// pathHookImporter returns the importer object responsible for entry,
// consulting sys.path_importer_cache first and then sys.path_hooks. A
// hook that raises (ImportError) declines entry, so the next hook is
// tried; when none claim it the result is cached as None and ok is false.
//
// CPython: Lib/importlib/_bootstrap_external.py:1236 PathFinder._path_importer_cache
func pathHookImporter(entry string) (objects.Object, bool) {
	sysMod, ok := GetModule("sys")
	if !ok {
		return nil, false
	}
	key := objects.NewStr(entry)
	cacheObj, _ := sysMod.Dict().GetItem(objects.NewStr("path_importer_cache"))
	cache, _ := cacheObj.(*objects.Dict)
	if cache != nil {
		if v, err := cache.GetItem(key); err == nil && v != nil {
			if objects.IsNone(v) {
				return nil, false
			}
			return v, true
		}
	}
	hooksObj, _ := sysMod.Dict().GetItem(objects.NewStr("path_hooks"))
	hooks, _ := hooksObj.(*objects.List)
	if hooks == nil {
		return nil, false
	}
	for i := 0; i < hooks.Len(); i++ {
		importer, err := objects.Call(hooks.Item(i), objects.NewTuple([]objects.Object{key}), nil)
		if err != nil {
			// ImportError from a hook means "I do not handle this entry".
			continue
		}
		if importer != nil && !objects.IsNone(importer) {
			if cache != nil {
				_ = cache.SetItem(key, importer)
			}
			return importer, true
		}
	}
	if cache != nil {
		_ = cache.SetItem(key, objects.None())
	}
	return nil, false
}

// loadFromSpec builds a module from spec via importlib.util.module_from_spec,
// registers it in sys.modules, and runs spec.loader.exec_module, mirroring
// the body of _bootstrap._load_unlocked. gopy cannot call _load itself
// because that path enters the import lock machinery, which needs the
// _weakref injection CPython performs in _setup and gopy does not run.
//
// CPython: Lib/importlib/_bootstrap.py:921 _load_unlocked
func loadFromSpec(exec Executor, name string, spec objects.Object) (*objects.Module, error) {
	util, ok := GetModule("importlib.util")
	if !ok {
		util, ok = ensureImportlibUtil(exec)
		if !ok {
			return nil, fmt.Errorf("imp: loadFromSpec %q: importlib.util unavailable", name)
		}
	}
	mfs, err := util.Dict().GetItem(objects.NewStr("module_from_spec"))
	if err != nil {
		return nil, fmt.Errorf("imp: loadFromSpec %q: module_from_spec missing: %w", name, err)
	}
	modObj, err := objects.Call(mfs, objects.NewTuple([]objects.Object{spec}), nil)
	if err != nil {
		return nil, fmt.Errorf("imp: loadFromSpec %q: module_from_spec: %w", name, err)
	}
	module, ok := modObj.(*objects.Module)
	if !ok {
		return nil, fmt.Errorf("imp: loadFromSpec %q: module_from_spec returned %T", name, modObj)
	}
	AddModule(name, module)
	loader, err := objects.GetAttr(spec, objects.NewStr("loader"))
	if err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadFromSpec %q: spec.loader: %w", name, err)
	}
	// A namespace-package spec carries loader None: module_from_spec has
	// already populated __path__ from submodule_search_locations and there
	// is no body to run, exactly as _load_unlocked skips exec_module when
	// the loader is None.
	//
	// CPython: Lib/importlib/_bootstrap.py:945 _load_unlocked (loader is None)
	if objects.IsNone(loader) {
		if final, ok := GetModule(name); ok {
			return final, nil
		}
		return module, nil
	}
	execMod, err := objects.GetAttr(loader, objects.NewStr("exec_module"))
	if err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadFromSpec %q: loader.exec_module: %w", name, err)
	}
	if _, err := objects.Call(execMod, objects.NewTuple([]objects.Object{module}), nil); err != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadFromSpec %q: exec_module: %w: %w", name, err, ErrModuleExecFailed)
	}
	// exec_module may reassign sys.modules[name]; re-read it the way
	// CPython's _load_unlocked returns sys.modules[spec.name].
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return module, nil
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
	// CPython attaches __spec__ before exec_module; do the same so an
	// __init__.py that imports from its own package during init reads
	// spec.has_location / spec.origin.
	//
	// CPython: Lib/importlib/_bootstrap.py:573 module_from_spec
	attachSpecAttrs(exec, mod, name, initFile, []string{pkgDir})

	src, err := os.ReadFile(initFile) //nolint:gosec // initFile is filepath.Join of a trusted PathFinder.Paths entry.
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: %w", name, err)
	}
	code, err := compiler(src, initFile)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsPackage %q: compile: %w", name, err)
	}
	setSpecInitializing(mod, true)
	_, execErr := exec.ExecCode(code, mod)
	setSpecInitializing(mod, false)
	if execErr != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsPackage %q: exec: %w: %w", name, execErr, ErrModuleExecFailed)
	}
	// CPython: Python/import.c:2715 exec_code_in_module re-reads
	// sys.modules so an `__init__.py` that reassigns its own entry
	// (rare for packages, but the same shape as decimal/_pydecimal).
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return mod, nil
}

// loadAsPackageBytecode is loadAsPackage for a sourceless package: the
// code object comes from <pkgDir>/__init__.pyc instead of compiling
// __init__.py. __path__ is set before the body runs so a package whose
// __init__ does `from .submod import x` can resolve the parent's
// __path__.
//
// CPython: Lib/importlib/_bootstrap_external.py:1215 SourcelessFileLoader
func loadAsPackageBytecode(exec Executor, initFile, pkgDir, name string) (*objects.Module, error) {
	code, err := readPycCode(initFile)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsPackageBytecode %q: %w", name, err)
	}
	mod, exists := GetModule(name)
	if !exists {
		mod = objects.NewModule(name)
	}
	d := mod.Dict()
	if err := d.SetItem(objects.NewStr("__file__"), objects.NewStr(initFile)); err != nil {
		return nil, fmt.Errorf("imp: loadAsPackageBytecode %q: __file__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__path__"),
		objects.NewList([]objects.Object{objects.NewStr(pkgDir)})); err != nil {
		return nil, fmt.Errorf("imp: loadAsPackageBytecode %q: __path__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__package__"), objects.NewStr(name)); err != nil {
		return nil, fmt.Errorf("imp: loadAsPackageBytecode %q: __package__: %w", name, err)
	}
	AddModule(name, mod)
	attachSpecAttrs(exec, mod, name, initFile, []string{pkgDir})
	setSpecInitializing(mod, true)
	_, execErr := exec.ExecCode(code, mod)
	setSpecInitializing(mod, false)
	if execErr != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsPackageBytecode %q: exec: %w: %w", name, execErr, ErrModuleExecFailed)
	}
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return mod, nil
}

// loadAsModuleBytecode is loadAsModule for a sourceless module: the code
// object comes from <dir>/<tail>.pyc instead of compiling <tail>.py.
//
// CPython: Lib/importlib/_bootstrap_external.py:1215 SourcelessFileLoader
func loadAsModuleBytecode(exec Executor, file, name, parent string) (*objects.Module, error) {
	code, err := readPycCode(file)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsModuleBytecode %q: %w", name, err)
	}
	mod, exists := GetModule(name)
	if !exists {
		mod = objects.NewModule(name)
	}
	d := mod.Dict()
	if err := d.SetItem(objects.NewStr("__file__"), objects.NewStr(file)); err != nil {
		return nil, fmt.Errorf("imp: loadAsModuleBytecode %q: __file__: %w", name, err)
	}
	if err := d.SetItem(objects.NewStr("__package__"), objects.NewStr(parent)); err != nil {
		return nil, fmt.Errorf("imp: loadAsModuleBytecode %q: __package__: %w", name, err)
	}
	AddModule(name, mod)
	attachSpecAttrs(exec, mod, name, file, nil)
	setSpecInitializing(mod, true)
	_, execErr := exec.ExecCode(code, mod)
	setSpecInitializing(mod, false)
	if execErr != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsModuleBytecode %q: exec: %w: %w", name, execErr, ErrModuleExecFailed)
	}
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return mod, nil
}

// readPycCode opens a .pyc file and returns its embedded code object,
// validating the magic-number header along the way.
//
// CPython: Lib/importlib/_bootstrap_external.py:1215 SourcelessFileLoader.get_code
func readPycCode(file string) (*objects.Code, error) {
	f, err := os.Open(file) //nolint:gosec // file is filepath.Join of a trusted PathFinder.Paths entry.
	if err != nil {
		return nil, err
	}
	defer f.Close()
	code, _, err := marshal.ReadPyc(f)
	if err != nil {
		return nil, err
	}
	return code, nil
}

// loadAsNamespace builds a PEP 420 namespace package: a module with no
// __file__, a __path__ spanning every contributing directory, and a
// namespace __spec__ (loader None, origin None). The body is never
// executed because a namespace package has no __init__.py.
//
// CPython: Lib/importlib/_bootstrap.py:573 module_from_spec (namespace) /
// Lib/importlib/_bootstrap_external.py:1230 NamespaceLoader
func loadAsNamespace(exec Executor, name, parent string, portions []string) *objects.Module {
	mod, exists := GetModule(name)
	if !exists {
		mod = objects.NewModule(name)
	}
	d := mod.Dict()
	items := make([]objects.Object, len(portions))
	for i, s := range portions {
		items[i] = objects.NewStr(s)
	}
	_ = d.SetItem(objects.NewStr("__path__"), objects.NewList(items))
	_ = d.SetItem(objects.NewStr("__package__"), objects.NewStr(name))
	_ = d.SetItem(objects.NewStr("__file__"), objects.None())
	if _, err := d.GetItem(objects.NewStr("__doc__")); err != nil {
		_ = d.SetItem(objects.NewStr("__doc__"), objects.None())
	}
	_ = parent
	AddModule(name, mod)
	attachNamespaceSpec(exec, mod, name, portions)
	return mod
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
	// CPython sets __spec__ in module_from_spec before exec_module runs the
	// body, so a module that imports from itself during initialization can
	// read spec.has_location / spec.origin. Attach before exec.
	//
	// CPython: Lib/importlib/_bootstrap.py:573 module_from_spec
	attachSpecAttrs(exec, mod, name, file, nil)

	src, err := os.ReadFile(file) //nolint:gosec // file is filepath.Join of a trusted PathFinder.Paths entry.
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: %w", name, err)
	}
	code, err := compiler(src, file)
	if err != nil {
		return nil, fmt.Errorf("imp: loadAsModule %q: compile: %w", name, err)
	}
	setSpecInitializing(mod, true)
	_, execErr := exec.ExecCode(code, mod)
	setSpecInitializing(mod, false)
	if execErr != nil {
		RemoveModule(name)
		return nil, fmt.Errorf("imp: loadAsModule %q: exec: %w: %w", name, execErr, ErrModuleExecFailed)
	}
	// CPython: Python/import.c:2715 exec_code_in_module re-reads
	// sys.modules so a module body that reassigns its own entry
	// (`sys.modules[__name__] = other`, e.g. decimal/_pydecimal) wins.
	if final, ok := GetModule(name); ok {
		return final, nil
	}
	return mod, nil
}

// attachSpecAttrs populates the module-namespace surface CPython's
// _init_module_attrs fills from a ModuleSpec: __spec__, __loader__,
// __cached__ and a default __doc__. gopy's import runs Go-side, so the
// spec is built by calling importlib.util.spec_from_file_location once
// the body has run (the same shape CPython's FileFinder produces).
//
// importlib.util is itself a .py module, so the modules loaded before
// (and during) its own import cannot have their spec built yet. Those
// are queued in pendingSpecs and flushed the moment util becomes
// available, so importlib and its early dependencies still end up with
// a __spec__.
//
// CPython: Lib/importlib/_bootstrap.py:516 _init_module_attrs
func attachSpecAttrs(exec Executor, mod *objects.Module, name, origin string, searchLocations []string) {
	d := mod.Dict()
	// __doc__ defaults to None when the body stored no docstring.
	docKey := objects.NewStr("__doc__")
	if _, err := d.GetItem(docKey); err != nil {
		_ = d.SetItem(docKey, objects.None())
	}
	p := pendingSpec{mod: mod, name: name, origin: origin, search: searchLocations}
	util, ok := ensureImportlibUtil(exec)
	if !ok {
		pendingMu.Lock()
		pendingSpecs = append(pendingSpecs, p)
		pendingMu.Unlock()
		return
	}
	applySpec(util, p)
	flushPendingSpecs(util)
}

// setSpecInitializing flips mod.__spec__._initializing. CPython's
// module_from_spec wraps exec_module in `spec._initializing = True` /
// `finally: spec._initializing = False`, so a module that imports from
// itself during its own body sees a partially-initialized spec. gopy
// mirrors that around ExecCode so the circular-import and shadowing
// hints in _Py_module_getattro_impl / _PyEval_ImportFrom fire correctly.
//
// CPython: Lib/importlib/_bootstrap.py:573 module_from_spec
func setSpecInitializing(mod *objects.Module, on bool) {
	spec, err := mod.Dict().GetItem(objects.NewStr("__spec__"))
	if err != nil || spec == nil || objects.IsNone(spec) {
		return
	}
	var v objects.Object = objects.False()
	if on {
		v = objects.True()
	}
	_ = objects.SetAttr(spec, objects.NewStr("_initializing"), v)
}

// attachNamespaceSpec binds a PEP 420 namespace ModuleSpec (loader None,
// origin None, submodule_search_locations = the portions) onto mod. Like
// the file path it defers when importlib.util is not importable yet.
//
// CPython: Lib/importlib/_bootstrap.py:573 module_from_spec (namespace)
func attachNamespaceSpec(exec Executor, mod *objects.Module, name string, portions []string) {
	p := pendingSpec{mod: mod, name: name, search: portions, namespace: true}
	util, ok := ensureImportlibUtil(exec)
	if !ok {
		pendingMu.Lock()
		pendingSpecs = append(pendingSpecs, p)
		pendingMu.Unlock()
		return
	}
	applySpec(util, p)
	flushPendingSpecs(util)
}

// AttachBuiltinSpec gives a built-in (inittab) module the __spec__ /
// __loader__ surface CPython's BuiltinImporter installs: origin
// "built-in", no source, no file. It is deferred just like the
// file-based path when importlib.util is not importable yet.
//
// CPython: Lib/importlib/_bootstrap.py:736 BuiltinImporter.exec_module
func AttachBuiltinSpec(exec Executor, mod *objects.Module, name string) {
	d := mod.Dict()
	docKey := objects.NewStr("__doc__")
	if _, err := d.GetItem(docKey); err != nil {
		_ = d.SetItem(docKey, objects.None())
	}
	p := pendingSpec{mod: mod, name: name, builtin: true}
	util, ok := ensureImportlibUtil(exec)
	if !ok {
		pendingMu.Lock()
		pendingSpecs = append(pendingSpecs, p)
		pendingMu.Unlock()
		return
	}
	applySpec(util, p)
	flushPendingSpecs(util)
}

// pendingSpec records a module whose spec could not be built yet because
// importlib.util was not importable at the time.
type pendingSpec struct {
	mod       *objects.Module
	name      string
	origin    string
	search    []string
	builtin   bool
	namespace bool
}

var (
	pendingMu    sync.Mutex
	pendingSpecs []pendingSpec
)

// flushPendingSpecs drains the deferred-spec queue, building each
// module's spec now that importlib.util is available.
func flushPendingSpecs(util *objects.Module) {
	pendingMu.Lock()
	queue := pendingSpecs
	pendingSpecs = nil
	pendingMu.Unlock()
	for _, p := range queue {
		applySpec(util, p)
	}
}

// applySpec builds a ModuleSpec for p via importlib.util and binds the
// resulting __spec__/__loader__/__cached__ onto the module dict.
// Built-in modules use spec_from_loader with a "built-in" origin; file
// modules use spec_from_file_location.
//
// CPython: Lib/importlib/_bootstrap.py:516 _init_module_attrs
func applySpec(util *objects.Module, p pendingSpec) {
	spec := buildSpec(util, p)
	if spec == nil {
		return
	}
	d := p.mod.Dict()
	_ = d.SetItem(objects.NewStr("__spec__"), spec)
	if loader, lerr := objects.GetAttr(spec, objects.NewStr("loader")); lerr == nil {
		_ = d.SetItem(objects.NewStr("__loader__"), loader)
	}
	// __cached__ mirrors spec.cached (None for gopy's bytecode-less load).
	if cached, cerr := objects.GetAttr(spec, objects.NewStr("cached")); cerr == nil {
		_ = d.SetItem(objects.NewStr("__cached__"), cached)
	} else {
		_ = d.SetItem(objects.NewStr("__cached__"), objects.None())
	}
}

// buildSpec calls the appropriate importlib.util constructor for p.
func buildSpec(util *objects.Module, p pendingSpec) objects.Object {
	if p.namespace {
		// A PEP 420 namespace spec: loader None, origin None, the
		// portions as submodule_search_locations. machinery.ModuleSpec
		// is the faithful constructor; util re-exports it.
		//
		// CPython: Lib/importlib/_bootstrap.py:573 module_from_spec
		machinery, ok := GetModule("importlib.machinery")
		if !ok {
			return nil
		}
		ctor, err := machinery.Dict().GetItem(objects.NewStr("ModuleSpec"))
		if err != nil {
			return nil
		}
		kwargs := objects.NewDict()
		_ = kwargs.SetItem(objects.NewStr("is_package"), objects.True())
		args := objects.NewTuple([]objects.Object{objects.NewStr(p.name), objects.None()})
		spec, cerr := objects.Call(ctor, args, kwargs)
		if cerr != nil || spec == objects.None() {
			return nil
		}
		items := make([]objects.Object, len(p.search))
		for i, s := range p.search {
			items[i] = objects.NewStr(s)
		}
		_ = objects.SetAttr(spec, objects.NewStr("submodule_search_locations"), objects.NewList(items))
		return spec
	}
	if p.builtin {
		fn, err := util.Dict().GetItem(objects.NewStr("spec_from_loader"))
		if err != nil {
			return nil
		}
		kwargs := objects.NewDict()
		_ = kwargs.SetItem(objects.NewStr("origin"), objects.NewStr("built-in"))
		args := objects.NewTuple([]objects.Object{objects.NewStr(p.name), objects.None()})
		spec, cerr := objects.Call(fn, args, kwargs)
		if cerr != nil || spec == objects.None() {
			return nil
		}
		return spec
	}
	fn, err := util.Dict().GetItem(objects.NewStr("spec_from_file_location"))
	if err != nil {
		return nil
	}
	kwargs := objects.NewDict()
	if p.search != nil {
		items := make([]objects.Object, len(p.search))
		for i, s := range p.search {
			items[i] = objects.NewStr(s)
		}
		_ = kwargs.SetItem(objects.NewStr("submodule_search_locations"),
			objects.NewList(items))
	}
	args := objects.NewTuple([]objects.Object{objects.NewStr(p.name), objects.NewStr(p.origin)})
	spec, cerr := objects.Call(fn, args, kwargs)
	if cerr != nil || spec == objects.None() {
		return nil
	}
	return spec
}

var (
	specBootstrapMu  sync.Mutex
	specBootstrapped bool
)

// ensureImportlibUtil returns the importlib.util module, importing it on
// first use. The lazy import is guarded by specBootstrapped so the
// modules pulled in by importlib.util's own load (os, types,
// importlib._bootstrap_external) do not re-enter and recurse while that
// import is still in flight.
func ensureImportlibUtil(exec Executor) (*objects.Module, bool) {
	if util, ok := GetModule("importlib.util"); ok {
		// util is registered before its body runs, so a mid-import
		// lookup sees the module without spec_from_file_location yet.
		// Treat that partial state as "not ready" so the caller defers
		// rather than flushing the pending queue against a stub.
		if _, err := util.Dict().GetItem(objects.NewStr("spec_from_file_location")); err == nil {
			return util, true
		}
		return nil, false
	}
	specBootstrapMu.Lock()
	if specBootstrapped {
		specBootstrapMu.Unlock()
		return nil, false
	}
	specBootstrapped = true
	specBootstrapMu.Unlock()
	util, err := ImportModule(exec, "importlib.util")
	specBootstrapMu.Lock()
	specBootstrapped = false
	specBootstrapMu.Unlock()
	if err != nil {
		return nil, false
	}
	return util, true
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

// isDir reports whether path exists and is a directory. It is the gopy
// stand-in for importlib's _path_isdir helper used by namespace-portion
// detection.
//
// CPython: Lib/importlib/_bootstrap_external.py:153 _path_isdir
func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// caseOK reports whether the final component of an existing candidate path
// matches a real on-disk directory entry with exact case. On a
// case-insensitive but case-preserving filesystem (macOS, Windows) os.Stat
// succeeds for any case spelling, so a plain existence probe would let
// `import RAnDoM` resolve random.py. CPython's FileFinder guards against
// this by testing membership in set(os.listdir(dir)), the exact-case path
// cache, unless _relax_case() is true. caseOK reproduces that membership
// test by scanning the directory for an exact-case name match.
//
// CPython: Lib/importlib/_bootstrap_external.py:1378 cache_module in cache
func caseOK(path string) bool {
	if relaxCase() {
		return true
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	base := filepath.Base(path)
	for _, e := range entries {
		if e.Name() == base {
			return true
		}
	}
	return false
}

// relaxCase mirrors importlib's _relax_case: case folding is relaxed only
// on case-insensitive platforms (Windows, macOS) and only when PYTHONCASEOK
// is present in the environment. Case-sensitive platforms are always
// strict, where caseOK's directory scan is a redundant but harmless match.
//
// CPython: Lib/importlib/_bootstrap_external.py:50 _relax_case
func relaxCase() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		_, ok := os.LookupEnv("PYTHONCASEOK")
		return ok
	default:
		return false
	}
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
