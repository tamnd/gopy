package pysync

import (
	"sync/atomic"
	"unsafe"
)

// RWMutex mirrors _PyRWMutex from cpython/Python/lock.c. It is a
// reader-writer lock with the same bit packing as the C code:
//
//	bit 0      writer holds the lock
//	bit 1      at least one waiter is parked
//	bits 2..   reader count
//
// Writers are not starved: once a writer parks, new readers also park
// rather than slipping past.
//
// The zero value is an unlocked RWMutex. Like the other pysync
// primitives, it must not be copied after first use.
type RWMutex struct {
	bits atomic.Uintptr
}

const (
	rwWriteLocked uintptr = 1 << 0
	rwHasParked   uintptr = 1 << 1
	rwReaderShift         = 2
	rwReaderUnit  uintptr = 1 << rwReaderShift
)

func rwReaderCount(bits uintptr) uintptr { return bits >> rwReaderShift }

// RLock acquires a read lock, parking until both the writer slot is
// free and no writer is waiting.
func (r *RWMutex) RLock() {
	bits := r.bits.Load()
	for {
		if bits&rwWriteLocked != 0 || bits&rwHasParked != 0 {
			bits = r.parkAndWait(bits)
			continue
		}
		newval := bits + rwReaderUnit
		if r.bits.CompareAndSwap(bits, newval) {
			return
		}
		bits = r.bits.Load()
	}
}

// RUnlock releases a read lock. It panics if no read lock is held.
func (r *RWMutex) RUnlock() {
	bits := r.bits.Add(^(rwReaderUnit - 1)) // subtract one reader unit
	if rwReaderCount(bits+rwReaderUnit) == 0 {
		panic("pysync: RUnlock of unlocked RWMutex")
	}
	if rwReaderCount(bits) == 0 && bits&rwHasParked != 0 {
		UnparkAll(unsafe.Pointer(&r.bits))
	}
}

// Lock acquires the write lock, parking until both readers and any
// other writer have released.
func (r *RWMutex) Lock() {
	bits := r.bits.Load()
	for {
		if bits & ^rwHasParked == 0 {
			if r.bits.CompareAndSwap(bits, bits|rwWriteLocked) {
				return
			}
			bits = r.bits.Load()
			continue
		}
		bits = r.parkAndWait(bits)
	}
}

// Unlock releases the write lock. It panics if the lock was not
// write-locked.
func (r *RWMutex) Unlock() {
	old := r.bits.Swap(0)
	if old&rwWriteLocked == 0 {
		panic("pysync: Unlock of non-write-locked RWMutex")
	}
	if rwReaderCount(old) != 0 {
		panic("pysync: Unlock of read-locked RWMutex")
	}
	if old&rwHasParked != 0 {
		UnparkAll(unsafe.Pointer(&r.bits))
	}
}

func (r *RWMutex) parkAndWait(bits uintptr) uintptr {
	if bits&rwHasParked == 0 {
		newval := bits | rwHasParked
		if !r.bits.CompareAndSwap(bits, newval) {
			return r.bits.Load()
		}
		bits = newval
	}
	expected := bits
	_ = Park(unsafe.Pointer(&r.bits), func() bool {
		return r.bits.Load() == expected
	}, -1, nil, true)
	return r.bits.Load()
}
