// RESUME / RETURN_GENERATOR / generator re-entry. CPython handles
// these in Python/ceval.c around the eval-loop entry. v0.6 keeps a
// minimal RESUME that polls the eval breaker and otherwise no-ops;
// the generator path lives behind the objects.Generator port (1687)
// and is wired in v0.7.
//
// CPython: Python/ceval.c RESUME
package vm

// DEPRECATED (spec 1714): Spec 1714 phases 5+6: RESUME family bodies migrate to typed op<NAME> functions invoked from vm/eval_dispatch_gen.go.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/monitor"
)

// handleResume is the hand-written RESUME arm. CPython distinguishes
// initial resume vs yield re-entry vs awaitable re-entry via the
// oparg low bits, plus polls the eval breaker on bits 0..1. v0.6 only
// honors the breaker poll; the generator state machine arrives with
// the rest of the async/generator port.
//
// CPython: Python/bytecodes.c RESUME
func (e *evalState) handleResume(op compile.Opcode, oparg uint32) (next int, err error) {
	_ = op
	// Instrument the code object when the global monitoring version has
	// changed since the last entry. This inserts INSTRUMENTED_LINE markers
	// so that sys.settrace line callbacks fire for this frame.
	//
	// CPython: Python/bytecodes.c:196 _Py_Instrument(_PyFrame_GetCode(frame), tstate->interp)
	if interp := e.ts.Interp(); interp != nil && interp.Monitors != nil {
		before := e.f.Code.Code[e.f.InstrPtr]
		if merr := monitor.Instrument(e.f.Code, interp.Monitors); merr != nil {
			return 0, merr
		}
		// CPython's RESUME re-reads this_instr after _Py_Instrument and
		// dispatches straight into INSTRUMENTED_RESUME, so the PY_START /
		// PY_RESUME event fires before the body runs. When the walk just
		// rewrote this slot (the frame began after sys.settrace and so
		// missed the chance to instrument at entry), re-dispatch the same
		// offset instead of advancing; the second pass fetches the now
		// INSTRUMENTED_RESUME, fires the event, and the no-op re-instrument
		// leaves the slot unchanged so this does not loop.
		//
		// CPython: Python/bytecodes.c:196 RESUME
		if e.f.Code.Code[e.f.InstrPtr] != before &&
			compile.Opcode(e.f.Code.Code[e.f.InstrPtr]) == compile.INSTRUMENTED_RESUME {
			return e.f.InstrPtr, nil
		}
	}
	if oparg < 2 {
		if e.gilTimer != nil {
			e.gilTimer.poll(e.gil, e.breaker)
		}
		if e.breaker != nil && e.breaker.Load() != 0 {
			if berr := e.handleEvalBreaker(); berr != nil {
				return 0, berr
			}
		}
	}
	return e.advance(), nil
}
