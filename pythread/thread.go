// Package pythread is the cross-platform threading shim ported from
// cpython/Python/thread.c. It layers over Go's goroutine scheduler;
// the platform-specific thread_pthread.h / thread_nt.h files in
// CPython have no Go counterpart.
//
// In v0.1 the surface is intentionally narrow: Init, stacksize stubs,
// the TimeoutMax constant, and a Start/Join handle. Lock-acquire
// retry, TSS, and sys.thread_info land in later phases when their
// dependencies (state, pytime, sysmod) exist.
package pythread

import (
	"math"
	"sync"
	"sync/atomic"
)

// TimeoutMax is the maximum lock-acquire timeout in microseconds.
// Matches PY_TIMEOUT_MAX from CPython's POSIX branch:
// LLONG_MAX / 1000, so that microseconds * 1000 (nanoseconds) fits
// in int64.
//
// CPython: Python/thread.c:L39 PY_TIMEOUT_MAX
const TimeoutMax int64 = math.MaxInt64 / 1000

// Ident is a unique identifier minted for each Started handle.
//
// CPython: Include/internal/pycore_pythread.h:L119 PyThread_ident_t (adapted from)
type Ident uint64

var (
	initOnce sync.Once
	identGen atomic.Uint64
)

// Init mirrors PyThread_init_thread. The Go runtime is already
// initialized when the program starts, so this is a no-op kept for
// source-shape parity.
//
// CPython: Python/thread.c:L47 PyThread_init_thread
func Init() {
	initOnce.Do(func() {})
}

// GetStacksize mirrors PyThread_get_stacksize. Returns 0 until the
// per-interpreter state lands and can carry a configured stack size.
//
// CPython: Python/thread.c:L76 PyThread_get_stacksize
func GetStacksize() int { return 0 }

// SetStacksize mirrors PyThread_set_stacksize. Goroutines grow their
// stacks dynamically, so configuring a fixed size is not meaningful.
// Returns -2 to signal "not supported", matching the C contract.
//
// CPython: Python/thread.c:L87 PyThread_set_stacksize
func SetStacksize(int) int { return -2 }

// Handle represents a goroutine started through Start. It records
// the goroutine's ident and a channel that is closed when the work
// finishes.
//
// CPython: Python/thread_pthread.h:L329 PyThread_start_joinable_thread (adapted from)
type Handle struct {
	ident Ident
	done  chan struct{}
	panic any
}

// Start runs fn on a new goroutine. fn must not be nil. Panics from
// fn are recovered; the value is surfaced through Join.
//
// CPython: Python/thread_pthread.h:L328 PyThread_start_joinable_thread
func Start(fn func()) *Handle {
	if fn == nil {
		panic("pythread: Start called with nil fn")
	}
	h := &Handle{
		ident: Ident(identGen.Add(1)),
		done:  make(chan struct{}),
	}
	go func() {
		defer close(h.done)
		defer func() {
			if r := recover(); r != nil {
				h.panic = r
			}
		}()
		fn()
	}()
	return h
}

// Ident returns the unique identifier assigned to this handle.
//
// CPython: Include/internal/pycore_pythread.h:L125 PyThread_get_thread_ident_ex (adapted from)
func (h *Handle) Ident() Ident { return h.ident }

// Join blocks until the goroutine returns. The recovered panic value
// (or nil) is returned. Repeated calls are safe and return the same
// value.
//
// CPython: Python/thread_pthread.h:L352 PyThread_join_thread
func (h *Handle) Join() any {
	<-h.done
	return h.panic
}
