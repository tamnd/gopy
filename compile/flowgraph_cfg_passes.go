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

// basicblockHasNoLineno reports whether every instruction in b lacks a
// source location (Lineno < 0). Used by the inline pass to decide
// whether a target block can be folded without losing line info.
//
// CPython: Python/flowgraph.c:1193 basicblock_has_no_lineno
func basicblockHasNoLineno(b *basicblock) bool {
	for i := range b.Instr {
		if b.Instr[i].Loc.Lineno >= 0 {
			return false
		}
	}
	return true
}

// MaxCopySize matches CPython's MAX_COPY_SIZE: a target block of at
// most this many instructions can be inlined into its predecessor.
//
// CPython: Python/flowgraph.c:1203 MAX_COPY_SIZE
const maxInlineCopySize = 4

// basicblockInlineSmallOrNoLinenoBlocks folds the target of bb's
// trailing unconditional jump into bb when the target is either a
// small exit block or a no-lineno block with no fallthrough. Returns
// true when bb was extended.
//
// CPython: Python/flowgraph.c:1210 basicblock_inline_small_or_no_lineno_blocks
func basicblockInlineSmallOrNoLinenoBlocks(bb *basicblock) bool {
	last := bb.lastInstr()
	if last == nil {
		return false
	}
	if !isUnconditionalJump(last.Op) {
		return false
	}
	target := last.Target
	if target == nil {
		return false
	}
	smallExit := target.exitsScope() && len(target.Instr) <= maxInlineCopySize
	noLinenoNoFallthrough := basicblockHasNoLineno(target) && target.nofallthrough()
	if !smallExit && !noLinenoNoFallthrough {
		return false
	}
	removedJump := last.Op
	last.Op = NOP
	last.Oparg = 0
	last.Target = nil
	bb.appendInstructions(target)
	if noLinenoNoFallthrough {
		newLast := bb.lastInstr()
		if newLast != nil && isUnconditionalJump(newLast.Op) && removedJump == JUMP {
			// Preserve eval-breaker semantics: a forward JUMP must
			// stay JUMP rather than becoming the appended JUMP_BACKWARD.
			newLast.Op = JUMP
		}
	}
	if target.Predecessors > 0 {
		target.Predecessors--
	}
	return true
}

// cfgInlineSmallOrNoLinenoBlocks iterates basicblockInlineSmallOrNo...
// across every block until no more inlining is possible. Each pass
// removes at least one jump, so the fixpoint is guaranteed.
//
// CPython: Python/flowgraph.c:1245 inline_small_or_no_lineno_blocks
func cfgInlineSmallOrNoLinenoBlocks(g *cfgBuilder) {
	for {
		changed := false
		for b := g.EntryBlock; b != nil; b = b.Next {
			if basicblockInlineSmallOrNoLinenoBlocks(b) {
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// basicblockFoldConstUnaryop folds UNARY_NEGATIVE / UNARY_INVERT /
// UNARY_NOT preceded by a LOAD_CONST(_SMALL_INT). The const loader
// becomes NOP and the unary slot becomes LOAD_CONST of the folded
// value. Within a basic block the run cannot be split by a jump
// landing in the middle, so no pinned-target gate is needed.
//
// CPython: Python/flowgraph.c:1935 fold_const_unaryop
func basicblockFoldConstUnaryop(bb *basicblock, consts *[]any) int {
	if consts == nil || len(bb.Instr) < 2 {
		return 0
	}
	folded := 0
	for i := 1; i < len(bb.Instr); i++ {
		ins := &bb.Instr[i]
		if !isFoldableUnary(ins.Op) {
			continue
		}
		operand, ok := cfgLoadsConstValue(&bb.Instr[i-1], *consts)
		if !ok {
			continue
		}
		result, ok := evalConstUnaryop(ins.Op, operand)
		if !ok {
			continue
		}
		bb.Instr[i-1].Op = NOP
		bb.Instr[i-1].Oparg = 0
		bb.Instr[i-1].Target = nil
		ins.Op = LOAD_CONST
		ins.Oparg = int32(appendConst(consts, result))
		folded++
	}
	return folded
}

// cfgLoadsConstValue is the cfgInstr-flavored loadsConstValue. Returns
// the underlying const for LOAD_CONST / LOAD_SMALL_INT, ok=false
// otherwise.
//
// CPython: Python/flowgraph.c:1389 loads_const + get_const_value
func cfgLoadsConstValue(ins *cfgInstr, consts []any) (any, bool) {
	switch ins.Op {
	case LOAD_CONST:
		idx := int(ins.Oparg)
		if idx < 0 || idx >= len(consts) {
			return nil, false
		}
		return consts[idx], true
	case LOAD_SMALL_INT:
		return int64(ins.Oparg), true
	}
	return nil, false
}

// basicblockFoldConstBinop rewrites `LOAD_CONST a; LOAD_CONST b;
// BINARY_OP op` triples where both operands are int64 constants and op
// has a defined integer result. The two loads become NOPs and the
// BINARY_OP becomes LOAD_CONST of the folded value.
//
// CPython: Python/flowgraph.c:1894 fold_const_binop
func basicblockFoldConstBinop(bb *basicblock, consts *[]any) int {
	if consts == nil {
		return 0
	}
	folded := 0
	for i := 0; i+2 < len(bb.Instr); i++ {
		a := &bb.Instr[i]
		b := &bb.Instr[i+1]
		c := &bb.Instr[i+2]
		if a.Op != LOAD_CONST || b.Op != LOAD_CONST || c.Op != BINARY_OP {
			continue
		}
		ai, bi := int(a.Oparg), int(b.Oparg)
		if ai < 0 || ai >= len(*consts) || bi < 0 || bi >= len(*consts) {
			continue
		}
		x, xok := (*consts)[ai].(int64)
		y, yok := (*consts)[bi].(int64)
		if !xok || !yok {
			continue
		}
		result, ok := evalIntBinop(c.Oparg, x, y)
		if !ok {
			continue
		}
		idx := appendConst(consts, result)
		a.Op = NOP
		a.Oparg = 0
		a.Target = nil
		b.Op = NOP
		b.Oparg = 0
		b.Target = nil
		c.Op = LOAD_CONST
		c.Oparg = int32(idx)
		folded++
		i += 2
	}
	return folded
}

// basicblockCollectConstLoaders walks n instructions starting at start
// and returns their const values when every slot is a LOAD_CONST or
// LOAD_SMALL_INT. Within a basic block no slot can be a jump target,
// so the flat-substrate pinned gate is unnecessary.
//
// CPython: Python/flowgraph.c:1430 get_const_loading_instrs
func basicblockCollectConstLoaders(bb *basicblock, consts []any, start, n int) (bool, []any) {
	values := make([]any, 0, n)
	for k := range n {
		v, ok := cfgLoadsConstValue(&bb.Instr[start+k], consts)
		if !ok {
			return false, nil
		}
		values = append(values, v)
	}
	return true, values
}

// basicblockFoldTupleOfConstants rewrites `LOAD_CONST c1; ...;
// LOAD_CONST cN; BUILD_TUPLE N` into `NOP; ...; NOP; LOAD_CONST
// (c1, ..., cN)`.
//
// CPython: Python/flowgraph.c:1454 fold_tuple_of_constants
func basicblockFoldTupleOfConstants(bb *basicblock, consts *[]any) int {
	if consts == nil || len(bb.Instr) == 0 {
		return 0
	}
	folded := 0
	for i := range bb.Instr {
		ins := &bb.Instr[i]
		if ins.Op != BUILD_TUPLE {
			continue
		}
		n := int(ins.Oparg)
		if n <= 0 || i < n {
			continue
		}
		start := i - n
		ok, values := basicblockCollectConstLoaders(bb, *consts, start, n)
		if !ok {
			continue
		}
		tuple := &ConstTuple{Values: append([]any(nil), values...)}
		for k := start; k < i; k++ {
			bb.Instr[k].Op = NOP
			bb.Instr[k].Oparg = 0
			bb.Instr[k].Target = nil
		}
		idx := appendConst(consts, tuple)
		ins.Op = LOAD_CONST
		ins.Oparg = int32(idx)
		folded++
	}
	return folded
}

// basicblockOptimizeListsAndSets folds `LOAD_CONST c1; ...; LOAD_CONST
// cN; BUILD_LIST N` (and BUILD_SET) into a const-tuple plus
// LIST_EXTEND/SET_UPDATE prelude.
//
// CPython: Python/flowgraph.c:1597 optimize_lists_and_sets
func basicblockOptimizeListsAndSets(bb *basicblock, consts *[]any) int {
	if consts == nil || len(bb.Instr) < 3 {
		return 0
	}
	folded := 0
	for i := range bb.Instr {
		ins := &bb.Instr[i]
		if ins.Op != BUILD_LIST && ins.Op != BUILD_SET {
			continue
		}
		n := int(ins.Oparg)
		if n < minConstSequenceSize || i < n {
			continue
		}
		start := i - n
		ok, values := basicblockCollectConstLoaders(bb, *consts, start, n)
		if !ok {
			continue
		}
		tuple := &ConstTuple{Values: append([]any(nil), values...)}
		idx := appendConst(consts, tuple)
		for k := start; k < i-2; k++ {
			bb.Instr[k].Op = NOP
			bb.Instr[k].Oparg = 0
			bb.Instr[k].Target = nil
		}
		preludeOp := ins.Op
		bb.Instr[i-2].Op = preludeOp
		bb.Instr[i-2].Oparg = 0
		bb.Instr[i-2].Loc = ins.Loc
		bb.Instr[i-1].Op = LOAD_CONST
		bb.Instr[i-1].Oparg = int32(idx)
		bb.Instr[i-1].Loc = ins.Loc
		if preludeOp == BUILD_LIST {
			ins.Op = LIST_EXTEND
		} else {
			ins.Op = SET_UPDATE
		}
		ins.Oparg = 1
		folded++
	}
	return folded
}

// basicblockSwaptimize collapses a run of SWAP/NOP instructions starting
// at *ix into the optimal SWAP sequence realizing the same permutation.
// Returns the count of opcodes turned into NOPs and advances *ix past
// the run. Within a basic block no instruction can be a jump target, so
// the flat-substrate "pinned" set is unnecessary here.
//
// CPython: Python/flowgraph.c:1982 swaptimize
func basicblockSwaptimize(bb *basicblock, ix *int) int {
	if *ix >= len(bb.Instr) || bb.Instr[*ix].Op != SWAP {
		return 0
	}
	instr := bb.Instr[*ix:]
	depth := int(instr[0].Oparg)
	length := 1
	more := false
	for length < len(instr) {
		op := instr[length].Op
		if op == SWAP {
			if d := int(instr[length].Oparg); d > depth {
				depth = d
			}
			more = true
		} else if op != NOP {
			break
		}
		length++
	}
	if !more {
		return 0
	}
	stack := make([]int, depth)
	for i := range stack {
		stack[i] = i
	}
	for k := range length {
		if instr[k].Op != SWAP {
			continue
		}
		oparg := int(instr[k].Oparg)
		stack[0], stack[oparg-1] = stack[oparg-1], stack[0]
	}
	current := basicblockEmitSwapCycles(instr[:length], stack, length-1)
	rewrites := basicblockNopOutRemaining(instr[:length], current)
	*ix += length - 1
	return rewrites
}

// basicblockEmitSwapCycles fills the swap-run instructions, starting at
// index `current` and working backward, with the SWAP opcodes that
// realize the cycle decomposition of stack[]. Returns the next free
// slot index (may be -1).
//
// CPython: Python/flowgraph.c:2036 swaptimize cycle loop
func basicblockEmitSwapCycles(run []cfgInstr, stack []int, current int) int {
	const visited = -1
	for k := range stack {
		if stack[k] == visited || stack[k] == k {
			continue
		}
		j := k
		for {
			if j != 0 {
				run[current].Op = SWAP
				run[current].Oparg = int32(j + 1)
				current--
			}
			if stack[j] == visited {
				break
			}
			nextJ := stack[j]
			stack[j] = visited
			j = nextJ
		}
	}
	return current
}

// basicblockNopOutRemaining NOPs the prefix of run[:current+1] left
// unused by basicblockEmitSwapCycles. Returns the number of opcodes
// newly turned into NOPs.
//
// CPython: Python/flowgraph.c:2069 swaptimize NOP-out tail
func basicblockNopOutRemaining(run []cfgInstr, current int) int {
	rewrites := 0
	for current >= 0 {
		if run[current].Op != NOP {
			run[current].Op = NOP
			run[current].Oparg = 0
			run[current].Target = nil
			rewrites++
		}
		current--
	}
	return rewrites
}

// isSwappableOpcode mirrors the SWAPPABLE macro: opcodes safe to slide
// a SWAP past.
//
// CPython: Python/flowgraph.c:2082 SWAPPABLE
func isSwappableOpcode(op Opcode) bool {
	return op == STORE_FAST || op == STORE_FAST_MAYBE_NULL || op == POP_TOP
}

// cfgInstrStoresTo returns the local index a STORE_FAST(_MAYBE_NULL)
// writes, or -1.
//
// CPython: Python/flowgraph.c:2087 STORES_TO
func cfgInstrStoresTo(ins *cfgInstr) int32 {
	if ins.Op == STORE_FAST || ins.Op == STORE_FAST_MAYBE_NULL {
		return ins.Oparg
	}
	return -1
}

// nextSwappableInstructionInBlock returns the index of the next
// swappable instruction past i in the same line group within bb, or -1.
//
// CPython: Python/flowgraph.c:2093 next_swappable_instruction
func nextSwappableInstructionInBlock(bb *basicblock, i, lineno int) int {
	for k := i + 1; k < len(bb.Instr); k++ {
		ins := &bb.Instr[k]
		if lineno >= 0 && ins.Loc.Lineno != lineno {
			return -1
		}
		if ins.Op == NOP {
			continue
		}
		if isSwappableOpcode(ins.Op) {
			return k
		}
		return -1
	}
	return -1
}

// basicblockApplyStaticSwaps walks back from index i, turning each SWAP
// it crosses into a direct reorder of the following swappable
// instructions. SWAP(2), POP_TOP, STORE_FAST(42) becomes NOP,
// STORE_FAST(42), POP_TOP.
//
// CPython: Python/flowgraph.c:2117 apply_static_swaps
func basicblockApplyStaticSwaps(bb *basicblock, i int) {
	for ; i >= 0; i-- {
		swap := &bb.Instr[i]
		if swap.Op != SWAP {
			if swap.Op == NOP || isSwappableOpcode(swap.Op) {
				continue
			}
			return
		}
		j := nextSwappableInstructionInBlock(bb, i, -1)
		if j < 0 {
			return
		}
		k := j
		lineno := bb.Instr[j].Loc.Lineno
		for count := int(swap.Oparg) - 1; count > 0; count-- {
			k = nextSwappableInstructionInBlock(bb, k, lineno)
			if k < 0 {
				return
			}
		}
		if !basicblockStaticSwapSafe(bb.Instr, j, k) {
			return
		}
		swap.Op = NOP
		swap.Oparg = 0
		swap.Target = nil
		bb.Instr[j], bb.Instr[k] = bb.Instr[k], bb.Instr[j]
	}
}

// basicblockStaticSwapSafe enforces the STORE_FAST aliasing constraint:
// the two endpoints must not write the same local, and no intervening
// store may write to either endpoint's local.
//
// CPython: Python/flowgraph.c:2144 apply_static_swaps store-aliasing block
func basicblockStaticSwapSafe(instrs []cfgInstr, j, k int) bool {
	storeJ := cfgInstrStoresTo(&instrs[j])
	storeK := cfgInstrStoresTo(&instrs[k])
	if storeJ < 0 && storeK < 0 {
		return true
	}
	if storeJ == storeK {
		return false
	}
	for idx := j + 1; idx < k; idx++ {
		storeIdx := cfgInstrStoresTo(&instrs[idx])
		if storeIdx >= 0 && (storeIdx == storeJ || storeIdx == storeK) {
			return false
		}
	}
	return true
}

// cfgJumpThread rewrites a trailing jump in bb whose target is itself
// a jump, so bb jumps directly to the second target with opcode op.
// Returns true when the rewrite happened. bpo-45773: if both jumps
// already share a destination, the rewrite is skipped to avoid an
// infinite loop.
//
// CPython: Python/flowgraph.c:1283 jump_thread
func cfgJumpThread(bb *basicblock, inst *cfgInstr, target *cfgInstr, op Opcode) bool {
	if inst.Target == target.Target {
		return false
	}
	loc := target.Loc
	newTarget := target.Target
	inst.Op = NOP
	inst.Oparg = 0
	inst.Target = nil
	bb.addJump(op, newTarget, loc)
	return true
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
