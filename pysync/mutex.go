package pysync

import (
	"sync/atomic"
	"time"
	"unsafe"
)

// Bit layout of the byte-flag mutex word, mirroring pycore_lock.h:
//
//	bit 0 (locked)     1 if owned by some goroutine
//	bit 1 (hasParked)  1 if at least one goroutine is parked waiting
//
// CPython: Include/internal/pycore_lock.h:L17 _Py_HAS_PARKED
const (
	flagLocked    uint8 = 1 << 0
	flagHasParked uint8 = 1 << 1
)

// LockFlags bitset for LockTimed.
//
// CPython: Include/internal/pycore_lock.h:L35 _PyLockFlags
type LockFlags uint32

const (
	// LockDetach asks the runtime to detach the current thread state
	// while parked. v0.1 has no thread state, so this is a no-op.
	LockDetach LockFlags = 1 << 0
	// LockHandleSignals asks the runtime to run pending signal
	// handlers if the wait was interrupted. v0.1 has no signal
	// plumbing, so this is a no-op.
	LockHandleSignals LockFlags = 1 << 1
)

// LockStatus mirrors PyLockStatus.
//
// CPython: Include/pythread.h:L12 PyLockStatus
type LockStatus int

const (
	// LockFailure means the lock could not be acquired.
	LockFailure LockStatus = iota
	// LockAcquired means the lock is now held by the caller.
	LockAcquired
	// LockIntr means the wait was interrupted by a signal. v0.1 has
	// no signal plumbing, so this value is reserved.
	LockIntr
)

// timeToBeFairNs is the fair-handoff window, mirroring lock.c
// (TIME_TO_BE_FAIR_NS = 1 ms).
//
// CPython: Python/lock.c:L22 TIME_TO_BE_FAIR_NS
const timeToBeFairNs = 1 * time.Millisecond

// mutexEntry is the parkArg payload for PyMutex waiters.
//
// CPython: Python/lock.c:L33 mutex_entry
type mutexEntry struct {
	timeToBeFair time.Time
	handedOff    bool
}

// Mutex is the byte-flag mutex from cpython/Python/lock.c.
//
// The zero value is an unlocked mutex. Mutex must not be copied
// after first use, the same as Go's sync.Mutex.
//
// CPython: Include/cpython/lock.h:L29 PyMutex
type Mutex struct {
	bits atomic.Uint32 // only the low byte is used; widened so atomic ops work
}

// Lock blocks until the mutex is acquired.
//
// CPython: Python/lock.c:L618 PyMutex_Lock
func (m *Mutex) Lock() {
	m.LockTimed(-1, LockDetach)
}

// Unlock releases the mutex. It panics if the mutex is not locked,
// matching the Py_FatalError in PyMutex_Unlock.
//
// CPython: Python/lock.c:L625 PyMutex_Unlock
func (m *Mutex) Unlock() {
	if m.tryUnlock() < 0 {
		panic("pysync: unlocking mutex that is not locked")
	}
}

// IsLocked reports whether the mutex is currently held.
//
// CPython: Python/lock.c:L635 PyMutex_IsLocked
func (m *Mutex) IsLocked() bool {
	return uint8(m.bits.Load())&flagLocked != 0
}

// TryLock acquires the mutex without blocking. It returns true on
// success.
//
// CPython: Include/internal/pycore_lock.h:L21 PyMutex_LockFast (adapted from)
func (m *Mutex) TryLock() bool {
	return m.LockTimed(0, 0) == LockAcquired
}

// LockTimed acquires the mutex with a timeout. timeout < 0 means
// block forever; timeout == 0 is a non-blocking try.
//
// CPython: Python/lock.c:L53 _PyMutex_LockTimed
func (m *Mutex) LockTimed(timeout time.Duration, flags LockFlags) LockStatus {
	v := uint8(m.bits.Load())
	if v&flagLocked == 0 {
		if m.bits.CompareAndSwap(uint32(v), uint32(v|flagLocked)) {
			return LockAcquired
		}
	}
	if timeout == 0 {
		return LockFailure
	}

	var endTime time.Time
	if timeout > 0 {
		endTime = time.Now().Add(timeout)
	}

	entry := &mutexEntry{
		timeToBeFair: time.Now().Add(timeToBeFairNs),
	}

	for {
		v = uint8(m.bits.Load())
		if v&flagLocked == 0 {
			if m.bits.CompareAndSwap(uint32(v), uint32(v|flagLocked)) {
				return LockAcquired
			}
			continue
		}

		// Spin path is empty in the GIL build (MAX_SPIN_COUNT == 0).
		// We skip the goto-yield loop entirely; under the
		// pygil_disabled tag we will reintroduce it.

		if timeout == 0 {
			return LockFailure
		}

		newv := v
		if v&flagHasParked == 0 {
			newv = v | flagHasParked
			if !m.bits.CompareAndSwap(uint32(v), uint32(newv)) {
				continue
			}
		}

		expected := uint32(newv)
		ret := Park(unsafe.Pointer(&m.bits), func() bool {
			return m.bits.Load() == expected
		}, timeout, entry, flags&LockDetach != 0)

		switch ret {
		case ParkOK:
			if entry.handedOff {
				return LockAcquired
			}
		case ParkTimeout:
			if timeout >= 0 {
				return LockFailure
			}
		}
		// ParkAgain or ParkOK without handoff: fall through and retry.

		if timeout > 0 {
			timeout = max(time.Until(endTime), 0)
		}
	}
}

// tryUnlock returns 0 on success and -1 if the mutex was not locked.
//
// CPython: Python/lock.c:L163 _PyMutex_TryUnlock
func (m *Mutex) tryUnlock() int {
	for {
		v := uint8(m.bits.Load())
		if v&flagLocked == 0 {
			return -1
		}
		if v&flagHasParked != 0 {
			Unpark(unsafe.Pointer(&m.bits), func(parkArg any, hasMore bool) {
				var nv uint8
				if entry, ok := parkArg.(*mutexEntry); ok && entry != nil {
					if time.Now().After(entry.timeToBeFair) {
						entry.handedOff = true
						nv |= flagLocked
					}
					if hasMore {
						nv |= flagHasParked
					}
				}
				m.bits.Store(uint32(nv))
			})
			return 0
		}
		if m.bits.CompareAndSwap(uint32(v), 0) {
			return 0
		}
	}
}
