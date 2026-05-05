// Instrumented opcode normalization. CPython 3.14 emits an
// INSTRUMENTED_<name> variant for each opcode that PEP 669 monitors;
// the variants live above 230 in the opcode table and run the same
// body as their base form when no monitoring tools are attached.
//
// v0.6 ports the base behavior only. Real monitoring lands with
// 1634 at v0.9. Until then, strip the INSTRUMENTED_ prefix and
// dispatch the base opcode so generated code that includes a
// monitored region still runs.
//
// CPython: Python/instrumentation.c de_instrument_per_instruction
// CPython: Python/ceval.c TARGET(INSTRUMENTED_*)

package vm

import "github.com/tamnd/gopy/compile"

// instrumentedToBase pairs each INSTRUMENTED_<name> with the base
// opcode that handles it. Pure marker variants (INSTRUMENTED_LINE,
// INSTRUMENTED_INSTRUCTION) map to NOP; real monitoring will fire
// the per-instruction / per-line hook before falling through.
var instrumentedToBase = map[compile.Opcode]compile.Opcode{
	compile.INSTRUMENTED_END_FOR:              compile.END_FOR,
	compile.INSTRUMENTED_POP_ITER:             compile.POP_ITER,
	compile.INSTRUMENTED_END_SEND:             compile.END_SEND,
	compile.INSTRUMENTED_FOR_ITER:             compile.FOR_ITER,
	compile.INSTRUMENTED_JUMP_FORWARD:         compile.JUMP_FORWARD,
	compile.INSTRUMENTED_NOT_TAKEN:            compile.NOT_TAKEN,
	compile.INSTRUMENTED_POP_JUMP_IF_TRUE:     compile.POP_JUMP_IF_TRUE,
	compile.INSTRUMENTED_POP_JUMP_IF_FALSE:    compile.POP_JUMP_IF_FALSE,
	compile.INSTRUMENTED_POP_JUMP_IF_NONE:     compile.POP_JUMP_IF_NONE,
	compile.INSTRUMENTED_POP_JUMP_IF_NOT_NONE: compile.POP_JUMP_IF_NOT_NONE,
	compile.INSTRUMENTED_RESUME:               compile.RESUME,
	compile.INSTRUMENTED_RETURN_VALUE:         compile.RETURN_VALUE,
	compile.INSTRUMENTED_YIELD_VALUE:          compile.YIELD_VALUE,
	compile.INSTRUMENTED_END_ASYNC_FOR:        compile.END_ASYNC_FOR,
	compile.INSTRUMENTED_LOAD_SUPER_ATTR:      compile.LOAD_SUPER_ATTR,
	compile.INSTRUMENTED_CALL:                 compile.CALL,
	compile.INSTRUMENTED_CALL_KW:              compile.CALL_KW,
	compile.INSTRUMENTED_CALL_FUNCTION_EX:     compile.CALL_FUNCTION_EX,
	compile.INSTRUMENTED_JUMP_BACKWARD:        compile.JUMP_BACKWARD,
	compile.INSTRUMENTED_INSTRUCTION:          compile.NOP,
	compile.INSTRUMENTED_LINE:                 compile.NOP,
}

// baseForInstrumented returns the non-instrumented form of op when
// op is one of the INSTRUMENTED_ variants. The second return is true
// only when a rewrite happened so callers can keep counting cycles
// correctly once monitoring lands.
func baseForInstrumented(op compile.Opcode) (compile.Opcode, bool) {
	base, ok := instrumentedToBase[op]
	if !ok {
		return op, false
	}
	return base, true
}
