// Bridge between the flat Sequence (codegen output) and the
// cfgBuilder graph (flowgraph optimization input). Mirrors the
// _PyCfg_FromInstructionSequence / _PyCfg_ToInstructionSequence
// glue in Python/flowgraph.c so gopy's flowgraph passes can move
// onto the graph without changing the codegen-side contract.

package compile

// cfgFromSequence folds seq into a fresh cfgBuilder. Mirrors
// _PyCfg_FromInstructionSequence: resolves the label map, marks every
// instruction that is a jump target, then walks the sequence calling
// useLabel + addOp on the builder. After the build pass, every jump
// instruction's Target is set to the *basicblock it lands on, and
// every in-region instruction's Except is set to the handler block,
// so subsequent flowgraph passes can ignore label ids entirely.
//
// When seq.AnnoCode is set (the PEP 649 annotation stash produced by
// stashAnnotationCode), its instructions are appended to g first so
// __annotate__ is defined before any body statement executes.
//
// CPython: Python/flowgraph.c:3923 _PyCfg_FromInstructionSequence
func cfgFromSequence(seq *Sequence) *cfgBuilder {
	seq.ApplyLabelMap(hasJumpTarget)
	g := newCfgBuilder()
	if seq.AnnoCode != nil {
		seq.AnnoCode.ApplyLabelMap(hasJumpTarget)
		appendSeqToGraph(g, seq.AnnoCode)
	}
	if len(seq.Instrs) > 0 {
		appendSeqToGraph(g, seq)
	}
	return g
}

// appendSeqToGraph walks one instruction sequence and appends its
// blocks to g, then resolves jump and exception-handler targets.
// Extracted from cfgFromSequence so the anno-code stash and the main
// body can be processed in two sequential passes over the same graph.
//
// CPython: Python/flowgraph.c:3923 _PyCfg_FromInstructionSequence
// (inner loop; called twice when s_annotations_code is present)
func appendSeqToGraph(g *cfgBuilder, seq *Sequence) {
	isTarget := buildTargetSet(seq)

	// Walk the sequence, calling useLabel at every target instruction.
	// Track which cfg block each original seqIdx ends up in so the
	// jump-target / handler-target rewrites below can resolve indices
	// to *basicblock pointers.
	//
	// After each jump, force the next instruction into a fresh block.
	// CPython relies on IS_TERMINATOR_OPCODE (jumps OR scope exits) in
	// cfg_builder_current_block_is_terminated, so every jump is always
	// the last instruction in its block. gopy's narrower isTerminator
	// predicate already handles scope exits; the explicit useNextBlock
	// closes the gap for jumps so passes like cfgLabelExceptionTargets,
	// which assume a jump terminates its block, see the same invariant.
	idxToBlock := make([]*basicblock, len(seq.Instrs))
	idxToInstr := make([]*cfgInstr, len(seq.Instrs))
	for i, ins := range seq.Instrs {
		if isTarget[i] {
			g.useLabel(JumpTargetLabel{id: i + 1})
		}
		g.addOp(ins.Op, ins.Oparg, ins.Loc)
		idxToBlock[i] = g.CurBlock
		idxToInstr[i] = g.CurBlock.lastInstr()
		if hasJumpTarget(ins.Op) {
			g.useNextBlock(g.newBlock())
		}
	}

	rewriteJumpTargets(g, idxToBlock)
	rewriteExceptTargets(seq, idxToBlock, idxToInstr)
}

// rewriteExceptTargets resolves each in-region instruction's
// Handler.Label (a seq index) to the basicblock containing that index,
// stamping it on cfgInstr.Except. Mirrors CPython's
// _PyCfg_FromInstructionSequence handler wiring loop.
//
// CPython: Python/flowgraph.c:3976 _PyCfg_FromInstructionSequence
// (handler wiring)
func rewriteExceptTargets(seq *Sequence, idxToBlock []*basicblock, idxToInstr []*cfgInstr) {
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Handler.Label < 0 {
			continue
		}
		idx := ins.Handler.Label
		if idx >= 0 && idx < len(idxToBlock) {
			idxToInstr[i].Except = idxToBlock[idx]
		}
	}
}

// buildTargetSet flags every seq index that is the target of a jump
// or an exception handler. After ApplyLabelMap, jump opargs hold
// direct instruction indexes, so the scan is one linear pass.
//
// CPython: Python/flowgraph.c:3935 _PyCfg_FromInstructionSequence target marking
func buildTargetSet(seq *Sequence) []bool {
	isTarget := make([]bool, len(seq.Instrs))
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if hasJumpTarget(ins.Op) {
			idx := int(ins.Oparg)
			if idx >= 0 && idx < len(seq.Instrs) {
				isTarget[idx] = true
			}
		}
		if ins.Handler.Label >= 0 && ins.Handler.Label < len(seq.Instrs) {
			isTarget[ins.Handler.Label] = true
		}
	}
	return isTarget
}

// cfgToSequence flattens g back into a fresh instruction sequence.
// Mirrors _PyCfg_ToInstructionSequence: assign every block a label,
// walk the graph in fallthrough order calling UseLabel + Addop, then
// ApplyLabelMap to resolve jump opargs back to instruction offsets.
//
// Every jump's oparg is rewritten from "stale id" to "the target
// block's freshly assigned label id" before it lands in the sequence,
// so ApplyLabelMap can resolve it. ExceptHandlerInfo on a cfg
// instruction (StartDepth + PreserveLasti carried via the Except
// block's fields) is recreated on the sequence side.
//
// CPython: Python/flowgraph.c:3988 _PyCfg_ToInstructionSequence
func cfgToSequence(g *cfgBuilder, seq *Sequence) {
	// Assign each block a fresh label id. Matches CPython's lbl++
	// starting at 0; seq.NewLabel post-increments from 0 as well.
	for b := g.EntryBlock; b != nil; b = b.Next {
		b.Label = seq.NewLabel()
	}
	for b := g.EntryBlock; b != nil; b = b.Next {
		seq.UseLabel(b.Label)
		for i := range b.Instr {
			ins := &b.Instr[i]
			oparg := ins.Oparg
			if hasJumpTarget(ins.Op) && ins.Target != nil {
				oparg = int32(ins.Target.Label.id)
			}
			seq.Addop(ins.Op, oparg, ins.Loc)
			if ins.Except != nil {
				h := &seq.Instrs[len(seq.Instrs)-1].Handler
				h.Label = ins.Except.Label.id
				h.StartDepth = ins.Except.StartDepth
				if ins.Except.PreserveLasti {
					h.PreserveLasti = 1
				} else {
					h.PreserveLasti = 0
				}
			}
		}
	}
	seq.ApplyLabelMap(hasJumpTarget)
}

// cfgOptimizedCfgToInstructionSequence is the closer that turns an
// optimized cfg into a flat instruction sequence ready for the
// assembler. After the optimizer pass returns, this routine: expands
// the pseudo conditional jumps; computes the running stack depth;
// builds the localsplus table and rewrites cell/free opargs through
// it; rewrites the remaining assembler-time pseudo ops; normalizes
// jumps; and runs optimize_load_fast as the last bytecode mutation.
// The graph is then flattened by cfgToSequence into seq.
//
// CPython: Python/flowgraph.c:4026 _PyCfg_OptimizedCfgToInstructionSequence
func cfgOptimizedCfgToInstructionSequence(g *cfgBuilder, unit *Unit, codeFlags uint32, seq *Sequence) (stackdepth, nlocalsplus int, err error) {
	cfgConvertPseudoConditionalJumps(g)

	stackdepth, err = cfgCalculateStackdepth(g)
	if err != nil {
		return 0, 0, err
	}

	nlocalsplus = cfgPrepareLocalsPlus(unit, g, codeFlags)

	cfgConvertPseudoOps(g)
	cfgNormalizeJumps(g)
	if err := optimizeLoadFast(g); err != nil {
		return 0, 0, err
	}

	cfgToSequence(g, seq)
	return stackdepth, nlocalsplus, nil
}

// rewriteJumpTargets converts every jump's oparg from
// "target seqIdx" into a *basicblock pointer. Uses the idxToBlock
// map produced during cfgFromSequence.
//
// CPython: Python/flowgraph.c:3963 _PyCfg_FromInstructionSequence
// (the oparg += offset / use_label loop)
func rewriteJumpTargets(g *cfgBuilder, idxToBlock []*basicblock) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			if hasJumpTarget(ins.Op) {
				idx := int(ins.Oparg)
				if idx >= 0 && idx < len(idxToBlock) {
					target := idxToBlock[idx]
					ins.Target = target
					if target != nil {
						ins.Oparg = int32(target.Label.id)
					}
				}
			}
		}
	}
}
