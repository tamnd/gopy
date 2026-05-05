// Eval-breaker bitfield. The eval loop reads Load() at every RESUME
// and every backward jump; any non-zero result diverts dispatch to a
// handler matching the bit.
//
// CPython: Include/internal/pycore_ceval.h:338 _Py_set_eval_breaker_bit
// CPython: Python/ceval_gil.c

package gil

import "sync/atomic"

// Breaker holds the eval-loop poll bits. One per Thread.
type Breaker struct {
	bits atomic.Uint32
}

// Set turns on the named bit. Safe from any goroutine.
//
// CPython: Include/internal/pycore_ceval.h _Py_set_eval_breaker_bit
func (b *Breaker) Set(bit uint32) {
	for {
		old := b.bits.Load()
		if b.bits.CompareAndSwap(old, old|bit) {
			return
		}
	}
}

// Clear turns off the named bit.
//
// CPython: Include/internal/pycore_ceval.h _Py_unset_eval_breaker_bit
func (b *Breaker) Clear(bit uint32) {
	for {
		old := b.bits.Load()
		if b.bits.CompareAndSwap(old, old&^bit) {
			return
		}
	}
}

// Load reads the bitfield. Hot path.
func (b *Breaker) Load() uint32 {
	return b.bits.Load()
}

// IsSet reports whether the named bit is on.
func (b *Breaker) IsSet(bit uint32) bool {
	return b.bits.Load()&bit != 0
}
