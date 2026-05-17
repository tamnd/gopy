// Port of cpython/Python/flowgraph.c (~4200 lines) to
// compile/flowgraph.go. This file holds the public driver, the
// Info / ExceptHandler types, and the isTerminator predicate.
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
// CPython: Python/flowgraph.c:4026 _PyCfg_OptimizeCodeUnit
// CPython: Python/flowgraph.c:4026 _PyCfg_OptimizedCfgToInstructionSequence
func OptimizeWithFlags(seq *Sequence, consts *[]any, nlocals int, codeFlags uint32) (*Info, error) {
	if seq == nil {
		return nil, fmt.Errorf("compile: Optimize called with nil sequence")
	}
	if consts == nil {
		empty := []any{}
		consts = &empty
	}
	// Build CFG, run the graph-side pipeline, flatten back.
	// nparams = nlocals so addChecksForLoadsOfUninitializedVariables is a
	// no-op (all locals treated as params) until the caller threads the
	// real count. firstlineno = 0 (cfgResolveLineNumbers ignores it).
	g := cfgFromSequence(seq)
	if err := cfgOptimizeCodeUnit(g, consts, nlocals, nlocals, 0); err != nil {
		return nil, err
	}
	// normalize_jumps runs after convert_pseudo_ops in CPython's
	// _PyCfg_OptimizedCfgToInstructionSequence, just before flattening.
	//
	// CPython: Python/flowgraph.c:4056 normalize_jumps
	cfgNormalizeJumps(g)
	out := &Sequence{Nested: seq.Nested, AnnoCode: seq.AnnoCode}
	cfgToSequence(g, out)
	seq.Instrs = out.Instrs
	seq.labelmap = out.labelmap
	seq.nextFreeLabel = out.nextFreeLabel
	// Assembler-side passes that still run on the flat sequence.
	depth, startDepth, err := calculateStackdepth(seq)
	if err != nil {
		return nil, err
	}
	stampHandlerStartDepths(seq, startDepth)
	insertPrefixInstructions(seq, codeFlags)
	convertPseudoOps(seq)
	resolveUnconditionalJumps(seq)
	resolveJumpOffsets(seq)
	return &Info{MaxStackDepth: depth, Consts: *consts, NLocals: nlocals, LocalsPlus: nlocals}, nil
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
