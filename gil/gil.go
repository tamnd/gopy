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
