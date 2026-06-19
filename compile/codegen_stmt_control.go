// Port of cpython/Python/codegen.c control-flow statement visitors
// (L2043-L2289). If / For / While / Break / Continue.

package compile

import "github.com/tamnd/gopy/ast"

// visitIf compiles If: emit test, POP_JUMP_IF_FALSE to else label,
// body, JUMP to end label, else label, orelse, end label.
//
// CPython: Python/codegen.c:L2043 codegen_if
func (c *Compiler) visitIf(s *ast.If) error {
	c.unit().InConditionalBlock++
	defer func() { c.unit().InConditionalBlock-- }()
	end := c.newLabel()
	var elseLbl JumpTargetLabel
	hasElse := len(s.Orelse) > 0
	if hasElse {
		elseLbl = c.newLabel()
	} else {
		elseLbl = end
	}
	if err := c.codegenJumpIf(s.Test, elseLbl, false); err != nil {
		return err
	}
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
	c.unit().InConditionalBlock++
	defer func() { c.unit().InConditionalBlock-- }()
	loop := c.newLabel()
	body := c.newLabel()
	anchor := c.newLabel()
	end := c.newLabel()

	c.useLabel(loop)
	c.pushFblock(fblockWhileLoop, loop, end, s)

	if err := c.codegenJumpIf(s.Test, anchor, false); err != nil {
		return err
	}

	c.useLabel(body)
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	// The loop backedge is artificial: NO_LOCATION lets it inherit the
	// line of the body instruction it follows via propagate_line_numbers,
	// instead of stamping the whole tail block with the while-test line.
	//
	// CPython: Python/codegen.c:2178 ADDOP_JUMP(c, NO_LOCATION, JUMP, loop)
	c.addOpJump(JUMP, loop, ast.Pos{Lineno: -1})

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
	c.unit().InConditionalBlock++
	defer func() { c.unit().InConditionalBlock-- }()
	start := c.newLabel()
	body := c.newLabel()
	cleanup := c.newLabel()
	end := c.newLabel()

	if err := c.visitExpr(s.Iter); err != nil {
		return err
	}
	// CPython: Python/codegen.c:2084 codegen_for uses LOC(s->v.For.iter)
	// so that GET_ITER and FOR_ITER carry the iterator expression's span,
	// not the full for-statement span. traceback._get_code_position then
	// resolves to the iterator expression's line/col, which is what
	// test_exception_locations expects.
	iterLoc := loc(s.Iter)
	c.addOp(GET_ITER, iterLoc)

	c.useLabel(start)
	c.pushFblock(fblockForLoop, start, end, s)

	c.addOpJump(FOR_ITER, cleanup, iterLoc)

	// NOP carrying the target's location keeps line tracing correct for
	// multiline for statements; remove_redundant_nops drops it later when
	// it adds nothing. Without it a `pass` body's NOP and the artificial
	// JUMP_BACKWARD mis-attribute their lines.
	//
	// CPython: Python/codegen.c:2091 ADDOP(c, LOC(s->v.For.target), NOP)
	c.addOp(NOP, loc(s.Target))

	c.useLabel(body)
	if err := c.assignTo(s.Target, loc(s.Target)); err != nil {
		return err
	}
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	// The loop-back jump is artificial: NO_LOCATION lets it inherit the
	// line of whatever precedes it (the removed body NOP donates forward).
	//
	// CPython: Python/codegen.c:2097 ADDOP_JUMP(c, NO_LOCATION, JUMP, start)
	noLoc := ast.Pos{Lineno: -1}
	c.addOpJump(JUMP, start, noLoc)

	// CPython: Python/codegen.c:2105 END_FOR comes first so instrumentation
	// can attach to it, then POP_ITER drops the exhausted iterator. Both
	// carry NO_LOCATION and inherit their line via propagate_line_numbers.
	c.useLabel(cleanup)
	c.addOp(END_FOR, noLoc)
	c.addOp(POP_ITER, noLoc)
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

// visitAsyncFor compiles `async for target in iter: body else: orelse`.
// Mirrors CPython: evaluate iter, GET_AITER, then loop:
// SETUP_FINALLY, GET_ANEXT, LOAD_CONST None, send/yield loop, POP_BLOCK,
// NOT_TAKEN, target assign, body, JUMP back to start; except block runs
// END_ASYNC_FOR which jumps back to the send label on StopAsyncIteration
// so the await machinery can complete; orelse runs after the loop.
//
// CPython: Python/codegen.c:2117 codegen_async_for
func (c *Compiler) visitAsyncFor(s *ast.AsyncFor) error {
	c.unit().InConditionalBlock++
	defer func() { c.unit().InConditionalBlock-- }()
	start := c.newLabel()
	send := c.newLabel()
	except := c.newLabel()
	end := c.newLabel()

	if err := c.visitExpr(s.Iter); err != nil {
		return err
	}
	c.addOp(GET_AITER, loc(s.Iter))

	c.useLabel(start)
	c.pushFblock(fblockForLoop, start, end, s)

	c.addOpJump(SETUP_FINALLY, except, loc(s))
	c.addOp(GET_ANEXT, loc(s))
	c.addLoadConst(nil, loc(s))
	c.useLabel(send)
	c.addYieldFromLoop(loc(s))
	c.addOp(POP_BLOCK, loc(s))
	c.addOp(NOT_TAKEN, loc(s))

	if err := c.assignTo(s.Target, loc(s.Target)); err != nil {
		return err
	}
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	c.addOpJump(JUMP, start, ast.Pos{})

	if err := c.popFblock(fblockForLoop); err != nil {
		return err
	}

	c.useLabel(except)
	c.addOpJump(END_ASYNC_FOR, send, loc(s.Iter))

	if len(s.Orelse) > 0 {
		if err := c.visitStmts(s.Orelse); err != nil {
			return err
		}
	}
	c.useLabel(end)
	return nil
}

// visitBreak emits a JUMP to the enclosing loop's exit. Intermediate
// fblocks (try-finally cleanup, with __exit__) are unwound first.
//
// CPython: Python/codegen.c:L2232 codegen_break
func (c *Compiler) visitBreak(s *ast.Break) error {
	l := loc(s)
	idx := c.topFblockIdx(fblockWhileLoop, fblockForLoop)
	if idx < 0 {
		return c.errorAt(l, "'break' outside loop")
	}
	c.unwindToLoop(idx, &l)
	loop := &c.fblocks[idx]
	c.unwindFblock(loop, &l, false)
	c.addOpJump(JUMP, loop.Exit, l)
	return nil
}

// visitContinue emits a JUMP back to the enclosing loop's start.
//
// CPython: Python/codegen.c:L2248 codegen_continue
func (c *Compiler) visitContinue(s *ast.Continue) error {
	l := loc(s)
	idx := c.topFblockIdx(fblockWhileLoop, fblockForLoop)
	if idx < 0 {
		return c.errorAt(l, "'continue' not properly in loop")
	}
	c.unwindToLoop(idx, &l)
	c.addOpJump(JUMP, c.fblocks[idx].Block, l)
	return nil
}

// unwindToLoop emits the cleanup ops for every fblock above loopIdx
// (exclusive), walking inner to outer. The loop frame itself is not
// unwound.
//
// CPython: Python/codegen.c:L622 codegen_unwind_fblock_stack
func (c *Compiler) unwindToLoop(loopIdx int, ploc *ast.Pos) {
	for i := len(c.fblocks) - 1; i > loopIdx; i-- {
		c.unwindFblock(&c.fblocks[i], ploc, false)
	}
}

// unwindForReturn emits the cleanup ops for every active fblock so a
// return statement leaves the surrounding handlers/finallys/withs in a
// clean state. preserveTos is true when the return value sits on top
// of the stack and the unwind sequence must SWAP past it before
// popping (CPython's codegen_unwind_fblock preserve_tos branch).
//
// CPython: Python/codegen.c:L622 codegen_unwind_fblock_stack
func (c *Compiler) unwindForReturn(preserveTos bool, ploc *ast.Pos) {
	for i := len(c.fblocks) - 1; i >= 0; i-- {
		c.unwindFblock(&c.fblocks[i], ploc, preserveTos)
	}
}

// unwindFblock emits the cleanup ops for one fblock kind. Mirrors
// CPython's codegen_unwind_fblock.
//
// CPython: Python/codegen.c:L518 codegen_unwind_fblock
func (c *Compiler) unwindFblock(fb *fblock, ploc *ast.Pos, preserveTos bool) {
	l := *ploc
	switch fb.Kind {
	case fblockWhileLoop,
		fblockExceptionHandler,
		fblockExceptionGroupHandler,
		fblockAsyncComprehensionGenerator,
		fblockStopIteration:
		return
	case fblockForLoop:
		if preserveTos {
			c.addOpI(SWAP, 2, l)
		}
		c.addOp(POP_TOP, l)
	case fblockTryExcept:
		c.addOp(POP_BLOCK, l)
	case fblockFinallyTry:
		// This POP_BLOCK gets the line number of the unwinding statement.
		c.addOp(POP_BLOCK, l)
		// Datum is ast.Seq[ast.Stmt], not []ast.Stmt. Go does not consider
		// named generic types (type Seq[T] []T) identical to their base
		// slice type in type assertions, so we must assert the actual type.
		//
		// CPython: Python/codegen.c:541 codegen_unwind_fblock FB_FINALLY_TRY
		if stmts, ok := fb.Datum.(ast.Seq[ast.Stmt]); ok {
			// Deep-copy the fblock stack before visiting the inline
			// finally body. The copy is used to restore the stack
			// afterward, undoing any push/pop mutations that visitStmts
			// makes to the underlying array (a bare slice header would
			// share the array and see clobbered data after restoration).
			//
			// Also truncate to hide fb so that break/continue inside
			// the finally body cannot re-enter this block and recurse
			// infinitely. CPython achieves the same by decrementing
			// u_nfblocks before VISIT_SEQ and incrementing it after.
			//
			// CPython: Python/codegen.c:541 codegen_unwind_fblock FB_FINALLY_TRY
			origFblocks := append([]fblock{}, c.fblocks...)
			for i := range c.fblocks {
				if &c.fblocks[i] == fb {
					c.fblocks = c.fblocks[:i]
					break
				}
			}
			// When unwinding a return (preserve_tos=true), the return value
			// sits on top of the stack while the finally body runs. Push a
			// fblockPopValue so that any break/continue inside the finally
			// body knows to POP_TOP the return value before jumping to the
			// loop target, exactly matching CPython's behavior.
			//
			// CPython: Python/codegen.c:544 preserve_tos PushFBlock POP_VALUE
			if preserveTos {
				noLabel := JumpTargetLabel{}
				c.pushFblock(fblockPopValue, noLabel, noLabel, nil)
			}
			_ = c.visitStmts(stmts)
			if preserveTos {
				_ = c.popFblock(fblockPopValue)
			}
			c.fblocks = origFblocks
		}
		// The finally block should appear to execute after the statement
		// causing the unwinding, so make the unwinding instruction
		// artificial.
		//
		// CPython: Python/codegen.c:557 codegen_unwind_fblock FB_FINALLY_TRY
		*ploc = noLocation
	case fblockFinallyEnd:
		if preserveTos {
			c.addOpI(SWAP, 2, l)
		}
		c.addOp(POP_TOP, l)
		if preserveTos {
			c.addOpI(SWAP, 2, l)
		}
		c.addOp(POP_BLOCK, l)
		c.addOp(POP_EXCEPT, l)
	case fblockWith, fblockAsyncWith:
		// The with exit ops carry the location of the with statement,
		// not the unwinding statement.
		//
		// CPython: Python/codegen.c:570 codegen_unwind_fblock FB_WITH
		*ploc = fb.Loc
		l = *ploc
		c.addOp(POP_BLOCK, l)
		if preserveTos {
			// Each with-statement preamble pushes __exit__ AND its
			// self_or_null below the body's TOS (LOAD_SPECIAL leaves
			// two slots). To get the retval out of the way before the
			// __exit__(None, None, None) call we have to rotate it
			// past both: SWAP 3 then SWAP 2.
			//
			// CPython: Python/codegen.c:576 codegen_unwind_fblock FB_WITH
			c.addOpI(SWAP, 3, l)
			c.addOpI(SWAP, 2, l)
		}
		_ = c.callExitWithNones(l)
		if fb.Kind == fblockAsyncWith {
			// Async-with's __aexit__ returns an awaitable: drive it
			// through GET_AWAITABLE 2 + yield-from loop before the
			// POP_TOP, mirroring CPython's FB_ASYNC_WITH unwind.
			//
			// CPython: Python/codegen.c:597 codegen_unwind_fblock FB_ASYNC_WITH
			c.addOpI(GET_AWAITABLE, 2, l)
			c.addLoadConst(nil, l)
			c.addYieldFromLoop(l)
		}
		c.addOp(POP_TOP, l)
		// The exit block should appear to execute after the unwinding
		// statement, so make the unwinding instruction artificial.
		//
		// CPython: Python/codegen.c:604 codegen_unwind_fblock FB_WITH
		*ploc = noLocation
	case fblockHandlerCleanup:
		name, hasName := fb.Datum.(string)
		if hasName {
			c.addOp(POP_BLOCK, l)
		}
		if preserveTos {
			c.addOpI(SWAP, 2, l)
		}
		c.addOp(POP_BLOCK, l)
		c.addOp(POP_EXCEPT, l)
		if hasName {
			c.addLoadConst(nil, l)
			_ = c.nameOpStore(name, l)
			_ = c.nameOpDelete(name, l)
		}
	case fblockPopValue:
		if preserveTos {
			c.addOpI(SWAP, 2, l)
		}
		c.addOp(POP_TOP, l)
	}
}
