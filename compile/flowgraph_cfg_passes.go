// Passes that operate on the *cfgBuilder graph rather than the flat
// Sequence. Each function here is a 1:1 port of the corresponding
// CPython routine in Python/flowgraph.c.
//
// These coexist with the flat-sequence passes in flowgraph_passes.go
// until spec 1715 phase 6 retires the flat shim.

package compile

import "github.com/tamnd/gopy/ast"

// cfgRemoveRedundantNops drops NOPs whose location info adds nothing.
// Walks every block and calls basicblockRemoveRedundantNops.
//
// CPython: Python/flowgraph.c:1104 remove_redundant_nops
func cfgRemoveRedundantNops(g *cfgBuilder) int {
	changes := 0
	for b := g.EntryBlock; b != nil; b = b.Next {
		changes += basicblockRemoveRedundantNops(b)
	}
	return changes
}

// basicblockRemoveRedundantNops compacts b in place. A NOP is dropped
// when it carries no lineno, when its lineno matches the previous
// instruction's, when the next instruction shares its lineno (or has
// none, in which case the NOP donates its location forward), or when
// it sits last in a block whose next non-empty block opens on the
// same line. Returns the number of NOPs removed.
//
// CPython: Python/flowgraph.c:1043 basicblock_remove_redundant_nops
func basicblockRemoveRedundantNops(b *basicblock) int {
	dest := 0
	prevLineno := -1
	for src := 0; src < len(b.Instr); src++ {
		lineno := b.Instr[src].Loc.Lineno
		if b.Instr[src].Op == NOP {
			if lineno < 0 {
				continue
			}
			if prevLineno == lineno {
				continue
			}
			if src < len(b.Instr)-1 {
				nextLineno := b.Instr[src+1].Loc.Lineno
				if nextLineno == lineno {
					continue
				}
				if nextLineno < 0 {
					b.Instr[src+1].Loc = b.Instr[src].Loc
					continue
				}
			} else {
				next := nextNonemptyBlock(b.Next)
				if next != nil {
					nextLineno := nextBlockFirstLineno(next)
					if lineno == nextLineno {
						continue
					}
				}
			}
		}
		if dest != src {
			b.Instr[dest] = b.Instr[src]
		}
		dest++
		prevLineno = lineno
	}
	removed := len(b.Instr) - dest
	b.Instr = b.Instr[:dest]
	return removed
}

// cfgNormalizeJumps rewrites backward conditional jumps into a
// reversed forward conditional plus an unconditional backward jump,
// and inserts a NOT_TAKEN marker on every fall-through edge of a
// forward conditional. Walk order is fallthrough so b_visited reliably
// distinguishes "already seen / backward target" from "still ahead".
//
// CPython: Python/flowgraph.c:590 normalize_jumps
func cfgNormalizeJumps(g *cfgBuilder) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		b.Visited = false
	}
	for b := g.EntryBlock; b != nil; b = b.Next {
		b.Visited = true
		normalizeJumpsInBlock(g, b)
	}
}

// normalizeJumpsInBlock applies the rewrite to a single block.
//
// CPython: Python/flowgraph.c:536 normalize_jumps_in_block
func normalizeJumpsInBlock(g *cfgBuilder, b *basicblock) {
	last := b.lastInstr()
	if last == nil || !isConditionalJump(last.Op) {
		return
	}

	if !last.Target.Visited {
		// Forward conditional: mark the fall-through edge with NOT_TAKEN
		// so a later assembler pass can record it precisely.
		b.addOp(NOT_TAKEN, 0, last.Loc)
		return
	}

	reversed, ok := reverseConditionalJumpOp(last.Op)
	if !ok {
		return
	}

	target := last.Target
	backwards := g.newBlock()
	backwards.addOp(NOT_TAKEN, 0, last.Loc)
	backwards.addJump(JUMP, target, last.Loc)
	backwards.StartDepth = target.StartDepth
	backwards.Cold = b.Cold

	last.Op = reversed
	last.Target = b.Next

	backwards.Next = b.Next
	b.Next = backwards
}

// reverseConditionalJumpOp returns the opposite-sense conditional
// jump opcode, or (op,false) when op is not a reversible conditional.
//
// CPython: Python/flowgraph.c:551 normalize_jumps_in_block switch
func reverseConditionalJumpOp(op Opcode) (Opcode, bool) {
	switch op {
	case POP_JUMP_IF_NOT_NONE:
		return POP_JUMP_IF_NONE, true
	case POP_JUMP_IF_NONE:
		return POP_JUMP_IF_NOT_NONE, true
	case POP_JUMP_IF_FALSE:
		return POP_JUMP_IF_TRUE, true
	case POP_JUMP_IF_TRUE:
		return POP_JUMP_IF_FALSE, true
	}
	return op, false
}

// cfgRemoveRedundantJumps NOPs out unconditional jumps whose target
// is the same block control would fall through to anyway.
//
// CPython: Python/flowgraph.c:1159 remove_redundant_jumps
func cfgRemoveRedundantJumps(g *cfgBuilder) int {
	changes := 0
	for b := g.EntryBlock; b != nil; b = b.Next {
		last := b.lastInstr()
		if last == nil {
			continue
		}
		if !isUnconditionalJump(last.Op) {
			continue
		}
		jumpTarget := nextNonemptyBlock(last.Target)
		next := nextNonemptyBlock(b.Next)
		if jumpTarget == next && jumpTarget != nil {
			last.Op = NOP
			last.Oparg = 0
			last.Target = nil
			changes++
		}
	}
	return changes
}

// cfgRemoveUnreachable walks the graph from the entry block via
// fallthrough edges and jump targets, counts predecessors, then
// empties any block that ended up unreachable.
//
// CPython: Python/flowgraph.c:996 remove_unreachable
func cfgRemoveUnreachable(g *cfgBuilder) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		b.Predecessors = 0
		b.Visited = false
	}
	stack := []*basicblock{g.EntryBlock}
	g.EntryBlock.Visited = true
	g.EntryBlock.Predecessors = 1
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if b.Next != nil && !b.nofallthrough() {
			if !b.Next.Visited {
				stack = append(stack, b.Next)
				b.Next.Visited = true
			}
			b.Next.Predecessors++
		}
		for i := range b.Instr {
			ins := &b.Instr[i]
			if !hasJumpTarget(ins.Op) || ins.Target == nil {
				continue
			}
			target := ins.Target
			if !target.Visited {
				stack = append(stack, target)
				target.Visited = true
			}
			target.Predecessors++
		}
	}
	for b := g.EntryBlock; b != nil; b = b.Next {
		if b.Predecessors == 0 {
			b.Instr = b.Instr[:0]
			b.ExceptHandler = false
		}
	}
}

// cfgPropagateLineNumbers fills in missing per-instruction line
// numbers from the previous located instruction inside the same
// block. Also seeds the first instruction of any single-predecessor
// successor (fallthrough or jump target) so a NOP-less head still
// reports a sensible line.
//
// CPython: Python/flowgraph.c:3616 propagate_line_numbers
func cfgPropagateLineNumbers(g *cfgBuilder) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		if b.lastInstr() == nil {
			continue
		}
		prev := ast.Pos{Lineno: -1}
		for i := range b.Instr {
			ins := &b.Instr[i]
			if ins.Loc.Lineno < 0 {
				ins.Loc = prev
			} else {
				prev = ins.Loc
			}
		}
		if !b.nofallthrough() && b.Next != nil && b.Next.Predecessors == 1 && len(b.Next.Instr) > 0 {
			if b.Next.Instr[0].Loc.Lineno < 0 {
				b.Next.Instr[0].Loc = prev
			}
		}
		last := b.lastInstr()
		if hasJumpTarget(last.Op) && last.Target != nil {
			target := last.Target
			if target.Predecessors == 1 && len(target.Instr) > 0 && target.Instr[0].Loc.Lineno < 0 {
				target.Instr[0].Loc = prev
			}
		}
	}
}

// cfgDuplicateExitsWithoutLineno duplicates every shared scope-exit
// block that has no line numbers so each jump into it owns a private
// copy. PEP 626 requires the f_lineno of a frame to be correct after
// the frame terminates; rather than tracking that at runtime, the
// optimizer ensures each exit block has exactly one predecessor and
// inherits its line number from there.
//
// CPython: Python/flowgraph.c:3563 duplicate_exits_without_lineno
func cfgDuplicateExitsWithoutLineno(g *cfgBuilder) {
	nextLbl := getMaxLabel(g) + 1
	for b := g.EntryBlock; b != nil; b = b.Next {
		last := b.lastInstr()
		if last == nil || !hasJumpTarget(last.Op) || last.Target == nil {
			continue
		}
		target := nextNonemptyBlock(last.Target)
		if target == nil || !isExitWithoutLineno(target) || target.Predecessors <= 1 {
			continue
		}
		newTarget := g.copyBasicblock(target)
		newTarget.Instr[0].Loc = last.Loc
		last.Target = newTarget
		target.Predecessors--
		newTarget.Predecessors = 1
		newTarget.Next = target.Next
		newTarget.Label = JumpTargetLabel{id: nextLbl}
		nextLbl++
		target.Next = newTarget
	}
	for b := g.EntryBlock; b != nil; b = b.Next {
		if b.nofallthrough() || b.Next == nil || len(b.Instr) == 0 {
			continue
		}
		if isExitWithoutLineno(b.Next) {
			b.Next.Instr[0].Loc = b.lastInstr().Loc
		}
	}
}

// isExitWithoutLineno mirrors is_exit_or_eval_check_without_lineno.
// gopy has no opcodes carrying the HAS_EVAL_BREAK_FLAG yet, so the
// eval-break leg of the disjunction is always false.
//
// CPython: Python/flowgraph.c:3543 is_exit_or_eval_check_without_lineno
func isExitWithoutLineno(b *basicblock) bool {
	if !b.exitsScope() {
		return false
	}
	for i := range b.Instr {
		if b.Instr[i].Loc.Lineno >= 0 {
			return false
		}
	}
	return true
}

// getMaxLabel returns the largest label id in use across g.
//
// CPython: Python/flowgraph.c:622 get_max_label
func getMaxLabel(g *cfgBuilder) int {
	maxLbl := 0
	for b := g.EntryBlock; b != nil; b = b.Next {
		if b.Label.id > maxLbl {
			maxLbl = b.Label.id
		}
	}
	return maxLbl
}

// cfgResolveLineNumbers runs the two-step PEP 626 finishing pass:
// duplicate every shared exit-without-lineno so exits have a single
// predecessor, then propagate line numbers forward through the graph.
// firstlineno is accepted to mirror the CPython signature but the
// body itself does not consult it.
//
// CPython: Python/flowgraph.c:3650 resolve_line_numbers
func cfgResolveLineNumbers(g *cfgBuilder, _ int) {
	cfgDuplicateExitsWithoutLineno(g)
	cfgPropagateLineNumbers(g)
}

// cfgConvertPseudoOps rewrites the assembler-time pseudo opcodes
// into their real counterparts: SETUP_FINALLY/WITH/CLEANUP become
// NOPs (the exception-handler info has already been baked into the
// block by the codegen stage), LOAD_CLOSURE becomes LOAD_FAST, and
// STORE_FAST_MAYBE_NULL becomes STORE_FAST. After the rewrite,
// runs remove_redundant_nops to compact the NOPs we just produced.
//
// CPython: Python/flowgraph.c:3520 convert_pseudo_ops
func cfgConvertPseudoOps(g *cfgBuilder) int {
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			switch {
			case isBlockPushOpcode(ins.Op):
				ins.Op = NOP
				ins.Oparg = 0
				ins.Target = nil
			case ins.Op == LOAD_CLOSURE:
				ins.Op = LOAD_FAST
			case ins.Op == STORE_FAST_MAYBE_NULL:
				ins.Op = STORE_FAST
			}
		}
	}
	return cfgRemoveRedundantNops(g)
}

// nextBlockFirstLineno returns the lineno of the first
// location-bearing instruction in next, skipping NOPs that have no
// location (those will be removed too). Returns -1 if none found.
//
// CPython: inline loop in Python/flowgraph.c:1074 basicblock_remove_redundant_nops
func nextBlockFirstLineno(next *basicblock) int {
	for i := range next.Instr {
		ins := &next.Instr[i]
		if ins.Op == NOP && ins.Loc.Lineno < 0 {
			continue
		}
		return ins.Loc.Lineno
	}
	return -1
}
