// The global interpreter lock. v0.6 has at most one waiter per
// interpreter (one goroutine runs Python code), so this is a thin
// mutex with a wake condition for the drop-request handshake. v0.13
// turns this into a real contended lock between sub-interpreters.
//
// CPython: Python/ceval_gil.c

package gil

import (
	"sync"
	"sync/atomic"
	"time"
)

// holderID identifies the goroutine currently holding the GIL.
// We use an opaque pointer rather than the Thread to keep the gil
// package free of the state import cycle.
type holderID = any

// GIL is the per-interpreter execution lock.
type GIL struct {
	mu          sync.Mutex
	cond        *sync.Cond
	locked      bool
	holder      holderID
	holderGoid  uint64 // goroutine that actually took the lock
	requestDrop atomic.Bool
	interval    atomic.Int64 // sys.setswitchinterval, nanoseconds
}

// New returns an unlocked GIL.
func New() *GIL {
	g := &GIL{}
	g.cond = sync.NewCond(&g.mu)
	g.interval.Store(int64(5 * time.Millisecond))
	return g
}

// Acquire blocks until ts holds the GIL. Pass any opaque per-thread
// pointer for ts; the GIL only uses it for ownership checks.
//
// CPython: Python/ceval_gil.c take_gil
func (g *GIL) Acquire(ts holderID) {
	g.mu.Lock()
	for g.locked {
		g.cond.Wait()
	}
	g.locked = true
	g.holder = ts
	g.requestDrop.Store(false)
	g.mu.Unlock()
}

// Release drops the GIL. Mismatched releases panic to surface lock
// inversion bugs early.
//
// CPython: Python/ceval_gil.c drop_gil
func (g *GIL) Release(ts holderID) {
	g.mu.Lock()
	if !g.locked || g.holder != ts {
		g.mu.Unlock()
		panic("gil: Release by non-holder")
	}
	g.locked = false
	g.holder = nil
	g.cond.Signal()
	g.mu.Unlock()
}

// AcquireReentrant takes the GIL for logical thread ts running on
// goroutine goid. It returns true when this call actually acquired the
// lock and the caller must later balance it with ReleaseGoid.
//
// When ts already holds the GIL (the generator/coroutine body case:
// gopy runs a generator on its own goroutine but it shares its driver's
// PyThreadState, and the driver holds the GIL while blocked waiting for
// the body to yield), the body is already covered by the driver's hold,
// so this returns false without blocking and without re-taking the lock.
//
// A contended acquire arms requestDrop so the current holder yields at
// its next eval-breaker poll, mirroring take_gil's drop-request set.
//
// CPython: Python/ceval_gil.c take_gil
func (g *GIL) AcquireReentrant(ts holderID, goid uint64) bool {
	g.mu.Lock()
	if g.locked && g.holder == ts {
		g.mu.Unlock()
		return false
	}
	for g.locked {
		g.requestDrop.Store(true)
		g.cond.Wait()
	}
	g.locked = true
	g.holder = ts
	g.holderGoid = goid
	g.requestDrop.Store(false)
	g.mu.Unlock()
	return true
}

// ReleaseGoid drops a GIL acquired by goroutine goid. It is a no-op when
// goid is not the goroutine that took the lock, so a generator body that
// no-op'd its AcquireReentrant (and any nested caller) never drops the
// driver's hold.
//
// CPython: Python/ceval_gil.c drop_gil
func (g *GIL) ReleaseGoid(ts holderID, goid uint64) {
	g.mu.Lock()
	if !g.locked || g.holderGoid != goid {
		g.mu.Unlock()
		return
	}
	g.locked = false
	g.holder = nil
	g.holderGoid = 0
	g.cond.Signal()
	g.mu.Unlock()
}

// Handoff transfers an existing hold owned by logical thread ts from
// whatever goroutine currently owns it to goroutine toGoid, without
// releasing the lock. It models the generator baton: a generator body
// runs on its own goroutine but shares its driver's PyThreadState, and
// the two never run concurrently (the driver is parked waiting for the
// body to yield, and vice versa). Passing the GIL between them is a
// reassignment of holderGoid, not a contended acquire, so no other
// thread can slip in during the handoff. A no-op unless ts already
// holds the lock.
//
// CPython: generators run on the driver's OS thread, so there is no
// analog; this models gopy's goroutine-per-generator divergence.
func (g *GIL) Handoff(ts holderID, toGoid uint64) {
	g.mu.Lock()
	if g.locked && g.holder == ts {
		g.holderGoid = toGoid
	}
	g.mu.Unlock()
}

// HeldByGoid reports whether goid is the goroutine that currently owns
// the GIL. Used by the eval breaker to decide whether a GIL-drop request
// applies to the running goroutine.
func (g *GIL) HeldByGoid(goid uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.locked && g.holderGoid == goid
}

// RequestDrop marks the GIL as wanting to be dropped at the next
// poll point. The eval loop checks DropRequested on each iteration
// and releases voluntarily when set.
//
// CPython: Python/ceval_gil.c COMPUTE_EVAL_BREAKER (gil-drop bit)
func (g *GIL) RequestDrop() {
	g.requestDrop.Store(true)
}

// DropRequested reports whether another goroutine asked the holder
// to drop. Cheap atomic read.
func (g *GIL) DropRequested() bool {
	return g.requestDrop.Load()
}

// Holder returns the current holder (or nil). Read with the lock
// held; useful only for diagnostics.
func (g *GIL) Holder() holderID {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.holder
}

// SetSwitchInterval updates the cooperative-yield interval.
//
// CPython: Python/sysmodule.c sys_setswitchinterval
func (g *GIL) SetSwitchInterval(d time.Duration) {
	g.interval.Store(int64(d))
}

// SwitchInterval returns the cooperative-yield interval.
//
// CPython: Python/sysmodule.c sys_getswitchinterval
func (g *GIL) SwitchInterval() time.Duration {
	return time.Duration(g.interval.Load())
}
