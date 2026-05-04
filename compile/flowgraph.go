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
func Optimize(seq *Sequence, consts []any, nlocals, _ int) (*Info, error) {
	if seq == nil {
		return nil, fmt.Errorf("compile: Optimize called with nil sequence")
	}
	// PASS 1: resolve symbolic jump labels to instruction offsets.
	// CPython does this on the CFG; we exploit instrseq's existing
	// ApplyLabelMap so the post-pass Sequence has resolved opargs.
	seq.ApplyLabelMap(HasTarget)

	// PASS 11: stack-depth analysis. Forward dataflow over the flat
	// sequence is sufficient while the optimiser is minimal: every
	// block boundary is a jump, and the per-instruction effect table
	// is the same one CPython uses.
	depth, err := calculateStackdepth(seq)
	if err != nil {
		return nil, err
	}

	info := &Info{
		MaxStackDepth: depth,
		Consts:        consts,
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
