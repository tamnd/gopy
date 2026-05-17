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
// instruction's Target is set to the *basicblock it lands on, so
// subsequent flowgraph passes can ignore label ids entirely.
//
// CPython: Python/flowgraph.c:3923 _PyCfg_FromInstructionSequence
func cfgFromSequence(seq *Sequence) *cfgBuilder {
	seq.ApplyLabelMap(hasJumpTarget)

	g := newCfgBuilder()
	if len(seq.Instrs) == 0 {
		return g
	}

	isTarget := buildTargetSet(seq)

	// Walk the sequence, calling useLabel at every target instruction.
	// Track which cfg block each original seqIdx ends up in so the
	// jump-target rewrite below can resolve oparg -> *basicblock.
	idxToBlock := make([]*basicblock, len(seq.Instrs))
	for i, ins := range seq.Instrs {
		if isTarget[i] {
			g.useLabel(JumpTargetLabel{id: i + 1})
		}
		g.addOp(ins.Op, ins.Oparg, ins.Loc)
		idxToBlock[i] = g.CurBlock
		// Handler.Label is NOT stashed: CPython's _PyCfg_FromInstructionSequence
		// does not preserve ExceptHandlerInfo into the CFG. The exception handler
		// connections are established later by cfgLabelExceptionTargets which
		// reads the SETUP_X jump targets to reconstruct the handler chain.
		// Stashing here would corrupt non-jump instruction opargs for every
		// instruction inside a try-block.
		//
		// CPython: Python/flowgraph.c:3923 _PyCfg_FromInstructionSequence
	}

	rewriteJumpTargets(g, idxToBlock)
	return g
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
	// Assign each block a fresh label id. CPython uses lbl++ starting
	// at 0; gopy reserves id 0 as NO_LABEL, so we allocate from
	// seq.NewLabel which is 1-based.
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
