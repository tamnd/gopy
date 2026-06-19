// Package testsinglephase is the gopy port of CPython's
// Modules/_testsinglephase.c, the legacy single-phase-init extension the
// import test suite drives through ExtensionFileLoader. CPython ships one
// compiled .so exposing several PyInit_ entry points; gopy registers each
// one as an extension module keyed by name, carrying the single-phase
// marker and the PyModuleDef m_size the import machinery reads.
//
// The variants mirror the kinds Python/import.c documents:
//   - "basic"      (_testsinglephase, m_size == -1): no per-module state, a
//     process-global initialized_count, cached, reloaded from m_copy.
//   - the indirect (_basic_wrapper) and direct (_basic_copy) basic variants.
//   - "reinit"     (_with_reinit, m_size == 0): re-runs init, no state.
//   - "with state" (_with_state, m_size > 0): per-module state, re-runs init.
//   - the *_check_cache_first variants: return PyState_FindModule first.
//   - _testsinglephase_raise_exception: PyInit raises and returns NULL.
//
// CPython: Modules/_testsinglephase.c:489 init__testsinglephase_basic
package testsinglephase

import (
	"fmt"
	"sync"
	"time"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// variant selects how a registered entry stores its module state.
type variant int

const (
	vBasic     variant = iota // m_size == -1, process-global state
	vReinit                   // m_size == 0, no state
	vWithState                // m_size > 0, per-module state
)

func init() {
	// gopy cannot dlopen the compiled extension, so each PyInit_ entry the C
	// module exposes is registered as a single-phase gopy extension module.
	// _imp.create_dynamic dispatches here; the single-phase marker drives the
	// subinterpreter compat gate and the m_size selects the reload behaviour.
	//
	// CPython: Modules/_testsinglephase.c:489 _testsinglephase_basic
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:        "_testsinglephase",
		SinglePhase: true,
		MSize:       -1,
		Init:        func() (*objects.Module, error) { return buildBasic("_testsinglephase") },
	})
	// PyInit__testsinglephase_basic_wrapper just calls PyInit__testsinglephase,
	// so it shares the def (and modules_by_index slot) and builds a module
	// named "_testsinglephase".
	//
	// CPython: Modules/_testsinglephase.c:537 PyInit__testsinglephase_basic_wrapper
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:         "_testsinglephase_basic_wrapper",
		SinglePhase:  true,
		MSize:        -1,
		DefName:      "_testsinglephase",
		ShareDefWith: "_testsinglephase",
		Init:         func() (*objects.Module, error) { return buildBasic("_testsinglephase") },
	})
	// PyInit__testsinglephase_basic_copy has its own def but shares the basic
	// methods and the process-global state.
	//
	// CPython: Modules/_testsinglephase.c:544 PyInit__testsinglephase_basic_copy
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:        "_testsinglephase_basic_copy",
		SinglePhase: true,
		MSize:       -1,
		Init:        func() (*objects.Module, error) { return buildBasic("_testsinglephase_basic_copy") },
	})
	// CPython: Modules/_testsinglephase.c:582 PyInit__testsinglephase_with_reinit
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:        "_testsinglephase_with_reinit",
		SinglePhase: true,
		MSize:       0,
		Init:        func() (*objects.Module, error) { return buildStateful("_testsinglephase_with_reinit", vReinit) },
	})
	// CPython: Modules/_testsinglephase.c:659 PyInit__testsinglephase_with_state
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:        "_testsinglephase_with_state",
		SinglePhase: true,
		MSize:       42,
		Init:        func() (*objects.Module, error) { return buildStateful("_testsinglephase_with_state", vWithState) },
	})
	// The *_check_cache_first variants return PyState_FindModule(def) before
	// creating a fresh module and are never recorded in the extensions cache.
	//
	// CPython: Modules/_testsinglephase.c:704 _check_cache_first modules
	for _, cc := range []struct {
		name  string
		mSize int
	}{
		{"_testsinglephase_check_cache_first", -1},
		{"_testsinglephase_with_reinit_check_cache_first", 0},
		{"_testsinglephase_with_state_check_cache_first", 42},
	} {
		name := cc.name
		imp.RegisterExtModule(&imp.ExtModuleDef{
			Name:            name,
			SinglePhase:     true,
			MSize:           cc.mSize,
			CheckCacheFirst: true,
			Init:            func() (*objects.Module, error) { return buildCheckCacheFirst(name) },
		})
	}
	// CPython: Modules/_testsinglephase.c:805 PyInit__testsinglephase_raise_exception
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:        "_testsinglephase_raise_exception",
		SinglePhase: true,
		MSize:       -1,
		Init:        raiseException,
	})
	// _testsinglephase_circular manages its own static cache (a process-global
	// pointer) and imports a helper module from PyInit before adding itself to
	// sys.modules, the gh-123950 circular-import fixture. Its def leaves m_size
	// unset, so it is the reinit (m_size == 0) kind.
	//
	// CPython: Modules/_testsinglephase.c:780 PyInit__testsinglephase_circular
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:        "_testsinglephase_circular",
		SinglePhase: true,
		MSize:       0,
		Init:        buildCircular,
	})
}

// errorType is the _testsinglephase.error exception the module installs.
//
// CPython: Modules/_testsinglephase.c:303 PyErr_NewException("_testsinglephase.error")
var errorType = pyerrors.NewExcType("_testsinglephase.error", []*objects.Type{pyerrors.PyExc_Exception})

// moduleState mirrors the C module_state: the time the state was
// initialized. A zero initialized time means uninitialized.
//
// CPython: Modules/_testsinglephase.c:174 module_state
type moduleState struct {
	initialized time.Time
}

// notInitialized is global_state.initialized_count's sentinel value before
// the basic module is loaded or after _clear_globals.
//
// CPython: Modules/_testsinglephase.c:229 NOT_INITIALIZED
const notInitialized = -1

// globalState mirrors the C global_state shared by the basic module and its
// variants across (sub)interpreters: an initialized count and a single
// module_state.
//
// CPython: Modules/_testsinglephase.c:185 global_state
var globalState = struct {
	mu               sync.Mutex
	initializedCount int64
	module           moduleState
}{initializedCount: notInitialized}

func secondsSinceEpoch(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}

// commonSum ports common_sum: sum(i, j) returns i + j.
//
// CPython: Modules/_testsinglephase.c:396 common_sum
func commonSum(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: sum() takes exactly 2 arguments (%d given)", len(args))
	}
	i, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[0].Type().Name)
	}
	j, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[1].Type().Name)
	}
	iv, _ := i.Int64()
	jv, _ := j.Int64()
	return objects.NewInt(iv + jv), nil
}

// raiseException is PyInit__testsinglephase_raise_exception: it sets
// RuntimeError("evil") and returns NULL, the gh-144601 fixture for a
// PyInit that fails.
//
// CPython: Modules/_testsinglephase.c:805 PyInit__testsinglephase_raise_exception
func raiseException() (*objects.Module, error) {
	exc := pyerrors.New(pyerrors.PyExc_RuntimeError, objects.NewTuple([]objects.Object{objects.NewStr("evil")}))
	return nil, objects.NewRaisedError(exc, "")
}

// installCommon installs the methods and constants every variant shares:
// look_up_self, sum, state_initialized, plus the error type and the
// int_const / str_const / _module_initialized attributes init_module sets.
//
// CPython: Modules/_testsinglephase.c:325 init_module
func installCommon(m *objects.Module, st *moduleState, hasState bool) error {
	d := m.Dict()
	methods := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"look_up_self", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
			return imp.ModuleSelf(m), nil
		}},
		{"sum", commonSum},
		{"state_initialized", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
			// common_state_initialized returns None when the module has no
			// state (m_size == 0); otherwise the seconds-since-epoch the
			// state was initialized (0.0 once cleared).
			if !hasState {
				return objects.None(), nil
			}
			return objects.NewFloat(secondsSinceEpoch(st.initialized)), nil
		}},
	}
	for _, mm := range methods {
		if err := d.SetItem(objects.NewStr(mm.name), objects.NewBuiltinFunction(mm.name, mm.fn)); err != nil {
			return err
		}
	}
	// CPython: Modules/_testsinglephase.c:303 state->error
	if err := d.SetItem(objects.NewStr("error"), errorType); err != nil {
		return err
	}
	// CPython: Modules/_testsinglephase.c:308 state->int_const 1969
	if err := d.SetItem(objects.NewStr("int_const"), objects.NewInt(1969)); err != nil {
		return err
	}
	// CPython: Modules/_testsinglephase.c:313 state->str_const
	if err := d.SetItem(objects.NewStr("str_const"), objects.NewStr("something different")); err != nil {
		return err
	}
	// CPython: Modules/_testsinglephase.c:338 _module_initialized
	if err := d.SetItem(objects.NewStr("_module_initialized"), objects.NewFloat(secondsSinceEpoch(st.initialized))); err != nil {
		return err
	}
	return nil
}

// buildBasic ports init__testsinglephase_basic: the basic module shares the
// process-global module_state and bumps the global initialized_count.
// state_initialized reads that shared state, so it returns 0.0 (not None)
// once the globals are cleared.
//
// CPython: Modules/_testsinglephase.c:497 init__testsinglephase_basic
func buildBasic(defName string) (*objects.Module, error) {
	globalState.mu.Lock()
	if globalState.initializedCount == notInitialized {
		globalState.initializedCount = 0
	}
	// clear_state then init_state: stamp the global state's initialized time.
	globalState.module.initialized = time.Now()
	st := globalState.module
	globalState.initializedCount++
	globalState.mu.Unlock()

	m := objects.NewModule(defName)
	// state_initialized must read the live global state, not a copy, so the
	// closure captures &globalState.module.
	if err := installCommon(m, &globalState.module, true); err != nil {
		return nil, err
	}
	// Re-stamp _module_initialized from the snapshot taken under the lock so
	// it matches state_initialized at load time.
	if err := m.Dict().SetItem(objects.NewStr("_module_initialized"), objects.NewFloat(secondsSinceEpoch(st.initialized))); err != nil {
		return nil, err
	}
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("initialized_count"), objects.NewBuiltinFunction("initialized_count", basicInitializedCount)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("_clear_globals"), objects.NewBuiltinFunction("_clear_globals", basicClearGlobals)); err != nil {
		return nil, err
	}
	return m, nil
}

// basicInitializedCount ports basic_initialized_count.
//
// CPython: Modules/_testsinglephase.c:416 basic_initialized_count
func basicInitializedCount(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	globalState.mu.Lock()
	defer globalState.mu.Unlock()
	return objects.NewInt(globalState.initializedCount), nil
}

// basicClearGlobals ports basic__clear_globals -> clear_global_state: it
// clears the shared module_state and resets initialized_count to
// NOT_INITIALIZED (-1).
//
// CPython: Modules/_testsinglephase.c:434 basic__clear_globals
//
//	(Modules/_testsinglephase.c:197 clear_global_state)
func basicClearGlobals(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	globalState.mu.Lock()
	defer globalState.mu.Unlock()
	globalState.module.initialized = time.Time{}
	globalState.initializedCount = notInitialized
	return objects.None(), nil
}

// buildStateful ports the with_reinit (m_size == 0) and with_state
// (m_size > 0) variants: each load runs init fresh against a state that is
// not the process-global one. A reinit module has no readable state
// (state_initialized returns None); a with_state module reads its own.
//
// CPython: Modules/_testsinglephase.c:582 / 659
func buildStateful(name string, v variant) (*objects.Module, error) {
	st := &moduleState{initialized: time.Now()}
	m := objects.NewModule(name)
	hasState := v == vWithState
	if err := installCommon(m, st, hasState); err != nil {
		return nil, err
	}
	if v == vWithState {
		// _clear_module_state clears the per-module state.
		//
		// CPython: Modules/_testsinglephase.c:452 basic__clear_module_state
		if err := m.Dict().SetItem(objects.NewStr("_clear_module_state"),
			objects.NewBuiltinFunction("_clear_module_state", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
				st.initialized = time.Time{}
				return objects.None(), nil
			})); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// staticModuleCircular mirrors the C static_module_circular pointer: the
// _testsinglephase_circular module caches itself in a process-global so a
// re-entrant PyInit (the circular import) reuses the same partially built
// object. clear_static_var resets it.
//
// CPython: Modules/_testsinglephase.c:758 static_module_circular
var staticModuleCircular *objects.Module

// circularHelperName is the module PyInit imports before returning, the
// half of the cycle that imports _testsinglephase_circular back again.
//
// CPython: Modules/_testsinglephase.c:788 helper_mod_name
const circularHelperName = "test.test_import.data.circular_imports.singlephase"

// buildCircular ports PyInit__testsinglephase_circular: it lazily builds the
// module into a static pointer, imports the helper module (which re-imports
// this module before it is in sys.modules), then records helper_mod_name and
// returns the cached object.
//
// CPython: Modules/_testsinglephase.c:780 PyInit__testsinglephase_circular
func buildCircular() (*objects.Module, error) {
	if staticModuleCircular == nil {
		m := objects.NewModule("_testsinglephase_circular")
		// CPython: Modules/_testsinglephase.c:761 circularmod_clear_static_var
		if err := m.Dict().SetItem(objects.NewStr("clear_static_var"),
			objects.NewBuiltinFunction("clear_static_var", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
				result := staticModuleCircular
				staticModuleCircular = nil
				if result == nil {
					return objects.None(), nil
				}
				return result, nil
			})); err != nil {
			return nil, err
		}
		staticModuleCircular = m
	}
	if objects.ImportModuleHook == nil {
		return nil, fmt.Errorf("ImportError: import machinery unavailable")
	}
	if _, err := objects.ImportModuleHook(circularHelperName); err != nil {
		return nil, err
	}
	// CPython: Modules/_testsinglephase.c:795 PyModule_AddStringConstant
	if err := staticModuleCircular.Dict().SetItem(objects.NewStr("helper_mod_name"), objects.NewStr(circularHelperName)); err != nil {
		return nil, err
	}
	return staticModuleCircular, nil
}

// buildCheckCacheFirst ports the *_check_cache_first PyInit functions, which
// only ever load fresh: a bare module carrying its own name.
//
// CPython: Modules/_testsinglephase.c:704 _check_cache_first modules
func buildCheckCacheFirst(name string) (*objects.Module, error) {
	return objects.NewModule(name), nil
}
