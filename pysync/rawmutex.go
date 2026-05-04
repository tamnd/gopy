package pysync

import "sync"

// RawMutex mirrors _PyRawMutex from cpython/Python/lock.c. The C
// implementation packs the linked list of waiters into the mutex
// word itself, so it does not need the parking lot. CPython uses it
// only as the per-bucket lock inside the parking lot, where reusing
// the parking lot would create a bootstrap cycle.
//
// In gopy the parking-lot bucket lock is Go's sync.Mutex, so the
// bootstrap cycle does not exist. This type is a thin wrapper around
// sync.Mutex provided for source-shape parity. The pointer-tagged
// uintptr trick that lock.c uses cannot be expressed without
// triggering go vet's unsafeptr check; if a future caller needs the
// exact CPython memory layout we will revisit.
//
// CPython: Include/internal/pycore_lock.h:L97 _PyRawMutex
type RawMutex struct {
	mu     sync.Mutex
	locked bool // protected by mu
}

// Lock acquires the mutex.
//
// CPython: Python/lock.c:L191 _PyRawMutex_LockSlow
func (r *RawMutex) Lock() {
	r.mu.Lock()
	r.locked = true
}

// Unlock releases the mutex. It panics if the mutex was not locked,
// matching the Py_FatalError in _PyRawMutex_UnlockSlow.
//
// CPython: Python/lock.c:L231 _PyRawMutex_UnlockSlow
func (r *RawMutex) Unlock() {
	if !r.locked {
		panic("pysync: unlocking RawMutex that is not locked")
	}
	r.locked = false
	r.mu.Unlock()
}
