package pysync

import (
	"sync/atomic"
	"time"
	"unsafe"
)

// Event mirrors PyEvent from cpython/Python/lock.c. It is a one-shot
// flag: once Notified, it stays set forever and every Wait returns
// immediately.
//
// State machine on the byte-flag word:
//
//	0          unset, no waiters
//	flagLocked set
//	flagHasParked unset, with parked waiters
type Event struct {
	v atomic.Uint32 // only the low byte is used
}

// IsSet reports whether the event has been notified.
func (e *Event) IsSet() bool {
	return e.v.Load() == uint32(flagLocked)
}

// Notify sets the event. If waiters are parked, all are woken.
// Repeat calls are safe and have no effect.
func (e *Event) Notify() {
	for {
		v := uint8(e.v.Load())
		if v == flagLocked {
			return
		}
		if !e.v.CompareAndSwap(uint32(v), uint32(flagLocked)) {
			continue
		}
		if v == flagHasParked {
			UnparkAll(unsafe.Pointer(&e.v))
		}
		return
	}
}

// Wait blocks until the event is set.
func (e *Event) Wait() {
	for !e.WaitTimed(-1, true) { //nolint:revive // poll until WaitTimed returns true on a real wakeup
	}
}

// WaitTimed blocks until the event is set or timeout elapses. It
// returns true if the event is set, false on timeout.
//
// timeout < 0 means block forever. detach is plumbed through for the
// PEP 703 protocol; it has no effect in v0.1.
func (e *Event) WaitTimed(timeout time.Duration, detach bool) bool {
	for {
		v := uint8(e.v.Load())
		if v == flagLocked {
			return true
		}
		if v == 0 {
			if !e.v.CompareAndSwap(uint32(v), uint32(flagHasParked)) {
				continue
			}
		}

		expected := uint32(flagHasParked)
		_ = Park(unsafe.Pointer(&e.v), func() bool {
			return e.v.Load() == expected
		}, timeout, nil, detach)

		return e.v.Load() == uint32(flagLocked)
	}
}
