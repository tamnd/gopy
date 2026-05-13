// Recursive lock for _thread. Mirrors CPython's rlockobject in
// Modules/_threadmodule.c. The CPython side wraps _PyRecursiveMutex;
// the Go port tracks the owning goroutine ID and a recursion counter
// guarded by a sync.Mutex.
//
// CPython: Modules/_threadmodule.c:1009 rlockobject

package _thread

import (
	"fmt"
	"sync"
	"time"

	"github.com/tamnd/gopy/objects"
)

// rlockObject backs _thread.RLock instances. owner is the goroutine
// holding the lock (0 when free); count is the recursion depth.
//
// CPython: Modules/_threadmodule.c:1009 rlockobject
type rlockObject struct {
	objects.Header
	mu    sync.Mutex // guards owner and count below
	gate  sync.Mutex // held while a non-owner thread waits
	owner int64
	count int64
}

// RLockType is the type singleton for _thread.RLock.
//
// CPython: Modules/_threadmodule.c:1278 PyRLock_Type
var RLockType = objects.NewType("RLock", []*objects.Type{objects.ObjectType()})

func init() {
	RLockType.Repr = rlockRepr
	RLockType.Str = rlockRepr
	RLockType.Getattro = rlockGetattr
	RLockType.TpNew = rlockNew
	// LOAD_SPECIAL walks the type MRO for __enter__ / __exit__ rather
	// than going through instance Getattro, so context-manager use
	// (`with rlock:`) needs type-level descriptors.
	//
	// CPython: Modules/_threadmodule.c:1246 rlock_methods (__enter__/__exit__)
	objects.SetTypeDescr(RLockType, "__enter__", objects.NewBuiltinFunction("__enter__", rlockEnterDescr))
	objects.SetTypeDescr(RLockType, "__exit__", objects.NewBuiltinFunction("__exit__", rlockExitDescr))
}

// rlockEnterDescr is the type-level __enter__ binding. LOAD_SPECIAL
// pushes (descr, self) so args[0] is the RLock instance.
func rlockEnterDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	r, ok := args[0].(*rlockObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected RLock self")
	}
	return rlockAcquire(r, nil, nil)
}

// rlockExitDescr is the type-level __exit__ binding.
func rlockExitDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	r, ok := args[0].(*rlockObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected RLock self")
	}
	if _, err := rlockRelease(r); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// rlockNew implements RLock.__new__.
//
// CPython: Modules/_threadmodule.c:1204 rlock_new
func rlockNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	r := &rlockObject{}
	r.Init(RLockType)
	return r, nil
}

// rlockRepr formats the RLock the way CPython does, including owner
// goroutine ID and recursion count.
//
// CPython: Modules/_threadmodule.c:1215 rlock_repr
func rlockRepr(o objects.Object) (string, error) {
	r := o.(*rlockObject)
	r.mu.Lock()
	owner := r.owner
	count := r.count
	r.mu.Unlock()
	state := "unlocked"
	if count > 0 {
		state = "locked"
	}
	return fmt.Sprintf("<%s _thread.RLock object owner=%d count=%d at %p>", state, owner, count, r), nil
}

// rlockGetattr dispatches the method table for RLock instances.
//
// CPython: Modules/_threadmodule.c:1246 rlock_methods
func rlockGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	r, ok := o.(*rlockObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected RLock object")
	}
	n, ok2 := name.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "acquire":
		return objects.NewBuiltinFunction("acquire", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockAcquire(r, args, kwargs)
		}), nil
	case "release":
		return objects.NewBuiltinFunction("release", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockRelease(r)
		}), nil
	case "locked":
		return objects.NewBuiltinFunction("locked", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockLocked(r), nil
		}), nil
	case "_is_owned":
		return objects.NewBuiltinFunction("_is_owned", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockIsOwned(r), nil
		}), nil
	case "_acquire_restore":
		return objects.NewBuiltinFunction("_acquire_restore", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockAcquireRestore(r, args)
		}), nil
	case "_release_save":
		return objects.NewBuiltinFunction("_release_save", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockReleaseSave(r)
		}), nil
	case "_recursion_count":
		return objects.NewBuiltinFunction("_recursion_count", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockRecursionCount(r), nil
		}), nil
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return rlockAcquire(r, nil, nil)
		}), nil
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if _, err := rlockRelease(r); err != nil {
				return nil, err
			}
			return objects.None(), nil
		}), nil
	}
	return nil, fmt.Errorf("AttributeError: 'RLock' object has no attribute '%s'", n.Value())
}

// rlockAcquire implements RLock.acquire(blocking=True, timeout=-1).
// If the calling goroutine already owns the lock the recursion count
// is bumped; otherwise the gate mutex is taken and the calling
// goroutine becomes the owner.
//
// CPython: Modules/_threadmodule.c:1041 rlock_acquire
func rlockAcquire(r *rlockObject, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
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
	if len(args) >= 2 {
		f, ok := args[1].(*objects.Float)
		if !ok {
			return nil, fmt.Errorf("TypeError: acquire() timeout must be float")
		}
		timeoutSecs = f.Float64()
	}
	if t, ok := kwargs["timeout"]; ok {
		f, ok2 := t.(*objects.Float)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: acquire() timeout must be float")
		}
		timeoutSecs = f.Float64()
	}

	me := goid()
	r.mu.Lock()
	if r.owner == me {
		r.count++
		r.mu.Unlock()
		return objects.True(), nil
	}
	r.mu.Unlock()

	if !blocking {
		if !r.gate.TryLock() {
			return objects.False(), nil
		}
		r.mu.Lock()
		r.owner = me
		r.count = 1
		r.mu.Unlock()
		return objects.True(), nil
	}

	if timeoutSecs < 0 {
		r.gate.Lock()
		r.mu.Lock()
		r.owner = me
		r.count = 1
		r.mu.Unlock()
		return objects.True(), nil
	}

	deadline := time.Now().Add(time.Duration(timeoutSecs * float64(time.Second)))
	for {
		if r.gate.TryLock() {
			r.mu.Lock()
			r.owner = me
			r.count = 1
			r.mu.Unlock()
			return objects.True(), nil
		}
		if time.Now().After(deadline) {
			return objects.False(), nil
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// rlockRelease decrements the recursion counter; when it hits zero the
// gate mutex is released so a waiting goroutine can take ownership.
//
// CPython: Modules/_threadmodule.c:1083 rlock_release
func rlockRelease(r *rlockObject) (objects.Object, error) {
	me := goid()
	r.mu.Lock()
	if r.owner != me || r.count == 0 {
		r.mu.Unlock()
		return nil, fmt.Errorf("RuntimeError: cannot release un-acquired lock")
	}
	r.count--
	released := r.count == 0
	if released {
		r.owner = 0
	}
	r.mu.Unlock()
	if released {
		r.gate.Unlock()
	}
	return objects.None(), nil
}

// rlockLocked reports whether anyone (not just the caller) holds the
// lock.
//
// CPython: Modules/_threadmodule.c:1114 rlock_locked
func rlockLocked(r *rlockObject) objects.Object {
	r.mu.Lock()
	locked := r.count > 0
	r.mu.Unlock()
	return objects.NewBool(locked)
}

// rlockIsOwned reports whether the current goroutine owns the lock.
//
// CPython: Modules/_threadmodule.c:1190 rlock_is_owned
func rlockIsOwned(r *rlockObject) objects.Object {
	me := goid()
	r.mu.Lock()
	owned := r.owner == me && r.count > 0
	r.mu.Unlock()
	return objects.NewBool(owned)
}

// rlockRecursionCount reports the current owner's recursion depth, 0
// when the current thread does not hold the lock.
//
// CPython: Modules/_threadmodule.c:1174 rlock_recursion_count
func rlockRecursionCount(r *rlockObject) objects.Object {
	me := goid()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner == me {
		return objects.NewInt(r.count)
	}
	return objects.NewInt(0)
}

// rlockReleaseSave fully releases the lock and returns a (count, owner)
// tuple a later _acquire_restore can use. Used by threading.Condition.
//
// CPython: Modules/_threadmodule.c:1150 rlock_release_save
func rlockReleaseSave(r *rlockObject) (objects.Object, error) {
	me := goid()
	r.mu.Lock()
	if r.owner != me || r.count == 0 {
		r.mu.Unlock()
		return nil, fmt.Errorf("RuntimeError: cannot release un-acquired lock")
	}
	owner := r.owner
	count := r.count
	r.owner = 0
	r.count = 0
	r.mu.Unlock()
	r.gate.Unlock()
	return objects.NewTuple([]objects.Object{
		objects.NewInt(count),
		objects.NewInt(owner),
	}), nil
}

// rlockAcquireRestore re-takes the lock and sets owner/count from
// state.
//
// CPython: Modules/_threadmodule.c:1127 rlock_acquire_restore
func rlockAcquireRestore(r *rlockObject, args []objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _acquire_restore() takes exactly one argument")
	}
	t, ok := args[0].(*objects.Tuple)
	if !ok || t.Len() != 2 {
		return nil, fmt.Errorf("TypeError: _acquire_restore() argument must be a 2-tuple")
	}
	countObj, ok := t.Item(0).(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: _acquire_restore count must be int")
	}
	ownerObj, ok := t.Item(1).(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: _acquire_restore owner must be int")
	}
	count, _ := countObj.Int64()
	owner, _ := ownerObj.Int64()
	r.gate.Lock()
	r.mu.Lock()
	r.owner = owner
	r.count = count
	r.mu.Unlock()
	return objects.None(), nil
}
