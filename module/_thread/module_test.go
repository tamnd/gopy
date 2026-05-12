package _thread

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestGetIdentNonzero verifies that get_ident() returns a nonzero int
// from the current goroutine.
//
// CPython: Modules/_threadmodule.c:2082 thread_get_ident
func TestGetIdentNonzero(t *testing.T) {
	result, err := threadGetIdent(nil, nil)
	if err != nil {
		t.Fatalf("get_ident() returned error: %v", err)
	}
	id, ok := result.(*objects.Int)
	if !ok {
		t.Fatalf("get_ident() returned %T, want *objects.Int", result)
	}
	v, _ := id.Int64()
	if v == 0 {
		t.Errorf("get_ident() returned 0, want nonzero")
	}
}

// TestGetIdentSameGoroutine verifies that two consecutive calls in the
// same goroutine return the same value.
//
// CPython: Modules/_threadmodule.c:2082 thread_get_ident
func TestGetIdentSameGoroutine(t *testing.T) {
	r1, err1 := threadGetIdent(nil, nil)
	r2, err2 := threadGetIdent(nil, nil)
	if err1 != nil || err2 != nil {
		t.Fatalf("get_ident() errors: %v / %v", err1, err2)
	}
	v1, _ := r1.(*objects.Int).Int64()
	v2, _ := r2.(*objects.Int).Int64()
	if v1 != v2 {
		t.Errorf("get_ident() returned different values in same goroutine: %d != %d", v1, v2)
	}
}

// TestAllocateLockReturnsLock verifies that allocate_lock() returns a
// *lockObject.
//
// CPython: Modules/_threadmodule.c:2063 thread_PyThread_allocate_lock
func TestAllocateLockReturnsLock(t *testing.T) {
	result, err := threadAllocateLock(nil, nil)
	if err != nil {
		t.Fatalf("allocate_lock() returned error: %v", err)
	}
	if _, ok := result.(*lockObject); !ok {
		t.Errorf("allocate_lock() returned %T, want *lockObject", result)
	}
}

// TestLockAcquireRelease verifies that a lock can be acquired and
// released without error and that locked() reflects the state.
//
// CPython: Modules/_threadmodule.c:817 lock_PyThread_acquire_lock
// CPython: Modules/_threadmodule.c:858 lock_PyThread_release_lock
func TestLockAcquireRelease(t *testing.T) {
	lk := newLockObject()

	// Initially unlocked.
	lockedResult, err := lockLocked(lk, nil, nil)
	if err != nil {
		t.Fatalf("locked() error: %v", err)
	}
	if lockedResult != objects.False() {
		t.Errorf("new lock should be unlocked")
	}

	// Acquire (non-blocking should succeed on an unlocked lock).
	acq, err := lockAcquire(lk, []objects.Object{objects.True()}, nil)
	if err != nil {
		t.Fatalf("acquire(True) error: %v", err)
	}
	if acq != objects.True() {
		t.Errorf("acquire(True) on unlocked lock should return True, got %v", acq)
	}

	// Now locked.
	lockedResult, err = lockLocked(lk, nil, nil)
	if err != nil {
		t.Fatalf("locked() error after acquire: %v", err)
	}
	if lockedResult != objects.True() {
		t.Errorf("lock should be locked after acquire")
	}

	// Release.
	_, err = lockRelease(lk, nil, nil)
	if err != nil {
		t.Fatalf("release() error: %v", err)
	}

	// Unlocked again.
	lockedResult, err = lockLocked(lk, nil, nil)
	if err != nil {
		t.Fatalf("locked() error after release: %v", err)
	}
	if lockedResult != objects.False() {
		t.Errorf("lock should be unlocked after release")
	}
}

// TestLockReleaseUnlockedErrors verifies that releasing an unlocked
// lock raises RuntimeError.
//
// CPython: Modules/_threadmodule.c:858 lock_PyThread_release_lock
func TestLockReleaseUnlockedErrors(t *testing.T) {
	lk := newLockObject()
	_, err := lockRelease(lk, nil, nil)
	if err == nil {
		t.Error("release() on unlocked lock should return an error")
	}
}

// TestBuildModule verifies that the module builds without error and
// exports TIMEOUT_MAX and get_ident.
func TestBuildModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule() error: %v", err)
	}
	d := m.Dict()
	for _, name := range []string{"get_ident", "get_native_id", "allocate_lock", "start_new_thread", "stack_size", "TIMEOUT_MAX", "lock"} {
		v, err := d.GetItem(objects.NewStr(name))
		if err != nil || v == nil {
			t.Errorf("module missing attribute %q", name)
		}
	}
}
