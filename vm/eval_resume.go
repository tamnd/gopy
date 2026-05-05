// RESUME / RETURN_GENERATOR / generator re-entry. CPython handles
// these in Python/ceval.c around the eval-loop entry. v0.6 keeps a
// minimal RESUME that polls the eval breaker and otherwise no-ops;
// the generator path lives behind the objects.Generator port (1687)
// and is wired in v0.7.
//
// CPython: Python/ceval.c RESUME
package vm

import "github.com/tamnd/gopy/compile"

// handleResume is the hand-written RESUME arm. CPython distinguishes
// initial resume vs yield re-entry vs awaitable re-entry via the
// oparg low bits, plus polls the eval breaker on bits 0..1. v0.6 only
// honours the breaker poll; the generator state machine arrives with
// the rest of the async/generator port.
//
// CPython: Python/bytecodes.c RESUME
func (e *evalState) handleResume(op compile.Opcode, oparg uint32) (next int, err error) {
	_ = op
	if oparg < 2 && e.breaker != nil && e.breaker.Load() != 0 {
		if berr := e.handleEvalBreaker(); berr != nil {
			return 0, berr
		}
	}
	return e.advance(), nil
}
