// Port of cpython/Python/codegen.c frame block stack (L518-L647).
//
// The fblock stack tracks unwinding for break / continue / return /
// raise / generator close through nested loops, try-finally, with,
// async-with, and exception handlers.

package compile

import (
	"fmt"
	"slices"

	"github.com/tamnd/gopy/ast"
)

// fblockKind tags the kind of frame on the fblock stack. Mirrors
// CPython's enum fblocktype.
//
// CPython: Python/codegen.c fblocktype
type fblockKind int

const (
	fblockWhileLoop fblockKind = iota + 1
	fblockForLoop
	fblockTryExcept
	fblockFinallyTry
	fblockFinallyEnd
	fblockWith
	fblockAsyncWith
	fblockHandlerCleanup
	fblockPopValue
	fblockExceptionHandler
	fblockExceptionGroupHandler
	fblockAsyncComprehensionGenerator
	fblockStopIteration
)

// fblock is one entry on the frame block stack. The fields match the
// CPython struct fblockinfo line for line so the unwind algorithms
// can be ported verbatim.
//
// CPython: Python/codegen.c fblockinfo
type fblock struct {
	Kind  fblockKind
	Block JumpTargetLabel
	Exit  JumpTargetLabel
	// Datum carries the original AST node so
	// unwindFblock can re-evaluate context expressions.
	Datum any
	// Loc mirrors CPython's fblockinfo.fb_loc. The with/async-with
	// unwind restores it so the exit-protocol ops carry the location
	// of the with statement rather than the unwinding statement.
	//
	// CPython: Python/codegen.c fblockinfo.fb_loc
	Loc ast.Pos
}

// pushFblock pushes a frame block onto the active unit's stack.
//
// CPython: Python/codegen.c codegen_push_fblock
func (c *Compiler) pushFblock(kind fblockKind, block, exit JumpTargetLabel, datum any) {
	c.fblocks = append(c.fblocks, fblock{Kind: kind, Block: block, Exit: exit, Datum: datum})
	// The FINALLY_END fblock guards the second (exception-path) copy of
	// a finally body; suppress duplicate SyntaxWarnings while it is live.
	// CPython: Python/compile.c:769 _PyCompile_PushFBlock
	if kind == fblockFinallyEnd {
		c.disableWarning++
	}
}

// popFblock pops the top frame block. Asserts the kind matches what
// the caller expected, mirroring CPython's RETURN_IF_ERROR check.
//
// CPython: Python/codegen.c codegen_pop_fblock
func (c *Compiler) popFblock(kind fblockKind) error {
	n := len(c.fblocks)
	if n == 0 {
		return fmt.Errorf("compile: fblock stack empty on pop %v", kind)
	}
	if c.fblocks[n-1].Kind != kind {
		return fmt.Errorf("compile: fblock pop kind mismatch: have %v, want %v",
			c.fblocks[n-1].Kind, kind)
	}
	c.fblocks = c.fblocks[:n-1]
	// CPython: Python/compile.c:783 _PyCompile_PopFBlock
	if kind == fblockFinallyEnd {
		c.disableWarning--
	}
	return nil
}

// topFblockIdx returns the stack index of the innermost fblock with
// one of the requested kinds, or -1 if none. break / continue use
// this to locate the enclosing loop.
//
// CPython: Python/codegen.c walk fblock_stack from top in
// codegen_break and codegen_continue.
func (c *Compiler) topFblockIdx(kinds ...fblockKind) int {
	for i := len(c.fblocks) - 1; i >= 0; i-- {
		if slices.Contains(kinds, c.fblocks[i].Kind) {
			return i
		}
	}
	return -1
}

// useLabel binds the next-emitted instruction to lbl in the active
// sequence. Mirrors USE_LABEL.
//
// CPython: Python/codegen.c USE_LABEL macro
func (c *Compiler) useLabel(lbl JumpTargetLabel) {
	c.seq().UseLabel(lbl)
}

// newLabel allocates a fresh label in the active sequence.
//
// CPython: Python/codegen.c NEW_JUMP_TARGET_LABEL macro
func (c *Compiler) newLabel() JumpTargetLabel {
	return c.seq().NewLabel()
}

// addOpJump emits a jump opcode whose oparg is a label id. The
// flowgraph resolves the label to a real instruction offset.
//
// CPython: Python/codegen.c:L400 codegen_addop_j
func (c *Compiler) addOpJump(op Opcode, target JumpTargetLabel, l ast.Pos) {
	c.seq().Addop(op, int32(target.ID()), l)
}
