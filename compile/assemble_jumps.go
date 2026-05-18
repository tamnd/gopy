// Post-flatten jump-resolution passes. Mirrors the
// resolve_unconditional_jumps + resolve_jump_offsets section of
// Python/assemble.c. Both operate on a flat Sequence (CPython's
// instr_sequence), called by _PyAssemble_MakeCodeObject before the
// per-instruction emit loop converts opargs into the wire format.

package compile

// endSendOffset is the code-unit distance from a SEND to its matching
// END_SEND inside the `yield from` lowering. CPython names this
// END_SEND_OFFSET and uses it to bias the END_ASYNC_FOR jump back to
// the END_SEND so sys.monitoring can find the matching pair.
//
// CPython: Python/assemble.c:672 END_SEND_OFFSET
const endSendOffset = 5

// resolveUnconditionalJumps lowers the pseudo unconditional jumps
// `JUMP` and `JUMP_NO_INTERRUPT` to their real forward / backward
// counterparts based on whether the target instruction sits ahead of
// or behind the jump in the linear sequence.
//
// CPython: Python/assemble.c:749 resolve_unconditional_jumps
func resolveUnconditionalJumps(instrs *Sequence) {
	for i := 0; i < len(instrs.Instrs); i++ {
		instr := &instrs.Instrs[i]
		isForward := int(instr.Oparg) > i
		switch instr.Op {
		case JUMP:
			if isForward {
				instr.Op = JUMP_FORWARD
			} else {
				instr.Op = JUMP_BACKWARD
			}
		case JUMP_NO_INTERRUPT:
			if isForward {
				instr.Op = JUMP_FORWARD
			} else {
				instr.Op = JUMP_BACKWARD_NO_INTERRUPT
			}
		}
	}
}

// resolveJumpOffsets converts every jump oparg from an absolute
// instruction index into the relative code-unit delta the VM consumes.
// Mirrors the CPython structure: a do/while loop that re-runs once any
// jump's oparg widens past 0xFF (because the new EXTENDED_ARG prefix
// shifts every offset that follows). gopy's instr_size collapses to 1
// + extended_args; the loop usually settles in one pass.
//
// In CPython, i_target and i_offset are stored on each instruction.
// gopy does not need them outside this pass, so they live as locals.
//
// CPython: Python/assemble.c:674 resolve_jump_offsets
func resolveJumpOffsets(instrs *Sequence) {
	target := make([]int32, len(instrs.Instrs))
	offset := make([]int, len(instrs.Instrs))
	for i := range instrs.Instrs {
		ins := &instrs.Instrs[i]
		if HasTarget(ins.Op) {
			target[i] = ins.Oparg
		}
	}
	for {
		totsize := 0
		for i := range instrs.Instrs {
			ins := &instrs.Instrs[i]
			offset[i] = totsize
			totsize += instrSize(ins)
		}
		extendedArgRecompile := false
		curOffset := 0
		for i := range instrs.Instrs {
			ins := &instrs.Instrs[i]
			isize := instrSize(ins)
			// jump offsets are computed relative to the instruction
			// pointer after fetching the jump instruction.
			curOffset += isize
			if !HasTarget(ins.Op) {
				continue
			}
			tgtIdx := int(target[i])
			tgtOff := 0
			if tgtIdx >= 0 && tgtIdx < len(offset) {
				tgtOff = offset[tgtIdx]
			}
			ins.Oparg = int32(tgtOff)
			switch {
			case ins.Op == END_ASYNC_FOR:
				// sys.monitoring needs to be able to find the matching
				// END_SEND but the target is the SEND, so we adjust it
				// here.
				ins.Oparg = int32(curOffset - int(ins.Oparg) - endSendOffset)
			case int(ins.Oparg) < curOffset:
				ins.Oparg = int32(curOffset - int(ins.Oparg))
			default:
				ins.Oparg = int32(int(ins.Oparg) - curOffset)
			}
			if instrSize(ins) != isize {
				extendedArgRecompile = true
			}
		}
		if !extendedArgRecompile {
			break
		}
	}
}
