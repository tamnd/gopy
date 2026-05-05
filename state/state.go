// Package state ports the skeleton of cpython/Python/pystate.c. v0.3
// ships only the structs and the exception slot needed by the errors
// package; the lifecycle (Initialize/Finalize/NewInterpreter) lands in
// v0.7.
package state

import "sync/atomic"

// Exception is the interface state stores in the per-thread exception
// slot. The concrete type lives in the errors package; state knows
// only the marker method so the dependency graph is errors to state,
// not the reverse.
//
// CPython: Include/cpython/pyerrors.h:L11 PyBaseExceptionObject
type Exception interface {
	// IsException is a marker so unrelated types cannot satisfy this
	// interface by accident.
	IsException()
}

// Runtime is the process-wide runtime state. Mirrors _PyRuntimeState.
//
// Preinitialized, CoreInitialized, and Initialized are the staged
// init flags the lifecycle layer flips as it walks pre-init / core
// init / main init. They are exported so the lifecycle and finalize
// paths in package lifecycle can read and set them without an
// accessor sprawl.
//
// CPython: Include/internal/pycore_runtime_structs.h:171 preinitialized
type Runtime struct {
	interpreters []*Interpreter

	Preinitialized  int
	CoreInitialized int
	Initialized     int
}

// Interpreter is the per-interpreter state. v0.7 fills in the dict of
// builtins, sys module, import locks, and so on.
//
// Config is the *initconfig.PyConfig the lifecycle stamps in during
// pyinit_core. Stored as any so package state stays independent of
// initconfig; the lifecycle layer is the sole reader and re-asserts
// the concrete type at use sites.
//
// Finalizing is the bool flag the lifecycle flips during Py_Finalize
// before clearing modules; it stays at 1 until the interpreter is
// dropped from the runtime.
//
// CPython: Include/internal/pycore_interp.h:L113 PyInterpreterState
type Interpreter struct {
	runtime *Runtime
	threads []*Thread
	Config  any

	Finalizing int
}

// Thread is the per-goroutine state. v0.3 carries the current
// exception pointer; v0.6 adds the frame stack and v0.7 adds the
// dict/globals slots.
//
// CPython: Include/cpython/pystate.h:L75 PyThreadState
type Thread struct {
	interp *Interpreter
	exc    atomic.Value // holds Exception or nil
}

// CurrentException returns the current exception or nil. Mirrors
// _PyErr_Occurred.
//
// CPython: Python/errors.c:L138 _PyErr_Occurred
func (t *Thread) CurrentException() Exception {
	v := t.exc.Load()
	if v == nil {
		return nil
	}
	h, _ := v.(excHolder)
	return h.e
}

// SetException installs exc as the current exception. exc may be nil
// to clear.
//
// CPython: Python/errors.c:L83 _PyErr_SetObject (excerpt)
func (t *Thread) SetException(exc Exception) {
	if exc == nil {
		t.exc.Store(excHolder{})
		return
	}
	t.exc.Store(excHolder{e: exc})
}

// SwapException atomically replaces the current exception and returns
// the old value. Used by errors.Fetch.
//
// CPython: Python/errors.c:L460 _PyErr_Fetch
func (t *Thread) SwapException(exc Exception) Exception {
	var newV any = excHolder{}
	if exc != nil {
		newV = excHolder{e: exc}
	}
	old := t.exc.Swap(newV)
	if old == nil {
		return nil
	}
	h, _ := old.(excHolder)
	return h.e
}

// excHolder gives atomic.Value a concrete static type to avoid the
// "store of inconsistent type" panic when going from nil to a typed
// nil interface.
type excHolder struct{ e Exception }

// NewThread builds a thread state owned by an implicit default
// interpreter. v0.3 has no real lifecycle; tests use this to obtain a
// Thread value to pass to errors.Set*.
//
// CPython: Python/pystate.c:L915 PyThreadState_New
func NewThread() *Thread {
	r := NewRuntime()
	i := r.NewInterpreter()
	return i.AttachThread()
}

// NewRuntime allocates a fresh process-wide runtime. The lifecycle
// layer calls this on its way through pyinit_core; tests poke at it
// directly to build a runtime without going through the full init
// staging.
//
// CPython: Python/pystate.c:_PyRuntime_Initialize
func NewRuntime() *Runtime {
	return &Runtime{}
}

// NewInterpreter allocates an interpreter owned by r and registers
// it in r.interpreters. The first call returns the main interpreter;
// later calls (sub-interpreters) land in v0.13.
//
// CPython: Python/pystate.c:_PyInterpreterState_Enable
func (r *Runtime) NewInterpreter() *Interpreter {
	i := &Interpreter{runtime: r}
	r.interpreters = append(r.interpreters, i)
	return i
}

// Interpreters returns the interpreters owned by r in registration
// order. The slice aliases internal state; treat it as read-only.
func (r *Runtime) Interpreters() []*Interpreter { return r.interpreters }

// DropInterpreters detaches every interpreter from r. Used by
// lifecycle.Finalize to reset the runtime so a subsequent Initialize
// starts from a clean slate. Each detached interpreter has its
// runtime backpointer cleared so dangling references stop sharing
// state.
//
// CPython: Python/pylifecycle.c:_PyInterpreterState_DeleteAll
func (r *Runtime) DropInterpreters() {
	for _, i := range r.interpreters {
		i.runtime = nil
		i.threads = nil
	}
	r.interpreters = nil
}

// AttachThread builds a thread state bound to i and registers it.
// Mirrors PyThreadState_New for the simple case where the thread is
// being attached fresh (no detached predecessor).
//
// CPython: Python/pystate.c:L915 PyThreadState_New
func (i *Interpreter) AttachThread() *Thread {
	t := &Thread{interp: i}
	t.exc.Store(excHolder{})
	i.threads = append(i.threads, t)
	return t
}

// Runtime returns the runtime that owns i.
//
// CPython: Python/pystate.c:_PyInterpreterState_GetRuntime
func (i *Interpreter) Runtime() *Runtime { return i.runtime }

// Threads returns the threads attached to i in attach order. The
// slice aliases internal state; treat it as read-only.
func (i *Interpreter) Threads() []*Thread { return i.threads }

// Interp returns the interpreter that owns t. Mirrors
// PyThreadState_GetInterpreter.
//
// CPython: Python/pystate.c:L2114 PyThreadState_GetInterpreter
func (t *Thread) Interp() *Interpreter { return t.interp }
