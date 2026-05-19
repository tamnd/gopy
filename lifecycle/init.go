// Package lifecycle ports the cpython/Python/pylifecycle.c boot
// sequence: pre-init, core init, main init, finalize. v0.7 lands the
// core skeleton; later subtasks (1622-F path config, 1622-G
// finalize, 1622-H Main) flesh out each phase as the depended-on
// subsystems land.
package lifecycle

import (
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/pathconfig"
	"github.com/tamnd/gopy/state"
)

// Initialize is the public Py_InitializeFromConfig entry. It runs
// the pre-init then core init then main init phases against rt and
// returns the bound thread state.
//
// rt may be nil; a fresh runtime is allocated in that case so
// callers in tests and cmd/gopy do not have to thread one through
// by hand.
//
// CPython: Python/pylifecycle.c:1422 Py_InitializeFromConfig
func Initialize(rt *state.Runtime, config *initconfig.PyConfig) (*state.Thread, initconfig.Status) {
	if config == nil {
		return nil, initconfig.StatusErr("initialization config is NULL")
	}
	if rt == nil {
		rt = state.NewRuntime()
	}

	tstate, status := pyinitCore(rt, config)
	if status.IsException() {
		return nil, status
	}

	cfg, _ := tstate.Interp().Config.(*initconfig.PyConfig)
	if cfg != nil && cfg.InitMain != 0 {
		if status := pyinitMain(tstate); status.IsException() {
			return nil, status
		}
	}
	return tstate, initconfig.StatusOk()
}

// pyinitCore runs the core init phase: copy the caller's config,
// read it, then build runtime + interpreter + thread and stash the
// resolved config on the interpreter.
//
// The reconfigure branch (pyinit_core_reconfigure in CPython) lifts
// the existing main interpreter's config; v0.7 keeps it minimal
// because Py_NewInterpreter is out of scope.
//
// CPython: Python/pylifecycle.c:1091 pyinit_core
func pyinitCore(rt *state.Runtime, src *initconfig.PyConfig) (*state.Thread, initconfig.Status) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()

	if status := initconfig.ConfigCopy(cfg, src); status.IsException() {
		return nil, status
	}
	if status := initconfig.ConfigRead(cfg); status.IsException() {
		return nil, status
	}

	if rt.CoreInitialized == 0 {
		return pyinitConfig(rt, cfg)
	}
	return pyinitCoreReconfigure(rt, cfg)
}

// pyinitConfig is the first-time core init path. It writes the config
// onto the runtime, allocates the main interpreter and its thread,
// and flips runtime.CoreInitialized.
//
// Phases the v0.7 skeleton stubs:
//   - hash randomization init (handled inline by ConfigRead)
//   - import system init (deferred to v0.8 spec 1623)
//   - type system init (objects package, called here once it lands)
//
// CPython: Python/pylifecycle.c:939 pyinit_config
func pyinitConfig(rt *state.Runtime, cfg *initconfig.PyConfig) (*state.Thread, initconfig.Status) {
	if status := initRuntime(rt, cfg); status.IsException() {
		return nil, status
	}

	interp := rt.NewInterpreter()
	interp.Config = cfg

	tstate := interp.AttachThread()
	rt.CoreInitialized = 1
	return tstate, initconfig.StatusOk()
}

// initRuntime is the runtime-alloc phase: snapshot the config onto
// the runtime so phases that consume the resolved values (signal
// handler init, sys module, hash init) can read them off rt.
//
// CPython: Python/pylifecycle.c:482 pycore_init_runtime
func initRuntime(rt *state.Runtime, cfg *initconfig.PyConfig) initconfig.Status {
	if rt.Initialized != 0 {
		return initconfig.StatusErr("main interpreter already initialized")
	}
	_ = cfg
	return initconfig.StatusOk()
}

// pyinitCoreReconfigure handles the second-time core init: lift the
// freshly read config onto the main interpreter without reallocating
// the runtime. v0.7 ships the subset Py_Main exercises, which is the
// "called after Py_Initialize" reentry case.
//
// CPython: Python/pylifecycle.c:443 pyinit_core_reconfigure
func pyinitCoreReconfigure(rt *state.Runtime, cfg *initconfig.PyConfig) (*state.Thread, initconfig.Status) {
	interps := rt.Interpreters()
	if len(interps) == 0 {
		return nil, initconfig.StatusErr("runtime has no main interpreter")
	}
	interp := interps[0]
	interp.Config = cfg

	if len(interp.Threads()) == 0 {
		return nil, initconfig.StatusErr("main interpreter has no thread state")
	}
	return interp.Threads()[0], initconfig.StatusOk()
}

// pyinitMain runs the main init phase. The skeleton wires the runtime
// initialized flag and leaves the per-subsystem init points (signal
// handlers, sys streams, builtins.Init, faulthandler, codecs, site)
// for the later v0.7 subtasks. Each will hook into this function as
// it lands so the staging order matches CPython.
//
// CPython: Python/pylifecycle.c:1403 pyinit_main
func pyinitMain(tstate *state.Thread) initconfig.Status {
	rt := tstate.Interp().Runtime()
	if rt.CoreInitialized == 0 {
		return initconfig.StatusErr("runtime core not initialized")
	}
	if rt.Initialized != 0 {
		return pyinitMainReconfigure(tstate)
	}
	if status := initInterpMain(tstate); status.IsException() {
		return status
	}
	rt.Initialized = 1
	return initconfig.StatusOk()
}

// pyinitMainReconfigure refreshes the existing main interpreter when
// pyinitMain is reentered after Py_Initialize. The full version
// rebuilds sys-derived attributes; the v0.7 skeleton is a no-op.
//
// CPython: Python/pylifecycle.c:1137 pyinit_main_reconfigure
func pyinitMainReconfigure(tstate *state.Thread) initconfig.Status {
	_ = tstate
	return initconfig.StatusOk()
}

// initInterpMain is the per-phase fan-out called once the runtime is
// up. The v0.7 skeleton lands path-config resolution; the next
// subtasks (signal init, sys module, builtins, codecs, site) attach
// here as they land.
//
// CPython: Python/pylifecycle.c:1177 init_interp_main
func initInterpMain(tstate *state.Thread) initconfig.Status {
	cfg, _ := tstate.Interp().Config.(*initconfig.PyConfig)
	if cfg == nil {
		return initconfig.StatusErr("interpreter has no config")
	}
	if status := pathconfig.Resolve(cfg); status.IsException() {
		return status
	}
	// CPython: Python/pylifecycle.c:1325-1352 init_interp_main reads
	// $PYTHON_JIT here so the tier-2 gate is open before any user
	// code runs. gopy only flips the main-interp gate; sub-interpreters
	// inherit interp.JIT=false until v0.13 lands.
	ApplyJITEnv(tstate.Interp())
	return initconfig.StatusOk()
}
