// Pending-call queue. Signal handlers and other interrupt-context
// code cannot run Python directly, so they enqueue a callback here
// and set BreakerCallsPending. The eval loop drains the queue at
// the next poll point.
//
// CPython: Python/ceval_gil.c _Py_AddPendingCall

package gil

import (
	"errors"
	"sync"
)

// PendingQueueSize is the bounded ring-buffer capacity.
//
// CPython: Include/cpython/ceval.h NPENDINGCALLS (32)
const PendingQueueSize = 32

// ErrPendingFull is returned by Add when the queue is full.
var ErrPendingFull = errors.New("gil: pending-call queue full")

// PendingFunc is a callback to run on the eval-loop thread.
type PendingFunc func() error

// Pending is a per-interpreter ring buffer of callbacks.
type Pending struct {
	mu    sync.Mutex
	queue [PendingQueueSize]PendingFunc
	head  int // next slot to drain
	tail  int // next slot to fill
	count int
}

// Add enqueues fn. Returns ErrPendingFull if the queue is at
// capacity. Existing entries are not lost on overflow.
//
// CPython: Python/ceval_gil.c _Py_AddPendingCall
func (p *Pending) Add(fn PendingFunc) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.count == PendingQueueSize {
		return ErrPendingFull
	}
	p.queue[p.tail] = fn
	p.tail = (p.tail + 1) % PendingQueueSize
	p.count++
	return nil
}

// Drain runs queued callbacks in FIFO order until the queue is
// empty or one of them errors. The first error stops draining and
// is returned; remaining callbacks stay queued.
//
// CPython: Python/ceval_gil.c _Py_MakePendingCalls
func (p *Pending) Drain() error {
	for {
		p.mu.Lock()
		if p.count == 0 {
			p.mu.Unlock()
			return nil
		}
		fn := p.queue[p.head]
		p.queue[p.head] = nil
		p.head = (p.head + 1) % PendingQueueSize
		p.count--
		p.mu.Unlock()

		if err := fn(); err != nil {
			return err
		}
	}
}

// Len returns the number of queued callbacks.
func (p *Pending) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}
