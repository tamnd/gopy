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

// getMaxLabel returns the largest label id in use across g, or -1 if
// no block is labeled. Callers use the return value + 1 as the next
// fresh label id (matches CPython's get_max_label, which starts at -1).
//
// CPython: Python/flowgraph.c:622 get_max_label
func getMaxLabel(g *cfgBuilder) int {
	maxLbl := -1
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

// cfgConvertPseudoConditionalJumps rewrites the codegen-time pseudo
// conditional jumps (JUMP_IF_FALSE / JUMP_IF_TRUE) into the real
// POP_JUMP_IF_* sequence preceded by COPY 1 + TO_BOOL 0. The pseudo
// op kept the condition value on the stack so optimize_cfg could see
// it; here we materialise the duplicate-and-coerce-to-bool dance the
// VM actually wants. The pseudo jump is always the last instruction
// in its block (codegen invariant), and the inserted helpers inherit
// its location and exception-handler pointer.
//
// CPython: Python/flowgraph.c:3485 convert_pseudo_conditional_jumps
func cfgConvertPseudoConditionalJumps(g *cfgBuilder) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := 0; i < len(b.Instr); i++ {
			op := b.Instr[i].Op
			if op != JUMP_IF_FALSE && op != JUMP_IF_TRUE {
				continue
			}
			if i != len(b.Instr)-1 {
				panic("convert_pseudo_conditional_jumps: pseudo jump must be last in block")
			}
			if op == JUMP_IF_FALSE {
				b.Instr[i].Op = POP_JUMP_IF_FALSE
			} else {
				b.Instr[i].Op = POP_JUMP_IF_TRUE
			}
			loc := b.Instr[i].Loc
			except := b.Instr[i].Except
			b.insertInstruction(i, cfgInstr{Op: COPY, Oparg: 1, Loc: loc, Except: except})
			i++
			b.insertInstruction(i, cfgInstr{Op: TO_BOOL, Oparg: 0, Loc: loc, Except: except})
			i++
		}
	}
}

// cfgBuildCellFixedOffsets returns the localsplus offset for each cell
// or free var. The slot order is [varnames | cellvars | freevars]; the
// returned slice maps cell/free index (0..ncellvars+nfreevars-1) to the
// final localsplus offset. The default offset is nlocals + i, but cell
// vars whose name also appears in varnames are arg cells: they reuse
// the arg's varname slot, and the localsplus shrinks by the matching
// duplicate count after fix_cell_offsets compacts the table.
//
// CPython: Python/flowgraph.c:3711 build_cellfixedoffsets
func cfgBuildCellFixedOffsets(unit *Unit) []int {
	nlocals := len(unit.VarNames)
	ncellvars := len(unit.CellVars)
	nfreevars := len(unit.FreeVars)
	noffsets := ncellvars + nfreevars
	fixed := make([]int, noffsets)
	for i := range noffsets {
		fixed[i] = nlocals + i
	}
	varIdx := make(map[string]int, len(unit.VarNames))
	for i, name := range unit.VarNames {
		varIdx[name] = i
	}
	for cellPos, name := range unit.CellVars {
		if argOffset, ok := varIdx[name]; ok {
			fixed[cellPos] = argOffset
		}
	}
	return fixed
}

// cfgInsertPrefixInstructions prepends the per-scope prologue to entry.
// For generator / coroutine scopes: RETURN_GENERATOR + POP_TOP at
// positions 0,1 with location LOCATION(firstlineno, firstlineno, -1, -1).
// For ncellvars > 0: MAKE_CELL instructions, sorted by their fixed
// localsplus offset, inserted at positions 0..ncellvars-1 with
// NO_LOCATION. For nfreevars > 0: COPY_FREE_VARS nfreevars at position
// 0 with NO_LOCATION.
//
// The MAKE_CELL sort matches CPython: cells are first listed in
// localsplus order via the fixed map, then visited at slot positions
// 0..nvars-1, only counting slots that are actually cells (sorted[i]-1
// >= 0). The result is arg cells in varname-declaration order followed
// by non-arg cells in their CellVars order.
//
// CPython: Python/flowgraph.c:3760 insert_prefix_instructions
func cfgInsertPrefixInstructions(unit *Unit, entry *basicblock, fixed []int, nfreevars int, codeFlags uint32) {
	if codeFlags&(CoGenerator|CoCoroutine|CoAsyncGenerator) != 0 {
		loc := ast.Pos{Lineno: unit.FirstLineno, EndLineno: unit.FirstLineno, ColOffset: -1, EndColOffset: -1}
		entry.insertInstruction(0, cfgInstr{Op: RETURN_GENERATOR, Oparg: 0, Loc: loc})
		entry.insertInstruction(1, cfgInstr{Op: POP_TOP, Oparg: 0, Loc: loc})
	}
	ncellvars := len(unit.CellVars)
	if ncellvars > 0 {
		nvars := ncellvars + len(unit.VarNames)
		sorted := make([]int, nvars)
		for i := range ncellvars {
			sorted[fixed[i]] = i + 1
		}
		noLoc := ast.Pos{Lineno: -1, EndLineno: -1, ColOffset: -1, EndColOffset: -1}
		ncellsused := 0
		for i := 0; ncellsused < ncellvars; i++ {
			oldindex := sorted[i] - 1
			if oldindex == -1 {
				continue
			}
			entry.insertInstruction(ncellsused, cfgInstr{Op: MAKE_CELL, Oparg: int32(oldindex), Loc: noLoc})
			ncellsused++
		}
	}
	if nfreevars > 0 {
		noLoc := ast.Pos{Lineno: -1, EndLineno: -1, ColOffset: -1, EndColOffset: -1}
		entry.insertInstruction(0, cfgInstr{Op: COPY_FREE_VARS, Oparg: int32(nfreevars), Loc: noLoc})
	}
}

// cfgFixCellOffsets rewrites every deref-style oparg from "cell/free
// table index" to "final localsplus offset" and reports the number of
// arg-cell duplicates dropped (cells whose slot was reassigned to an
// arg slot). The first pass walks the fixed map: a slot is a normal
// (post-locals) cell iff fixedmap[i] == i + nlocals; otherwise it was
// an arg cell whose offset was rewritten to the arg's varname index.
// Normal cells shift down by the running duplicate count so the
// surviving slots stay packed.
//
// CPython: Python/flowgraph.c:3843 fix_cell_offsets
func cfgFixCellOffsets(unit *Unit, entry *basicblock, fixedmap []int) int {
	nlocals := len(unit.VarNames)
	ncellvars := len(unit.CellVars)
	nfreevars := len(unit.FreeVars)
	noffsets := ncellvars + nfreevars

	numdropped := 0
	for i := range noffsets {
		if fixedmap[i] == i+nlocals {
			fixedmap[i] -= numdropped
		} else {
			numdropped++
		}
	}

	for b := entry; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			oldoffset := int(ins.Oparg)
			switch ins.Op {
			case MAKE_CELL, LOAD_CLOSURE, LOAD_DEREF, STORE_DEREF, DELETE_DEREF, LOAD_FROM_DICT_OR_DEREF:
				ins.Oparg = int32(fixedmap[oldoffset])
			}
		}
	}
	return numdropped
}

// cfgPrepareLocalsPlus runs the three-step localsplus pipeline that
// CPython's optimize_and_assemble_code_unit calls after the graph has
// been fully optimized: build the cell/free offset table, insert the
// scope prologue (RETURN_GENERATOR + MAKE_CELL + COPY_FREE_VARS), then
// rewrite every deref oparg through the table. Returns the localsplus
// table size, which is nlocals + ncellvars + nfreevars minus the
// arg-cell duplicates dropped by fix_cell_offsets.
//
// CPython: Python/flowgraph.c:3888 prepare_localsplus
func cfgPrepareLocalsPlus(unit *Unit, g *cfgBuilder, codeFlags uint32) int {
	nlocals := len(unit.VarNames)
	ncellvars := len(unit.CellVars)
	nfreevars := len(unit.FreeVars)
	nlocalsplus := nlocals + ncellvars + nfreevars
	fixed := cfgBuildCellFixedOffsets(unit)
	cfgInsertPrefixInstructions(unit, g.EntryBlock, fixed, nfreevars, codeFlags)
	numdropped := cfgFixCellOffsets(unit, g.EntryBlock, fixed)
	nlocalsplus -= numdropped
	return nlocalsplus
}

// cfgCheckCfg verifies that every terminator opcode sits at the end of
// its block. Returns a non-nil error (via panic-free
// boolean) when violated; the codegen invariants make this a debug
// check in practice.
//
// CPython: Python/flowgraph.c:604 check_cfg
func cfgCheckCfg(g *cfgBuilder) error {
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := range b.Instr {
			if isTerminator(b.Instr[i].Op) && i != len(b.Instr)-1 {
				return errMalformedCFG
			}
		}
	}
	return nil
}

// errMalformedCFG mirrors CPython's "malformed control flow graph"
// SystemError raised by check_cfg.
var errMalformedCFG = cfgError("malformed control flow graph")

type cfgError string

func (e cfgError) Error() string { return string(e) }

// cfgTranslateJumpLabelsToTargets resolves every jump instruction's
// oparg label id into a *basicblock Target pointer. gopy's bridge
// already wires Target during cfgFromSequence, so this is a no-op
// when Target is already set; idempotency keeps it safe to call
// from _PyCfg_OptimizeCodeUnit's prelude.
//
// CPython: Python/flowgraph.c:635 translate_jump_labels_to_targets
func cfgTranslateJumpLabelsToTargets(g *cfgBuilder) {
	maxLbl := getMaxLabel(g)
	label2block := make([]*basicblock, maxLbl+1)
	for b := g.EntryBlock; b != nil; b = b.Next {
		if b.Label.IsValid() {
			label2block[b.Label.id] = b
		}
	}
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			if !hasJumpTarget(ins.Op) {
				continue
			}
			if ins.Target != nil {
				continue
			}
			lbl := int(ins.Oparg)
			if lbl >= 0 && lbl <= maxLbl {
				ins.Target = label2block[lbl]
			}
		}
	}
}

// cfgMarkExceptHandlers stamps ExceptHandler=true on every block that
// is the target of a SETUP_FINALLY / SETUP_WITH / SETUP_CLEANUP push.
//
// CPython: Python/flowgraph.c:668 mark_except_handlers
func cfgMarkExceptHandlers(g *cfgBuilder) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			if isBlockPushOpcode(ins.Op) && ins.Target != nil {
				ins.Target.ExceptHandler = true
			}
		}
	}
}

// coMaxBlocks bounds the static nesting depth of try / with regions
// inside a single function.
//
// CPython: Include/cpython/code.h:160 CO_MAXBLOCKS
const coMaxBlocks = 21

// resumeOpargDepth1Mask is the bit RESUME sets in its oparg when the
// surrounding YIELD ran with an exception stack of depth 1.
//
// CPython: Include/internal/pycore_opcode_utils.h:85 RESUME_OPARG_DEPTH1_MASK
const resumeOpargDepth1Mask int32 = 0x4

// cfgExceptStack is a transient handler stack used while
// labelExceptionTargets walks the graph. Each entry is the handler
// block of an open SETUP_X push.
//
// CPython: Python/flowgraph.c:686 _PyCfgExceptStack
type cfgExceptStack struct {
	handlers [coMaxBlocks + 2]*basicblock
	depth    int
}

// pushExceptBlock pushes setup's target onto stack. Marks the target
// PreserveLasti when setup is SETUP_WITH or SETUP_CLEANUP.
//
// CPython: Python/flowgraph.c:692 push_except_block
func pushExceptBlock(stack *cfgExceptStack, setup *cfgInstr) *basicblock {
	target := setup.Target
	if setup.Op == SETUP_WITH || setup.Op == SETUP_CLEANUP {
		target.PreserveLasti = true
	}
	stack.depth++
	stack.handlers[stack.depth] = target
	return target
}

// popExceptBlock pops the topmost handler. Returns the new top.
//
// CPython: Python/flowgraph.c:705 pop_except_block
func popExceptBlock(stack *cfgExceptStack) *basicblock {
	stack.depth--
	return stack.handlers[stack.depth]
}

// exceptStackTop returns the topmost handler, or nil when the stack
// is empty.
//
// CPython: Python/flowgraph.c:711 except_stack_top
func exceptStackTop(stack *cfgExceptStack) *basicblock {
	return stack.handlers[stack.depth]
}

// makeExceptStack allocates an empty stack with depth 0 and a nil
// handler at the bottom.
//
// CPython: Python/flowgraph.c:716 make_except_stack
func makeExceptStack() *cfgExceptStack {
	return &cfgExceptStack{}
}

// copyExceptStack returns a deep copy of stack. Used at branch points
// where the two successors must each own their handler chain.
//
// CPython: Python/flowgraph.c:728 copy_except_stack
func copyExceptStack(stack *cfgExceptStack) *cfgExceptStack {
	out := *stack
	return &out
}

// basicblockHasFallthrough is the inverse of nofallthrough. Mirrors
// the BB_HAS_FALLTHROUGH macro.
//
// CPython: Python/flowgraph.c:247 BB_HAS_FALLTHROUGH
func basicblockHasFallthrough(b *basicblock) bool {
	return !b.nofallthrough()
}

// cfgLabelExceptionTargets propagates the exception-handler stack
// through the graph and stamps Except on every instruction inside an
// open region. POP_BLOCK is rewritten to NOP here, matching CPython.
//
// CPython: Python/flowgraph.c:886 label_exception_targets
//
// switch hurts the 1:1 mapping with flowgraph.c:886.
//
//nolint:gocognit // direct port of the CPython algorithm; flattening the
func cfgLabelExceptionTargets(entry *basicblock) {
	for b := entry; b != nil; b = b.Next {
		b.Visited = false
	}
	exceptStack := makeExceptStack()
	entry.Visited = true
	entry.ExceptStack = exceptStack
	todo := []*basicblock{entry}
	for len(todo) > 0 {
		b := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		exceptStack = b.ExceptStack
		b.ExceptStack = nil
		handler := exceptStackTop(exceptStack)
		lastYieldExceptDepth := -1
		for i := range b.Instr {
			ins := &b.Instr[i]
			switch {
			case isBlockPushOpcode(ins.Op):
				if ins.Target != nil && !ins.Target.Visited {
					ins.Target.ExceptStack = copyExceptStack(exceptStack)
					ins.Target.Visited = true
					todo = append(todo, ins.Target)
				}
				handler = pushExceptBlock(exceptStack, ins)
			case ins.Op == POP_BLOCK:
				handler = popExceptBlock(exceptStack)
				ins.Op = NOP
				ins.Oparg = 0
			case isJumpOpcode(ins.Op):
				ins.Except = handler
				if ins.Target != nil && !ins.Target.Visited {
					if basicblockHasFallthrough(b) {
						ins.Target.ExceptStack = copyExceptStack(exceptStack)
					} else {
						ins.Target.ExceptStack = exceptStack
						exceptStack = nil
					}
					ins.Target.Visited = true
					todo = append(todo, ins.Target)
				}
			case ins.Op == YIELD_VALUE:
				ins.Except = handler
				lastYieldExceptDepth = exceptStack.depth
			case ins.Op == RESUME:
				ins.Except = handler
				if ins.Oparg != resumeAtFuncStart {
					if lastYieldExceptDepth == 1 {
						ins.Oparg |= resumeOpargDepth1Mask
					}
					lastYieldExceptDepth = -1
				}
			default:
				ins.Except = handler
			}
		}
		if basicblockHasFallthrough(b) && b.Next != nil && !b.Next.Visited {
			b.Next.ExceptStack = exceptStack
			b.Next.Visited = true
			todo = append(todo, b.Next)
		}
	}
}

// cfgRemoveRedundantNopsAndPairs rewrites every LOAD_CONST/POP_TOP,
// LOAD_SMALL_INT/POP_TOP, and COPY 1/POP_TOP pair into two NOPs, then
// re-runs basicblock_remove_redundant_nops to drop the freshly produced
// NOPs. Loops until no rewrite fires. The walk forgets prev_instr when
// it crosses a labeled block (jump target) or a block that does not
// fall through, since the previous instruction is then not necessarily
// the dynamic predecessor.
//
// CPython: Python/flowgraph.c:1114 remove_redundant_nops_and_pairs
//
//nolint:gocognit // 1:1 port of the CPython loop structure.
func cfgRemoveRedundantNopsAndPairs(entry *basicblock) {
	for done := false; !done; {
		done = true
		var prev, cur *cfgInstr
		for b := entry; b != nil; b = b.Next {
			basicblockRemoveRedundantNops(b)
			if b.Label.IsValid() {
				cur = nil
			}
			for i := range b.Instr {
				prev = cur
				cur = &b.Instr[i]
				prevOp := Opcode(0)
				var prevArg int32
				if prev != nil {
					prevOp = prev.Op
					prevArg = prev.Oparg
				}
				if cur.Op != POP_TOP {
					continue
				}
				redundant := false
				switch {
				case prevOp == LOAD_CONST || prevOp == LOAD_SMALL_INT:
					redundant = true
				case prevOp == COPY && prevArg == 1:
					redundant = true
				}
				if redundant {
					prev.Op = NOP
					prev.Oparg = 0
					cur.Op = NOP
					cur.Oparg = 0
					done = false
				}
			}
			if (cur != nil && isJumpOpcode(cur.Op)) || !basicblockHasFallthrough(b) {
				cur = nil
			}
		}
	}
}

// cfgRemoveRedundantNopsAndJumps loops removeRedundantNops and
// removeRedundantJumps until neither makes progress. Convergence is
// guaranteed because both passes only remove instructions.
//
// CPython: Python/flowgraph.c:2529 remove_redundant_nops_and_jumps
func cfgRemoveRedundantNopsAndJumps(g *cfgBuilder) {
	for {
		nops := cfgRemoveRedundantNops(g)
		jumps := cfgRemoveRedundantJumps(g)
		if nops+jumps == 0 {
			return
		}
	}
}

// makeSuperInstruction folds the (inst1, inst2) pair into a single
// super-opcode when both opargs fit in the low 4 bits and the pair
// shares (or is missing) line information. inst2 becomes a NOP.
//
// CPython: Python/flowgraph.c:2572 make_super_instruction
func makeSuperInstruction(inst1, inst2 *cfgInstr, superOp Opcode) {
	line1 := inst1.Loc.Lineno
	line2 := inst2.Loc.Lineno
	if line1 >= 0 && line2 >= 0 && line1 != line2 {
		return
	}
	if inst1.Oparg >= 16 || inst2.Oparg >= 16 {
		return
	}
	inst1.Op = superOp
	inst1.Oparg = (inst1.Oparg << 4) | inst2.Oparg
	inst2.Op = NOP
	inst2.Oparg = 0
}

// cfgInsertSuperinstructions scans each block for adjacent LOAD_FAST /
// STORE_FAST pairs and folds them into LOAD_FAST_LOAD_FAST /
// STORE_FAST_LOAD_FAST / STORE_FAST_STORE_FAST. Runs
// remove_redundant_nops afterwards to drop the NOPs left by the fold.
//
// CPython: Python/flowgraph.c:2588 insert_superinstructions
func cfgInsertSuperinstructions(g *cfgBuilder) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		for i := range b.Instr {
			cur := &b.Instr[i]
			var nextOp Opcode
			if i+1 < len(b.Instr) {
				nextOp = b.Instr[i+1].Op
			}
			switch cur.Op {
			case LOAD_FAST:
				if nextOp == LOAD_FAST {
					makeSuperInstruction(cur, &b.Instr[i+1], LOAD_FAST_LOAD_FAST)
				}
			case STORE_FAST:
				switch nextOp {
				case LOAD_FAST:
					makeSuperInstruction(cur, &b.Instr[i+1], STORE_FAST_LOAD_FAST)
				case STORE_FAST:
					makeSuperInstruction(cur, &b.Instr[i+1], STORE_FAST_STORE_FAST)
				}
			}
		}
	}
	cfgRemoveRedundantNops(g)
}

// cfgMarkWarm walks the graph from the entry following fallthroughs
// and jumps, marking every reachable non-handler block as Warm.
//
// CPython: Python/flowgraph.c:3323 mark_warm
func cfgMarkWarm(entry *basicblock) {
	for b := entry; b != nil; b = b.Next {
		b.Visited = false
	}
	stack := []*basicblock{entry}
	entry.Visited = true
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		b.Warm = true
		if b.Next != nil && basicblockHasFallthrough(b) && !b.Next.Visited {
			b.Next.Visited = true
			stack = append(stack, b.Next)
		}
		for i := range b.Instr {
			ins := &b.Instr[i]
			if isJumpOpcode(ins.Op) && ins.Target != nil && !ins.Target.Visited {
				ins.Target.Visited = true
				stack = append(stack, ins.Target)
			}
		}
	}
}

// cfgMarkCold runs cfgMarkWarm first, then seeds a second worklist
// from every ExceptHandler block and marks every reachable non-warm
// block as Cold.
//
// CPython: Python/flowgraph.c:3354 mark_cold
func cfgMarkCold(entry *basicblock) {
	cfgMarkWarm(entry)
	for b := entry; b != nil; b = b.Next {
		b.Visited = false
	}
	var stack []*basicblock
	for b := entry; b != nil; b = b.Next {
		if b.ExceptHandler {
			stack = append(stack, b)
			b.Visited = true
		}
	}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		b.Cold = true
		if b.Next != nil && basicblockHasFallthrough(b) {
			if !b.Next.Warm && !b.Next.Visited {
				b.Next.Visited = true
				stack = append(stack, b.Next)
			}
		}
		for i := range b.Instr {
			ins := &b.Instr[i]
			if isJumpOpcode(ins.Op) && ins.Target != nil {
				if !ins.Target.Warm && !ins.Target.Visited {
					ins.Target.Visited = true
					stack = append(stack, ins.Target)
				}
			}
		}
	}
}

// cfgPushColdBlocksToEnd reorders the block list so every cold block
// trails every warm block. When a cold block has a fallthrough edge to
// a warm successor, a synthetic JUMP_NO_INTERRUPT bridge is inserted
// so the reordering does not change the program's control flow.
//
// CPython: Python/flowgraph.c:3404 push_cold_blocks_to_end
func cfgPushColdBlocksToEnd(g *cfgBuilder) {
	entry := g.EntryBlock
	if entry.Next == nil {
		return
	}
	cfgMarkCold(entry)

	nextLbl := getMaxLabel(g) + 1
	for b := entry; b != nil; b = b.Next {
		if !b.Cold || !basicblockHasFallthrough(b) {
			continue
		}
		if b.Next == nil || !b.Next.Warm {
			continue
		}
		bridge := g.newBlock()
		if !b.Next.Label.IsValid() {
			b.Next.Label = JumpTargetLabel{id: nextLbl}
			nextLbl++
		}
		bridge.addOp(JUMP_NO_INTERRUPT, int32(b.Next.Label.id), ast.Pos{Lineno: -1})
		bridge.Instr[0].Target = b.Next
		bridge.Cold = true
		bridge.Next = b.Next
		bridge.Predecessors = 1
		b.Next = bridge
	}

	var coldHead, coldTail *basicblock
	b := entry
	for b.Next != nil {
		for b.Next != nil && !b.Next.Cold {
			b = b.Next
		}
		if b.Next == nil {
			break
		}
		bEnd := b.Next
		for bEnd.Next != nil && bEnd.Next.Cold {
			bEnd = bEnd.Next
		}
		if coldHead == nil {
			coldHead = b.Next
		} else {
			coldTail.Next = b.Next
		}
		coldTail = bEnd
		b.Next = bEnd.Next
		bEnd.Next = nil
	}
	b.Next = coldHead
	if coldHead != nil {
		cfgRemoveRedundantNopsAndJumps(g)
	}
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
		cfgInstrMakeLoadConst(ins, result, consts)
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

// optimizeBasicBlockCFG runs CPython's per-block peephole/const-fold
// pass over bb. Each switch case mirrors a case in CPython's
// optimize_basic_block in source order; the trailing loop reruns
// swaptimize + apply_static_swaps once new SWAPs become visible.
//
// CPython: Python/flowgraph.c:2311 optimize_basic_block
//
//nolint:gocognit,gocyclo // direct port of CPython's monolithic switch; splitting cases would diverge from the source.
func optimizeBasicBlockCFG(bb *basicblock, consts *[]any) {
	var nop cfgInstr
	nop.Op = NOP
	for i := 0; i < len(bb.Instr); i++ {
		inst := &bb.Instr[i]
		var target *cfgInstr
		if hasJumpTarget(inst.Op) && inst.Target != nil && len(inst.Target.Instr) > 0 {
			target = &inst.Target.Instr[0]
		} else {
			target = &nop
		}
		var nextop Opcode
		if i+1 < len(bb.Instr) {
			nextop = bb.Instr[i+1].Op
		}
		opcode := inst.Op
		oparg := inst.Oparg
		switch opcode {
		case BUILD_TUPLE:
			if nextop == UNPACK_SEQUENCE && oparg == bb.Instr[i+1].Oparg {
				switch oparg {
				case 1:
					inst.Op = NOP
					inst.Oparg = 0
					inst.Target = nil
					bb.Instr[i+1].Op = NOP
					bb.Instr[i+1].Oparg = 0
					continue
				case 2, 3:
					inst.Op = NOP
					inst.Oparg = 0
					inst.Target = nil
					bb.Instr[i+1].Op = SWAP
					continue
				}
			}
			basicblockFoldTupleOfConstants(bb, consts)
		case BUILD_LIST, BUILD_SET:
			basicblockOptimizeListsAndSets(bb, consts)
		case POP_JUMP_IF_NOT_NONE, POP_JUMP_IF_NONE:
			if target.Op == JUMP {
				if cfgJumpThread(bb, inst, target, opcode) {
					i--
				}
			}
		case POP_JUMP_IF_FALSE:
			if target.Op == JUMP {
				if cfgJumpThread(bb, inst, target, POP_JUMP_IF_FALSE) {
					i--
				}
			}
		case POP_JUMP_IF_TRUE:
			if target.Op == JUMP {
				if cfgJumpThread(bb, inst, target, POP_JUMP_IF_TRUE) {
					i--
				}
			}
		case JUMP_IF_FALSE:
			switch target.Op {
			case JUMP, JUMP_IF_FALSE:
				if cfgJumpThread(bb, inst, target, JUMP_IF_FALSE) {
					i--
				}
				continue
			case JUMP_IF_TRUE:
				if inst.Target != nil && inst.Target.Next != nil {
					inst.Target = inst.Target.Next
					inst.Oparg = int32(inst.Target.Label.id)
					i--
				}
				continue
			}
		case JUMP_IF_TRUE:
			switch target.Op {
			case JUMP, JUMP_IF_TRUE:
				if cfgJumpThread(bb, inst, target, JUMP_IF_TRUE) {
					i--
				}
				continue
			case JUMP_IF_FALSE:
				if inst.Target != nil && inst.Target.Next != nil {
					inst.Target = inst.Target.Next
					inst.Oparg = int32(inst.Target.Label.id)
					i--
				}
				continue
			}
		case JUMP, JUMP_NO_INTERRUPT:
			switch target.Op {
			case JUMP:
				if cfgJumpThread(bb, inst, target, JUMP) {
					i--
				}
				continue
			case JUMP_NO_INTERRUPT:
				if cfgJumpThread(bb, inst, target, opcode) {
					i--
				}
				continue
			}
		case FOR_ITER:
			// CPython leaves the JUMP target rewrite commented out
			// since FOR_ITER only jumps forward. Match that.
		case STORE_FAST:
			if opcode == nextop &&
				oparg == bb.Instr[i+1].Oparg &&
				bb.Instr[i].Loc.Lineno == bb.Instr[i+1].Loc.Lineno {
				bb.Instr[i].Op = POP_TOP
				bb.Instr[i].Oparg = 0
			}
		case SWAP:
			if oparg == 1 {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
			}
		case LOAD_GLOBAL:
			if nextop == PUSH_NULL && (oparg&1) == 0 {
				inst.Op = LOAD_GLOBAL
				inst.Oparg = oparg | 1
				bb.Instr[i+1].Op = NOP
				bb.Instr[i+1].Oparg = 0
				bb.Instr[i+1].Target = nil
			}
		case COMPARE_OP:
			if nextop == TO_BOOL {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
				bb.Instr[i+1].Op = COMPARE_OP
				bb.Instr[i+1].Oparg = oparg | 16
				continue
			}
		case CONTAINS_OP, IS_OP:
			if nextop == TO_BOOL {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
				bb.Instr[i+1].Op = opcode
				bb.Instr[i+1].Oparg = oparg
				continue
			}
			if nextop == UNARY_NOT {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
				bb.Instr[i+1].Op = opcode
				bb.Instr[i+1].Oparg = oparg ^ 1
				continue
			}
		case TO_BOOL:
			if nextop == TO_BOOL {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
				continue
			}
		case UNARY_NOT:
			if nextop == TO_BOOL {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
				bb.Instr[i+1].Op = UNARY_NOT
				bb.Instr[i+1].Oparg = 0
				continue
			}
			if nextop == UNARY_NOT {
				inst.Op = NOP
				inst.Oparg = 0
				inst.Target = nil
				bb.Instr[i+1].Op = NOP
				bb.Instr[i+1].Oparg = 0
				continue
			}
			fallthrough
		case UNARY_INVERT, UNARY_NEGATIVE:
			basicblockFoldConstUnaryop(bb, consts)
		case CALL_INTRINSIC_1:
			switch oparg {
			case intrinsicListToTuple:
				if nextop == GET_ITER {
					inst.Op = NOP
					inst.Oparg = 0
					inst.Target = nil
				} else {
					basicblockFoldConstantIntrinsicListToTuple(bb, i, consts)
				}
			case intrinsicUnaryPositive:
				basicblockFoldConstUnaryop(bb, consts)
			}
		case BINARY_OP:
			basicblockFoldConstBinop(bb, consts)
		}
	}
	for i := 0; i < len(bb.Instr); i++ {
		if bb.Instr[i].Op == SWAP {
			basicblockSwaptimize(bb, &i)
			basicblockApplyStaticSwaps(bb, i)
		}
	}
}

// basicblockFoldConstantIntrinsicListToTuple folds a
//
//	BUILD_LIST 0; (LOAD_CONST cK; LIST_APPEND 1)+ ; CALL_INTRINSIC_1 LIST_TO_TUPLE
//
// run at position i into a single LOAD_CONST(c1, ..., cN). Returns 1
// when a fold happened.
//
// CPython: Python/flowgraph.c:1509 fold_constant_intrinsic_list_to_tuple
//
//nolint:gocognit // single-function port of CPython's reverse scan.
func basicblockFoldConstantIntrinsicListToTuple(bb *basicblock, i int, consts *[]any) int {
	if consts == nil {
		return 0
	}
	intrinsic := &bb.Instr[i]
	if intrinsic.Op != CALL_INTRINSIC_1 || intrinsic.Oparg != intrinsicListToTuple {
		return 0
	}
	constsFound := 0
	expectAppend := true
	for pos := i - 1; pos >= 0; pos-- {
		ins := &bb.Instr[pos]
		if ins.Op == NOP {
			continue
		}
		if ins.Op == BUILD_LIST && ins.Oparg == 0 {
			if !expectAppend {
				return 0
			}
			values := make([]any, constsFound)
			cursor := constsFound
			for newpos := i - 1; newpos >= pos; newpos-- {
				p := &bb.Instr[newpos]
				if p.Op == NOP {
					continue
				}
				if v, ok := cfgLoadsConstValue(p, *consts); ok {
					cursor--
					values[cursor] = v
				}
				p.Op = NOP
				p.Oparg = 0
				p.Target = nil
			}
			tuple := &ConstTuple{Values: values}
			intrinsic.Op = LOAD_CONST
			intrinsic.Oparg = int32(appendConst(consts, tuple))
			return 1
		}
		if expectAppend {
			if ins.Op != LIST_APPEND || ins.Oparg != 1 {
				return 0
			}
		} else {
			if _, ok := cfgLoadsConstValue(ins, *consts); !ok {
				return 0
			}
			constsFound++
		}
		expectAppend = !expectAppend
	}
	return 0
}

// basicblockFoldConstBinop rewrites `(LOAD_CONST | LOAD_SMALL_INT) a;
// (LOAD_CONST | LOAD_SMALL_INT) b; BINARY_OP op` triples where both
// operands are int64 constants and op has a defined integer result. The
// two loads become NOPs and the BINARY_OP becomes LOAD_CONST of the
// folded value.
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
		if c.Op != BINARY_OP {
			continue
		}
		va, okA := cfgLoadsConstValue(a, *consts)
		if !okA {
			continue
		}
		vb, okB := cfgLoadsConstValue(b, *consts)
		if !okB {
			continue
		}
		x, xok := va.(int64)
		y, yok := vb.(int64)
		if !xok || !yok {
			continue
		}
		result, ok := evalIntBinop(c.Oparg, x, y)
		if !ok {
			continue
		}
		a.Op = NOP
		a.Oparg = 0
		a.Target = nil
		b.Op = NOP
		b.Oparg = 0
		b.Target = nil
		cfgInstrMakeLoadConst(c, result, consts)
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

// maybeInstrMakeLoadSmallint promotes a LOAD_CONST of a non-negative
// int that fits in 8 bits to LOAD_SMALL_INT. The orphaned const slot
// stays in the pool until cfgRemoveUnusedConsts runs.
//
// CPython: Python/flowgraph.c:1408 maybe_instr_make_load_smallint
func maybeInstrMakeLoadSmallint(inst *cfgInstr, consts []any) {
	if inst.Op != LOAD_CONST {
		return
	}
	idx := int(inst.Oparg)
	if idx < 0 || idx >= len(consts) {
		return
	}
	v, ok := consts[idx].(int64)
	if !ok {
		return
	}
	if v < 0 || v > 255 {
		return
	}
	inst.Op = LOAD_SMALL_INT
	inst.Oparg = int32(v)
}

// cfgInstrMakeLoadConst stamps inst with the bytecode that loads
// newconst. Small non-negative ints become LOAD_SMALL_INT; everything
// else lands as LOAD_CONST with the const appended to the pool.
//
// CPython: Python/flowgraph.c:1429 instr_make_load_const
func cfgInstrMakeLoadConst(inst *cfgInstr, newconst any, consts *[]any) {
	if v, ok := newconst.(int64); ok && v >= 0 && v <= 255 {
		inst.Op = LOAD_SMALL_INT
		inst.Oparg = int32(v)
		return
	}
	inst.Op = LOAD_CONST
	inst.Oparg = int32(appendConst(consts, newconst))
}

// basicblockOptimizeLoadConst folds LOAD_CONST / LOAD_SMALL_INT against
// the instruction that follows. Four cases mirror CPython exactly:
//
//   - LOAD_CONST X; POP_JUMP_IF_FALSE/TRUE | JUMP_IF_FALSE/TRUE:
//     resolve the branch statically; rewrite as JUMP / NOP.
//   - LOAD_CONST None; IS_OP k; (TO_BOOL?) POP_JUMP_IF_*:
//     fold into POP_JUMP_IF_NONE / POP_JUMP_IF_NOT_NONE.
//   - LOAD_CONST X; TO_BOOL: replace TO_BOOL with LOAD_CONST bool(X).
//   - COPY 1 right after a LOAD_CONST is transparent.
//
// Also runs maybeInstrMakeLoadSmallint on every LOAD_CONST in the
// block, matching CPython's interleaved smallint conversion.
//
// CPython: Python/flowgraph.c:2168 basicblock_optimize_load_const
//
//nolint:gocognit,gocyclo // direct port of the CPython switch; flattening hurts the 1:1 mapping with flowgraph.c:2168.
func basicblockOptimizeLoadConst(bb *basicblock, consts *[]any) {
	var opcode Opcode
	var oparg int32
	for i := 0; i < len(bb.Instr); i++ {
		inst := &bb.Instr[i]
		if inst.Op == LOAD_CONST {
			maybeInstrMakeLoadSmallint(inst, *consts)
		}
		isCopyOfLoadConst := opcode == LOAD_CONST && inst.Op == COPY && inst.Oparg == 1
		if !isCopyOfLoadConst {
			opcode = inst.Op
			oparg = inst.Oparg
		}
		if opcode != LOAD_CONST && opcode != LOAD_SMALL_INT {
			continue
		}
		if i+1 >= len(bb.Instr) {
			continue
		}
		nextop := bb.Instr[i+1].Op
		switch nextop {
		case POP_JUMP_IF_FALSE, POP_JUMP_IF_TRUE, JUMP_IF_FALSE, JUMP_IF_TRUE:
			cnt, ok := cfgLoadsConstValue(&cfgInstr{Op: opcode, Oparg: oparg}, *consts)
			if !ok {
				continue
			}
			isTrue, ok := constTruthValue(cnt)
			if !ok {
				continue
			}
			if nextop == POP_JUMP_IF_FALSE || nextop == POP_JUMP_IF_TRUE {
				setNopCfg(inst)
			}
			jumpIfTrue := nextop == POP_JUMP_IF_TRUE || nextop == JUMP_IF_TRUE
			if isTrue == jumpIfTrue {
				bb.Instr[i+1].Op = JUMP
			} else {
				setNopCfg(&bb.Instr[i+1])
			}
		case IS_OP:
			cnt, ok := cfgLoadsConstValue(&cfgInstr{Op: opcode, Oparg: oparg}, *consts)
			if !ok {
				continue
			}
			if cnt != nil {
				continue
			}
			if i+2 >= len(bb.Instr) {
				continue
			}
			isInstr := &bb.Instr[i+1]
			jumpIdx := i + 2
			if bb.Instr[jumpIdx].Op == TO_BOOL {
				setNopCfg(&bb.Instr[jumpIdx])
				jumpIdx = i + 3
				if jumpIdx >= len(bb.Instr) {
					continue
				}
			}
			jumpInstr := &bb.Instr[jumpIdx]
			invert := isInstr.Oparg != 0
			switch jumpInstr.Op {
			case POP_JUMP_IF_FALSE:
				invert = !invert
			case POP_JUMP_IF_TRUE:
				// keep invert as-is
			default:
				continue
			}
			setNopCfg(inst)
			setNopCfg(isInstr)
			if invert {
				jumpInstr.Op = POP_JUMP_IF_NOT_NONE
			} else {
				jumpInstr.Op = POP_JUMP_IF_NONE
			}
		case TO_BOOL:
			cnt, ok := cfgLoadsConstValue(&cfgInstr{Op: opcode, Oparg: oparg}, *consts)
			if !ok {
				continue
			}
			isTrue, ok := constTruthValue(cnt)
			if !ok {
				continue
			}
			idx := appendConst(consts, isTrue)
			setNopCfg(inst)
			bb.Instr[i+1].Op = LOAD_CONST
			bb.Instr[i+1].Oparg = int32(idx)
		}
	}
}

// cfgOptimizeLoadConst is the outer driver: runs
// basicblockOptimizeLoadConst over every block.
//
// CPython: Python/flowgraph.c:2301 optimize_load_const
func cfgOptimizeLoadConst(g *cfgBuilder, consts *[]any) {
	for b := g.EntryBlock; b != nil; b = b.Next {
		basicblockOptimizeLoadConst(b, consts)
	}
}

// setNopCfg clears a cfgInstr to a plain NOP, preserving Loc so the
// later nop-removal pass can fold the lineno forward.
func setNopCfg(ins *cfgInstr) {
	ins.Op = NOP
	ins.Oparg = 0
	ins.Target = nil
}

// cfgRemoveUnusedConsts compacts the consts pool. Scans every block
// for instructions with the HasConst flag, marks used slots, condenses
// the list while keeping slot 0 (potential docstring), then rewrites
// every CONST-bearing oparg through a reverse index map.
//
// CPython: Python/flowgraph.c:3174 remove_unused_consts
//
//nolint:gocyclo // 1:1 port of remove_unused_consts; splitting the index_map dance hurts the source mapping.
func cfgRemoveUnusedConsts(entry *basicblock, consts *[]any) {
	if consts == nil {
		return
	}
	nconsts := len(*consts)
	if nconsts == 0 {
		return
	}
	indexMap := make([]int, nconsts)
	for i := 1; i < nconsts; i++ {
		indexMap[i] = -1
	}
	indexMap[0] = 0
	for b := entry; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			if !ins.Op.HasConst() {
				continue
			}
			idx := int(ins.Oparg)
			if idx < 0 || idx >= nconsts {
				continue
			}
			indexMap[idx] = idx
		}
	}
	nUsed := 0
	for i := range nconsts {
		if indexMap[i] != -1 {
			indexMap[nUsed] = indexMap[i]
			nUsed++
		}
	}
	if nUsed == nconsts {
		return
	}
	newConsts := make([]any, nUsed)
	for i := range nUsed {
		newConsts[i] = (*consts)[indexMap[i]]
	}
	reverse := make([]int, nconsts)
	for i := range nconsts {
		reverse[i] = -1
	}
	for i := range nUsed {
		reverse[indexMap[i]] = i
	}
	for b := entry; b != nil; b = b.Next {
		for i := range b.Instr {
			ins := &b.Instr[i]
			if !ins.Op.HasConst() {
				continue
			}
			idx := int(ins.Oparg)
			if idx < 0 || idx >= nconsts || reverse[idx] < 0 {
				continue
			}
			ins.Oparg = int32(reverse[idx])
		}
	}
	*consts = newConsts
}

// cfgOptimizeCfg runs the optimization pipeline that lives inside
// CPython's optimize_cfg, in source order: check, inline small/no-lineno
// blocks, drop unreachable, resolve line numbers, optimize_load_const,
// per-block peephole, remove redundant nops/pairs, remove unreachable
// again, then remove redundant nops/jumps.
//
// CPython: Python/flowgraph.c:2552 optimize_cfg
func cfgOptimizeCfg(g *cfgBuilder, consts *[]any, firstlineno int) error {
	if err := cfgCheckCfg(g); err != nil {
		return err
	}
	cfgInlineSmallOrNoLinenoBlocks(g)
	cfgRemoveUnreachable(g)
	cfgResolveLineNumbers(g, firstlineno)
	cfgOptimizeLoadConst(g, consts)
	for b := g.EntryBlock; b != nil; b = b.Next {
		optimizeBasicBlockCFG(b, consts)
	}
	cfgRemoveRedundantNopsAndPairs(g.EntryBlock)
	cfgRemoveUnreachable(g)
	cfgRemoveRedundantNopsAndJumps(g)
	return nil
}

// CfgPhaseHook is called after every named phase inside
// cfgOptimizeCodeUnit. Per-phase compat tests (spec 1716 Phase B)
// register a hook that dumps the graph at each call point so the
// dumps diff against a CPython-side dump at the same phase boundary.
type CfgPhaseHook func(phase string, g *cfgBuilder)

// cfgOptimizeCodeUnit is the public entry point that wraps the whole
// pipeline. Preprocessing translates jump labels to targets, marks
// exception handlers, and propagates the handler stack through the
// graph. Then optimize_cfg runs the per-block / whole-CFG passes.
// Postprocessing trims unused consts, inserts LOAD_FAST_CHECK at
// loads of possibly-uninitialized variables, folds in
// super-instructions, pushes cold blocks to the tail, and re-resolves
// line numbers for the new layout.
//
// CPython: Python/flowgraph.c:3658 _PyCfg_OptimizeCodeUnit
func cfgOptimizeCodeUnit(g *cfgBuilder, consts *[]any, nlocals, nparams, firstlineno int) error {
	return cfgOptimizeCodeUnitWithHook(g, consts, nlocals, nparams, firstlineno, nil)
}

// cfgOptimizeCodeUnitWithHook is cfgOptimizeCodeUnit plus a hook that
// fires after every phase boundary. Passing nil for hook is identical
// to calling cfgOptimizeCodeUnit directly.
func cfgOptimizeCodeUnitWithHook(g *cfgBuilder, consts *[]any, nlocals, nparams, firstlineno int, hook CfgPhaseHook) error {
	fire := func(phase string) {
		if hook != nil {
			hook(phase, g)
		}
	}
	fire("entry")
	cfgTranslateJumpLabelsToTargets(g)
	fire("translate_jump_labels_to_targets")
	cfgMarkExceptHandlers(g)
	fire("mark_except_handlers")
	cfgLabelExceptionTargets(g.EntryBlock)
	fire("label_exception_targets")
	if err := cfgOptimizeCfg(g, consts, firstlineno); err != nil {
		return err
	}
	fire("optimize_cfg")
	cfgRemoveUnusedConsts(g.EntryBlock, consts)
	fire("remove_unused_consts")
	if err := addChecksForLoadsOfUninitializedVariables(g.EntryBlock, nlocals, nparams); err != nil {
		return err
	}
	fire("add_checks_for_loads_of_uninitialized_variables")
	cfgInsertSuperinstructions(g)
	fire("insert_superinstructions")
	cfgPushColdBlocksToEnd(g)
	fire("push_cold_blocks_to_end")
	cfgResolveLineNumbers(g, firstlineno)
	fire("resolve_line_numbers")
	return nil
}
