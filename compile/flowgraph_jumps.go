// Jump-related optimisation passes that run after ApplyLabelMap has
// resolved every jump oparg to an absolute instruction index. These
// passes operate on the flat sequence; the CFG-shaped variants in
// CPython produce the same results, and the linear forms are simpler
// when the label table is already baked.
//
// CPython: Python/flowgraph.c jump_thread / propagate_conditional /
//          remove_unreachable

package compile

// threadJumps follows chains of unconditional jumps. After this pass,
// every jump oparg points at a non-jump instruction (or, in the loop
// case, at a back-edge target that does not itself unconditionally
// branch elsewhere).
//
// CPython: Python/flowgraph.c:L998 thread_unconditional_jumps
func threadJumps(seq *Sequence) int {
	rewritten := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if !isUnconditionalJump(ins.Op) {
			continue
		}
		final := chaseJumpTarget(seq, int(ins.Oparg), i)
		if final != int(ins.Oparg) {
			ins.Oparg = int32(final)
			rewritten++
		}
	}
	return rewritten
}

// propagateConditionalJumps re-targets `POP_JUMP_IF_X label` when
// `label` itself is an unconditional jump, skipping over the trampoline
// in one hop. The rewrite is only safe when the final landing site is
// still ahead of the conditional jump. POP_JUMP_IF_* opcodes encode a
// forward-only offset (see resolve_jump_offsets' IS_BACKWARDS_JUMP_OPCODE
// assertion in CPython's assembler); chasing through a JUMP_BACKWARD
// trampoline would otherwise leave a forward conditional pointing at
// an earlier index, which the offset resolver then encodes as a bogus
// positive delta past end-of-code. The pattern shows up whenever an
// `if` is the last statement of a `for` body: the if's end label sits
// on top of the loop back-edge, and without this guard the conditional
// gets threaded straight onto the FOR_ITER.
//
// CPython sidesteps this by inverting the conditional and inserting a
// JUMP_BACKWARD trampoline; gopy does not do that rewrite yet, so we
// just leave the conditional pointing at its original (forward) label.
//
// CPython: Python/flowgraph.c:L1051 propagate_conditional_branches
func propagateConditionalJumps(seq *Sequence) int {
	rewritten := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if !isConditionalJump(ins.Op) {
			continue
		}
		final := chaseJumpTarget(seq, int(ins.Oparg), i)
		if final == int(ins.Oparg) {
			continue
		}
		if final <= i {
			continue
		}
		ins.Oparg = int32(final)
		rewritten++
	}
	return rewritten
}

// chaseJumpTarget walks past any consecutive unconditional jumps
// starting at idx, returning the final landing index. The visited set
// guards against degenerate cycles (a jump to itself or a tight loop
// of jumps).
//
// CPython: Python/flowgraph.c jump_thread_helper
func chaseJumpTarget(seq *Sequence, idx, origin int) int {
	visited := map[int]bool{origin: true}
	cur := idx
	for {
		if cur < 0 || cur >= len(seq.Instrs) {
			return idx
		}
		// Skip over NOPs while threading; they're inert and the NOP
		// compaction pass drops them later. If the run pushes us past
		// the end, leave the oparg alone, since there's nothing to retarget
		// to.
		for cur < len(seq.Instrs) && seq.Instrs[cur].Op == NOP {
			cur++
		}
		if cur >= len(seq.Instrs) {
			return idx
		}
		if !isUnconditionalJump(seq.Instrs[cur].Op) {
			return cur
		}
		if visited[cur] {
			return cur
		}
		visited[cur] = true
		cur = int(seq.Instrs[cur].Oparg)
	}
}

// isUnconditionalJump reports whether op is one of the unconditional
// jump opcodes (real or pseudo). The pseudo-op `JUMP` is treated as
// unconditional too because pseudo-op lowering still hands it to the
// jump panel.
//
// CPython: Python/flowgraph.c IS_UNCONDITIONAL_JUMP_OPCODE
func isUnconditionalJump(op Opcode) bool {
	switch op {
	case JUMP, JUMP_NO_INTERRUPT, JUMP_FORWARD, JUMP_BACKWARD, JUMP_BACKWARD_NO_INTERRUPT:
		return true
	}
	return false
}

// isBackwardsJump matches IS_BACKWARDS_JUMP_OPCODE.
//
// CPython: Include/internal/pycore_opcode_utils.h:35 IS_BACKWARDS_JUMP_OPCODE
func isBackwardsJump(op Opcode) bool {
	switch op {
	case JUMP_BACKWARD, JUMP_BACKWARD_NO_INTERRUPT:
		return true
	}
	return false
}

// isConditionalJump reports whether op is one of the POP_JUMP_IF_*
// family.
//
// CPython: Python/flowgraph.c IS_CONDITIONAL_JUMP_OPCODE
func isConditionalJump(op Opcode) bool {
	switch op {
	case POP_JUMP_IF_FALSE, POP_JUMP_IF_TRUE,
		POP_JUMP_IF_NONE, POP_JUMP_IF_NOT_NONE:
		return true
	}
	return false
}

// removeUnreachableBlocks sweeps the flat sequence by following
// successor edges from index 0 and NOPs out every instruction that no
// reachable path lands on. The pass is length-preserving so the label
// map and jump opargs stay valid; the next NOP-compaction pass deletes
// the dead slots.
//
// CPython: Python/flowgraph.c:L1453 remove_unreachable (CFG variant)
func removeUnreachableBlocks(seq *Sequence) int {
	if len(seq.Instrs) == 0 {
		return 0
	}
	reachable := make([]bool, len(seq.Instrs))
	// Pin handler targets as roots; the runtime can land on them via
	// an exception edge that the linear walk does not see.
	roots := []int{0}
	for i, ins := range seq.Instrs {
		if ins.Handler.Label >= 0 && ins.Handler.Label < len(seq.Instrs) {
			roots = append(roots, ins.Handler.Label)
		}
		_ = i
	}
	for _, r := range roots {
		walkReachable(seq, r, reachable)
	}
	dropped := 0
	for i := range seq.Instrs {
		if reachable[i] || seq.Instrs[i].Op == NOP {
			continue
		}
		seq.Instrs[i].Op = NOP
		seq.Instrs[i].Oparg = 0
		dropped++
	}
	return dropped
}

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

// instrSize matches CPython's instr_size: 1 code unit for the opcode,
// plus one extended-arg prefix per non-zero high byte of the oparg.
// gopy does not yet attach inline caches, so the `caches` term is
// always zero.
//
// CPython: Python/assemble.c:38 instr_size
func instrSize(op Opcode, oparg int32) int {
	_ = op
	arg := uint32(oparg)
	extended := 0
	if 0xFFFFFF < arg {
		extended++
	}
	if 0xFFFF < arg {
		extended++
	}
	if 0xFF < arg {
		extended++
	}
	return extended + 1
}

// endSendOffset is the code-unit distance from a SEND to its matching
// END_SEND inside the `yield from` lowering. CPython names this
// END_SEND_OFFSET and uses it to bias the END_ASYNC_FOR jump back to
// the END_SEND so sys.monitoring can find the matching pair.
//
// CPython: Python/assemble.c:672 END_SEND_OFFSET
const endSendOffset = 5

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
			totsize += instrSize(ins.Op, ins.Oparg)
		}
		extendedArgRecompile := false
		curOffset := 0
		for i := range instrs.Instrs {
			ins := &instrs.Instrs[i]
			isize := instrSize(ins.Op, ins.Oparg)
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
				// IS_BACKWARDS_JUMP_OPCODE assertion in CPython.
				_ = isBackwardsJump
				ins.Oparg = int32(curOffset - int(ins.Oparg))
			default:
				ins.Oparg = int32(int(ins.Oparg) - curOffset)
			}
			if instrSize(ins.Op, ins.Oparg) != isize {
				extendedArgRecompile = true
			}
		}
		if !extendedArgRecompile {
			break
		}
	}
}

// walkReachable does a DFS from start, marking each reachable index
// in marks. Successors are: fall-through (i+1) for non-terminators
// and non-unconditional-jumps; the jump target for any HasTarget op;
// nothing for terminators (RETURN_VALUE / RAISE_VARARGS / RERAISE /
// INTERPRETER_EXIT).
//
// CPython: Python/flowgraph.c mark_reachable
func walkReachable(seq *Sequence, start int, marks []bool) {
	stack := []int{start}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if i < 0 || i >= len(seq.Instrs) || marks[i] {
			continue
		}
		marks[i] = true
		ins := seq.Instrs[i]
		if HasTarget(ins.Op) {
			stack = append(stack, int(ins.Oparg))
		}
		if isTerminator(ins.Op) {
			continue
		}
		if isUnconditionalJump(ins.Op) {
			// no fall-through
			continue
		}
		stack = append(stack, i+1)
	}
}
