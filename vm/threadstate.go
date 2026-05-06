// Per-thread VM state: the eval breaker, the frame chunk arena, the
// pending-call queue. Kept in a sync.Map keyed by *state.Thread so
// state.Thread doesn't need to import gil/frame (the dependency
// edge would cycle once errors -> state -> gil -> errors lands).
//
// CPython resolves this by hanging fields off PyThreadState; we keep
// the same shape with an out-of-band map until 1615 lifts state.Thread.

package vm

import (
	"sync"

	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/state"
)

// threadVM is the per-Thread VM-private state.
type threadVM struct {
	breaker  *gil.Breaker
	frames   *frame.FrameStack
	pending  *gil.Pending
	gil      *gil.GIL // nil in v0.9; v0.13 sub-interpreter wiring populates this
	gilTimer gilSwitchTimer
}

var threadVMs sync.Map // map[*state.Thread]*threadVM

func vmFor(ts *state.Thread) *threadVM {
	if v, ok := threadVMs.Load(ts); ok {
		return v.(*threadVM)
	}
	v := &threadVM{
		breaker: &gil.Breaker{},
		frames:  frame.New(),
		pending: &gil.Pending{},
	}
	v.gilTimer.reset()
	actual, _ := threadVMs.LoadOrStore(ts, v)
	return actual.(*threadVM)
}

// breakerFor returns the eval breaker associated with ts. Lazy-init.
func breakerFor(ts *state.Thread) *gil.Breaker { return vmFor(ts).breaker }

// frameStackFor returns the frame chunk arena for ts.
func frameStackFor(ts *state.Thread) *frame.FrameStack { return vmFor(ts).frames }

// PendingFor returns the pending-call queue for ts. Public so the
// signal bridge in user code (1651) can wire SIGINT into it.
func PendingFor(ts *state.Thread) *gil.Pending { return vmFor(ts).pending }

// BreakerFor is the public form of breakerFor for the same reason.
func BreakerFor(ts *state.Thread) *gil.Breaker { return vmFor(ts).breaker }

// SetGIL attaches g to ts so the eval loop can drive the switch-interval
// handshake. v0.9 leaves this nil except in tests; v0.13's sub-interpreter
// scheduler will set it during interpreter creation.
//
// CPython: pycore_interp.h _gil_runtime_state lives on PyInterpreterState
func SetGIL(ts *state.Thread, g *gil.GIL) {
	v := vmFor(ts)
	v.gil = g
	v.gilTimer.reset()
}
