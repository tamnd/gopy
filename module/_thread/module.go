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

// threadGetIdent implements _thread.get_ident().
//
// CPython: Modules/_threadmodule.c:2082 thread_get_ident
func threadGetIdent(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_ident() takes no arguments")
	}
	id := goid()
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

	// Channel to return the goroutine ID before the goroutine runs.
	idCh := make(chan int64, 1)

	atomic.AddInt64(&activeThreadCount, 1)
	go func() {
		defer atomic.AddInt64(&activeThreadCount, -1)
		idCh <- goid()
		tp := callable.Type()
		if tp.Call != nil {
			tp.Call(callable, pos, tkwargs) //nolint:errcheck
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
// _local stub (thread-local storage type).
// ---------------------------------------------------------------------------

// localType is a minimal stub for _thread._local. The full
// implementation stores per-thread instance dicts; this version is
// enough for `import threading` to complete without crashing.
//
// CPython: Modules/_threadmodule.c:1174 localtype
var localType = objects.NewType("_local", []*objects.Type{objects.ObjectType()})

func init() {
	localType.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		lc := &localObj{}
		lc.Init(localType)
		return lc, nil
	}
	localType.Getattro = localGetattr
	localType.Setattro = localSetattr
}

type localObj struct {
	objects.Header
	mu   sync.Mutex
	data sync.Map // map[int64]*objects.Dict per goroutine
}

func localGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	lc := o.(*localObj)
	id := goid()
	lc.mu.Lock()
	var d *objects.Dict
	if v, ok := lc.data.Load(id); ok {
		d = v.(*objects.Dict)
	} else {
		d = objects.NewDict()
		lc.data.Store(id, d)
	}
	lc.mu.Unlock()
	return d.GetItem(name)
}

func localSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	lc := o.(*localObj)
	id := goid()
	lc.mu.Lock()
	var d *objects.Dict
	if v, ok := lc.data.Load(id); ok {
		d = v.(*objects.Dict)
	} else {
		d = objects.NewDict()
		lc.data.Store(id, d)
	}
	lc.mu.Unlock()
	if value == nil {
		return d.DelItem(name)
	}
	return d.SetItem(name, value)
}
