// Package pysync ports the synchronization primitives in
// cpython/Python/lock.c, parking_lot.c, and critical_section.c.
//
// The package is named pysync (not sync) because the primitives have
// semantics distinct from Go's standard sync package: byte-flag
// PyMutex with parked-waiter bits, address-keyed parking, PEP 703
// critical sections, and so on.
package pysync

import (
	"sync"
	"time"
	"unsafe"
)

// ParkStatus is the result of a Park call.
//
// CPython: Include/internal/pycore_parking_lot.h:L22 Py_PARK_OK enum (adapted from)
type ParkStatus int

const (
	// ParkOK means the thread was woken by Unpark or UnparkAll.
	ParkOK ParkStatus = iota
	// ParkTimeout means the timeout elapsed before a wakeup.
	ParkTimeout
	// ParkAgain means the address-equality check failed before parking.
	ParkAgain
	// ParkIntr is reserved for future signal handling. It is never
	// returned in v0.1 because gopy has no signal plumbing yet.
	ParkIntr
)

// numBuckets is the prime from cpython/Python/parking_lot.c. It is
// chosen to avoid correlations with memory addresses while keeping
// memory use bounded.
//
// CPython: Python/parking_lot.c:L35 NUM_BUCKETS
const numBuckets = 257

// waiter is one parked goroutine. Mirrors struct wait_entry in C
// with the OS semaphore replaced by a Go channel.
//
// CPython: Python/parking_lot.c:L23 wait_entry
type waiter struct {
	addr        uintptr
	parkArg     any
	notify      chan struct{}
	prev, next  *waiter
	isUnparking bool
}

// bucket holds the waiter list for a slice of the address space.
//
// CPython: Python/parking_lot.c:L12 Bucket
type bucket struct {
	mu         sync.Mutex
	head       *waiter // doubly-linked list sentinel sentinel.next == first
	numWaiters int
}

// enqueue links w at the tail of the bucket's circular waiter list.
//
// CPython: Python/parking_lot.c:L23 wait_entry (adapted from)
func (b *bucket) enqueue(w *waiter) {
	if b.head == nil {
		b.head = &waiter{}
		b.head.prev = b.head
		b.head.next = b.head
	}
	tail := b.head.prev
	w.prev = tail
	w.next = b.head
	tail.next = w
	b.head.prev = w
	b.numWaiters++
}

// remove unlinks w from the bucket's waiter list.
//
// CPython: Python/parking_lot.c:L329 _PyParkingLot_Park (adapted from)
func (b *bucket) remove(w *waiter) {
	w.prev.next = w.next
	w.next.prev = w.prev
	w.prev, w.next = nil, nil
	b.numWaiters--
}

// dequeue removes and returns the first waiter parked on addr, or
// nil. The waiter is marked as unparking so a concurrent timeout
// path knows to wait for the wakeup channel.
//
// CPython: Python/parking_lot.c:L393 _PyParkingLot_Unpark (adapted from)
func (b *bucket) dequeue(addr uintptr) *waiter {
	if b.head == nil {
		return nil
	}
	for n := b.head.next; n != b.head; n = n.next {
		if n.addr == addr {
			b.remove(n)
			n.isUnparking = true
			return n
		}
	}
	return nil
}

// dequeueAll removes every waiter parked on addr.
//
// CPython: Python/parking_lot.c:L416 _PyParkingLot_UnparkAll (adapted from)
func (b *bucket) dequeueAll(addr uintptr) []*waiter {
	var out []*waiter
	if b.head == nil {
		return out
	}
	n := b.head.next
	for n != b.head {
		next := n.next
		if n.addr == addr {
			b.remove(n)
			n.isUnparking = true
			out = append(out, n)
		}
		n = next
	}
	return out
}

var buckets [numBuckets]bucket

// bucketFor maps an address to its bucket using the same modulo
// scheme as the C parking lot.
//
// CPython: Python/parking_lot.c:L338 _PyParkingLot_Park (adapted from)
func bucketFor(addr uintptr) *bucket {
	return &buckets[addr%numBuckets]
}

// Park atomically checks `check()` and, if it returns true, blocks
// the caller until Unpark or UnparkAll fires for the same addr, the
// timeout elapses, or the address-equality check fails before
// parking.
//
// addr is the unique key. It is hashed by uintptr identity, not by
// the bytes it points at; the C code's `atomic_memcmp` step is the
// caller's responsibility, performed inside `check`.
//
// If check returns false, Park returns ParkAgain without blocking.
//
// timeout < 0 means "no timeout". timeout == 0 still parks (matches
// CPython: zero is special only inside Mutex.LockTimed, not at the
// parking-lot layer). Callers wanting a no-block try should not call
// Park.
//
// detach is plumbed through for the PEP 703 attach/detach protocol;
// it has no effect in v0.1.
//
// CPython: Python/parking_lot.c:L329 _PyParkingLot_Park
func Park(addr unsafe.Pointer, check func() bool, timeout time.Duration,
	parkArg any, detach bool,
) ParkStatus {
	_ = detach
	key := uintptr(addr)
	b := bucketFor(key)

	b.mu.Lock()
	if !check() {
		b.mu.Unlock()
		return ParkAgain
	}
	w := &waiter{
		addr:    key,
		parkArg: parkArg,
		notify:  make(chan struct{}, 1),
	}
	b.enqueue(w)
	b.mu.Unlock()

	var res ParkStatus
	if timeout < 0 {
		<-w.notify
		res = ParkOK
	} else {
		t := time.NewTimer(timeout)
		select {
		case <-w.notify:
			t.Stop()
			res = ParkOK
		case <-t.C:
			res = ParkTimeout
		}
	}

	if res == ParkOK {
		return ParkOK
	}

	// Timeout. Either we are still queued or an unparker has already
	// claimed us. Match the C behavior: if claimed, wait for the
	// wakeup signal we will eventually receive.
	b.mu.Lock()
	if w.isUnparking {
		b.mu.Unlock()
		<-w.notify
		return ParkOK
	}
	b.remove(w)
	b.mu.Unlock()
	return res
}

// UnparkFn is invoked under the bucket lock when Unpark finds a
// waiter (or with parkArg == nil if no waiter was queued). hasMore
// reports whether the bucket still has waiters on the same addr
// after the dequeue.
//
// CPython: Include/internal/pycore_parking_lot.h _Py_unpark_fn_t (adapted from)
type UnparkFn func(parkArg any, hasMore bool)

// Unpark wakes one waiter parked on addr. fn is called while the
// bucket lock is held; the waiter is signaled after the lock is
// released. If no waiter is queued, fn is called with (nil, false).
//
// CPython: Python/parking_lot.c:L393 _PyParkingLot_Unpark
func Unpark(addr unsafe.Pointer, fn UnparkFn) {
	key := uintptr(addr)
	b := bucketFor(key)

	b.mu.Lock()
	w := b.dequeue(key)
	hasMore := false
	if w != nil {
		// Check whether any other waiter on the same address remains.
		if b.head != nil {
			for n := b.head.next; n != b.head; n = n.next {
				if n.addr == key {
					hasMore = true
					break
				}
			}
		}
	}
	if fn != nil {
		var pa any
		if w != nil {
			pa = w.parkArg
		}
		fn(pa, hasMore)
	}
	b.mu.Unlock()

	if w != nil {
		w.notify <- struct{}{}
	}
}

// UnparkAll wakes every waiter parked on addr.
//
// CPython: Python/parking_lot.c:L416 _PyParkingLot_UnparkAll
func UnparkAll(addr unsafe.Pointer) {
	key := uintptr(addr)
	b := bucketFor(key)

	b.mu.Lock()
	ws := b.dequeueAll(key)
	b.mu.Unlock()

	for _, w := range ws {
		w.notify <- struct{}{}
	}
}

// AfterFork resets the parking lot. In CPython this is required after
// fork() because parked waiters belong to threads that no longer
// exist in the child. Go programs do not POSIX-fork the runtime, so
// the function is preserved for source-shape parity but is a no-op
// in practice. Calling it on a live process would deadlock waiters;
// only call it when the program is single-threaded.
//
// CPython: Python/parking_lot.c:L434 _PyParkingLot_AfterFork
func AfterFork() {
	for i := range buckets {
		buckets[i].mu.Lock()
		buckets[i].head = nil
		buckets[i].numWaiters = 0
		buckets[i].mu.Unlock()
	}
}
