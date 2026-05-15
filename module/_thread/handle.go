// _thread._ThreadHandle + start_joinable_thread + _make_thread_handle
// + _get_main_thread_ident.
//
// CPython 3.14 added these so Lib/threading.py's Thread class can track
// thread lifecycle through a handle object instead of polling. The
// surface required at threading.py module load time is:
//
//   _start_joinable_thread = _thread.start_joinable_thread
//   _make_thread_handle    = _thread._make_thread_handle
//   _ThreadHandle          = _thread._ThreadHandle
//   _get_main_thread_ident = _thread._get_main_thread_ident
//
// The gopy port backs each thread with a goroutine and uses a `done`
// channel to signal completion. join() blocks on that channel.
// is_done() / _set_done() manipulate it. ident is the goroutine id.
//
// CPython: Modules/_threadmodule.c:66 ThreadHandle type
// CPython: Modules/_threadmodule.c:300 thread_run (joinable boot)
// CPython: Modules/_threadmodule.c:1879 thread_PyThread_start_joinable_thread

package _thread

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tamnd/gopy/objects"
)

// ThreadHandleType is the type singleton for _thread._ThreadHandle.
//
// CPython: Modules/_threadmodule.c:66 ThreadHandle_Type_spec
var ThreadHandleType = objects.NewType("_thread._ThreadHandle", []*objects.Type{objects.ObjectType()})

// mainThreadIdent captures the goroutine that imported _thread.
// CPython exposes it via _get_main_thread_ident so _MainThread can
// reuse the existing ident instead of asking the OS for a fresh one.
//
// CPython: Modules/_threadmodule.c thread__get_main_thread_ident
var mainThreadIdent int64

func init() {
	mainThreadIdent = goid()

	ThreadHandleType.Repr = threadHandleRepr
	ThreadHandleType.Str = threadHandleRepr
	objects.SetTypeDescr(ThreadHandleType, "ident",
		objects.NewGetSetDescr("ident", threadHandleGetIdent, nil))
	objects.SetTypeDescr(ThreadHandleType, "join",
		objects.NewBuiltinFunction("join", threadHandleJoin))
	objects.SetTypeDescr(ThreadHandleType, "is_done",
		objects.NewBuiltinFunction("is_done", threadHandleIsDone))
	objects.SetTypeDescr(ThreadHandleType, "_set_done",
		objects.NewBuiltinFunction("_set_done", threadHandleSetDone))
}

// threadHandleObject backs _ThreadHandle. `done` is closed exactly once
// when the thread finishes; join() blocks on it and is_done() polls it.
//
// CPython: Modules/_threadmodule.c:103 ThreadHandle
type threadHandleObject struct {
	objects.Header
	ident int64
	done  chan struct{}
	once  sync.Once
}

func newThreadHandle(ident int64) *threadHandleObject {
	h := &threadHandleObject{ident: ident, done: make(chan struct{})}
	h.Init(ThreadHandleType)
	return h
}

func threadHandleRepr(o objects.Object) (string, error) {
	h := o.(*threadHandleObject)
	state := "running"
	if h.isDone() {
		state = "done"
	}
	return fmt.Sprintf("<_thread._ThreadHandle ident=%d state=%s>", h.ident, state), nil
}

func (h *threadHandleObject) isDone() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

func (h *threadHandleObject) setDone() {
	h.once.Do(func() { close(h.done) })
}

func threadHandleGetIdent(o objects.Object) (objects.Object, error) {
	h, ok := o.(*threadHandleObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'ident' requires '_ThreadHandle' object")
	}
	return objects.NewInt(h.ident), nil
}

// threadHandleJoin blocks until the underlying thread finishes or the
// timeout (seconds, float) expires. CPython treats a negative or None
// timeout as wait-forever.
//
// CPython: Modules/_threadmodule.c:451 ThreadHandle_join
func threadHandleJoin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: join() missing self")
	}
	h, ok := args[0].(*threadHandleObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: join() requires _ThreadHandle")
	}
	var timeoutSecs float64 = -1
	if len(args) > 1 && args[1] != objects.None() {
		switch v := args[1].(type) {
		case *objects.Float:
			timeoutSecs = v.Float64()
		case *objects.Int:
			i, _ := v.Int64()
			timeoutSecs = float64(i)
		}
	}
	if timeoutSecs < 0 {
		<-h.done
		return objects.None(), nil
	}
	select {
	case <-h.done:
	case <-time.After(time.Duration(timeoutSecs * float64(time.Second))):
	}
	return objects.None(), nil
}

func threadHandleIsDone(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: is_done() missing self")
	}
	h, ok := args[0].(*threadHandleObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: is_done() requires _ThreadHandle")
	}
	return objects.NewBool(h.isDone()), nil
}

func threadHandleSetDone(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: _set_done() missing self")
	}
	h, ok := args[0].(*threadHandleObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: _set_done() requires _ThreadHandle")
	}
	h.setDone()
	return objects.None(), nil
}

// threadMakeThreadHandle implements _thread._make_thread_handle(ident).
// Wraps an existing thread ident in a handle; used by _MainThread and
// _DummyThread which adopt the goroutine they're running on.
//
// CPython: Modules/_threadmodule.c thread__make_thread_handle
func threadMakeThreadHandle(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _make_thread_handle() takes 1 argument")
	}
	var ident int64
	switch v := args[0].(type) {
	case *objects.Int:
		i, _ := v.Int64()
		ident = i
	default:
		return nil, fmt.Errorf("TypeError: _make_thread_handle() ident must be int")
	}
	return newThreadHandle(ident), nil
}

// threadDaemonThreadsAllowed: gopy always permits daemon threads; the
// flag exists so threading.py can refuse daemons in subinterpreters
// that don't allow them, which gopy doesn't model.
//
// CPython: Modules/_threadmodule.c thread_daemon_threads_allowed
func threadDaemonThreadsAllowed(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return objects.True(), nil
}

// threadShutdown is called from threading.py at interpreter shutdown
// to wait for non-daemon threads. gopy's goroutine lifecycle is
// managed by Go's runtime; there is nothing extra to wait on here.
//
// CPython: Modules/_threadmodule.c thread_shutdown
func threadShutdown(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// threadIsMainInterpreter: gopy runs one interpreter, so this is
// always True.
//
// CPython: Modules/_threadmodule.c thread__is_main_interpreter
func threadIsMainInterpreter(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return objects.True(), nil
}

// threadGetMainThreadIdent returns the ident of the goroutine that
// imported _thread, modelling CPython's main-interpreter thread.
//
// CPython: Modules/_threadmodule.c thread__get_main_thread_ident
func threadGetMainThreadIdent(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("TypeError: _get_main_thread_ident() takes no arguments")
	}
	return objects.NewInt(mainThreadIdent), nil
}

// threadStartJoinableThread implements
// _thread.start_joinable_thread(function, *, handle=None, daemon=True).
// Starts a goroutine that calls function() and signals completion on
// the returned (or supplied) handle. The handle's ident is set to the
// new goroutine's id.
//
// CPython: Modules/_threadmodule.c:1879 thread_PyThread_start_joinable_thread
func threadStartJoinableThread(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: start_joinable_thread() takes 1 positional argument (%d given)", len(args))
	}
	function := args[0]

	var handle *threadHandleObject
	if h, ok := kwargs["handle"]; ok && h != objects.None() {
		hh, ok2 := h.(*threadHandleObject)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: handle must be _ThreadHandle or None")
		}
		handle = hh
	}
	if handle == nil {
		handle = newThreadHandle(0)
	}

	idCh := make(chan int64, 1)
	atomic.AddInt64(&activeThreadCount, 1)
	go func() {
		defer atomic.AddInt64(&activeThreadCount, -1)
		defer handle.setDone()
		id := goid()
		handle.ident = id
		idCh <- id
		tp := function.Type()
		if tp.Call != nil {
			tp.Call(function, nil, nil) //nolint:errcheck
		}
	}()
	<-idCh
	return handle, nil
}
