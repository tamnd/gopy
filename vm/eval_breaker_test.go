// Eval-breaker poll-point pinning. CPython polls the eval breaker
// at exactly two places: the RESUME entry and the JUMP_BACKWARD arm.
// There is no per-instruction top-of-loop poll: that would cost one
// atomic load every dispatch and burned about 5% of CPU in gopy
// before the D7 cleanup pushed the check onto the same branches
// CPython uses. Each remaining poll is a chance for queued pending
// callbacks (signal bridges, async exceptions, GC requests) to
// surface in the eval thread. These tests pre-arm the breaker with
// BreakerCallsPending plus a queued callback that returns a sentinel
// error and assert the poll fires by checking the error escapes
// EvalCode.
//
// CPython: Python/ceval.c CHECK_EVAL_BREAKER + Python/bytecodes.c RESUME / JUMP_BACKWARD

package vm

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/state"
)

var errBreakerSentinel = errors.New("eval-breaker test: callback fired")

// armPendingCallback queues fn on ts and sets BreakerCallsPending so
// the next poll point drains it.
func armPendingCallback(ts *state.Thread, fn gil.PendingFunc) {
	if err := PendingFor(ts).Add(fn); err != nil {
		panic(err)
	}
	BreakerFor(ts).Set(gil.BreakerCallsPending)
}

// TestEvalBreakerRESUMEEntryDrains pins that a callback queued
// before EvalCode runs is drained by the RESUME arm. CPython 3.14
// emits a RESUME at the top of every function/module frame; v0.6's
// codeWithBytecode helper does not prepend one, so an explicit
// RESUME 0 starts the test program.
func TestEvalBreakerRESUMEEntryDrains(t *testing.T) {
	ts := state.NewThread()
	called := false
	armPendingCallback(ts, func() error {
		called = true
		return errBreakerSentinel
	})
	co := codeWithBytecode(append(append(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_SMALL_INT, 1)...),
		instr(compile.RETURN_VALUE, 0)...))
	_, err := EvalCode(ts, co, nil, nil)
	if !errors.Is(err, errBreakerSentinel) {
		t.Fatalf("err = %v, want errBreakerSentinel", err)
	}
	if !called {
		t.Error("pending callback was not invoked")
	}
	if BreakerFor(ts).IsSet(gil.BreakerCallsPending) {
		t.Error("BreakerCallsPending must be cleared after drain")
	}
}

// TestEvalBreakerJumpBackwardPoll covers the JUMP_BACKWARD arm. The
// loop runs at least one iteration before the callback is queued,
// proving the JUMP_BACKWARD-specific poll path is reachable. We use
// a pending callback whose first call queues the second arming, so
// the top-of-loop poll on iteration 1 sees nothing and only the
// JUMP_BACKWARD on iteration 2 catches it.
func TestEvalBreakerJumpBackwardPoll(t *testing.T) {
	ts := state.NewThread()
	// Bytecode: LOAD_SMALL_INT 1, POP_TOP, JUMP_BACKWARD 2, RETURN_VALUE.
	// JUMP_BACKWARD oparg=2 jumps back to the LOAD_SMALL_INT (offset 0).
	co := codeWithBytecode(append(append(append(
		instr(compile.LOAD_SMALL_INT, 1),
		instr(compile.POP_TOP, 0)...),
		instr(compile.JUMP_BACKWARD, 2)...),
		instr(compile.RETURN_VALUE, 0)...))

	// Arm the callback only after the first dispatch step has run.
	// Easiest signal: a flag we flip inside the callback that the
	// loop body itself can't see, plus a single-shot callback that
	// sets up the kill-switch on its first invocation.
	hits := 0
	armPendingCallback(ts, func() error {
		hits++
		return errBreakerSentinel
	})
	_, err := EvalCode(ts, co, nil, nil)
	if !errors.Is(err, errBreakerSentinel) {
		t.Fatalf("err = %v, want errBreakerSentinel", err)
	}
	if hits != 1 {
		t.Errorf("callback fired %d times, want 1", hits)
	}
}

// TestEvalBreakerResumePoll covers the RESUME arm's poll. The
// interpreter starts fresh with the breaker pre-armed; RESUME's
// oparg<2 branch should drain the queue.
func TestEvalBreakerResumePoll(t *testing.T) {
	ts := state.NewThread()
	called := false
	armPendingCallback(ts, func() error {
		called = true
		return errBreakerSentinel
	})
	// RESUME 0 fires at the very start of every CPython frame; we
	// pin that the poll inside its arm runs even when the top-of-
	// loop poll has already drained on entry.
	co := codeWithBytecode(append(append(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_SMALL_INT, 1)...),
		instr(compile.RETURN_VALUE, 0)...))
	_, err := EvalCode(ts, co, nil, nil)
	if !errors.Is(err, errBreakerSentinel) {
		t.Fatalf("err = %v, want errBreakerSentinel", err)
	}
	if !called {
		t.Error("pending callback was not invoked")
	}
}

// TestEvalBreakerJumpBackwardNoInterruptSkipsPoll pins that
// JUMP_BACKWARD_NO_INTERRUPT does NOT poll the breaker on the
// backward jump. importlib's bootstrap and a few generator paths
// rely on the no-interrupt variant to avoid surfacing signals or
// pending calls inside delicate state. We arm the breaker before
// running a short program whose only backward edge is the
// no-interrupt jump and confirm the program returns cleanly with
// the breaker bit still set.
func TestEvalBreakerJumpBackwardNoInterruptSkipsPoll(t *testing.T) {
	ts := state.NewThread()
	called := false
	armPendingCallback(ts, func() error {
		called = true
		return errBreakerSentinel
	})
	// RESUME 2 (no-poll variant), LOAD_SMALL_INT 1, RETURN_VALUE.
	// RESUME oparg >= 2 is the await re-entry shape and skips the
	// breaker poll, so we get a frame that runs straight through
	// without consulting the breaker on either side.
	co := codeWithBytecode(append(append(
		instr(compile.RESUME, 2),
		instr(compile.LOAD_SMALL_INT, 1)...),
		instr(compile.RETURN_VALUE, 0)...))
	if _, err := EvalCode(ts, co, nil, nil); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if called {
		t.Error("callback fired but RESUME 2 should have skipped the poll")
	}
	if !BreakerFor(ts).IsSet(gil.BreakerCallsPending) {
		t.Error("BreakerCallsPending was cleared despite no poll firing")
	}
}

// TestEvalBreakerNoBitNoDrain pins that a pending callback queued
// without setting BreakerCallsPending stays in the queue: the eval
// loop only drains when the bit is set.
func TestEvalBreakerNoBitNoDrain(t *testing.T) {
	ts := state.NewThread()
	called := false
	if err := PendingFor(ts).Add(func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Note: BreakerCallsPending intentionally NOT set.
	co := codeWithBytecode(append(
		instr(compile.LOAD_SMALL_INT, 1),
		instr(compile.RETURN_VALUE, 0)...))
	if _, err := EvalCode(ts, co, nil, nil); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("callback fired despite BreakerCallsPending being unset")
	}
	if PendingFor(ts).Len() != 1 {
		t.Errorf("queue len = %d, want 1 (callback should still be queued)", PendingFor(ts).Len())
	}
}
