// Package gc ports cpython/Python/gc.c. v0.3 shipped Track / Untrack /
// RegisterFinalizer / Finalize against a package-level state
// variable; v0.10 grows that into a full GCState (see state.go) and
// adds the cycle collector. The public functions in this file keep
// their v0.3 signatures so the rest of the runtime compiles
// unchanged.
//
// CPython: Python/gc.c file overview
package gc

import (
	"github.com/tamnd/gopy/objects"
)

// Finalizer is the Go equivalent of tp_finalize. The runtime invokes
// it once, immediately before reclaiming the object.
//
// CPython: Include/cpython/object.h:237 tp_finalize
type Finalizer func(o objects.Object)

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
	if g, gtracked := state.tracked[o]; gtracked {
		listRemove(g)
		delete(state.tracked, o)
	}
	state.mu.Unlock()
	if ok {
		fn(o)
		return
	}
	if slot := typeFinalize(o); slot != nil {
		slot(o)
	}
}

// Track adds o to the youngest generation. CPython appends to the
// generation0 list head; we follow exactly that pattern.
//
// CPython: Include/internal/pycore_object.h:225 _PyObject_GC_TRACK
func Track(o objects.Object) {
	if o == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if _, ok := state.tracked[o]; ok {
		return
	}
	g := &gcHead{obj: o}
	listAppend(g, state.generations[0].head)
	state.tracked[o] = g
	state.generations[0].count++
	maybeAutoCollect()
}

// Untrack removes o from whichever generation list it currently sits
// on. No-op if the object was never tracked. CPython internally
// keeps the FINALIZED bit while clearing COLLECTING; gopy lets the
// gcHead vanish since the per-object map drops it too.
//
// CPython: Include/internal/pycore_object.h:248 _PyObject_GC_UNTRACK
func Untrack(o objects.Object) {
	if o == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	g, ok := state.tracked[o]
	if !ok {
		return
	}
	listRemove(g)
	delete(state.tracked, o)
}

// IsTracked reports whether o is currently tracked.
//
// CPython: Include/internal/pycore_object.h:268 _PyObject_GC_IS_TRACKED
func IsTracked(o objects.Object) bool {
	state.mu.Lock()
	_, ok := state.tracked[o]
	state.mu.Unlock()
	return ok
}
