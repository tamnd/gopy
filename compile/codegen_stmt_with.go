// Port of cpython/Python/codegen.c with-statement visitors
// (L5070-L5165). With and AsyncWith share most of the structure;
// AsyncWith threads each __aenter__ / __aexit__ call through an
// awaitable.

package compile

import (
	"github.com/tamnd/gopy/ast"
)

// visitWith compiles a `with` statement. Each Withitem is lowered as
// a SETUP_WITH frame around its body. With multiple items the
// compiler nests recursively; the innermost call body is the
// statement body.
//
// CPython: Python/codegen.c:L5161 codegen_with
func (c *Compiler) visitWith(s *ast.With) error {
	c.unit().InConditionalBlock++
	defer func() { c.unit().InConditionalBlock-- }()
	return c.visitWithInner(s, 0)
}

// visitWithInner emits the SETUP_WITH skeleton for items[pos] and
// recurses into items[pos+1:] (or the body when no items remain).
//
// CPython: Python/codegen.c:L5093 codegen_with_inner
func (c *Compiler) visitWithInner(s *ast.With, pos int) error {
	item := s.Items[pos]
	block := c.newLabel()
	final := c.newLabel()
	exit := c.newLabel()
	cleanup := c.newLabel()

	if err := c.visitExpr(item.ContextExpr); err != nil {
		return err
	}
	l := loc(item.ContextExpr)
	c.addOpI(COPY, 1, l)
	c.addOpI(LOAD_SPECIAL, specialExit, l)
	c.addOpI(SWAP, 2, l)
	c.addOpI(SWAP, 3, l)
	c.addOpI(LOAD_SPECIAL, specialEnter, l)
	c.addOpI(CALL, 0, l)
	c.addOpJump(SETUP_WITH, final, l)

	c.useLabel(block)
	c.pushFblock(fblockWith, block, final, s)
	c.fblocks[len(c.fblocks)-1].Loc = l

	if item.OptionalVars != nil {
		if err := c.assignTo(item.OptionalVars, l); err != nil {
			return err
		}
	} else {
		c.addOp(POP_TOP, l)
	}

	pos++
	if pos == len(s.Items) {
		if err := c.visitStmts(s.Body); err != nil {
			return err
		}
	} else {
		if err := c.visitWithInner(s, pos); err != nil {
			return err
		}
	}

	c.addOp(POP_BLOCK, ast.Pos{})
	if err := c.popFblock(fblockWith); err != nil {
		return err
	}

	if err := c.callExitWithNones(l); err != nil {
		return err
	}
	c.addOp(POP_TOP, l)
	c.addOpJump(JUMP, exit, l)

	c.useLabel(final)
	c.addOpJump(SETUP_CLEANUP, cleanup, l)
	c.addOp(PUSH_EXC_INFO, l)
	c.addOp(WITH_EXCEPT_START, l)
	if err := c.withExceptFinish(cleanup); err != nil {
		return err
	}

	c.useLabel(exit)
	return nil
}

// visitAsyncWith compiles an `async with` statement.
//
// CPython: Python/codegen.c:L5064 codegen_async_with
func (c *Compiler) visitAsyncWith(s *ast.AsyncWith) error {
	c.unit().InConditionalBlock++
	defer func() { c.unit().InConditionalBlock-- }()
	return c.visitAsyncWithInner(s, 0)
}

// visitAsyncWithInner is the recursive body of visitAsyncWith.
//
// CPython: Python/codegen.c:L4984 codegen_async_with_inner
func (c *Compiler) visitAsyncWithInner(s *ast.AsyncWith, pos int) error {
	item := s.Items[pos]
	block := c.newLabel()
	final := c.newLabel()
	exit := c.newLabel()
	cleanup := c.newLabel()

	if err := c.visitExpr(item.ContextExpr); err != nil {
		return err
	}
	l := loc(item.ContextExpr)
	c.addOpI(COPY, 1, l)
	c.addOpI(LOAD_SPECIAL, specialAExit, l)
	c.addOpI(SWAP, 2, l)
	c.addOpI(SWAP, 3, l)
	c.addOpI(LOAD_SPECIAL, specialAEnter, l)
	c.addOpI(CALL, 0, l)
	c.addOpI(GET_AWAITABLE, 1, l)
	c.addLoadConst(nil, l)
	c.addYieldFromLoop(l)

	c.addOpJump(SETUP_WITH, final, l)

	c.useLabel(block)
	c.pushFblock(fblockAsyncWith, block, final, s)
	c.fblocks[len(c.fblocks)-1].Loc = l

	if item.OptionalVars != nil {
		if err := c.assignTo(item.OptionalVars, l); err != nil {
			return err
		}
	} else {
		c.addOp(POP_TOP, l)
	}

	pos++
	if pos == len(s.Items) {
		if err := c.visitStmts(s.Body); err != nil {
			return err
		}
	} else {
		if err := c.visitAsyncWithInner(s, pos); err != nil {
			return err
		}
	}

	if err := c.popFblock(fblockAsyncWith); err != nil {
		return err
	}
	c.addOp(POP_BLOCK, l)

	if err := c.callExitWithNones(l); err != nil {
		return err
	}
	c.addOpI(GET_AWAITABLE, 2, l)
	c.addLoadConst(nil, l)
	c.addYieldFromLoop(l)
	c.addOp(POP_TOP, l)
	c.addOpJump(JUMP, exit, l)

	c.useLabel(final)
	c.addOpJump(SETUP_CLEANUP, cleanup, l)
	c.addOp(PUSH_EXC_INFO, l)
	c.addOp(WITH_EXCEPT_START, l)
	c.addOpI(GET_AWAITABLE, 2, l)
	c.addLoadConst(nil, l)
	c.addYieldFromLoop(l)
	if err := c.withExceptFinish(cleanup); err != nil {
		return err
	}

	c.useLabel(exit)
	return nil
}

// callExitWithNones emits the LOAD_CONST None x3 + CALL 3 sequence
// used to invoke a context manager's __exit__ on the success path.
//
// CPython: Python/codegen.c:L462 codegen_call_exit_with_nones
func (c *Compiler) callExitWithNones(l ast.Pos) error {
	c.addLoadConst(nil, l)
	c.addLoadConst(nil, l)
	c.addLoadConst(nil, l)
	c.addOpI(CALL, 3, l)
	return nil
}

// withExceptFinish emits the SETUP_CLEANUP / suppression / reraise
// tail that runs after WITH_EXCEPT_START.
//
// CPython: Python/codegen.c:L4935 codegen_with_except_finish
func (c *Compiler) withExceptFinish(cleanup JumpTargetLabel) error {
	suppress := c.newLabel()
	exit := c.newLabel()
	c.addOp(TO_BOOL, ast.Pos{})
	c.addOpJump(POP_JUMP_IF_TRUE, suppress, ast.Pos{})
	c.addOpI(RERAISE, 2, ast.Pos{})

	c.useLabel(suppress)
	c.addOp(POP_TOP, ast.Pos{})
	c.addOp(POP_BLOCK, ast.Pos{})
	c.addOp(POP_EXCEPT, ast.Pos{})
	c.addOp(POP_TOP, ast.Pos{})
	c.addOp(POP_TOP, ast.Pos{})
	c.addOp(POP_TOP, ast.Pos{})
	c.addOpJump(JUMP_NO_INTERRUPT, exit, ast.Pos{})

	c.useLabel(cleanup)
	c.popExceptAndReraise()

	c.useLabel(exit)
	return nil
}

// popExceptAndReraise emits the COPY 3 / POP_EXCEPT / RERAISE 1
// preamble used by exception cleanup blocks. CPython attaches no
// location info to these ops, so we always emit them at zero Pos.
//
// CPython: Python/codegen.c:L496 codegen_pop_except_and_reraise
func (c *Compiler) popExceptAndReraise() {
	c.addOpI(COPY, 3, ast.Pos{})
	c.addOp(POP_EXCEPT, ast.Pos{})
	c.addOpI(RERAISE, 1, ast.Pos{})
}

// addYieldFromLoop emits the SEND / YIELD / RESUME / JUMP_BACKWARD
// loop used by await sites. CPython's codegen_add_yield_from takes a
// resume oparg to distinguish yield-from (RESUME_AFTER_YIELD_FROM)
// from await (RESUME_AFTER_AWAIT); right now every caller is an
// await, so the value is hardcoded. Yield-from at the statement
// level emits its loop inline.
//
// CPython: Python/codegen.c:L471 codegen_add_yield_from
func (c *Compiler) addYieldFromLoop(l ast.Pos) {
	send := c.newLabel()
	fail := c.newLabel()
	exit := c.newLabel()
	c.useLabel(send)
	c.addOpJump(SEND, exit, l)
	// Virtual try/except so a StopIteration raised by YIELD_VALUE
	// during a close or throw routes through CLEANUP_THROW.
	c.addOpJump(SETUP_FINALLY, fail, l)
	c.addOpI(YIELD_VALUE, 1, l)
	c.addOp(POP_BLOCK, l)
	c.addOpI(RESUME, resumeAfterAwait, l)
	c.addOpJump(JUMP_NO_INTERRUPT, send, l)
	c.useLabel(fail)
	c.addOp(CLEANUP_THROW, l)
	c.useLabel(exit)
	c.addOp(END_SEND, l)
}
