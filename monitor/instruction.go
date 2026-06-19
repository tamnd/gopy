// Per-instruction (opcode) event instrumentation. The INSTRUCTION
// event fires once per executed instruction; bdb / pdb drives it
// through frame.f_trace_opcodes so a debugger can single-step at the
// bytecode level. Like LINE, it gets its own side table: the original
// opcode hidden behind INSTRUMENTED_INSTRUCTION lives in
// PerInstructionOpcodes so the dispatcher can recover and run it after
// firing the event.
//
// CPython: Python/instrumentation.c:799 instrument_per_instruction
// CPython: Python/instrumentation.c:1401 _Py_call_instrumentation_instruction

package monitor

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// firstTraceable returns the codeunit index of the first RESUME, the
// boundary CPython stores as code->_co_firsttraceable. The prologue
// before it (MAKE_CELL / COPY_FREE_VARS / RETURN_GENERATOR) runs during
// frame setup and is never traced, so the per-instruction passes skip
// it.
//
// CPython: Objects/codeobject.c:581 entry_point loop
func firstTraceable(code *objects.Code) int {
	n := instructionCount(code)
	for i := 0; i < n; i++ {
		// The RESUME byte may already be hidden behind an instrumentation
		// marker (INSTRUMENTED_LINE / INSTRUMENTED_INSTRUCTION / a baked
		// INSTRUMENTED_RESUME) from an earlier add pass. Resolve through
		// GetBaseCodeUnit so the boundary is stable across add/remove
		// cycles; a raw-byte scan would skip the masked RESUME and shift
		// the boundary, orphaning prologue markers on the way back out.
		//
		// CPython stores this once as code->_co_firsttraceable, computed at
		// code creation, so it never drifts under instrumentation.
		if GetBaseCodeUnit(code, i) == compile.RESUME {
			return i
		}
	}
	return 0
}

// ensurePerInstruction lazily allocates the per-instruction opcode side
// table. Every codeunit is seeded with its de-instrumented opcode so a
// concurrent local-event change still knows the original to restore.
//
// CPython: Python/instrumentation.c:1747 update_instrumentation_data
func ensurePerInstruction(code *objects.Code, data *CoMonitoringData) {
	if data.PerInstructionOpcodes != nil {
		return
	}
	codeLen := instructionCount(code)
	data.PerInstructionOpcodes = make([]uint8, codeLen)
	for i := 0; i < codeLen; i++ {
		op := compile.Opcode(byteAt(code.Code, i))
		data.PerInstructionOpcodes[i] = byte(specialize.Deopt(op))
	}
}

// instrumentPerInstruction rewrites the opcode at codeunit i to
// INSTRUMENTED_INSTRUCTION, stashing the underlying opcode (resolving
// through INSTRUMENTED_LINE first) in PerInstructionOpcodes.
//
// CPython: Python/instrumentation.c:799 instrument_per_instruction
func instrumentPerInstruction(code *objects.Code, data *CoMonitoringData, i int) {
	op := compile.Opcode(byteAt(code.Code, i))
	if op == compile.INSTRUMENTED_LINE {
		op = compile.Opcode(getOriginalOpcode(data.Lines, i))
	}
	if op == compile.INSTRUMENTED_INSTRUCTION {
		return
	}
	if IsInstrumented(op) {
		data.PerInstructionOpcodes[i] = byte(op)
	} else {
		data.PerInstructionOpcodes[i] = byte(specialize.Deopt(op))
	}
	// When the line marker hides this codeunit the real opcode lives in
	// the line table; otherwise it lives in the bytecode. Either way the
	// visible byte that dispatch fetches becomes INSTRUMENTED_INSTRUCTION.
	if compile.Opcode(byteAt(code.Code, i)) == compile.INSTRUMENTED_LINE {
		setOriginalOpcode(data.Lines, i, byte(compile.INSTRUMENTED_INSTRUCTION))
	} else {
		code.Code[2*i] = byte(compile.INSTRUMENTED_INSTRUCTION)
	}
}

// deInstrumentPerInstruction restores the opcode hidden behind
// INSTRUMENTED_INSTRUCTION at codeunit i.
//
// CPython: Python/instrumentation.c:731 de_instrument_per_instruction
func deInstrumentPerInstruction(code *objects.Code, data *CoMonitoringData, i int) {
	hidesLine := false
	op := compile.Opcode(byteAt(code.Code, i))
	if op == compile.INSTRUMENTED_LINE {
		hidesLine = true
		op = compile.Opcode(getOriginalOpcode(data.Lines, i))
	}
	if op != compile.INSTRUMENTED_INSTRUCTION {
		return
	}
	original := data.PerInstructionOpcodes[i]
	if hidesLine {
		setOriginalOpcode(data.Lines, i, original)
	} else {
		code.Code[2*i] = original
	}
}

// addPerInstructionTools sets the per-instruction tool bits at offset
// and stamps the bytecode with INSTRUMENTED_INSTRUCTION.
//
// CPython: Python/instrumentation.c:928 add_per_instruction_tools
func addPerInstructionTools(code *objects.Code, data *CoMonitoringData, offset int, tools uint8) {
	if data.PerInstructionTools != nil {
		data.PerInstructionTools[offset] |= tools
	}
	instrumentPerInstruction(code, data, offset)
}

// removePerInstructionTools clears the per-instruction tool bits at
// offset and restores the original opcode when none remain.
//
// CPython: Python/instrumentation.c:946 remove_per_instruction_tools
func removePerInstructionTools(code *objects.Code, data *CoMonitoringData, offset int, tools uint8) {
	shouldDeinstrument := false
	if data.PerInstructionTools != nil {
		data.PerInstructionTools[offset] &^= tools
		shouldDeinstrument = data.PerInstructionTools[offset] == 0
	} else {
		single := data.ActiveMonitors.Tools[EventInstruction]
		shouldDeinstrument = (single & tools) == single
	}
	if shouldDeinstrument {
		deInstrumentPerInstruction(code, data, offset)
	}
}

// CallInstrumentationInstruction fires INSTRUCTION for (code, instr)
// and returns the opcode the dispatcher should run in place of the
// INSTRUMENTED_INSTRUCTION marker. The re-entrance guard lives in the
// registered sys_trace_instruction handler, matching how the LINE path
// relies on its callback's tstate->tracing check.
//
// CPython: Python/instrumentation.c:1401 _Py_call_instrumentation_instruction
func CallInstrumentationInstruction(interp *InterpState, code *objects.Code, instr int) (compile.Opcode, error) {
	data := CoMonitoring(code)
	if data == nil || instr >= len(data.PerInstructionOpcodes) {
		return compile.NOP, nil
	}
	next := compile.Opcode(data.PerInstructionOpcodes[instr])
	tools := uint8(0)
	if data.PerInstructionTools != nil {
		tools = data.PerInstructionTools[instr]
	} else {
		tools = data.ActiveMonitors.Tools[EventInstruction]
	}
	if tools != 0 && interp != nil {
		state := MonState{Active: tools}
		if err := FireInstruction(interp, &state, code, int32(instr)); err != nil {
			return next, err
		}
		if data.PerInstructionTools != nil {
			data.PerInstructionTools[instr] = state.Active
		}
	}
	return next, nil
}
