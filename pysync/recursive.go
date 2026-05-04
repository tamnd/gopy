package pysync

import (
	"sync/atomic"

	"github.com/tamnd/gopy/pythread"
)

// RecursiveMutex mirrors _PyRecursiveMutex from cpython/Python/lock.c.
// The same goroutine ident may relock; other goroutines block until
// every relock has been released.
//
// The owner is identified by a pythread.Ident, so RecursiveMutex is
// only useful when callers have an explicit ident to pass. v0.1's
// usage is bounded; v0.7 (state) wires it up to the per-thread
// state.
//
// CPython: Include/internal/pycore_lock.h:L149 _PyRecursiveMutex
type RecursiveMutex struct {
	mu     Mutex
	thread atomic.Uint64 // pythread.Ident; 0 means unowned
	level  int           // protected by mu
}

// IsLockedBy reports whether the recursive mutex is currently held
// by the given ident.
//
// CPython: Python/lock.c:L375 _PyRecursiveMutex_IsLockedByCurrentThread
func (r *RecursiveMutex) IsLockedBy(id pythread.Ident) bool {
	return pythread.Ident(r.thread.Load()) == id
}

// Lock acquires the mutex on behalf of id. If id already holds it,
// the recursion depth is incremented.
//
// CPython: Python/lock.c:L381 _PyRecursiveMutex_Lock
func (r *RecursiveMutex) Lock(id pythread.Ident) {
	if pythread.Ident(r.thread.Load()) == id {
		r.level++
		return
	}
	r.mu.Lock()
	r.thread.Store(uint64(id))
}

// Unlock releases one level. It panics if id does not hold the
// mutex.
//
// CPython: Python/lock.c:L410 _PyRecursiveMutex_Unlock
func (r *RecursiveMutex) Unlock(id pythread.Ident) {
	if pythread.Ident(r.thread.Load()) != id {
		panic("pysync: unlocking a recursive mutex not owned by the current thread")
	}
	if r.level > 0 {
		r.level--
		return
	}
	r.thread.Store(0)
	r.mu.Unlock()
}
