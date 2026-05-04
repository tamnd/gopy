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
// CPython: Include/internal/pycore_runtime.h:L162 _PyRuntimeState
type Runtime struct {
	interpreters []*Interpreter
}

// Interpreter is the per-interpreter state. v0.7 fills in the dict of
// builtins, sys module, import locks, and so on.
//
// CPython: Include/internal/pycore_interp.h:L113 PyInterpreterState
type Interpreter struct {
	runtime *Runtime
	threads []*Thread
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
	r := &Runtime{}
	i := &Interpreter{runtime: r}
	r.interpreters = append(r.interpreters, i)
	t := &Thread{interp: i}
	i.threads = append(i.threads, t)
	t.exc.Store(excHolder{})
	return t
}

// Interp returns the interpreter that owns t. Mirrors
// PyThreadState_GetInterpreter.
//
// CPython: Python/pystate.c:L2114 PyThreadState_GetInterpreter
func (t *Thread) Interp() *Interpreter { return t.interp }
