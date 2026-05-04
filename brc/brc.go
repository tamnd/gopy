// Package brc ports cpython/Python/brc.c. v0.3 ships only the field
// layout and no-op operations; the GIL build does not need biased
// reference counting. The free-threading build wires the queues in a
// later phase (see roadmap v0.14).
//
// CPython: Python/brc.c file overview
package brc

import "sync/atomic"

// QueuedObject is the link entry that a remote thread pushes onto an
// owner's biased-refcount queue when it decrements an object that the
// owner allocated. The owner drains its own queue without atomics.
//
// CPython: Include/internal/pycore_brc.h:L29 _PyObjectQueue
type QueuedObject struct {
	Next *QueuedObject
}

// ThreadState is the per-thread biased-refcount slot. Each Python
// thread embeds one of these in its state.Thread (wired up in v0.14).
// The owner pushes "merged" decrements into LocalQueue; remote
// threads push into RemoteQueue, which the owner drains under
// QueueMutex.
//
// CPython: Include/internal/pycore_brc.h:L57 _Py_brc_per_thread_state
type ThreadState struct {
	LocalQueue  *QueuedObject
	RemoteQueue atomic.Pointer[QueuedObject]
	QueueMutex  uint32
	TID         uint64
}

// QueueRemote enqueues obj on the owner's remote queue. In the GIL
// build this is a no-op because every thread shares one refcount. In
// the free-threading build (v0.14) this walks the lock-free push path
// from CPython.
//
// CPython: Python/brc.c:L66 _Py_brc_queue_object
func QueueRemote(_ *ThreadState, _ *QueuedObject) {
	// no-op in the GIL build
}

// DrainQueue runs the owner-side drain step. v0.3 has nothing to
// drain; the loop body lands in v0.14.
//
// CPython: Python/brc.c:L131 _Py_brc_merge_refcounts
func DrainQueue(_ *ThreadState) {
	// no-op in the GIL build
}
