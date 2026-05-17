// Dispatch driver. The generated per-opcode arms in opcodes_gen.go
// (1621) are reached through dispatch, which the loop calls each
// iteration. Until the generator runs, dispatch returns
// ErrNotImplemented for every opcode so the loop driver can be tested
// in isolation.
//
// CPython: Python/ceval.c TARGET / DISPATCH macros expand into the
// per-opcode arm in CPython; here the indirection is one extra
// switch.

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// dispatch executes one instruction and returns the next-pc, a
// terminal return value (set when retDone is true), or an error.
//
// Return contract:
//
//   - retDone=true: the loop returns retVal/retErr immediately. Used
//     by RETURN_VALUE / RETURN_CONST / INTERPRETER_EXIT.
//   - err != nil: the loop walks the exception table.
//   - otherwise: the loop sets InstrPtr = next and continues.
//
// CPython: Python/ceval.c switch over op
func (e *evalState) dispatch(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone bool, err error) {
	// Fire any registered PEP 669 callbacks subscribed to this
	// (event, offset) pair, then strip the INSTRUMENTED_ prefix so
	// the base body runs. INSTRUMENTED_LINE is handled separately:
	// the original opcode is hidden in CoMonitoringData.Lines, not
	// recoverable through the deinstrument table.
	if op == compile.INSTRUMENTED_LINE {
		newOp, err := e.handleInstrumentedLine()
		if err != nil {
			return 0, nil, nil, false, err
		}
		op = newOp
	}
	if err := e.fireInstrumented(op, oparg); err != nil {
		return 0, nil, nil, false, err
	}
	if base, rewritten := baseForInstrumented(op); rewritten {
		op = base
	}
	// On Quickened code, give specialized variants a chance at their
	// fast-path arm first. trySpecialized returns ok=true only when
	// the guards held and the dispatch was taken; otherwise we fall
	// through to the deopt path that rewrites the bytecode back to
	// the adaptive parent and runs the generic body.
	if next, ok, err := e.trySpecialized(op, oparg); ok {
		return next, nil, nil, false, err
	}
	// On Quickened code, fold any remaining specialized variant back
	// to its adaptive parent so the generic body runs, and tick the
	// adaptive counter so a hot site eventually re-specializes.
	if base, deopted := e.maybeDeopt(op); deopted {
		op = base
	} else if e.adaptiveTick(op, oparg) {
		// adaptiveTick rewrote the opcode in place; pick up the
		// fresh op and give the fast-path arm a shot before
		// falling back to the generic body.
		op = compile.Opcode(e.f.Code.Code[e.f.InstrPtr])
		if next, ok, err := e.trySpecialized(op, oparg); ok {
			return next, nil, nil, false, err
		}
		if base2, deopted2 := e.maybeDeopt(op); deopted2 {
			op = base2
		}
	}
	// Spec 1714 phase 5.2: route opcodes whose generated body in
	// vm/eval_dispatch_gen.go has been verified equivalent to the
	// hand-written arm through the generated harness. The whitelist
	// grows as more arms gain real bodies via the action translator.
	if dispatchGenSupported[op] {
		return e.dispatchGen(op, oparg)
	}
	// Spec 1714 phase 5: hand-written op<NAME> bodies, one per opcode,
	// matching the DSL signature. The action translator will replace
	// these with generated arms in vm/eval_dispatch_gen.go as it gains
	// coverage; until then, the bodies live in eval_dispatch_handwritten.go.
	if next, retVal, retErr, retDone, ok, err := e.dispatchHandwritten(op, oparg); ok {
		return next, retVal, retErr, retDone, err
	}
	// Hand-written panel for the smallest core opcodes so trivial
	// programs run end-to-end before 1621 codegen lands.
	if next, retVal, retErr, retDone, ok, err := e.trySimple(op, oparg); ok {
		return next, retVal, retErr, retDone, err
	}
	// Import machinery arms.
	if next, ok, err := e.tryImport(op, oparg); ok {
		return next, nil, nil, false, err
	}
	// Generator / coroutine / context-manager arms.
	if r, err := e.tryGen(op, oparg); r.ok {
		return r.next, r.retVal, r.retErr, r.retDone, err
	}
	// Pattern-match arms.
	if next, ok, err := e.tryMatch(op, oparg); ok {
		return next, nil, nil, false, err
	}
	return 0, nil, nil, false, opcodeNotImplemented(op)
}

// opcodeNotImplemented wraps ErrNotImplemented with the offending op.
func opcodeNotImplemented(op compile.Opcode) error {
	return &notImplemented{op: op}
}

type notImplemented struct{ op compile.Opcode }

func (e *notImplemented) Error() string {
	return "vm: opcode " + opcodeName(e.op) + " not implemented in v0.6"
}

func (e *notImplemented) Unwrap() error { return ErrNotImplemented }

// opcodeName returns the mnemonic for op, falling back to "<unknown>".
func opcodeName(op compile.Opcode) string {
	if name := op.Name(); name != "" {
		return name
	}
	return "<unknown>"
}
