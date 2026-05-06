// Switch-interval timing for the GIL-drop handshake. CPython's
// ceval_gil.c arms _PY_GIL_DROP_REQUEST_BIT after another thread has
// waited interval microseconds on gil_cond. The waiter side runs
// pthread_cond_timedwait against the kernel monotonic clock; the
// holder side reads the bit on each eval-breaker poll and releases
// the GIL.
//
// gopy v0.9 has at most one Python-running goroutine per interpreter
// (sub-interpreter contention lands in v0.13). Even so, this file
// holds the timing math the v0.13 wiring will plug into, and the
// math goes through pytime.Monotonic so sys.setswitchinterval has
// the same nanosecond semantics as CPython instead of bouncing
// through Go's time.Now() like the v0.6 stub did.
//
// CPython: Python/ceval_gil.c take_gil interval-wait branch
// CPython: Python/ceval_gil.c set_eval_breaker_bit (GIL_DROP_REQUEST)

package vm

import (
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/pytime"
)

// gilSwitchTimer tracks how long the running goroutine has held the
// GIL since the last hand-off. The eval loop nudges Poll on every
// breaker check; once the configured switch interval has elapsed and
// another thread has registered a drop request via gil.RequestDrop,
// Poll arms BreakerGILDropRequest so the next dispatch iteration
// drops the GIL and lets the waiter through.
//
// In v0.9 nothing calls RequestDrop in steady state, so Poll is a
// no-op that costs one atomic load per breaker check. The timer
// bookkeeping still runs; v0.13's sub-interpreter scheduler will
// flip the contention path live without further changes here.
//
// CPython: Python/ceval_gil.c take_gil saved_switchnum + COND_TIMED_WAIT
type gilSwitchTimer struct {
	start pytime.Time // monotonic ns at the last hand-off
	armed bool        // BreakerGILDropRequest already set this hold
}

// reset records the current monotonic reading as the start of a new
// hold window. Called when the GIL is acquired.
//
// CPython: Python/ceval_gil.c take_gil after MUTEX_UNLOCK
func (t *gilSwitchTimer) reset() {
	var now pytime.Time
	_ = pytime.Monotonic(&now)
	t.start = now
	t.armed = false
}

// elapsed returns the monotonic nanoseconds since the last reset.
//
// CPython: Python/ceval_gil.c take_gil's `now - initial_ts` shape
func (t *gilSwitchTimer) elapsed() pytime.Time {
	var now pytime.Time
	_ = pytime.Monotonic(&now)
	return now - t.start
}

// poll arms BreakerGILDropRequest if a drop has been requested AND
// the switch interval has elapsed since the last hand-off. Returns
// true when this call flipped the bit.
//
// CPython: Python/ceval_gil.c take_gil's
//
//	if (timed_out && ... gil->switch_number == saved_switchnum) {
//	    _Py_set_eval_breaker_bit(holder, _PY_GIL_DROP_REQUEST_BIT);
//	}
func (t *gilSwitchTimer) poll(g *gil.GIL, b *gil.Breaker) bool {
	if t.armed || g == nil || b == nil {
		return false
	}
	if !g.DropRequested() {
		return false
	}
	interval := pytime.FromNanoseconds(g.SwitchInterval().Nanoseconds())
	if t.elapsed() < interval {
		return false
	}
	b.Set(gil.BreakerGILDropRequest)
	t.armed = true
	return true
}
