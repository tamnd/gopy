// Wiring for the deferred finalizer queue in objects. The Go runtime
// finalizer goroutine queues teardown work (snapshot Decref, weakref
// callbacks) into objects' deferred queue; the eval loop drains it on
// the interpreter goroutine at its breaker poll. This file connects the
// two: it arms every live thread's breaker when work is queued and
// enables deferral once an Eval thread exists.
//
// CPython: Python/ceval_gil.c _Py_AddPendingCall / _Py_HandlePending

package vm

import (
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/objects"
)

func init() {
	// Arm every known thread's breaker so whichever goroutine is running
	// Python drains the deferred finalizer queue at its next poll. The
	// queue itself is thread-safe and the drain is idempotent, so arming
	// all breakers (rather than guessing the running one) is safe.
	objects.FinalizerArmHook = func() {
		threadVMs.Range(func(_, v any) bool {
			if tv, ok := v.(*threadVM); ok && tv.breaker != nil {
				tv.breaker.Set(gil.BreakerCallsPending)
			}
			return true
		})
	}
}
