// Package _thread is the gopy port of CPython's _thread builtin module.
// It backs Lib/threading.py and reprlib.py with goroutine identity,
// mutual-exclusion locks, and thread creation. The CPython source lives
// in Modules/_threadmodule.c (~2000 lines); this port covers the surface
// those two library files actually use.
//
// CPython: Modules/_threadmodule.c:1 (module init)

package _thread

import (
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_thread", buildModule)
}

// buildModule materializes the _thread module dict. Mirrors
// thread_module_exec on the C side.
//
// CPython: Modules/_threadmodule.c:2700 thread_module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_thread")
	d := m.Dict()

	entries := []struct {
		name string
		fn   func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"get_ident", threadGetIdent},
		{"get_native_id", threadGetNativeID},
		{"allocate_lock", threadAllocateLock},
		{"start_new_thread", threadStartNewThread},
		{"start_new", threadStartNewThread},
		{"start_joinable_thread", threadStartJoinableThread},
		{"_make_thread_handle", threadMakeThreadHandle},
		{"_get_main_thread_ident", threadGetMainThreadIdent},
		{"daemon_threads_allowed", threadDaemonThreadsAllowed},
		{"_shutdown", threadShutdown},
		{"_is_main_interpreter", threadIsMainInterpreter},
		{"stack_size", threadStackSize},
		{"_count", threadCount},
	}
	for _, e := range entries {
		bf := objects.NewBuiltinFunction(e.name, e.fn)
		if err := d.SetItem(objects.NewStr(e.name), bf); err != nil {
			return nil, err
		}
	}

	// TIMEOUT_MAX: max seconds expressible as microseconds fitting in
	// a signed 64-bit integer. CPython formula: PY_TIMEOUT_MAX * 1e-6.
	// PY_TIMEOUT_MAX is LLONG_MAX (9223372036854775807 us).
	// We expose math.MaxFloat64 / 1e9 as instructed.
	//
	// CPython: Modules/_threadmodule.c:2718 TIMEOUT_MAX
	timeoutMax := math.MaxFloat64 / 1e9
	if err := d.SetItem(objects.NewStr("TIMEOUT_MAX"), objects.NewFloat(timeoutMax)); err != nil {
		return nil, err
	}

	// Expose the lock type so `isinstance(lk, _thread.lock)` works.
	if err := d.SetItem(objects.NewStr("lock"), LockType); err != nil {
		return nil, err
	}
	// RLock is the recursive lock type used by threading.RLock and
	// (via _py_warnings) by warnings._lock.
	//
	// CPython: Modules/_threadmodule.c:2691 _thread_module addtype RLock
	if err := d.SetItem(objects.NewStr("RLock"), RLockType); err != nil {
		return nil, err
	}
	// _ThreadHandle: 3.14 thread-lifecycle handle type.
	//
	// CPython: Modules/_threadmodule.c:2661 PyDict_SetItemString "_ThreadHandle"
	if err := d.SetItem(objects.NewStr("_ThreadHandle"), ThreadHandleType); err != nil {
		return nil, err
	}
	// LockType: attribute alias exposed for threading.py module load.
	//
	// CPython: Modules/_threadmodule.c PyModule_AddType(module, lock_type)
	if err := d.SetItem(objects.NewStr("LockType"), LockType); err != nil {
		return nil, err
	}
	// _local is the thread-local storage type; provide a minimal stub.
	if err := d.SetItem(objects.NewStr("_local"), localType); err != nil {
		return nil, err
	}

	// error is the module-level exception class.
	errCls := objects.NewType("_thread.error", []*objects.Type{objects.ObjectType()})
	if err := d.SetItem(objects.NewStr("error"), errCls); err != nil {
		return nil, err
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// goroutine ID helper (mirrors PyThread_get_thread_ident_ex).
// ---------------------------------------------------------------------------

// goid returns the current goroutine's numeric ID by parsing the first
// line of runtime.Stack output. This is the same technique used by
// vm/eval_call.go.
//
// CPython: Modules/_threadmodule.c:2082 thread_get_ident (reads
// PyThread_get_thread_ident_ex which wraps pthread_self / GetCurrentThreadId)
func goid() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const prefix = "goroutine "
	if n <= len(prefix) {
		return 0
	}
	s := buf[len(prefix):n]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	id, _ := strconv.ParseInt(string(s[:end]), 10, 64)
	return id
}

// ThreadIdentHook resolves the active Python thread's identity for the
// current goroutine. The vm installs it so get_ident and thread-local
// storage key on the Python thread state rather than the raw goroutine.
// gopy backs each generator and coroutine with its own goroutine, but
// CPython runs them on the thread that resumes them, so their identity
// must stay constant across a yield. Returns 0 when no Python thread is
// active on this goroutine, in which case callers fall back to goid().
//
// CPython: Include/cpython/pystate.h PyThreadState.thread_id
var ThreadIdentHook func() int64

// ThreadSpawnHook allocates a Python thread state for a goroutine that
// start_new_thread is about to launch. It runs on the parent goroutine
// and returns the new thread's identity (so start_new_thread hands it
// back synchronously, matching get_ident inside the child) plus enter /
// leave callbacks the child goroutine runs to install and retire the
// thread as its active thread.
//
// CPython: Modules/_threadmodule.c:1166 thread_PyThread_start_new_thread
var ThreadSpawnHook func() (ident int64, enter func(), leave func())

// pyThreadIdent returns the stable Python-thread identity for the
// current goroutine, preferring the active thread state over the raw
// goroutine id so generator and coroutine bodies keep their creator's
// identity across the goroutine boundary.
func pyThreadIdent() int64 {
	if ThreadIdentHook != nil {
		if id := ThreadIdentHook(); id != 0 {
			return id
		}
	}
	return goid()
}

// threadGetIdent implements _thread.get_ident().
//
// CPython: Modules/_threadmodule.c:2082 thread_get_ident
func threadGetIdent(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_ident() takes no arguments")
	}
	id := pyThreadIdent()
	if id == 0 {
		return nil, fmt.Errorf("_thread.error: no current thread ident")
	}
	return objects.NewInt(id), nil
}

// threadGetNativeID implements _thread.get_native_id(). On Go we have
// no OS thread ID; return the goroutine ID as a reasonable proxy.
//
// CPython: Modules/_threadmodule.c:2106 thread_get_native_id
func threadGetNativeID(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_native_id() takes no arguments")
	}
	return objects.NewInt(goid()), nil
}

// ---------------------------------------------------------------------------
// lock type.
// ---------------------------------------------------------------------------

// lockObject backs _thread.lock instances. A zero-value mutex with a
// separate "locked" flag mirrors CPython's PyThread_type_lock, which
// wraps pthreads or Win32 CRITICAL_SECTION.
//
// CPython: Modules/_threadmodule.c:98 lockobject
type lockObject struct {
	objects.Header
	mu     sync.Mutex
	locked int32 // 1 when held, 0 when free; guarded by mu transitions
}

// LockType is the type singleton for _thread.lock.
//
// CPython: Modules/_threadmodule.c:949 Locktype
var LockType = objects.NewType("lock", []*objects.Type{objects.ObjectType()})

func init() {
	LockType.Repr = lockRepr
	LockType.Str = lockRepr
	LockType.Getattro = lockGetattr
	LockType.TpNew = lockNew
	// `with lock:` uses LOAD_SPECIAL which walks the type MRO; expose
	// __enter__/__exit__ as type-level descriptors so the context
	// manager protocol works.
	//
	// CPython: Modules/_threadmodule.c:907 lock_methods
	objects.SetTypeDescr(LockType, "__enter__", objects.NewBuiltinFunction("__enter__", lockEnterDescr))
	objects.SetTypeDescr(LockType, "__exit__", objects.NewBuiltinFunction("__exit__", lockExitDescr))
}

func lockEnterDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	lk, ok := args[0].(*lockObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected lock self")
	}
	return lockAcquire(lk, nil, nil)
}

func lockExitDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	lk, ok := args[0].(*lockObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected lock self")
	}
	if _, err := lockRelease(lk, nil, nil); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// newLockObject allocates a new, unlocked lock.
//
// CPython: Modules/_threadmodule.c:963 newlockobject
func newLockObject() *lockObject {
	lk := &lockObject{}
	lk.Init(LockType)
	return lk
}

func lockRepr(o objects.Object) (string, error) {
	lk := o.(*lockObject)
	state := "unlocked"
	if atomic.LoadInt32(&lk.locked) != 0 {
		state = "locked"
	}
	return fmt.Sprintf("<%s _thread.lock object at %p>", state, lk), nil
}

// lockGetattr dispatches attribute access for lock method names and the
// 'locked' property, mirroring lock_methods and the lockobject getset.
//
// CPython: Modules/_threadmodule.c:907 lock_methods
// CPython: Modules/_threadmodule.c:944 lock_getsetlist
func lockGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	lk, ok := o.(*lockObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected lock object")
	}
	n, ok2 := name.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "acquire", "acquire_lock":
		return objects.NewBuiltinFunction("acquire", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return lockAcquire(lk, args, kwargs)
		}), nil
	case "release", "release_lock":
		return objects.NewBuiltinFunction("release", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return lockRelease(lk, args, kwargs)
		}), nil
	case "locked", "locked_lock":
		return objects.NewBuiltinFunction("locked", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return lockLocked(lk, args, kwargs)
		}), nil
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return lockAcquire(lk, nil, nil)
		}), nil
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return lockRelease(lk, nil, nil)
		}), nil
	}
	return nil, fmt.Errorf("AttributeError: 'lock' object has no attribute '%s'", n.Value())
}

// lockAcquire implements lock.acquire(blocking=True, timeout=-1).
// A timeout of -1 means wait indefinitely (same as CPython's
// TIMEOUT_MAX sentinel). Non-blocking (blocking=False) returns
// immediately if the lock cannot be obtained.
//
// CPython: Modules/_threadmodule.c:817 lock_PyThread_acquire_lock
func lockAcquire(lk *lockObject, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	blocking := true
	timeoutSecs := -1.0

	if len(args) >= 1 {
		switch a0 := args[0].(type) {
		case *objects.Bool:
			v, _ := a0.Int64()
			blocking = v != 0
		case *objects.Int:
			v, _ := a0.Int64()
			blocking = v != 0
		default:
			return nil, fmt.Errorf("TypeError: acquire() blocking must be bool or int")
		}
	}
	if t, ok := kwargs["timeout"]; ok {
		f, ok2 := t.(*objects.Float)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: acquire() timeout must be float")
		}
		timeoutSecs = f.Float64()
	}
	if len(args) >= 2 {
		f, ok := args[1].(*objects.Float)
		if !ok {
			return nil, fmt.Errorf("TypeError: acquire() timeout must be float")
		}
		timeoutSecs = f.Float64()
	}

	if !blocking {
		// trylock: succeed only if currently unlocked.
		acquired := lk.mu.TryLock()
		if acquired {
			atomic.StoreInt32(&lk.locked, 1)
		}
		return objects.NewBool(acquired), nil
	}

	if timeoutSecs < 0 {
		// Block indefinitely.
		lk.mu.Lock()
		atomic.StoreInt32(&lk.locked, 1)
		return objects.True(), nil
	}

	// Timed wait using a poll loop (no native timed mutex in Go stdlib).
	deadline := time.Now().Add(time.Duration(timeoutSecs * float64(time.Second)))
	for {
		if lk.mu.TryLock() {
			atomic.StoreInt32(&lk.locked, 1)
			return objects.True(), nil
		}
		if time.Now().After(deadline) {
			return objects.False(), nil
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// lockRelease implements lock.release().
//
// CPython: Modules/_threadmodule.c:858 lock_PyThread_release_lock
func lockRelease(lk *lockObject, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if atomic.LoadInt32(&lk.locked) == 0 {
		return nil, fmt.Errorf("RuntimeError: release unlocked lock")
	}
	atomic.StoreInt32(&lk.locked, 0)
	lk.mu.Unlock()
	return objects.None(), nil
}

// lockLocked implements lock.locked().
//
// CPython: Modules/_threadmodule.c:893 lock_locked_lock
func lockLocked(lk *lockObject, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(atomic.LoadInt32(&lk.locked) != 0), nil
}

// lockNew is the tp_new slot. threading.Lock() (alias for
// _thread.LockType()) calls this; CPython 3.14 added direct
// instantiation alongside allocate_lock().
//
// CPython: Modules/_threadmodule.c lock_new
func lockNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: lock() takes no arguments")
	}
	return newLockObject(), nil
}

// threadAllocateLock implements _thread.allocate_lock().
//
// CPython: Modules/_threadmodule.c:2063 thread_PyThread_allocate_lock
func threadAllocateLock(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: allocate_lock() takes no arguments")
	}
	return newLockObject(), nil
}

// ---------------------------------------------------------------------------
// start_new_thread.
// ---------------------------------------------------------------------------

// activeThreadCount tracks threads started via start_new_thread that
// have not yet returned, mirroring interp->threads.count.
//
// CPython: Modules/_threadmodule.c:339 (count management)
var activeThreadCount int64

// threadStartNewThread implements _thread.start_new_thread(func, args[, kwargs]).
// Starts a goroutine that calls func(*args, **kwargs) and returns the
// goroutine ID as an int. Any exception raised inside the thread is
// silently swallowed (matching CPython's behavior for daemon threads).
//
// CPython: Modules/_threadmodule.c:1879 thread_PyThread_start_new_thread
func threadStartNewThread(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: start_new_thread() takes 2 or 3 arguments (%d given)", len(args))
	}
	callable := args[0]
	targs, ok := args[1].(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: 2nd arg must be a tuple")
	}
	var tkwargs map[string]objects.Object
	if len(args) == 3 {
		d, ok2 := args[2].(*objects.Dict)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: optional 3rd arg must be a dictionary")
		}
		tkwargs = dictToGoMap(d)
	}

	// Convert tuple to positional slice.
	pos := make([]objects.Object, targs.Len())
	for i := range pos {
		pos[i] = targs.Item(i)
	}

	// Allocate the child's Python thread state up front (on the parent
	// goroutine) so start_new_thread can return the identity that
	// get_ident will report inside the child. Falls back to the raw
	// goroutine id when the vm has not installed the spawn hook.
	//
	// CPython: Modules/_threadmodule.c:1166 thread_PyThread_start_new_thread
	var (
		ident int64
		enter func()
		leave func()
	)
	if ThreadSpawnHook != nil {
		ident, enter, leave = ThreadSpawnHook()
	}

	// Channel to return the thread identity before the goroutine runs.
	idCh := make(chan int64, 1)

	atomic.AddInt64(&activeThreadCount, 1)
	go func() {
		defer atomic.AddInt64(&activeThreadCount, -1)
		if enter != nil {
			enter()
			defer leave()
			idCh <- ident
		} else {
			idCh <- goid()
		}
		// Route through objects.Call so bound methods and other
		// vectorcall-only callables run. callable.Type().Call is nil for
		// such objects, which would silently skip the thread body.
		//
		// CPython: Modules/_threadmodule.c:333 thread_run (PyObject_Call)
		argsTuple := objects.NewTuple(pos)
		var kwDict *objects.Dict
		if len(tkwargs) > 0 {
			kwDict = objects.NewDict()
			for k, v := range tkwargs {
				if err := kwDict.SetItem(objects.NewStr(k), v); err != nil {
					writeThreadUnraisable(callable, err)
					return
				}
			}
		}
		if _, err := objects.Call(callable, argsTuple, kwDict); err != nil {
			writeThreadUnraisable(callable, err)
		}
	}()

	id := <-idCh
	return objects.NewInt(id), nil
}

// dictToGoMap converts a Python dict to a Go map for use as kwargs.
func dictToGoMap(d *objects.Dict) map[string]objects.Object {
	keys := d.Keys()
	if len(keys) == 0 {
		return nil
	}
	m := make(map[string]objects.Object, len(keys))
	for _, k := range keys {
		ks, ok := k.(*objects.Unicode)
		if !ok {
			continue
		}
		v, err := d.GetItem(k)
		if err != nil {
			continue
		}
		m[ks.Value()] = v
	}
	return m
}

// ---------------------------------------------------------------------------
// stack_size stub.
// ---------------------------------------------------------------------------

// threadStackSize implements _thread.stack_size([size]). Returns 0 (no
// per-goroutine stack size is configurable in Go).
//
// CPython: Modules/_threadmodule.c:2141 thread_stack_size
func threadStackSize(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: stack_size() takes no keyword arguments")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: stack_size() takes at most 1 argument (%d given)", len(args))
	}
	return objects.NewInt(0), nil
}

// threadCount implements _thread._count().
//
// CPython: Modules/_threadmodule.c:2122 thread__count
func threadCount(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: _count() takes no arguments")
	}
	return objects.NewInt(atomic.LoadInt64(&activeThreadCount)), nil
}

// ---------------------------------------------------------------------------
// _thread._local — per-thread (here per-goroutine) attribute storage.
// CPython tracks one __dict__ per thread under a single PyObject; subclass
// __init__ reruns automatically the first time a thread touches the
// instance. We mirror that behavior using a sync.Map keyed by goroutine
// id, but drop CPython's localdummy/weakref dance. Go's GC takes care
// of lifetime for us.
//
// CPython: Modules/_threadmodule.c:1396 localobject
// CPython: Modules/_threadmodule.c:1452 local_new
// CPython: Modules/_threadmodule.c:1639 _ldict
// CPython: Modules/_threadmodule.c:1691 local_setattro
// CPython: Modules/_threadmodule.c:1752 local_getattro
// ---------------------------------------------------------------------------

var localType = objects.NewType("_thread._local", []*objects.Type{objects.ObjectType()})

func init() {
	localType.TpNew = localNew
	localType.Getattro = localGetattr
	localType.Setattro = localSetattr
}

type localObj struct {
	objects.Header
	args *objects.Tuple            // ctor args replayed on each new thread
	kw   map[string]objects.Object // ctor kwargs replayed on each new thread
	mu   sync.Mutex                // serializes new-thread dict creation
	data sync.Map                  // goid -> *objects.Dict
}

// initInherited reports whether cls inherits __init__ straight from
// object. Mirrors objects.initInheritedFromObject (unexported there).
//
// CPython: Modules/_threadmodule.c:1454 local_new (tp_init == PyBaseObject_Type.tp_init)
func initInherited(cls *objects.Type) bool {
	descr, _ := objects.LookupDescriptor(cls, "__init__")
	if descr == nil {
		return true
	}
	stored, _ := objects.LookupDescriptor(objects.ObjectType(), "__init__")
	return descr == stored
}

// localNew allocates a _local. With the default __init__, CPython
// rejects any constructor arguments; otherwise it stashes (args, kw)
// for per-thread replay.
//
// CPython: Modules/_threadmodule.c:1452 local_new
func localNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if (len(args) > 0 || len(kwargs) > 0) && initInherited(cls) {
		return nil, fmt.Errorf("TypeError: Initialization arguments are not supported")
	}
	lc := &localObj{}
	lc.Init(cls)
	if len(args) > 0 {
		lc.args = objects.NewTuple(append([]objects.Object(nil), args...))
	}
	if len(kwargs) > 0 {
		lc.kw = make(map[string]objects.Object, len(kwargs))
		for k, v := range kwargs {
			lc.kw[k] = v
		}
	}
	// Pre-seed the construction thread's dict so __init__ (run by
	// type_call after we return) does not double-fire from ldict.
	//
	// CPython: Modules/_threadmodule.c:1497 create_localsdict
	lc.data.Store(pyThreadIdent(), objects.NewDict())
	return lc, nil
}

// ldict returns the dict for the current goroutine, creating it on
// first access and replaying subclass __init__ for new threads.
//
// CPython: Modules/_threadmodule.c:1639 _ldict
func (lc *localObj) ldict() (*objects.Dict, error) {
	id := pyThreadIdent()
	if v, ok := lc.data.Load(id); ok {
		return v.(*objects.Dict), nil
	}
	lc.mu.Lock()
	if v, ok := lc.data.Load(id); ok {
		lc.mu.Unlock()
		return v.(*objects.Dict), nil
	}
	d := objects.NewDict()
	lc.data.Store(id, d)
	lc.mu.Unlock()

	// Run subclass __init__ for this thread. The construction thread
	// already ran it via type_call and gets short-circuited above.
	//
	// CPython: Modules/_threadmodule.c:1664 _ldict (Py_TYPE(self)->tp_init)
	if !initInherited(lc.Type()) {
		initDescr, _ := objects.LookupDescriptor(lc.Type(), "__init__")
		if initDescr != nil {
			callArgs := []objects.Object{lc}
			if lc.args != nil {
				for i := 0; i < lc.args.Len(); i++ {
					callArgs = append(callArgs, lc.args.Item(i))
				}
			}
			var kw *objects.Dict
			if len(lc.kw) > 0 {
				kw = objects.NewDict()
				for k, v := range lc.kw {
					_ = kw.SetItem(objects.NewStr(k), v)
				}
			}
			if _, err := objects.Call(initDescr, objects.NewTuple(callArgs), kw); err != nil {
				// Drop the half-built dict so the next access retries.
				//
				// CPython: Modules/_threadmodule.c:1670 PyDict_DelItem
				lc.data.Delete(id)
				return nil, err
			}
		}
	}
	return d, nil
}

// localGetattr exposes __dict__ as the per-thread dict, looks attrs up
// in it first, and falls back to the type MRO for class attributes.
//
// CPython: Modules/_threadmodule.c:1752 local_getattro
func localGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	lc, ok := o.(*localObj)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	d, err := lc.ldict()
	if err != nil {
		return nil, err
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	if n.Value() == "__dict__" {
		return d, nil
	}
	if v, gerr := d.GetItem(name); gerr == nil {
		return v, nil
	}
	return objects.GenericGetAttr(o, name)
}

// localSetattr stores into the per-thread dict. __dict__ itself is
// read-only, matching CPython.
//
// CPython: Modules/_threadmodule.c:1691 local_setattro
func localSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	lc, ok := o.(*localObj)
	if !ok {
		return objects.GenericSetAttr(o, name, value)
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: attribute name must be string")
	}
	if n.Value() == "__dict__" {
		return fmt.Errorf("AttributeError: '%s' object attribute '__dict__' is read-only", lc.Type().Name)
	}
	d, err := lc.ldict()
	if err != nil {
		return err
	}
	if value == nil {
		if _, gerr := d.GetItem(name); gerr != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", lc.Type().Name, n.Value())
		}
		return d.DelItem(name)
	}
	return d.SetItem(name, value)
}
