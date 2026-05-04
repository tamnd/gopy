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
const (
	flagLocked    uint8 = 1 << 0
	flagHasParked uint8 = 1 << 1
)

// LockFlags bitset for LockTimed.
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
type LockStatus int

const (
	LockFailure LockStatus = iota
	LockAcquired
	LockIntr
)

// timeToBeFairNs is the fair-handoff window, mirroring lock.c
// (TIME_TO_BE_FAIR_NS = 1 ms).
const timeToBeFairNs = 1 * time.Millisecond

type mutexEntry struct {
	timeToBeFair time.Time
	handedOff    bool
}

// Mutex is the byte-flag mutex from cpython/Python/lock.c.
//
// The zero value is an unlocked mutex. Mutex must not be copied
// after first use, the same as Go's sync.Mutex.
type Mutex struct {
	bits atomic.Uint32 // only the low byte is used; widened so atomic ops work
}

// Lock blocks until the mutex is acquired.
func (m *Mutex) Lock() {
	m.LockTimed(-1, LockDetach)
}

// Unlock releases the mutex. It panics if the mutex is not locked,
// matching the Py_FatalError in PyMutex_Unlock.
func (m *Mutex) Unlock() {
	if m.tryUnlock() < 0 {
		panic("pysync: unlocking mutex that is not locked")
	}
}

// IsLocked reports whether the mutex is currently held.
func (m *Mutex) IsLocked() bool {
	return uint8(m.bits.Load())&flagLocked != 0
}

// TryLock acquires the mutex without blocking. It returns true on
// success.
func (m *Mutex) TryLock() bool {
	return m.LockTimed(0, 0) == LockAcquired
}

// LockTimed acquires the mutex with a timeout. timeout < 0 means
// block forever; timeout == 0 is a non-blocking try.
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

		expected := newv
		ret := Park(unsafe.Pointer(&m.bits), func() bool {
			return uint8(m.bits.Load()) == expected
		}, timeout, entry, flags&LockDetach != 0)

		if ret == ParkOK {
			if entry.handedOff {
				return LockAcquired
			}
		} else if ret == ParkTimeout {
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
