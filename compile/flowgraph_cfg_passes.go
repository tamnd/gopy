// Passes that operate on the *cfgBuilder graph rather than the flat
// Sequence. Each function here is a 1:1 port of the corresponding
// CPython routine in Python/flowgraph.c.
//
// These coexist with the flat-sequence passes in flowgraph_passes.go
// until spec 1715 phase 6 retires the flat shim.

package compile

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
