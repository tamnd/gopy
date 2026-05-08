// Tier-2 dispatch gate. The projector lays down a trace, the analysis
// pass cleans it up, and RunExecutor drives the trace through the
// hand-ported per-uop methods until one of the four terminal statuses
// fires. This gate pins the integration: real traces dispatch under
// the same loop the synthetic tests in optimizer/uops_test.go exercise.
//
// Coverage is split across three checks:
//
//   - Prologue-only happy path through hand-built traces, exercising
//     each hand-ported method in turn.
//   - Stub-bail path: a stubbed uop in the middle of a trace returns
//     StatusDeopt without panic or stack corruption.
//   - End-to-end: the projector-produced trace from a real loop runs
//     through enough hand-ported prologue rows to either terminate or
//     deopt at the first un-ported body.

package v012test

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/optimizer"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// uopsGateFrame allocates a frame backed by a code object with one
// fast local and a 4-slot value stack, big enough for every starter
// uop in the v0.12 hand-port.
func uopsGateFrame() *frame.Frame {
	co := &objects.Code{
		Code:      []byte{byte(compile.NOP), 0},
		Varnames:  []string{"x"},
		Stacksize: 4,
	}
	f := &frame.Frame{}
	f.Init(co, nil, nil, nil, nil)
	return f
}

// validExecutor wraps a hand-built trace into a minimum-viable
// Executor with VMData.Valid set so _CHECK_VALIDITY does not trigger
// the side-exit arm.
func validExecutor(trace []optimizer.UOPInstruction) *optimizer.Executor {
	exec := &optimizer.Executor{Trace: trace}
	exec.VMData.Valid = true
	return exec
}

// TestUopsGateHandPortedHappyPath drives a synthetic trace covering
// every hand-ported uop in uops_impl.go: _START_EXECUTOR,
// _CHECK_VALIDITY, _SET_IP, _NOP, _LOAD_FAST, _COPY, _SWAP,
// _LOAD_FAST_BORROW, _STORE_FAST, _POP_TOP, _PUSH_NULL, _MAKE_WARM,
// _EXIT_TRACE. A clean StatusExit means the dispatch loop stitched
// every method into the right method on *Tier2State.
func TestUopsGateHandPortedHappyPath(t *testing.T) {
	val := objects.NewInt(7)
	f := uopsGateFrame()
	f.SetLocal(0, stackref.FromObject(val))

	trace := []optimizer.UOPInstruction{
		{OpcodeAndFormat: optimizer.UopStartExecutor},
		{OpcodeAndFormat: optimizer.UopCheckValidity},
		{OpcodeAndFormat: optimizer.UopSetIp, Target: 4},
		{OpcodeAndFormat: optimizer.UopNop},
		{OpcodeAndFormat: optimizer.UopLoadFast, Oparg: 0},       // [v]
		{OpcodeAndFormat: optimizer.UopCopy, Oparg: 1},           // [v, v]
		{OpcodeAndFormat: optimizer.UopSwap, Oparg: 2},           // [v, v]
		{OpcodeAndFormat: optimizer.UopPushNull},                 // [v, v, NULL]
		{OpcodeAndFormat: optimizer.UopPopTop},                   // [v, v]
		{OpcodeAndFormat: optimizer.UopStoreFast, Oparg: 0},      // [v] -> local
		{OpcodeAndFormat: optimizer.UopPopTop},                   // []
		{OpcodeAndFormat: optimizer.UopLoadFastBorrow, Oparg: 0}, // [v]
		{OpcodeAndFormat: optimizer.UopPopTop},                   // []
		{OpcodeAndFormat: optimizer.UopMakeWarm},
		{OpcodeAndFormat: optimizer.UopExitTrace},
	}
	exec := validExecutor(trace)

	if st := optimizer.RunExecutor(nil, f, exec); st != optimizer.StatusExit {
		t.Fatalf("RunExecutor status = %d, want StatusExit", st)
	}
	if !exec.VMData.Warm {
		t.Error("_MAKE_WARM did not flip VMData.Warm")
	}
	if f.InstrPtr != 4 {
		t.Errorf("frame.InstrPtr = %d, want 4 (set by _SET_IP)", f.InstrPtr)
	}
	if f.StackTop != 0 {
		t.Errorf("frame.StackTop = %d, want 0 after balanced trace", f.StackTop)
	}
	if got := f.LocalAt(0).AsObject(); got != val {
		t.Errorf("local 0 round-trip failed: got %v, want %v", got, val)
	}
}

// TestUopsGateStubReturnsDeopt asserts that hitting a stubbed uop
// (every Tier-2-viable uop the hand-port set has not covered yet)
// cleanly returns StatusDeopt instead of panicking or running a
// half-implemented body. _BINARY_OP is in the stub set as of v0.12.
func TestUopsGateStubReturnsDeopt(t *testing.T) {
	exec := validExecutor([]optimizer.UOPInstruction{
		{OpcodeAndFormat: optimizer.UopStartExecutor},
		{OpcodeAndFormat: optimizer.UopBinaryOp},
	})
	st := optimizer.RunExecutor(nil, uopsGateFrame(), exec)
	if st != optimizer.StatusDeopt {
		t.Fatalf("RunExecutor status = %d, want StatusDeopt", st)
	}
}

// TestUopsGateProjectedTraceTerminates pins the integration with a
// real Optimize-produced trace: dispatch must terminate (no infinite
// loop) and land on either StatusExit or StatusDeopt (the only two
// statuses a partially-ported trace can reach). The exact terminal
// status depends on which uop in the body is the first stub; both
// are acceptable as long as the loop ran.
func TestUopsGateProjectedTraceTerminates(t *testing.T) {
	co := loopBytecode()
	interp := &state.Interpreter{}
	exec, status := optimizer.Optimize(interp, nil, co, 0, 0, 0)
	if status != 1 || exec == nil {
		t.Fatalf("Optimize status=%d exec=%v, want a usable trace", status, exec)
	}

	// Dispatcher needs a frame whose code is laid out the way the
	// trace expects. The projected trace's _SET_IP rows carry
	// Tier-1 codeunit indices, so the frame must use the same
	// Code object the trace was projected from.
	f := &frame.Frame{}
	f.Init(co, nil, nil, nil, nil)

	st := optimizer.RunExecutor(nil, f, exec)
	switch st {
	case optimizer.StatusExit, optimizer.StatusDeopt:
		// Both are valid v0.12 outcomes. StatusExit means the
		// hand-port set was enough for this trace; StatusDeopt
		// means a stubbed uop bailed cleanly.
	default:
		t.Fatalf("RunExecutor status = %d, want StatusExit or StatusDeopt", st)
	}
}
