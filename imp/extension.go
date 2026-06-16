package imp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/tamnd/gopy/objects"
)

// This file ports the slice of CPython's extension-module import machinery
// the standard-library test suite drives through _testmultiphase /
// _testsinglephase and the SubinterpImportTests: the PEP 489 "multiple
// interpreters" / per-interpreter-GIL compatibility check and the
// subinterpreter interpreter-state that check consults.
//
// gopy cannot dlopen a compiled C extension, so the extensions are ported
// as Go builtins and registered here keyed by module name, each carrying
// the PEP 489 slot metadata its PyModuleDef declares. _imp.create_dynamic
// dispatches to this registry, applying CheckExtSubinterpCompat exactly the
// way Objects/moduleobject.c:359 PyModule_FromDefAndSpec2 and
// Python/import.c:1555 _PyImport_CheckSubinterpIncompatibleExtensionAllowed
// do before the module body runs.

// Multiple-interpreters support levels, the Py_mod_multiple_interpreters
// slot values an extension's PyModuleDef may carry.
//
// CPython: Include/moduleobject.h:90 Py_MOD_MULTIPLE_INTERPRETERS_*
const (
	// MultiInterpNotSupported is Py_MOD_MULTIPLE_INTERPRETERS_NOT_SUPPORTED.
	MultiInterpNotSupported = iota
	// MultiInterpSupported is Py_MOD_MULTIPLE_INTERPRETERS_SUPPORTED, the
	// default when a multi-phase module declares no slot.
	MultiInterpSupported
	// MultiInterpPerInterpreterGIL is Py_MOD_PER_INTERPRETER_GIL_SUPPORTED.
	MultiInterpPerInterpreterGIL
)

// ExtModuleDef is gopy's analogue of a C extension's PyModuleDef plus the
// PEP 489 slot table the loader reads. Init builds the fully populated
// module body (gopy has no separate create / exec phase for builtins, so
// the create_dynamic step runs Init and exec_dynamic is a no-op).
//
// CPython: Include/moduleobject.h:74 PyModuleDef_Slot
type ExtModuleDef struct {
	Name string
	// SinglePhase marks a legacy single-phase-init module. Such modules
	// never support loading under multiple interpreters, so the compat
	// check rejects them in any non-main interpreter that enforces it.
	SinglePhase bool
	// HasMultiInterpSlot records whether the def declared a
	// Py_mod_multiple_interpreters slot. When false a multi-phase module
	// defaults to MultiInterpSupported.
	HasMultiInterpSlot bool
	// MultiInterp is the Py_mod_multiple_interpreters slot value.
	MultiInterp int
	// MSize is the PyModuleDef.m_size of a single-phase module: -1 for a
	// "basic" module with no per-module state that does not support repeated
	// initialization (its __dict__ is cached in m_copy and copied on reload),
	// 0 for a "reinit" module, and >0 for a module that carries its own state.
	// Only -1 modules are reloaded from the cached dict; the others re-run
	// their init function on every load.
	//
	// CPython: Python/import.c:920 single-phase init module kinds
	MSize int
	// DefName is the def's m_name, the module's __name__. It defaults to Name
	// but differs for an "indirect" variant whose init function builds a
	// module under another def's name (PyInit__testsinglephase_basic_wrapper).
	DefName string
	// ShareDefWith names a registered module whose def (and thus its
	// modules_by_index slot and cached m_copy) this entry reuses, the gopy
	// analogue of one init function calling another's.
	//
	// CPython: Python/import.c:960 "two or more modules share a PyModuleDef"
	ShareDefWith string
	// CheckCacheFirst marks the *_check_cache_first variants, whose init
	// returns PyState_FindModule(def) before creating a fresh module and which
	// are never recorded in the extensions cache.
	//
	// CPython: Modules/_testsinglephase.c:690 _check_cache_first modules
	CheckCacheFirst bool
	// Init builds the module. A non-nil error models a PyInit function
	// that raised before returning its def.
	Init func() (*objects.Module, error)
}

var (
	extMu       sync.Mutex
	extRegistry = map[string]*ExtModuleDef{}
)

// RegisterExtModule records an extension module by name. Test-extension
// packages call it from their package init, the gopy stand-in for the
// inittab entry a compiled extension would expose.
func RegisterExtModule(def *ExtModuleDef) {
	extMu.Lock()
	extRegistry[def.Name] = def
	extMu.Unlock()
}

// FindExtModule returns the registered extension def for name, or nil.
func FindExtModule(name string) *ExtModuleDef {
	extMu.Lock()
	def := extRegistry[name]
	extMu.Unlock()
	return def
}

// ExtModuleNames returns the registered extension-module names, sorted.
func ExtModuleNames() []string {
	extMu.Lock()
	names := make([]string, 0, len(extRegistry))
	for n := range extRegistry {
		names = append(names, n)
	}
	extMu.Unlock()
	sort.Strings(names)
	return names
}

// interpState models the slice of PyInterpreterState the extension compat
// check reads: whether this is the main interpreter, whether it runs with
// its own GIL, and the check_multi_interp_extensions config flag (plus the
// _imp._override_multi_interp_extensions_check override).
//
// CPython: Include/internal/pycore_interp.h PyInterpreterState (ceval.own_gil,
// feature flags)
type interpState struct {
	isMain     bool
	ownGil     bool
	checkMulti bool
	// override is the _imp._override_multi_interp_extensions_check value:
	// <0 force-disable, 0 use config, >0 force-enable.
	override int
	// id is the interpreter id; the main interpreter is 0. It tags the
	// extensions-cache entries a single-phase module records so a reload only
	// reuses a dict the same interpreter owns.
	id int64
	// modByIndex is the interpreter's modules_by_index cache: m_index ->
	// module, the table PyState_FindModule / look_up_self consults.
	//
	// CPython: Include/internal/pycore_interp.h modules_by_index
	modByIndex map[int]*objects.Module
	// hiddenExt holds the registered extension-module sys.modules entries
	// this subinterpreter shadowed on entry. CPython gives every interpreter
	// its own sys.modules, so a subinterpreter re-imports an extension through
	// import_find_extension (firing the compat gate) even when the main
	// interpreter already cached it. gopy shares one sys.modules dict, so a
	// push removes those entries (forcing the re-import) and the matching pop
	// restores them. nil on the main interpreter.
	//
	// CPython: Include/internal/pycore_interp.h imports.modules
	hiddenExt map[string]objects.Object
}

var (
	interpMu     sync.Mutex
	interpStack  = []*interpState{{isMain: true, id: 0, modByIndex: map[int]*objects.Module{}}}
	nextInterpID int64
)

// currentInterp returns the interpreter state on top of the stack. gopy
// runs subinterpreter scripts synchronously on the calling goroutine, so a
// single push/pop stack tracks the active interpreter for the duration of a
// run_in_subinterp_with_config / _interpreters.run_string call.
func currentInterp() *interpState {
	interpMu.Lock()
	defer interpMu.Unlock()
	return interpStack[len(interpStack)-1]
}

// PushSubinterp pushes a fresh non-main interpreter state for the duration
// of a subinterpreter run. ownGil reflects the config gil ('own' -> true,
// 'shared'/'default' -> false); checkMulti is config.check_multi_interp_extensions.
//
// CPython: Python/pylifecycle.c:586 init_interp_create_gil (own_gil) and
// Python/interpconfig.c:262 check_multi_interp_extensions feature flag.
func PushSubinterp(ownGil, checkMulti bool) {
	s := &interpState{
		ownGil:     ownGil,
		checkMulti: checkMulti,
		modByIndex: map[int]*objects.Module{},
		hiddenExt:  hideExtModules(),
	}
	interpMu.Lock()
	nextInterpID++
	s.id = nextInterpID
	interpStack = append(interpStack, s)
	interpMu.Unlock()
}

// PopSubinterp pops the interpreter state pushed by PushSubinterp. The main
// interpreter at the bottom of the stack is never popped.
func PopSubinterp() {
	interpMu.Lock()
	var popped *interpState
	if len(interpStack) > 1 {
		popped = interpStack[len(interpStack)-1]
		interpStack = interpStack[:len(interpStack)-1]
	}
	interpMu.Unlock()
	if popped != nil {
		restoreExtModules(popped.hiddenExt)
	}
}

// hideExtModules removes every registered extension module's sys.modules
// entry, returning the removed entries so PopSubinterp can restore them. A
// fresh subinterpreter has an empty sys.modules, so its first `import name`
// of an extension misses and re-runs the import (firing the PEP 489 compat
// gate through import_find_extension) instead of returning the main
// interpreter's cached module. gopy shares the one sys.modules dict, so the
// removal models the per-interpreter cache for the duration of the run.
//
// CPython: Python/import.c:1964 import_find_extension
func hideExtModules() map[string]objects.Object {
	hidden := map[string]objects.Object{}
	for _, name := range ExtModuleNames() {
		if v, ok := GetModuleRaw(name); ok {
			hidden[name] = v
			RemoveModule(name)
		}
	}
	return hidden
}

// restoreExtModules undoes hideExtModules when a subinterpreter run ends: it
// drops any extension entry the subinterpreter left behind and reinstates the
// main interpreter's originals, so the shared sys.modules looks untouched.
func restoreExtModules(hidden map[string]objects.Object) {
	for _, name := range ExtModuleNames() {
		RemoveModule(name)
	}
	for name, v := range hidden {
		sysModulesMu.Lock()
		_ = sysModules.SetItem(objects.NewStr(name), v)
		sysModulesMu.Unlock()
	}
}

// SetMultiInterpOverride sets the current interpreter's
// check_multi_interp_extensions override and returns the previous value.
//
// CPython: Python/import.c:5052 _imp__override_multi_interp_extensions_check_impl
func SetMultiInterpOverride(override int) int {
	interpMu.Lock()
	defer interpMu.Unlock()
	s := interpStack[len(interpStack)-1]
	old := s.override
	s.override = override
	return old
}

// checkMultiInterpExtensions reports whether the current interpreter
// enforces the subinterpreter-incompatible-extension check.
//
// CPython: Python/import.c:1538 check_multi_interp_extensions
func checkMultiInterpExtensions(s *interpState) bool {
	if s.override < 0 {
		return false
	}
	if s.override > 0 {
		return true
	}
	return s.checkMulti
}

// CheckExtSubinterpCompat applies the PEP 489 multiple-interpreters /
// per-interpreter-GIL compatibility check to def against the active
// interpreter. It returns an ImportError-tagged error when the module may
// not be loaded in the current subinterpreter, and nil otherwise.
//
// CPython: Objects/moduleobject.c:359 PyModule_FromDefAndSpec2 (slot gate)
// CPython: Python/import.c:1555 _PyImport_CheckSubinterpIncompatibleExtensionAllowed
func CheckExtSubinterpCompat(def *ExtModuleDef) error {
	s := currentInterp()
	if s.isMain {
		return nil
	}
	// Single-phase-init modules never support multiple interpreters; the
	// fresh-import and cached-reload paths both call the check directly.
	//
	// CPython: Python/import.c:1983 import_find_extension / 2198 import_run_extension
	if def.SinglePhase {
		if checkMultiInterpExtensions(s) {
			return subinterpIncompatible(def.Name)
		}
		return nil
	}

	multi := MultiInterpSupported
	if def.HasMultiInterpSlot {
		multi = def.MultiInterp
	}
	switch {
	case multi == MultiInterpNotSupported:
		if checkMultiInterpExtensions(s) {
			return subinterpIncompatible(def.Name)
		}
	case multi != MultiInterpPerInterpreterGIL && s.ownGil:
		// Supported-but-not-per-interpreter-GIL: only rejected when the
		// subinterpreter runs with its own GIL.
		if checkMultiInterpExtensions(s) {
			return subinterpIncompatible(def.Name)
		}
	}
	return nil
}

// subinterpIncompatible builds the ImportError the compat check raises. The
// message matches CPython byte-for-byte so the SubinterpImportTests'
// equality assertions on str(exc) pass.
//
// CPython: Python/import.c:1560 PyErr_Format(PyExc_ImportError, ...)
func subinterpIncompatible(name string) error {
	return fmt.Errorf("ImportError: module %s does not support loading in subinterpreters", name)
}

// extDef is gopy's analogue of a single PyModuleDef instance: the unit the
// extensions cache and the modules_by_index table key on. Two registry
// entries that share a def (an init function that calls another's) point at
// the same extDef, so they share an m_index (PyState_FindModule / look_up_self)
// and, for a basic module, the cached m_copy.
//
// CPython: Include/internal/pycore_moduleobject.h PyModuleDef_Base
type extDef struct {
	name  string // m_name; the module's __name__, may differ from the import name
	mSize int    // PyModuleDef.m_size
	index int    // m_index into modules_by_index; 0 until assigned on first load
}

// extCacheKey keys the extensions cache by (filename, name), exactly as
// _PyRuntime.imports.extensions does.
//
// CPython: Python/import.c:1379 _extensions_cache_set
type extCacheKey struct{ path, name string }

// extCacheValue is the cached single-phase module record: its def, a shallow
// copy of the module __dict__ after the first load (m_copy, basic modules
// only), and the interpreter that owns the copy.
//
// CPython: Python/import.c:1024 struct extensions_cache_value
type extCacheValue struct {
	def      *extDef
	mCopy    *objects.Dict
	interpid int64
}

var (
	extCacheMu sync.Mutex
	extCache   = map[extCacheKey]*extCacheValue{}
	extDefs    = map[string]*extDef{} // def name -> shared def
	nextModIdx = 0
	modToDef   = map[*objects.Module]*extDef{} // built module -> its def
)

// defFor returns the shared extDef for a registered single-phase module,
// creating it on first use. Entries that name a ShareDefWith reuse the
// referenced module's def so they land in the same modules_by_index slot.
func defFor(def *ExtModuleDef) *extDef {
	name := def.DefName
	if name == "" {
		name = def.Name
	}
	if def.ShareDefWith != "" {
		if shared := FindExtModule(def.ShareDefWith); shared != nil {
			sn := shared.DefName
			if sn == "" {
				sn = shared.Name
			}
			name = sn
		}
	}
	if ed, ok := extDefs[name]; ok {
		return ed
	}
	ed := &extDef{name: name, mSize: def.MSize}
	extDefs[name] = ed
	return ed
}

// CreateExtModule dispatches _imp.create_dynamic to the extension registry.
// It mirrors Python/import.c import_run_extension: a cached single-phase
// module is reloaded from the cache, otherwise the init runs fresh behind the
// PEP 489 compat gate and (for single-phase modules) its result is recorded
// in the extensions cache. path is spec.origin, the extensions-cache key
// alongside name. The caller attaches __file__ / __spec__ / __loader__.
//
// found is false when name is not a registered gopy extension, letting the
// caller fall back to the "gopy cannot dlopen" ImportError.
//
// CPython: Python/import.c:2001 import_run_extension
func CreateExtModule(name, path string) (mod *objects.Module, found bool, err error) {
	def := FindExtModule(name)
	if def == nil {
		return nil, false, nil
	}
	if !def.SinglePhase {
		// Multi-phase modules apply the compat gate as part of
		// PyModule_FromDefAndSpec2 (the create step) before the body runs.
		if cerr := CheckExtSubinterpCompat(def); cerr != nil {
			return nil, true, cerr
		}
		mod, err = def.Init()
		if err != nil {
			return nil, true, err
		}
		return mod, true, nil
	}

	ed := func() *extDef {
		extCacheMu.Lock()
		defer extCacheMu.Unlock()
		return defFor(def)
	}()

	// import_find_extension: a cached single-phase module is reloaded without
	// re-running its init. The *_check_cache_first variants are never cached.
	//
	// CPython: Python/import.c:1964 import_find_extension
	if !def.CheckCacheFirst {
		extCacheMu.Lock()
		cached, ok := extCache[extCacheKey{path, name}]
		extCacheMu.Unlock()
		if ok {
			return reloadSinglephase(def, ed, cached, name)
		}
	}
	return runSinglephase(def, ed, name, path)
}

// runSinglephase ports the fresh-load path: it runs the init (on the "main
// interpreter", before the compat gate), applies the subinterpreter compat
// check, then records the module in modules_by_index and the extensions
// cache. A failing init inside a subinterpreter takes the gh-144601 path.
//
// CPython: Python/import.c:2078 import_run_extension
func runSinglephase(def *ExtModuleDef, ed *extDef, name, path string) (*objects.Module, bool, error) {
	inSubinterp := !currentInterp().isMain
	mod, initErr := def.Init()
	if initErr != nil {
		if inSubinterp {
			// gh-144601: the exception object can't be transferred across
			// interpreters. Print it as an unraisable exception, then raise
			// a different exception for the calling interpreter.
			//
			// CPython: Python/import.c:2156 PyErr_FormatUnraisable
			if objects.WriteUnraisableHook != nil {
				objects.WriteUnraisableHook(nil, "Exception while importing from subinterpreter", initErr)
			}
			// CPython: Python/import.c:2168 PyErr_SetString(PyExc_ImportError, ...)
			return nil, true, fmt.Errorf("ImportError: failed to import from subinterpreter due to exception")
		}
		return nil, true, initErr
	}
	if cerr := CheckExtSubinterpCompat(def); cerr != nil {
		return nil, true, cerr
	}

	s := currentInterp()
	extCacheMu.Lock()
	if ed.index == 0 {
		nextModIdx++
		ed.index = nextModIdx
	}
	modToDef[mod] = ed
	// update_global_state_for_extension caches the def under the main
	// interpreter or for any m_size == -1 module; a basic module also stores
	// a shallow copy of its dict for later reloads. The *_check_cache_first
	// variants are deliberately not cached.
	//
	// CPython: Python/import.c:1761 update_global_state_for_extension
	if !def.CheckCacheFirst && (s.isMain || ed.mSize == -1) {
		var mCopy *objects.Dict
		if ed.mSize == -1 {
			mCopy = snapshotDict(mod.Dict())
		}
		extCache[extCacheKey{path, name}] = &extCacheValue{def: ed, mCopy: mCopy, interpid: s.id}
	}
	extCacheMu.Unlock()

	setModuleByIndex(s, ed.index, mod)
	return mod, true, nil
}

// reloadSinglephase ports reload_singlephase_extension: a basic module
// (m_size == -1) is rebuilt by copying its cached dict into a fresh module
// without re-running init (so its global initialized_count is unchanged); a
// module with state re-runs its init function.
//
// CPython: Python/import.c:1869 reload_singlephase_extension
func reloadSinglephase(def *ExtModuleDef, ed *extDef, cached *extCacheValue, name string) (*objects.Module, bool, error) {
	// It may have been imported before in an interpreter that allows legacy
	// modules but is barred in the current one.
	if cerr := CheckExtSubinterpCompat(def); cerr != nil {
		return nil, true, cerr
	}
	s := currentInterp()
	if ed.mSize == -1 {
		// import_add_module: reuse the existing sys.modules entry so the
		// reloaded module is the same object, then PyDict_Update its dict
		// from the cached copy without re-running init.
		//
		// CPython: Python/import.c:1884 import_add_module / PyDict_Update
		mod, ok := GetModule(name)
		if !ok {
			mod = objects.NewModule(ed.name)
			AddModule(name, mod)
		}
		dst := mod.Dict()
		for _, k := range cached.mCopy.Keys() {
			v, gerr := cached.mCopy.GetItem(k)
			if gerr != nil {
				return nil, true, gerr
			}
			if serr := dst.SetItem(k, v); serr != nil {
				return nil, true, serr
			}
		}
		extCacheMu.Lock()
		modToDef[mod] = ed
		extCacheMu.Unlock()
		setModuleByIndex(s, ed.index, mod)
		return mod, true, nil
	}
	// m_size >= 0: re-run the init function.
	mod, err := def.Init()
	if err != nil {
		return nil, true, err
	}
	extCacheMu.Lock()
	modToDef[mod] = ed
	extCacheMu.Unlock()
	setModuleByIndex(s, ed.index, mod)
	return mod, true, nil
}

// snapshotDict returns a shallow copy of d, the gopy analogue of the m_copy
// the import machinery saves after a basic module is first loaded.
//
// CPython: Python/import.c:1140 fixup_cached_def (def->m_base.m_copy)
func snapshotDict(d *objects.Dict) *objects.Dict {
	out := objects.NewDict()
	for _, k := range d.Keys() {
		if v, err := d.GetItem(k); err == nil {
			_ = out.SetItem(k, v)
		}
	}
	return out
}

// setModuleByIndex records mod in the interpreter's modules_by_index table,
// the slot PyState_FindModule / look_up_self reads.
//
// CPython: Python/import.c:651 _modules_by_index_set
func setModuleByIndex(s *interpState, index int, mod *objects.Module) {
	if index <= 0 {
		return
	}
	interpMu.Lock()
	if s.modByIndex == nil {
		s.modByIndex = map[int]*objects.Module{}
	}
	s.modByIndex[index] = mod
	interpMu.Unlock()
}

// ModuleSelf returns the module currently cached in modules_by_index for the
// def mod belongs to, the value PyState_FindModule(def) yields. It backs the
// test extension's look_up_self() method.
//
// CPython: Modules/_testsinglephase.c:374 common_look_up_self (PyState_FindModule)
func ModuleSelf(mod *objects.Module) objects.Object {
	extCacheMu.Lock()
	ed := modToDef[mod]
	extCacheMu.Unlock()
	if ed == nil || ed.index == 0 {
		return objects.None()
	}
	s := currentInterp()
	interpMu.Lock()
	found := s.modByIndex[ed.index]
	interpMu.Unlock()
	if found == nil {
		return objects.None()
	}
	return found
}

// ClearExtension clears the internally cached data for a single-phase
// extension: its modules_by_index slot, the cached def's m_index/m_copy, and
// the extensions-cache entry. It backs _testinternalcapi.clear_extension.
//
// CPython: Python/import.c:903 _PyImport_ClearExtension
//
//	(Python/import.c:2241 clear_singlephase_extension)
func ClearExtension(name, path string) error {
	extCacheMu.Lock()
	cached, ok := extCache[extCacheKey{path, name}]
	if !ok {
		extCacheMu.Unlock()
		return nil
	}
	ed := cached.def
	index := ed.index
	ed.index = 0
	delete(extCache, extCacheKey{path, name})
	extCacheMu.Unlock()

	if index > 0 {
		s := currentInterp()
		interpMu.Lock()
		delete(s.modByIndex, index)
		interpMu.Unlock()
	}
	return nil
}

// extensionSuffix is the file suffix gopy advertises for its
// (Go-implemented) extension modules. CPython derives it from the ABI tag
// and platform triple; gopy keeps the shape (".<tag>.so") so __file__ reads
// like a real extension path and the ExtensionFileLoader path hook matches.
//
// CPython: Lib/importlib/_bootstrap_external.py:_get_supported_file_loaders
func extensionSuffix() string {
	return fmt.Sprintf(".gopy-314-%s-%s.so", runtime.GOOS, runtime.GOARCH)
}

// ExtensionSuffixes returns the extension-module suffixes _imp.extension_suffixes
// reports. A single gopy suffix is enough for the test extensions.
//
// CPython: Python/import.c:4807 _imp_extension_suffixes_impl
func ExtensionSuffixes() []string {
	return []string{extensionSuffix()}
}

var (
	extDirMu  sync.Mutex
	extDirVal string
)

// SetExtensionDir records the directory the materialized extension stub
// files live in (the gopy analogue of CPython's lib-dynload). The path
// finder discovers the stubs there and ExtensionOrigin reports __file__
// against it.
func SetExtensionDir(dir string) {
	extDirMu.Lock()
	extDirVal = dir
	extDirMu.Unlock()
}

func extensionDir() string {
	extDirMu.Lock()
	defer extDirMu.Unlock()
	return extDirVal
}

// ExtensionOrigin synthesizes the __file__ path for a Go-implemented
// extension: <extdir>/<name><suffix>, the location a compiled extension
// would occupy. When the extension dir is unset the bare filename is
// returned.
func ExtensionOrigin(name string) string {
	suffix := extensionSuffix()
	if dir := extensionDir(); dir != "" {
		return filepath.Join(dir, name+suffix)
	}
	return name + suffix
}

// MaterializeExtensions writes an empty stub file <name><suffix> into dir
// for every registered extension module, the gopy stand-in for the
// compiled .so files CPython ships in lib-dynload. The real Python
// PathFinder -> FileFinder discovers these by suffix and hands them to
// ExtensionFileLoader, whose create_module calls _imp.create_dynamic ->
// CreateExtModule. The stub bytes are never read; the Go registry holds
// the actual module body. dir is recorded as the extension dir.
func MaterializeExtensions(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	suffix := extensionSuffix()
	for _, name := range ExtModuleNames() {
		p := filepath.Join(dir, name+suffix)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return err
		}
	}
	SetExtensionDir(dir)
	return nil
}
