// CFG substrate for spec 1715: basicblock + cfgBuilder. Port of the
// data model in Python/flowgraph.c:32-89, plus the constructors that
// build the graph (cfg_builder_new_block, _PyCfgBuilder_New,
// _PyCfgBuilder_Free, _PyCfgBuilder_UseLabel, _PyCfgBuilder_Addop,
// basicblock_addop, basicblock_add_jump,
// basicblock_append_instructions, basicblock_insert_instruction,
// next_nonempty_block, basicblock_last_instr, basicblock_nofallthrough,
// basicblock_exits_scope).
//
// CPython: Python/flowgraph.c:32 _PyCfgInstruction
// CPython: Python/flowgraph.c:40 _PyCfgBasicblock
// CPython: Python/flowgraph.c:78 _PyCfgBuilder

package compile

import "github.com/tamnd/gopy/ast"

// cfgInstr is one instruction in a basicblock. Mirrors the C
// cfg_instr struct: jump targets are kept as *basicblock pointers
// rather than label ids, so passes that rewrite control flow do not
// have to maintain a separate labelmap.
//
// CPython: Python/flowgraph.c:32 _PyCfgInstruction
type cfgInstr struct {
	Op     Opcode
	Oparg  int32
	Loc    ast.Pos
	Target *basicblock
	Except *basicblock
}

// basicblock is one node in the control-flow graph. Block lifetime is
// managed by the cfgBuilder via the b_list singly-linked allocation
// list; b_next links along normal control flow (fallthrough).
//
// CPython: Python/flowgraph.c:40 _PyCfgBasicblock
type basicblock struct {
	List         *basicblock
	Next         *basicblock
	Instr        []cfgInstr
	Label        JumpTargetLabel
	Predecessors int
	StartDepth   int

	PreserveLasti bool
	Visited       bool
	ExceptHandler bool
	Cold          bool
	Warm          bool

	UnsafeLocalsMask uint64
}

// addOp appends an instruction. CPython's basicblock_addop asserts
// the opcode is in range and not an assembler-only opcode; gopy
// already gates those at the codegen / instruction-sequence layer.
//
// CPython: Python/flowgraph.c:189 basicblock_addop
func (b *basicblock) addOp(op Opcode, oparg int32, loc ast.Pos) {
	b.Instr = append(b.Instr, cfgInstr{Op: op, Oparg: oparg, Loc: loc})
}

// lastInstr returns the most recently appended instruction, or nil
// when the block is empty.
//
// CPython: Python/flowgraph.c:160 basicblock_last_instr
func (b *basicblock) lastInstr() *cfgInstr {
	if len(b.Instr) == 0 {
		return nil
	}
	return &b.Instr[len(b.Instr)-1]
}

// addJump appends a jump opcode pointing at target. The oparg is set
// to target.Label.id so a later flatten-to-Sequence step can resolve
// it through ApplyLabelMap if needed. Returns false if the last
// existing instruction is already a jump (CPython refuses to stack
// two jumps in the same block).
//
// CPython: Python/flowgraph.c:210 basicblock_add_jump
func (b *basicblock) addJump(op Opcode, target *basicblock, loc ast.Pos) bool {
	if last := b.lastInstr(); last != nil && isJumpOpcode(last.Op) {
		return false
	}
	b.Instr = append(b.Instr, cfgInstr{
		Op:     op,
		Oparg:  int32(target.Label.id),
		Loc:    loc,
		Target: target,
	})
	return true
}

// appendInstructions copies every instruction from src onto the end
// of b. Used by copy_basicblock and the block-inliner.
//
// CPython: Python/flowgraph.c:226 basicblock_append_instructions
func (b *basicblock) appendInstructions(src *basicblock) {
	b.Instr = append(b.Instr, src.Instr...)
}

// insertInstruction inserts ins at pos and shifts the tail right by
// one. Mirrors CPython's basicblock_insert_instruction.
//
// CPython: Python/flowgraph.c:267 basicblock_insert_instruction
func (b *basicblock) insertInstruction(pos int, ins cfgInstr) {
	b.Instr = append(b.Instr, cfgInstr{})
	copy(b.Instr[pos+1:], b.Instr[pos:])
	b.Instr[pos] = ins
}

// nofallthrough reports whether b's last instruction prevents normal
// fall-through to the next block (a scope exit or unconditional jump).
//
// CPython: Python/flowgraph.c:239 basicblock_nofallthrough
func (b *basicblock) nofallthrough() bool {
	last := b.lastInstr()
	if last == nil {
		return false
	}
	return isScopeExitOpcode(last.Op) || isUnconditionalJump(last.Op)
}

// exitsScope reports whether b ends with a scope-exit opcode
// (RETURN_VALUE / RAISE / RERAISE). Distinct from nofallthrough,
// which also includes unconditional jumps.
//
// CPython: Python/flowgraph.c:341 basicblock_exits_scope
func (b *basicblock) exitsScope() bool {
	last := b.lastInstr()
	return last != nil && isScopeExitOpcode(last.Op)
}

// cfgBuilder owns the graph being constructed. entryBlock is where
// control begins; blockList threads every allocated block in reverse
// allocation order; curBlock is the block instructions land in.
//
// CPython: Python/flowgraph.c:78 _PyCfgBuilder
type cfgBuilder struct {
	EntryBlock   *basicblock
	BlockList    *basicblock
	CurBlock     *basicblock
	CurrentLabel JumpTargetLabel
	HasLabel     bool
}

// newCfgBuilder allocates a builder with a single entry block. Mirrors
// _PyCfgBuilder_New + init_cfg_builder.
//
// CPython: Python/flowgraph.c:426 _PyCfgBuilder_New
// CPython: Python/flowgraph.c:413 init_cfg_builder
func newCfgBuilder() *cfgBuilder {
	g := &cfgBuilder{}
	block := g.newBlock()
	g.EntryBlock = block
	g.CurBlock = block
	return g
}

// newBlock allocates an empty basicblock and pushes it on the
// allocation list. The label defaults to the zero JumpTargetLabel
// (which compares equal to NO_LABEL since gopy uses id 0 as the
// "unlabeled" sentinel; live labels start at 1).
//
// CPython: Python/flowgraph.c:174 cfg_builder_new_block
func (g *cfgBuilder) newBlock() *basicblock {
	b := &basicblock{List: g.BlockList}
	g.BlockList = b
	return b
}

// useNextBlock sets b as the fallthrough successor of the current
// block and makes b the new current block.
//
// CPython: Python/flowgraph.c:332 cfg_builder_use_next_block
//
//nolint:unparam // CPython signature returns the block; passes ported later chain off it.
func (g *cfgBuilder) useNextBlock(b *basicblock) *basicblock {
	g.CurBlock.Next = b
	g.CurBlock = b
	return b
}

// useLabel pins lbl on the next instruction to be emitted. If the
// current block has already started (has any instructions or its own
// label), useLabel starts a fresh block so lbl can stand at its head.
//
// CPython: Python/flowgraph.c:474 _PyCfgBuilder_UseLabel
func (g *cfgBuilder) useLabel(lbl JumpTargetLabel) {
	g.CurrentLabel = lbl
	g.HasLabel = true
	g.maybeStartNewBlock()
}

// addOp emits an opcode into the current block, starting a fresh
// block first if the current one is already terminated.
//
// CPython: Python/flowgraph.c:482 _PyCfgBuilder_Addop
//
//nolint:unparam // oparg in CPython signature; non-zero callers arrive with the bridge.
func (g *cfgBuilder) addOp(op Opcode, oparg int32, loc ast.Pos) {
	g.maybeStartNewBlock()
	g.CurBlock.addOp(op, oparg, loc)
}

// currentBlockTerminated reports whether the current block ends in a
// terminator (RETURN, RAISE, JUMP), or whether a pending label
// requires a new block. The latter case relabels an empty current
// block instead of allocating a fresh one.
//
// CPython: Python/flowgraph.c:357 cfg_builder_current_block_is_terminated
func (g *cfgBuilder) currentBlockTerminated() bool {
	last := g.CurBlock.lastInstr()
	if last != nil && isTerminator(last.Op) {
		return true
	}
	if g.HasLabel {
		if last != nil || g.CurBlock.Label.id != 0 {
			return true
		}
		g.CurBlock.Label = g.CurrentLabel
		g.clearPendingLabel()
	}
	return false
}

// maybeStartNewBlock allocates and links a fresh block when
// currentBlockTerminated says the current one cannot accept more.
//
// CPython: Python/flowgraph.c:377 cfg_builder_maybe_start_new_block
func (g *cfgBuilder) maybeStartNewBlock() {
	if !g.currentBlockTerminated() {
		return
	}
	b := g.newBlock()
	if g.HasLabel {
		b.Label = g.CurrentLabel
		g.clearPendingLabel()
	}
	g.useNextBlock(b)
}

func (g *cfgBuilder) clearPendingLabel() {
	g.CurrentLabel = JumpTargetLabel{}
	g.HasLabel = false
}

// blocks returns every reachable block in fallthrough order starting
// from the entry. Mirrors the `for b := entryblock; b != NULL;
// b = b->b_next` walk CPython uses inside _PyCfgBuilder_DumpGraph and
// every optimization pass.
//
// CPython: Python/flowgraph.c:321 _PyCfgBuilder_DumpGraph walk
func (g *cfgBuilder) blocks() []*basicblock {
	out := make([]*basicblock, 0)
	for b := g.EntryBlock; b != nil; b = b.Next {
		out = append(out, b)
	}
	return out
}

// nextNonemptyBlock skips a forward chain of empty blocks. Used by
// jump-threading passes to land on the first block that actually
// holds an instruction.
//
// CPython: Python/flowgraph.c:490 next_nonempty_block
func nextNonemptyBlock(b *basicblock) *basicblock {
	for b != nil && len(b.Instr) == 0 {
		b = b.Next
	}
	return b
}

// copyBasicblock clones src into a fresh block on g. Asserts (matches
// CPython's runtime assert) that src has no fallthrough, since a
// block with fallthrough has exactly one predecessor edge and
// duplicating it would invalidate that invariant.
//
// CPython: Python/flowgraph.c:250 copy_basicblock
func (g *cfgBuilder) copyBasicblock(src *basicblock) *basicblock {
	if !src.nofallthrough() {
		panic("compile: copy_basicblock on block with fallthrough")
	}
	out := g.newBlock()
	out.appendInstructions(src)
	return out
}

// isJumpOpcode reports whether op carries a jump target. Mirrors the
// is_jump predicate inside flowgraph.c (which delegates to
// OPCODE_HAS_JUMP). gopy's hasJumpTarget already encodes this.
//
// CPython: Python/flowgraph.c:107 is_jump
func isJumpOpcode(op Opcode) bool {
	return hasJumpTarget(op)
}
