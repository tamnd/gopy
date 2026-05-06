// Pins the switch-interval math used by the future GIL-drop
// handshake. The timer goes through pytime.Monotonic instead of Go's
// time.Now() so sys.setswitchinterval(0.005) lands as 5_000_000 ns of
// elapsed monotonic time before the breaker arms, matching CPython.
//
// CPython: Python/ceval_gil.c take_gil interval-wait branch

package vm

import (
	"testing"
	"time"

	"github.com/tamnd/gopy/gil"
)

// TestGILSwitchTimerArmsAfterIntervalWithDropRequest pins the happy
// path: a drop has been requested and the switch interval has
// elapsed, so poll arms BreakerGILDropRequest and reports the flip.
func TestGILSwitchTimerArmsAfterIntervalWithDropRequest(t *testing.T) {
	g := gil.New()
	g.SetSwitchInterval(1 * time.Millisecond)
	g.RequestDrop()

	b := &gil.Breaker{}
	timer := &gilSwitchTimer{}
	timer.reset()
	time.Sleep(2 * time.Millisecond)

	if !timer.poll(g, b) {
		t.Fatal("poll returned false; expected the bit to flip")
	}
	if !b.IsSet(gil.BreakerGILDropRequest) {
		t.Error("BreakerGILDropRequest not set after poll")
	}
	if !timer.armed {
		t.Error("timer.armed should be true after a successful poll")
	}
	if timer.poll(g, b) {
		t.Error("second poll should be a no-op once armed")
	}
}

// TestGILSwitchTimerNoDropRequestNoArm pins the steady-state v0.9
// shape: no other thread has requested a drop, so the timer never
// arms even if the interval has elapsed.
func TestGILSwitchTimerNoDropRequestNoArm(t *testing.T) {
	g := gil.New()
	g.SetSwitchInterval(1 * time.Millisecond)

	b := &gil.Breaker{}
	timer := &gilSwitchTimer{}
	timer.reset()
	time.Sleep(2 * time.Millisecond)

	if timer.poll(g, b) {
		t.Error("poll armed without a drop request")
	}
	if b.IsSet(gil.BreakerGILDropRequest) {
		t.Error("BreakerGILDropRequest set without a drop request")
	}
}

// TestGILSwitchTimerIntervalNotElapsed pins that an outstanding drop
// request alone is not enough: the holder must have run for at least
// SwitchInterval since the last hand-off before the bit arms.
func TestGILSwitchTimerIntervalNotElapsed(t *testing.T) {
	g := gil.New()
	g.SetSwitchInterval(1 * time.Hour) // far longer than the test
	g.RequestDrop()

	b := &gil.Breaker{}
	timer := &gilSwitchTimer{}
	timer.reset()

	if timer.poll(g, b) {
		t.Error("poll armed before the interval elapsed")
	}
	if b.IsSet(gil.BreakerGILDropRequest) {
		t.Error("BreakerGILDropRequest set before the interval elapsed")
	}
}

// TestGILSwitchTimerResetClearsArm pins that reset starts a fresh
// hold window. Useful when a thread re-acquires the GIL after a
// release; the next interval-wait must measure from now, not from
// the previous hold.
func TestGILSwitchTimerResetClearsArm(t *testing.T) {
	g := gil.New()
	g.SetSwitchInterval(1 * time.Millisecond)
	g.RequestDrop()

	b := &gil.Breaker{}
	timer := &gilSwitchTimer{}
	timer.reset()
	time.Sleep(2 * time.Millisecond)
	timer.poll(g, b)
	if !timer.armed {
		t.Fatal("setup: timer should be armed before reset")
	}

	timer.reset()
	if timer.armed {
		t.Error("timer.armed should clear on reset")
	}
}
