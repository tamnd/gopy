// Smoke tests for the Tier-2 dispatch driver. Hand-built traces
// exercise the dispatch loop end to end against the hand-ported uops
// in uops_impl.go; stubbed uops are not reached so the trace runs to
// completion under StatusExit / StatusContinue only.
//
// CPython: Python/ceval.c:1240-1358 enter_tier_two

package optimizer

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// newTraceFrame allocates a Frame backed by a Code with one local
// slot and one stack slot, suitable for the small synthetic traces
// the dispatch tests run.
func newTraceFrame(t *testing.T) *frame.Frame {
	t.Helper()
	co := &objects.Code{
		Code:      []byte{byte(compile.NOP), 0},
		Varnames:  []string{"x"},
		Stacksize: 4,
	}
	f := &frame.Frame{}
	f.Init(co, nil, nil, nil, nil)
	return f
}

// newTraceExecutor wraps a hand-built trace into a minimum-viable
// Executor with a Valid bit set. The dispatch loop only reads
// VMData.Valid, so the rest of the bookkeeping fields stay zero.
func newTraceExecutor(trace []UOPInstruction) *Executor {
	exec := &Executor{Trace: trace}
	exec.VMData.Valid = true
	return exec
}

func TestRun_NopThenExit(t *testing.T) {
	exec := newTraceExecutor([]UOPInstruction{
		{OpcodeAndFormat: UopNop},
		{OpcodeAndFormat: UopExitTrace},
	})
	st := RunExecutor(nil, newTraceFrame(t), exec)
	if st != StatusExit {
		t.Fatalf("status = %d, want StatusExit", st)
	}
}

func TestRun_LoadFastStoreFastEcho(t *testing.T) {
	f := newTraceFrame(t)
	val := objects.NewInt(42)
	f.SetLocal(0, stackref.FromObject(val))

	// _LOAD_FAST 0 ; _STORE_FAST 0 ; _EXIT_TRACE.
	exec := newTraceExecutor([]UOPInstruction{
		{OpcodeAndFormat: UopLoadFast, Oparg: 0},
		{OpcodeAndFormat: UopStoreFast, Oparg: 0},
		{OpcodeAndFormat: UopExitTrace},
	})
	st := RunExecutor(nil, f, exec)
	if st != StatusExit {
		t.Fatalf("status = %d, want StatusExit", st)
	}
	if got := f.LocalAt(0).AsObject(); got != val {
		t.Errorf("local 0 round-trip failed: got %v, want %v", got, val)
	}
	if f.StackTop != 0 {
		t.Errorf("stack should be empty after store, got top=%d", f.StackTop)
	}
}

func TestRun_StubbedUopDeopts(t *testing.T) {
	// _BINARY_OP is in the stub set: the dispatch must return
	// StatusDeopt rather than panic or run an unimplemented body.
	exec := newTraceExecutor([]UOPInstruction{
		{OpcodeAndFormat: UopBinaryOp},
	})
	st := RunExecutor(nil, newTraceFrame(t), exec)
	if st != StatusDeopt {
		t.Fatalf("status = %d, want StatusDeopt", st)
	}
}

func TestRun_CheckValidityBailsOnInvalidExecutor(t *testing.T) {
	exec := newTraceExecutor([]UOPInstruction{
		// _CHECK_VALIDITY at index 0 with Format=Jump and Target
		// pointing at index 2 (the _EXIT_TRACE arm).
		{OpcodeAndFormat: UopCheckValidity | (1 << 15), Target: 2},
		{OpcodeAndFormat: UopNop}, // skipped on jump
		{OpcodeAndFormat: UopExitTrace},
	})
	exec.VMData.Valid = false
	st := RunExecutor(nil, newTraceFrame(t), exec)
	if st != StatusExit {
		t.Fatalf("status = %d, want StatusExit (jump landed on _EXIT_TRACE)", st)
	}
}
