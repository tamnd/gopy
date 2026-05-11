// Port of cpython/Python/codegen.c control-flow statement visitors
// (L2043-L2289). If / For / While / Break / Continue.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitIf compiles If: emit test, POP_JUMP_IF_FALSE to else label,
// body, JUMP to end label, else label, orelse, end label.
//
// CPython: Python/codegen.c:L2043 codegen_if
func (c *Compiler) visitIf(s *ast.If) error {
	end := c.newLabel()
	var elseLbl JumpTargetLabel
	hasElse := len(s.Orelse) > 0
	if hasElse {
		elseLbl = c.newLabel()
	} else {
		elseLbl = end
	}
	if err := c.visitExpr(s.Test); err != nil {
		return err
	}
	c.addOpJump(POP_JUMP_IF_FALSE, elseLbl, loc(s))
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	if hasElse {
		c.addOpJump(JUMP, end, loc(s))
		c.useLabel(elseLbl)
		if err := c.visitStmts(s.Orelse); err != nil {
			return err
		}
	}
	c.useLabel(end)
	return nil
}

// visitWhile compiles While. Body and orelse follow the
// CPython skeleton: start label, test, POP_JUMP_IF_FALSE to anchor
// (over orelse if present), body, JUMP_BACKWARD to start, anchor.
//
// CPython: Python/codegen.c:L2165 codegen_while
func (c *Compiler) visitWhile(s *ast.While) error {
	loop := c.newLabel()
	body := c.newLabel()
	anchor := c.newLabel()
	end := c.newLabel()

	c.useLabel(loop)
	c.pushFblock(fblockWhileLoop, loop, end, s)

	if err := c.visitExpr(s.Test); err != nil {
		return err
	}
	c.addOpJump(POP_JUMP_IF_FALSE, anchor, loc(s))

	c.useLabel(body)
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	c.addOpJump(JUMP, loop, loc(s))

	c.useLabel(anchor)
	if err := c.popFblock(fblockWhileLoop); err != nil {
		return err
	}
	if len(s.Orelse) > 0 {
		if err := c.visitStmts(s.Orelse); err != nil {
			return err
		}
	}
	c.useLabel(end)
	return nil
}

// visitFor compiles For: evaluate iter, GET_ITER, FOR_ITER to cleanup
// label, store target, body, JUMP to start; cleanup runs orelse.
//
// CPython: Python/codegen.c:L2071 codegen_for
func (c *Compiler) visitFor(s *ast.For) error {
	start := c.newLabel()
	body := c.newLabel()
	cleanup := c.newLabel()
	end := c.newLabel()

	if err := c.visitExpr(s.Iter); err != nil {
		return err
	}
	c.addOp(GET_ITER, loc(s))

	c.useLabel(start)
	c.pushFblock(fblockForLoop, start, end, s)

	c.addOpJump(FOR_ITER, cleanup, loc(s))
	c.useLabel(body)
	if err := c.assignTo(s.Target, loc(s)); err != nil {
		return err
	}
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	c.addOpJump(JUMP, start, loc(s))

	c.useLabel(cleanup)
	c.addOp(END_FOR, loc(s))
	c.addOp(POP_TOP, loc(s))
	if err := c.popFblock(fblockForLoop); err != nil {
		return err
	}
	if len(s.Orelse) > 0 {
		if err := c.visitStmts(s.Orelse); err != nil {
			return err
		}
	}
	c.useLabel(end)
	return nil
}

// visitBreak emits a JUMP to the enclosing loop's exit. The unwind
// of intermediate fblocks (try-finally cleanup, with __exit__) is
// pending; for now reject break inside any non-loop frame so we
// never silently drop unwinds.
//
// CPython: Python/codegen.c:L2232 codegen_break
func (c *Compiler) visitBreak(s *ast.Break) error {
	loop := c.topFblock(fblockWhileLoop, fblockForLoop)
	if loop == nil {
		return fmt.Errorf("compile: 'break' outside loop")
	}
	l := loc(s)
	c.unwindToLoop(loop, l)
	c.unwindFblock(loop, l)
	c.addOpJump(JUMP, loop.Exit, l)
	return nil
}

// visitContinue emits a JUMP back to the enclosing loop's start.
//
// CPython: Python/codegen.c:L2248 codegen_continue
func (c *Compiler) visitContinue(s *ast.Continue) error {
	loop := c.topFblock(fblockWhileLoop, fblockForLoop)
	if loop == nil {
		return fmt.Errorf("compile: 'continue' not properly in loop")
	}
	c.unwindToLoop(loop, loc(s))
	c.addOpJump(JUMP, loop.Block, loc(s))
	return nil
}

// unwindToLoop emits the cleanup ops for every fblock between the
// current top and the named loop, walking from inner to outer. Each
// kind contributes its own unwind sequence (POP_BLOCK, POP_EXCEPT,
// inline finally body, ...). The loop frame itself does not unwind.
//
// CPython: Python/codegen.c:L622 codegen_unwind_fblock_stack
func (c *Compiler) unwindToLoop(loop *fblock, l ast.Pos) {
	for i := len(c.fblocks) - 1; i >= 0; i-- {
		fb := &c.fblocks[i]
		if fb == loop {
			return
		}
		c.unwindFblock(fb, l)
	}
}

// unwindFblock emits the cleanup ops for one fblock kind. Mirrors
// CPython's codegen_unwind_fblock with preserve_tos == 0.
//
// CPython: Python/codegen.c:L518 codegen_unwind_fblock
func (c *Compiler) unwindFblock(fb *fblock, l ast.Pos) {
	switch fb.Kind {
	case fblockWhileLoop,
		fblockExceptionHandler,
		fblockExceptionGroupHandler,
		fblockAsyncComprehensionGenerator,
		fblockStopIteration:
		return
	case fblockForLoop:
		c.addOp(POP_TOP, l)
	case fblockTryExcept:
		c.addOp(POP_BLOCK, l)
	case fblockFinallyTry:
		c.addOp(POP_BLOCK, l)
		if stmts, ok := fb.Datum.([]ast.Stmt); ok {
			_ = c.visitStmts(stmts)
		}
	case fblockFinallyEnd:
		c.addOp(POP_TOP, l)
		c.addOp(POP_BLOCK, l)
		c.addOp(POP_EXCEPT, l)
	case fblockWith, fblockAsyncWith:
		c.addOp(POP_BLOCK, l)
		_ = c.callExitWithNones(l)
		c.addOp(POP_TOP, l)
	case fblockHandlerCleanup:
		name, hasName := fb.Datum.(string)
		if hasName {
			c.addOp(POP_BLOCK, l)
		}
		c.addOp(POP_BLOCK, l)
		c.addOp(POP_EXCEPT, l)
		if hasName {
			c.addLoadConst(nil, l)
			_ = c.nameOpStore(name, l)
			_ = c.nameOpDelete(name, l)
		}
	case fblockPopValue:
		c.addOp(POP_TOP, l)
	}
}
