// Exception unwind. When a dispatch arm returns an error, the loop
// walks the code object's PEP 657 exception table to find a handler.
// On hit, the loop pushes the exception, repoints InstrPtr at the
// handler, and resumes dispatch. On miss, the error escapes to the
// caller (or to the next frame up the chain).
//
// CPython: Python/ceval.c exception_unwind / get_exception_handler

package vm

import (
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/objects"
)

// handleException tries to find a handler for err in the current
// frame. Returns (residualValue, true) on hit (caller continues
// dispatch); (nil, false) on miss (caller propagates).
//
// The residual value will be the constructed exception object once
// #160 (exception hierarchy) lands; today it stays nil and the
// exception is reconstructed by the handler from VM state.
//
// CPython: Python/ceval.c:L1815 get_exception_handler + exception_unwind
//
//nolint:unparam // residual value is always nil today; #160 will populate it once exception objects land.
func (e *evalState) handleException(err error) (objects.Object, bool) {
	co := e.f.Code
	if co == nil || len(co.ExceptionTable) == 0 {
		return nil, false
	}
	entry, ok := findExcHandler(co.ExceptionTable, e.f.InstrPtr)
	if !ok {
		return nil, false
	}
	if entry.depth < e.f.StackTop {
		e.f.StackTop = entry.depth
	}
	_ = entry.preserveLasti
	_ = err
	e.f.InstrPtr = entry.target
	e.f.PrevInstr = entry.target
	return nil, true
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
	b := e.breaker
	if b == nil {
		return nil
	}
	if b.IsSet(gil.BreakerCallsPending) {
		b.Clear(gil.BreakerCallsPending)
		if p := PendingFor(e.ts); p != nil {
			if err := p.Drain(); err != nil {
				return err
			}
		}
	}
	return nil
}
