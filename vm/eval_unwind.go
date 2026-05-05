// Exception unwind. When a dispatch arm returns an error, the loop
// walks the code object's PEP 657 exception table to find a handler.
// On hit, the loop pushes the exception, repoints InstrPtr at the
// handler, and resumes dispatch. On miss, the error escapes to the
// caller (or to the next frame up the chain).
//
// CPython: Python/ceval.c exception_unwind / get_exception_handler

package vm

import (
	"github.com/tamnd/gopy/objects"
)

// handleException tries to find a handler for err in the current
// frame. Returns (residualValue, true) on hit (caller continues
// dispatch); (nil, false) on miss (caller propagates).
//
// CPython: Python/ceval.c exception_unwind
func (e *evalState) handleException(err error) (objects.Object, bool) {
	// Walk e.f.Code.ExceptionTable for an entry whose [start, end)
	// range covers e.f.PrevInstr. The encoding is the varint format
	// 1628 emits: see Python/ceval.c get_exception_handler.
	//
	// v0.6: not implemented. The loop just propagates the error.
	// Lands fully in #158.
	_ = err
	return nil, false
}

// unwind is invoked when the eval-breaker handler errors. It pops
// the current frame and returns the error so the caller frame can
// see it.
//
// CPython: Python/ceval.c goto exception_unwind
func (e *evalState) unwind(err error) (objects.Object, error) {
	return nil, err
}

// handleEvalBreaker drains pending state visible through the breaker:
// requested GIL drops, queued pending calls, async exceptions, GC
// requests. Returns an error if any handler errored.
//
// CPython: Python/ceval_gil.c handle_signals + _Py_HandlePending
func (e *evalState) handleEvalBreaker() error {
	// v0.6 just clears the bits we know how to handle; the rest stay
	// set so a real implementation in #158/#161 sees them.
	//
	// Pending-call drain and signal handling are wired through the
	// gil package; the eval loop only needs to ask the per-thread
	// state for them. The state plumbing lands alongside #161.
	return nil
}
