// Package state ports the skeleton of cpython/Python/pystate.c. v0.3
// ships only the structs and the exception slot needed by the errors
// package; the lifecycle (Initialize/Finalize/NewInterpreter) lands in
// v0.7.
package state

import (
	"sync/atomic"

	"github.com/tamnd/gopy/monitor"
)

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

	// Monitors is the per-interpreter PEP 669 monitoring state. Lazy-
	// allocated by NewInterpreter so unused interpreters do not pay
	// the (small) cost of the callable / version tables.
	//
	// CPython: Include/internal/pycore_interp_structs.h:948 monitors
	Monitors *monitor.InterpState

	// Optimizer is the per-interpreter Tier-2 executor list and
	// pending-deletion bookkeeping. Stored as any so package state
	// stays independent of optimizer; the optimizer package owns the
	// concrete *optimizer.InterpState type and asserts it back at
	// use sites.
	//
	// CPython: Include/internal/pycore_interp_structs.h executor_list_head /
	// executor_deletion_list_head / executor_deletion_list_remaining_capacity
	Optimizer any

	// Watchers is the per-interpreter dict / type watcher registry the
	// Tier-2 optimizer hangs invalidation callbacks on. Stored as any
	// so package state stays independent of optimizer; the optimizer
	// package owns the concrete *optimizer.WatcherTable type.
	//
	// CPython: Include/internal/pycore_interp_structs.h dict_state.watchers /
	// type_watchers
	Watchers any

	// Builtins is the canonical builtins dict. The Tier-2 globals
	// folder bails when a frame's f_builtins points elsewhere (i.e. a
	// non-default builtins dict). Stored as any so package state stays
	// independent of objects; the optimizer asserts it back to *Dict
	// at use sites.
	//
	// CPython: Include/internal/pycore_interp.h PyInterpreterState.builtins
	Builtins any

	// BuiltinDictMutations counts how many times the builtins dict has
	// been replaced wholesale (the rare event CPython tracks under
	// rare_events.builtin_dict). The Tier-2 folder stops specializing
	// _LOAD_GLOBAL_BUILTINS once this exceeds the cap.
	//
	// CPython: Include/internal/pycore_interp_structs.h rare_events.builtin_dict
	BuiltinDictMutations int

	// CommonConsts is the LOAD_COMMON_CONSTANT lookup table. Five slots,
	// indexed by the CONSTANT_* enum: AssertionError, NotImplementedError,
	// tuple, all, any. Stored as any so package state stays free of
	// objects/errors imports; the vm layer asserts back to objects.Object.
	// Populated lazily on first read by the vm package; CPython fills it
	// in during interpreter init.
	//
	// CPython: Include/internal/pycore_interp_structs.h common_consts
	// CPython: Python/pylifecycle.c:815 _PyInterpreterState_InitConsts
	CommonConsts [NumCommonConstants]any

	// JIT mirrors CPython's interp->jit flag. When false, the Tier-2
	// optimizer refuses to project a trace and ENTER_EXECUTOR is never
	// installed; all execution stays on the Tier-1 interpreter. CPython
	// flips this to true only when the interpreter was built with
	// --enable-experimental-jit and PYTHON_JIT does not opt out.
	//
	// CPython: Include/internal/pycore_interp_structs.h jit
	// CPython: Python/optimizer.c:120 _PyOptimizer_Optimize jit gate
	// CPython: Python/pystate.c:674 interp->jit = false (default)
	JIT bool
}

// CONSTANT_* enum: indices into Interpreter.CommonConsts. Matches the
// CPython opcode utility header byte-for-byte.
//
// CPython: Include/internal/pycore_opcode_utils.h CONSTANT_*
const (
	ConstantAssertionError      = 0
	ConstantNotImplementedError = 1
	ConstantBuiltinTuple        = 2
	ConstantBuiltinAll          = 3
	ConstantBuiltinAny          = 4
	NumCommonConstants          = 5
)

// Thread is the per-goroutine state. v0.3 carries the current
// exception pointer; v0.6 adds the frame stack and v0.7 adds the
// dict/globals slots.
//
// id is the per-thread identifier the contextvars cache compares
// against (matches PyThreadState.id). ctx holds the current
// *contextvar.Context as an untyped any so package state stays free
// of a contextvar import. ctxVersion is bumped on every Enter / Exit
// so the ContextVar tsid+version cache invalidates correctly.
//
// CPython: Include/cpython/pystate.h:L75 PyThreadState
type Thread struct {
	interp     *Interpreter
	exc        atomic.Value // holds Exception or nil
	handled    atomic.Value // currently-handled Exception, mirrors PyThreadState.exc_info->exc_value
	id         uint64
	ctx        any
	ctxVersion uint64
	// Tracing counts active trace/profile calls. Non-zero means a
	// trace callback is currently running; instrumentation entry
	// points skip firing to prevent infinite recursion.
	//
	// CPython: Include/cpython/pystate.h:128 tracing
	Tracing int

	// CoroutineOriginTrackingDepth is how many traceback frames to
	// capture on coroutine creation, set by
	// sys.set_coroutine_origin_tracking_depth. Depth 0 disables
	// capture; cr_origin then reads None.
	//
	// CPython: Include/cpython/pystate.h:185 coroutine_origin_tracking_depth
	CoroutineOriginTrackingDepth int
}

// HandledException returns the currently-handled exception (the one a
// running except handler is processing), or nil. Mirrors the
// exc_info->exc_value slot in CPython's _PyErr_StackItem chain. The
// raised-exception slot (CurrentException) is cleared on handler entry;
// this slot persists for sys.exc_info() to read.
//
// CPython: Include/internal/pycore_runtime.h _PyErr_StackItem
func (t *Thread) HandledException() Exception {
	v := t.handled.Load()
	if v == nil {
		return nil
	}
	h, _ := v.(excHolder)
	return h.e
}

// SetHandledException replaces the handled-exception slot. Passing nil
// clears it. CPython: bytecodes.c POP_EXCEPT / PUSH_EXC_INFO maintain
// the exc_info stack; this method is the equivalent setter.
func (t *Thread) SetHandledException(exc Exception) {
	if exc == nil {
		t.handled.Store(excHolder{})
		return
	}
	t.handled.Store(excHolder{e: exc})
}

// EnterTracing increments the re-entrance depth counter so
// instrumentation callbacks know not to fire while a trace function is
// running on this thread.
//
// CPython: Python/ceval.c:2607 PyThreadState_EnterTracing
func (t *Thread) EnterTracing() { t.Tracing++ }

// LeaveTracing decrements the re-entrance depth counter.
//
// CPython: Python/ceval.c:2614 PyThreadState_LeaveTracing
func (t *Thread) LeaveTracing() { t.Tracing-- }

// threadIDCounter feeds Thread.id. Starts at 1 so 0 is reserved as a
// "never seen" sentinel for the ContextVar cache.
//
// CPython: Python/pystate.c:L908 _PyThreadState_NewID
var threadIDCounter atomic.Uint64

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
// later calls (sub-interpreters) land in v0.13. The first interpreter
// minted in the process is also stashed as the main interpreter and
// can be retrieved through MainInterpreter() so callback paths that
// lack a thread state (notably the dict / type watcher callbacks the
// optimizer installs) can locate the rare-event counters.
//
// CPython: Python/pystate.c:_PyInterpreterState_Enable
// CPython: Python/pylifecycle.c:599 builtins_dict_watcher
func (r *Runtime) NewInterpreter() *Interpreter {
	i := &Interpreter{runtime: r, Monitors: monitor.NewInterpState()}
	r.interpreters = append(r.interpreters, i)
	mainInterpreter.CompareAndSwap(nil, i)
	return i
}

// mainInterpreter is the process-wide pointer to the first interpreter
// minted. Mirrors CPython's _PyInterpreterState_Main(), which other
// runtime paths reach via _PyInterpreterState_GET when no thread state
// is available. Single-interpreter at v0.12; sub-interpreter work
// (v0.13) will promote this to a runtime-scoped lookup.
//
// CPython: Python/pystate.c:_PyInterpreterState_Main
var mainInterpreter atomic.Pointer[Interpreter]

// MainInterpreter returns the process-wide main interpreter, or nil if
// none has been minted yet. Callback contexts (e.g. dict / type
// watchers fired off a mutation deep in the runtime) use this to
// locate counters and the executor list when no thread state is
// available.
//
// CPython: Python/pystate.c:_PyInterpreterState_Main
func MainInterpreter() *Interpreter { return mainInterpreter.Load() }

// DropMainInterpreter clears the main-interpreter pointer. Used by
// lifecycle.Finalize so a subsequent Initialize starts from a clean
// slate; tests also call it to reset between runs.
func DropMainInterpreter() { mainInterpreter.Store(nil) }

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
	DropMainInterpreter()
}

// AttachThread builds a thread state bound to i and registers it.
// Mirrors PyThreadState_New for the simple case where the thread is
// being attached fresh (no detached predecessor).
//
// CPython: Python/pystate.c:L915 PyThreadState_New
func (i *Interpreter) AttachThread() *Thread {
	t := &Thread{interp: i, id: threadIDCounter.Add(1)}
	t.exc.Store(excHolder{})
	i.threads = append(i.threads, t)
	return t
}

// ID returns the per-thread identifier. Used by the ContextVar cache.
//
// CPython: Include/cpython/pystate.h PyThreadState.id
func (t *Thread) ID() uint64 { return t.id }

// Context returns the current context, stored as any to avoid
// importing contextvar. The contextvar package re-asserts the
// concrete *Context type at use sites. nil signals "no context yet";
// the contextvar layer lazily allocates an empty one on first use,
// matching CPython's context_get.
//
// CPython: Include/cpython/pystate.h PyThreadState.context
func (t *Thread) Context() any { return t.ctx }

// SetContext installs a new current context. The contextvar layer
// passes a *contextvar.Context (or nil to clear). Each call bumps
// ContextVersion so the per-ContextVar cache invalidates.
//
// CPython: Python/context.c:L184 context_switched
func (t *Thread) SetContext(ctx any) {
	t.ctx = ctx
	t.ctxVersion++
}

// ContextVersion returns the version counter that increments on
// every context switch. The ContextVar cache stamps this value into
// the entry on every set / get so a switch invalidates stale reads.
//
// CPython: Include/cpython/pystate.h PyThreadState.context_ver
func (t *Thread) ContextVersion() uint64 { return t.ctxVersion }

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
