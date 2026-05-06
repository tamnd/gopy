package lifecycle

import (
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/state"
)

// Finalize tears the runtime down. Mirrors the staged
// shutdown order of _Py_Finalize: flip the finalizing flags first
// so other goroutines stop taking the GIL, then walk the per-phase
// teardown hooks in the order CPython documents.
//
// The v0.7 skeleton lands the phase scaffolding. Each per-phase
// helper is a no-op until the owning subtask attaches:
//
//   - flush sys.stdio: 1651-sys-D
//   - signal handlers: 1639 (already in place; finalize hook lands
//     with the lifecycle wiring task)
//   - import / modules teardown: v0.8 spec 1623
//   - faulthandler / hash stats: deferred to their own subtasks
//
// Returns Status so the caller can distinguish a clean shutdown
// from a flush failure (CPython returns -1 in that case).
//
// CPython: Python/pylifecycle.c:2016 _Py_Finalize
func Finalize(rt *state.Runtime) initconfig.Status {
	if rt == nil || rt.Initialized == 0 {
		return initconfig.StatusOk()
	}

	for _, interp := range rt.Interpreters() {
		interp.Finalizing = 1
	}

	finalizeWaitForThreadShutdown(rt)
	finalizePendingCalls(rt)
	finalizeAtExit(rt)
	finalizeSubinterpreters(rt)

	rt.Initialized = 0
	rt.CoreInitialized = 0

	finalizeFlushStdio(rt)
	finalizeSignals(rt)
	finalizeGC(rt)
	finalizeImport(rt)
	finalizeFlushStdio(rt)
	finalizeFaulthandler(rt)
	finalizeHashStats(rt)
	finalizeInterpClear(rt)
	finalizeInterpDelete(rt)

	rt.DropInterpreters()
	return initconfig.StatusOk()
}

// finalizeWaitForThreadShutdown joins any non-daemon threading
// goroutines.
//
// CPython: Python/pylifecycle.c:wait_for_thread_shutdown
func finalizeWaitForThreadShutdown(rt *state.Runtime) { _ = rt }

// finalizePendingCalls drains the pending-call queue. The 1639 GIL
// port already builds the queue; the finalize hook lands with the
// rest of the shutdown wiring.
//
// CPython: Python/pylifecycle.c:_Py_FinishPendingCalls
func finalizePendingCalls(rt *state.Runtime) { _ = rt }

// finalizeAtExit runs the registered atexit handlers. Lands with
// v0.8 atexit module port.
//
// CPython: Python/pylifecycle.c:_PyAtExit_Call
func finalizeAtExit(rt *state.Runtime) { _ = rt }

// finalizeSubinterpreters tears down any sub-interpreters spawned
// via Py_NewInterpreter. Stays empty until v0.13.
//
// CPython: Python/pylifecycle.c:finalize_subinterpreters
func finalizeSubinterpreters(rt *state.Runtime) { _ = rt }

// finalizeFlushStdio flushes sys.stdout and sys.stderr. Wired up by
// 1651-sys-D once the sys streams are real.
//
// CPython: Python/pylifecycle.c:flush_std_files
func finalizeFlushStdio(rt *state.Runtime) { _ = rt }

// finalizeSignals tears down the signal handler bridge.
//
// CPython: Python/pylifecycle.c:_PySignal_Fini
func finalizeSignals(rt *state.Runtime) { _ = rt }

// finalizeGC runs a final collection cycle so finalizers fire while
// the runtime is still mostly intact.
//
// CPython: Python/pylifecycle.c:PyGC_Collect
func finalizeGC(rt *state.Runtime) { _ = rt }

// finalizeImport tears down the import system. Lands with v0.8
// spec 1623.
//
// CPython: Python/pylifecycle.c:_PyImport_FiniExternal / FiniCore
func finalizeImport(rt *state.Runtime) { _ = rt }

// finalizeFaulthandler tears down the faulthandler bridge.
//
// CPython: Python/pylifecycle.c:_PyFaulthandler_Fini
func finalizeFaulthandler(rt *state.Runtime) { _ = rt }

// finalizeHashStats dumps hash statistics if PYTHONHASHSEED debug is
// configured. The hash package already exists; the dump hook lands
// with the diagnostics surface.
//
// CPython: Python/pylifecycle.c:_PyHash_Fini
func finalizeHashStats(rt *state.Runtime) { _ = rt }

// finalizeInterpClear releases interpreter-owned objects (modules,
// dicts, types). The skeleton resets the Config slot.
//
// CPython: Python/pylifecycle.c:finalize_interp_clear
func finalizeInterpClear(rt *state.Runtime) {
	for _, interp := range rt.Interpreters() {
		interp.Config = nil
	}
}

// finalizeInterpDelete is the per-interpreter destructor. The
// skeleton is empty; real cleanup lands as the per-subsystem state
// (modules dict, builtins dict, threading lock table) gets attached
// to Interpreter.
//
// CPython: Python/pylifecycle.c:finalize_interp_delete
func finalizeInterpDelete(rt *state.Runtime) { _ = rt }
