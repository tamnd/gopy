// Port of cpython/Python/flowgraph.c (~4200 lines) to
// compile/flowgraph.go. CPython splits the work across one giant file
// covering CFG construction, ~25 optimisation passes, and the final
// re-flatten step. This Go port mirrors the public surface defined in
// 1627 (Optimize / FromSequence / ToSequence / Info) and lands the
// passes incrementally.
//
// This file holds the public driver, the Info / ExceptHandler types,
// the BasicBlock / Builder model, and the simplest passes
// (translateJumpLabelsToTargets, removeUnreachable, removeRedundantNops,
// calculateStackdepth). Larger panels (const fold, jump thread,
// swaptimize, super-instruction, loadfast) follow the file split in
// the 1627 spec and land alongside.
//
// CPython: Python/flowgraph.c

package compile

import (
	"fmt"
)

// Info is the per-pass metadata flowgraph hands to assemble. Mirrors
// the bookkeeping CPython attaches to each cfg_builder.
//
// CPython: Python/flowgraph.c cfg_builder + _PyCfg_OptimizeCodeUnit
// returns
type Info struct {
	MaxStackDepth  int
	ExceptionTable []ExceptHandler
	Consts         []any
	LocalsPlus     int
	NLocals        int
	NCellvars      int
	NFreevars      int
}

// ExceptHandler is one row in the PEP 657 exception table. Start, End,
// Target are byte offsets into the final co_code (filled by 1628
// assemble); Depth is the stack depth at handler entry; Lasti is the
// PEP 657 push-lasti bit.
//
// CPython: Python/flowgraph.c ExceptionHandler / Python/assemble.c
// emit_exception_table_entry
type ExceptHandler struct {
	Start  int
	End    int
	Target int
	Depth  int
	Lasti  bool
}

// BasicBlock is one node of the CFG. CPython builds a doubly-linked
// list rather than a slice so that inline / merge passes can splice
// cheaply. The Go port does the same with Next.
//
// CPython: Python/flowgraph.c basicblock
type BasicBlock struct {
	Instrs       []Instr
	Next         *BasicBlock
	Label        int
	StartDepth   int
	Predecessors int
	Visited      bool
	Cold         bool
	Warm         bool
	Reachable    bool
}

// Builder is the CFG builder state: head/tail of the block list plus
// scratch the optimiser passes share (label table, current block).
//
// CPython: Python/flowgraph.c cfg_builder
type Builder struct {
	Head *BasicBlock
	Tail *BasicBlock

	// labelMap[id] is the BasicBlock that label id resolves to. id 0
	// is reserved (NewLabel returns ids >= 1).
	labelMap map[int]*BasicBlock
}

// Optimize runs every flowgraph pass on a Sequence in the same order
// as CPython's _PyCfg_OptimizeCodeUnit. The current port lands the
// minimum-viable subset (label resolution, stackdepth, NOP cleanup);
// the rest of the panel arrives in follow-on commits per the 1627
// spec.
//
// CPython: Python/flowgraph.c:L3659 _PyCfg_OptimizeCodeUnit
func Optimize(seq *Sequence, consts *[]any, nlocals, _ int) (*Info, error) {
	return OptimizeWithFlags(seq, consts, nlocals, 0)
}

// OptimizeWithFlags is Optimize plus a code_flags input. CO_GENERATOR
// (and its async/coroutine siblings) drives insert_prefix_instructions
// to prepend the RETURN_GENERATOR + POP_TOP generator entry stub.
//
// CPython: Python/flowgraph.c:L4026 _PyCfg_OptimizedCfgToInstructionSequence
// (code_flags parameter)
func OptimizeWithFlags(seq *Sequence, consts *[]any, nlocals int, codeFlags uint32) (*Info, error) {
	if seq == nil {
		return nil, fmt.Errorf("compile: Optimize called with nil sequence")
	}
	if consts == nil {
		empty := []any{}
		consts = &empty
	}
	// PASS 0: fold int-int BINARY_OP triples into a single LOAD_CONST,
	// then drop instructions after an unconditional terminator. Run to
	// a fixed point so a fold that exposes a new dead tail (or a new
	// foldable triple from an exposed const) gets caught in the same
	// optimisation pass.
	for {
		folded := foldBinaryIntConst(seq, consts)
		dead := eliminateDeadCodeAfterTerminator(seq)
		if folded == 0 && dead == 0 {
			break
		}
	}

	// PASS 0a-pre: duplicate exit-without-lineno blocks reached by more
	// than one predecessor so each control-flow path can carry its own
	// source line. Must run before normalizeJumps because that pass
	// adds opargs that this counter would otherwise overcount.
	//
	// CPython: Python/flowgraph.c:3563 duplicate_exits_without_lineno
	duplicateExitsWithoutLineno(seq)

	// PASS 0a: insert NOT_TAKEN after every forward conditional jump.
	// Runs before ApplyLabelMap so the Insert label-shift lands on
	// symbolic ids; running after offset resolution would require
	// patching every absolute jump oparg.
	//
	// CPython: Python/flowgraph.c:535 normalize_jumps_in_block
	normalizeJumps(seq)

	// PASS 0b: promote LOAD_CONST <small int> to LOAD_SMALL_INT.
	// Runs after fold so any folded result that lands in the small-int
	// range gets promoted in the same optimisation pass.
	//
	// CPython: Python/flowgraph.c:2169 basicblock_optimize_load_const
	rewriteLoadSmallInt(seq, consts)

	// PASS 0c: prune the const-pool entries no surviving instruction
	// references. rewriteLoadSmallInt leaves small-int slots orphaned;
	// removing them here is what brings co_consts byte-equal with
	// CPython's output for the spec 1713 gate.
	//
	// CPython: Python/flowgraph.c:3174 remove_unused_consts
	removeUnusedConsts(seq, consts)

	// PASS 2: resolve symbolic jump labels to instruction offsets.
	// CPython does this on the CFG; we exploit instrseq's existing
	// ApplyLabelMap so the post-pass Sequence has resolved opargs.
	// HasTarget is false for JUMP / JUMP_NO_INTERRUPT and for the
	// SETUP_X family because none of those pseudo opcodes carry the
	// jump flag in the opcode metadata, so we resolve their labels by
	// hand here.
	seq.ApplyLabelMap(func(op Opcode) bool {
		switch op {
		case JUMP, JUMP_NO_INTERRUPT, SETUP_FINALLY, SETUP_WITH, SETUP_CLEANUP:
			return true
		}
		return HasTarget(op)
	})

	// PASS 2b: walk SETUP_X / POP_BLOCK to attribute each in-region
	// instruction with its handler. POP_BLOCK is rewritten to NOP
	// here; SETUP_X survives until convertPseudoOps so the stackdepth
	// pass can still see its exception edge.
	//
	// CPython: Python/flowgraph.c:885 label_exception_targets
	labelExceptionTargets(seq)

	// PASS 3a: thread chains of unconditional jumps and re-target any
	// POP_JUMP_IF_X that lands on a trampoline. CPython runs these on
	// the CFG; on the resolved sequence the same rewrites are local.
	threadJumps(seq)
	propagateConditionalJumps(seq)

	// PASS 3b: NOP-out blocks no reachable path lands on. Length-
	// preserving so jump opargs and the handler table stay valid.
	removeUnreachableBlocks(seq)

	// PASS 3b2: fold (compare-like)+(TO_BOOL or UNARY_NOT) pairs so the
	// emitted stream matches CPython's optimize_basic_block output. The
	// pass NOP-outs the first half and rewrites the second; the freshly
	// generated NOPs are reclaimed by removeRedundantNops below.
	//
	// CPython: Python/flowgraph.c:2449 optimize_basic_block
	peepholeOpcodePairs(seq)

	// PASS 3: compact NOP runs. ApplyLabelMap has baked indices into
	// jump opargs, so removeRedundantNops can reindex safely.
	removeRedundantNops(seq)

	// PASS 3c: backfill NO_LOCATION instructions with the previous
	// valid location so the linetable does not encode a stray
	// PY_CODE_LOCATION_INFO_NONE row at every implicit RETURN epilogue
	// (which findlinestarts otherwise surfaces as an extra `(off, None)`
	// transition and dis.py renders as a `--` lineno on a blank line).
	//
	// CPython: Python/flowgraph.c:3616 propagate_line_numbers
	propagateLineNumbers(seq)

	// PASS 11: stack-depth analysis. Forward dataflow over the flat
	// sequence is sufficient while the optimiser is minimal: every
	// block boundary is a jump, and the per-instruction effect table
	// is the same one CPython uses. Runs before resolveJumpOffsets
	// because the dataflow needs jump opargs as absolute target
	// indices; once offsets are converted to relative byte deltas the
	// successor walk can no longer follow them.
	depth, startDepth, err := calculateStackdepth(seq)
	if err != nil {
		return nil, err
	}

	// PASS 11b: now that the dataflow has reached every handler block
	// via the SETUP_X exception edge, stamp Handler.StartDepth on
	// every in-region instruction so AssembleExceptionTable can emit
	// the encoded outer-depth term. CPython reads
	// handler_block->b_startdepth at emission time; gopy threads it
	// here.
	stampHandlerStartDepths(seq, startDepth)

	// PASS 11b2: prepend the generator entry stub (RETURN_GENERATOR +
	// POP_TOP) if this is a generator / coroutine / async generator.
	// CPython does this inside prepare_localsplus, after
	// calculate_stackdepth and before convert_pseudo_ops, so jump
	// opargs (still absolute indices at this point) and Handler.Label
	// indices both shift by 2.
	//
	// CPython: Python/flowgraph.c:3760 insert_prefix_instructions
	insertPrefixInstructions(seq, codeFlags)

	// PASS 11c: lower the SETUP_X pseudo opcodes to NOP now that the
	// exception table is fully described on ExceptHandlerInfo. After
	// this pass the sequence contains only real opcodes.
	//
	// CPython: Python/flowgraph.c:3520 convert_pseudo_ops
	convertPseudoOps(seq)

	// PASS 4: lower the pseudo unconditional jumps (`JUMP`,
	// `JUMP_NO_INTERRUPT`) to their forward / backward counterparts,
	// then rewrite every jump oparg from absolute index into the
	// relative code-unit delta the VM consumes. Mirrors the back half
	// of _PyAssemble_MakeCodeObject.
	//
	// CPython: Python/assemble.c:778 _PyAssemble_MakeCodeObject
	resolveUnconditionalJumps(seq)
	resolveJumpOffsets(seq)

	info := &Info{
		MaxStackDepth: depth,
		Consts:        *consts,
		NLocals:       nlocals,
		LocalsPlus:    nlocals,
	}
	return info, nil
}

// FromSequence builds a CFG from a flat instruction sequence. Each
// label target starts a new block; terminators (RETURN_VALUE,
// RAISE_VARARGS, RERAISE, unconditional JUMP) end the current block.
//
// CPython: Python/flowgraph.c:L3923 _PyCfgBuilder_FromInstructionSequence
func FromSequence(seq *Sequence) (*Builder, error) {
	if seq == nil {
		return nil, fmt.Errorf("compile: FromSequence called with nil sequence")
	}
	b := &Builder{labelMap: map[int]*BasicBlock{}}

	// Build a set of instruction indices that begin a new block. The
	// entry block always starts at 0.
	starts := map[int]bool{0: true}
	for _, off := range seq.labelmap {
		if off >= 0 {
			starts[off] = true
		}
	}
	for i, ins := range seq.Instrs {
		if isTerminator(ins.Op) && i+1 < len(seq.Instrs) {
			starts[i+1] = true
		}
	}

	// Walk the sequence; cut blocks at every start.
	var current *BasicBlock
	for i, ins := range seq.Instrs {
		if starts[i] {
			block := &BasicBlock{Label: i}
			if b.Head == nil {
				b.Head = block
			} else {
				b.Tail.Next = block
			}
			b.Tail = block
			current = block
		}
		current.Instrs = append(current.Instrs, ins)
	}

	// Bind label ids to their resolved block by walking labelmap.
	for id := 1; id < len(seq.labelmap); id++ {
		off := seq.labelmap[id]
		if off < 0 {
			continue
		}
		for blk := b.Head; blk != nil; blk = blk.Next {
			if blk.Label == off {
				b.labelMap[id] = blk
				break
			}
		}
	}
	return b, nil
}

// ToSequence flattens a CFG back to a flat sequence. Walks blocks in
// list order, copying each instruction out. Jump targets are already
// instruction offsets (label resolution happened before the CFG build),
// so the caller does not need a fresh ApplyLabelMap.
//
// CPython: Python/flowgraph.c:L3988 _PyCfg_ToInstructionSequence
func (b *Builder) ToSequence() (*Sequence, error) {
	out := &Sequence{}
	for blk := b.Head; blk != nil; blk = blk.Next {
		out.Instrs = append(out.Instrs, blk.Instrs...)
	}
	return out, nil
}

// isTerminator reports whether op ends a basic block by control flow.
// Mirrors the CPython predicate IS_TERMINATOR_OPCODE.
//
// CPython: Python/flowgraph.c:L100 IS_TERMINATOR_OPCODE
func isTerminator(op Opcode) bool {
	switch op {
	case RETURN_VALUE, RAISE_VARARGS, RERAISE, INTERPRETER_EXIT:
		return true
	}
	return false
}
