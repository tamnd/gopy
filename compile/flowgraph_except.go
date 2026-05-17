// Exception-table stamping and pseudo-op lowering for the flat
// instruction sequence. These pass over the assembled sequence after
// the CFG pipeline has run and returned a flat result.
//
// CPython: Python/flowgraph.c:3520 convert_pseudo_ops
// CPython: Python/assemble.c:132 assemble_emit_exception_table_entry

package compile

// stampHandlerStartDepths fills Handler.StartDepth on every
// in-try-region instruction by looking up the per-instruction
// startDepth at its handler index. CPython reads
// handler_block->b_startdepth directly when emitting the exception
// table; gopy threads the same depth through ExceptHandlerInfo so the
// existing assemble_exceptions writer needs no change.
//
// CPython: Python/assemble.c:132 assemble_emit_exception_table_entry
// (the `depth = handler->b_startdepth - 1` term)
func stampHandlerStartDepths(seq *Sequence, startDepth []int) {
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Handler.Label < 0 {
			continue
		}
		idx := ins.Handler.Label
		if idx < 0 || idx >= len(startDepth) {
			continue
		}
		ins.Handler.StartDepth = max(startDepth[idx], 0)
	}
}

// insertPrefixInstructions prepends RETURN_GENERATOR + POP_TOP to the
// sequence when codeFlags carries CO_GENERATOR, CO_COROUTINE, or
// CO_ASYNC_GENERATOR. The two ops have a net stack effect of 0:
// RETURN_GENERATOR pushes a value via _PyFrame_StackPush before
// switching to the generator frame, and POP_TOP drops it in the new
// frame's initial context.
//
// Because every jump oparg in the sequence is an absolute instruction
// index at this point (ApplyLabelMap has resolved labels but
// resolveJumpOffsets has not yet converted to relative deltas), and
// every Handler.Label is similarly an absolute index, the insertion
// shifts both by 2.
//
// CPython: Python/flowgraph.c:3760 insert_prefix_instructions
func insertPrefixInstructions(seq *Sequence, codeFlags uint32) {
	if codeFlags&(CoGenerator|CoCoroutine|CoAsyncGenerator) == 0 {
		return
	}
	if len(seq.Instrs) == 0 {
		return
	}
	loc := seq.Instrs[0].Loc
	prefix := make([]Instr, 2, 2+len(seq.Instrs))
	prefix[0] = Instr{Op: RETURN_GENERATOR, Oparg: 0, Loc: loc, Handler: ExceptHandlerInfo{Label: -1, StartDepth: -1, PreserveLasti: -1}}
	prefix[1] = Instr{Op: POP_TOP, Oparg: 0, Loc: loc, Handler: ExceptHandlerInfo{Label: -1, StartDepth: -1, PreserveLasti: -1}}
	seq.Instrs = append(prefix, seq.Instrs...)
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if i < len(prefix) {
			continue
		}
		if hasJumpTarget(ins.Op) {
			ins.Oparg += int32(len(prefix))
		}
		if ins.Handler.Label >= 0 {
			ins.Handler.Label += len(prefix)
		}
	}
}

// convertPseudoOps rewrites every remaining SETUP_X to NOP. POP_BLOCK
// is already a NOP after labelExceptionTargets. LOAD_CLOSURE is also
// lowered here to match CPython, although gopy's codegen already
// emits the runtime opcode in deref-index space, so the rewrite is a
// no-op until specialization adds STORE_FAST_MAYBE_NULL.
//
// CPython: Python/flowgraph.c:3520 convert_pseudo_ops
func convertPseudoOps(seq *Sequence) {
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		switch ins.Op {
		case SETUP_FINALLY, SETUP_WITH, SETUP_CLEANUP:
			ins.Op = NOP
			ins.Oparg = 0
		}
	}
}
