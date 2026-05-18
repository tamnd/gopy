// Public surface of the flowgraph package. The cfgBuilder pipeline
// lives in flowgraph_cfg*.go; this file holds the metadata types the
// assembler consumes after _PyCfg_OptimizedCfgToInstructionSequence.
//
// CPython: Python/flowgraph.c

package compile

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
