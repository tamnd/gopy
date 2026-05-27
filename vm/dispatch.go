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
	"github.com/tamnd/gopy/monitor"
)

// dispatch executes one instruction and returns the next-pc and any
// error. Frame termination uses the errFrameReturn sentinel (with the
// terminal value parked on e.retVal); the loop pattern-matches that
// sentinel before consulting handleException, matching CPython's
// goto exit_frame pattern.
//
// Return contract:
//
//   - err == errFrameReturn: loop returns e.retVal, nil. Used by
//     RETURN_VALUE / INTERPRETER_EXIT / RETURN_GENERATOR.
//   - err != nil: the loop walks the exception table.
//   - otherwise: the loop sets InstrPtr = next and continues.
//
// CPython: Python/ceval.c switch over op
//
//nolint:gocognit // mirrors CPython's ceval.c per-opcode dispatch; complexity is the surface, not algorithmic branching
func (e *evalState) dispatch(op compile.Opcode, oparg uint32) (next int, err error) {
	// CPython: Python/ceval_macros.h:63 INSTRUCTION_STATS. Bumps the
	// per-opcode counter + pair counter before any specializer / fast
	// arm runs, matching the CPython placement (DISPATCH expands to
	// INSTRUCTION_STATS(op) just before the TARGET label).
	e.recordOpcode(op)
	// Instrumentation routing: the common case (op is not an
	// INSTRUMENTED_ variant) bails on a single [256]bool load. Only
	// when op is one of the 21 INSTRUMENTED_ opcodes do we route
	// through the LINE handler / PEP 669 callback fire / base-rewrite
	// sequence. Pre-D1, monitor.IsInstrumented was called for every
	// dispatch and burned ~6% of CPU on the tight bench just on the
	// non-instrumented path.
	//
	// CPython: Python/ceval.c TARGET(INSTRUMENTED_*) labels are
	// reached directly via the computed-goto table, so the
	// non-instrumented path costs zero. Mirrored here by the
	// instrumentedRewrite gate.
	if instrumentedRewrite[op] {
		if op == compile.INSTRUMENTED_LINE {
			newOp, err := e.handleInstrumentedLine()
			if err != nil {
				return 0, err
			}
			op = newOp
			if !instrumentedRewrite[op] {
				goto afterInstrument
			}
		}
		if err := e.fireInstrumented(op, oparg); err != nil {
			return 0, err
		}
		op = instrumentedToBase[op]
	}
afterInstrument:
	// Specializer routing: only Quickened code carries inline-cache
	// counters and specialized variants; non-Quickened code (raw
	// compile output before specialize.Quicken) skips the entire
	// adaptive ladder. Pre-D1, the three Quickened-gated helpers
	// (trySpecialized + maybeDeopt + adaptiveTick) were each called
	// unconditionally and burned ~12% of CPU on the tight bench just
	// on their early-return paths.
	//
	// CPython: Python/ceval.c only enters the adaptive ladder under
	// the per-opcode TARGET(<adaptive>) label, never on the generic
	// non-quickened body.
	if e.f.Code.Quickened {
		if next, ok, err := e.trySpecialized(op, oparg); ok {
			return next, err
		}
		if base, deopted := e.maybeDeopt(op); deopted {
			op = base
		} else if e.adaptiveTick(op, oparg) {
			// adaptiveTick rewrote the opcode in place; pick up the
			// fresh op and give the fast-path arm a shot before
			// falling back to the generic body.
			op = compile.Opcode(e.f.Code.Code[e.f.InstrPtr])
			// The re-read may yield INSTRUMENTED_LINE when the slot
			// was overwritten by the monitoring shadow walk. Resolve
			// the original opcode without re-firing the line event;
			// the fire already happened above.
			//
			// CPython: Python/ceval.c DISPATCH_GOTO avoids this by
			// jumping directly to TARGET(INSTRUMENTED_LINE) from the
			// adaptive rewrite path, which then re-enters the line
			// handler. Here we short-circuit to the opcode lookup.
			if op == compile.INSTRUMENTED_LINE {
				instr := e.f.InstrPtr / 2
				data := monitor.CoMonitoring(e.f.Code)
				op = monitor.GetOriginalOpcode(data, instr)
				if op == 0 {
					op = compile.NOP
				}
			}
			if next, ok, err := e.trySpecialized(op, oparg); ok {
				return next, err
			}
			if base2, deopted2 := e.maybeDeopt(op); deopted2 {
				op = base2
			}
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
	if next, ok, err := e.dispatchHandwritten(op, oparg); ok {
		return next, err
	}
	// Hand-written panel for the smallest core opcodes so trivial
	// programs run end-to-end before 1621 codegen lands.
	if next, ok, err := e.trySimple(op, oparg); ok {
		return next, err
	}
	// Import machinery arms.
	if next, ok, err := e.tryImport(op, oparg); ok {
		return next, err
	}
	// Generator / coroutine / context-manager arms.
	if r, err := e.tryGen(op, oparg); r.ok {
		return r.next, err
	}
	// Pattern-match arms.
	if next, ok, err := e.tryMatch(op, oparg); ok {
		return next, err
	}
	return 0, opcodeNotImplemented(op)
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
