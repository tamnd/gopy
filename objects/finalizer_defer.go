// Deferred finalizer queue. Go installs runtime.SetFinalizer callbacks
// (on FrameSnapshot locals and on weakref referents) that run on a
// dedicated runtime goroutine, independent of the interpreter
// goroutine. Those callbacks call Decref, which can drop an object's
// last reference and run its tp_finalize (__del__) or fire weakref
// callbacks. Running that Python-visible teardown on the finalizer
// goroutine races the main eval loop on shared state (the per-type
// descriptor tables, instance dicts), which Go's map detector reports
// as "concurrent map read and map write".
//
// CPython never runs tp_finalize or weakref callbacks off-thread: they
// execute synchronously under the GIL on whichever thread drops the
// last reference. gopy mirrors that by having the runtime finalizer
// goroutine enqueue its cleanup here instead of running it inline; the
// eval loop drains the queue at its next breaker poll, on the
// interpreter goroutine, where it is the only thing touching Python
// state.
//
// CPython: Python/ceval_gil.c _Py_AddPendingCall (off-thread work is
// queued and run later under the GIL)

package objects

import "sync"

var (
	deferredMu         sync.Mutex
	deferredFinalizers []func()
	deferEnabled       bool

	// FinalizerArmHook wakes the interpreter goroutine after work is
	// queued so it drains at the next breaker poll. The vm package sets
	// it to raise BreakerCallsPending on the live threads. nil before an
	// interpreter is running, in which case deferFinalizer runs inline.
	FinalizerArmHook func()
)

// EnableFinalizerDeferral switches the Go-GC finalizer callbacks from
// running their cleanup inline to queueing it for the interpreter
// goroutine. The vm package calls this once an Eval thread exists.
func EnableFinalizerDeferral() {
	deferredMu.Lock()
	deferEnabled = true
	deferredMu.Unlock()
}

// deferFinalizer queues fn to run on the interpreter goroutine and
// returns true. It returns false when deferral is not yet enabled (no
// interpreter is running), so the caller can fall back to running the
// cleanup inline as the v0.9 behavior did.
func deferFinalizer(fn func()) bool {
	deferredMu.Lock()
	if !deferEnabled {
		deferredMu.Unlock()
		return false
	}
	deferredFinalizers = append(deferredFinalizers, fn)
	deferredMu.Unlock()
	if FinalizerArmHook != nil {
		FinalizerArmHook()
	}
	return true
}

// DrainDeferredFinalizers runs all queued finalizer cleanup. The eval
// loop calls it on the interpreter goroutine from its breaker handler.
// A drained callback may itself queue more work (a __del__ that drops
// the last reference to another snapshot), so the loop keeps going
// until the queue is empty.
func DrainDeferredFinalizers() {
	for {
		deferredMu.Lock()
		if len(deferredFinalizers) == 0 {
			deferredMu.Unlock()
			return
		}
		batch := deferredFinalizers
		deferredFinalizers = nil
		deferredMu.Unlock()
		for _, fn := range batch {
			fn()
		}
	}
}
