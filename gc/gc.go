// Package gc ports the refcount-only path of cpython/Python/gc.c.
// v0.3 ships Track, Untrack, RegisterFinalizer, and Finalize. Cycle
// collection is a no-op until v0.10 lands the cycle collector.
//
// CPython: Python/gc.c file overview
package gc

import (
	"sync"

	"github.com/tamnd/gopy/objects"
)

// Finalizer is the Go equivalent of tp_finalize. The runtime invokes
// it once, immediately before reclaiming the object.
//
// CPython: Include/cpython/object.h:L237 tp_finalize
type Finalizer func(o objects.Object)

// state holds the package-level finalizer table and tracked-object
// set. Mirrors the gc state struct in CPython but without the
// generation lists; cycles are not collected in v0.3.
//
// CPython: Include/internal/pycore_gc.h:L150 GCState
var state struct {
	mu         sync.Mutex
	finalizers map[objects.Object]Finalizer
	tracked    map[objects.Object]struct{}
}

func init() {
	state.finalizers = make(map[objects.Object]Finalizer)
	state.tracked = make(map[objects.Object]struct{})
}

// RegisterFinalizer associates fn with o. The runtime calls Finalize
// to invoke it. Mirrors PyObject_GC_RegisterFinalizer in spirit; in
// CPython the per-object slot is tp_finalize, but gopy keeps the
// mapping out-of-band so Header stays small.
//
// CPython: Objects/object.c:L489 PyObject_CallFinalizer (caller side)
func RegisterFinalizer(o objects.Object, fn Finalizer) {
	if o == nil || fn == nil {
		return
	}
	state.mu.Lock()
	state.finalizers[o] = fn
	state.mu.Unlock()
}

// Finalize runs the finalizer for o exactly once and clears it. Safe
// to call on objects that never registered one. Mirrors
// PyObject_CallFinalizerFromDealloc.
//
// CPython: Objects/object.c:L497 PyObject_CallFinalizerFromDealloc
func Finalize(o objects.Object) {
	if o == nil {
		return
	}
	state.mu.Lock()
	fn, ok := state.finalizers[o]
	if ok {
		delete(state.finalizers, o)
	}
	delete(state.tracked, o)
	state.mu.Unlock()
	if ok {
		fn(o)
	}
}

// Track adds o to the tracked-object set. Cycle collection (v0.10)
// will walk this set; for now it is bookkeeping only.
//
// CPython: Modules/gcmodule.c:L2178 PyObject_GC_Track
func Track(o objects.Object) {
	if o == nil {
		return
	}
	state.mu.Lock()
	state.tracked[o] = struct{}{}
	state.mu.Unlock()
}

// Untrack removes o from the tracked-object set. No-op if not
// tracked.
//
// CPython: Modules/gcmodule.c:L2200 PyObject_GC_UnTrack
func Untrack(o objects.Object) {
	if o == nil {
		return
	}
	state.mu.Lock()
	delete(state.tracked, o)
	state.mu.Unlock()
}

// IsTracked reports whether o is currently tracked.
//
// CPython: Include/internal/pycore_object.h:L268 _PyObject_GC_IS_TRACKED
func IsTracked(o objects.Object) bool {
	state.mu.Lock()
	_, ok := state.tracked[o]
	state.mu.Unlock()
	return ok
}

// Collect runs the cycle collector. v0.3 has none, so this returns 0.
// The signature is preserved so callers compile against v0.10 without
// changes.
//
// CPython: Modules/gcmodule.c:L1430 gc_collect_main
func Collect() int {
	return 0
}
