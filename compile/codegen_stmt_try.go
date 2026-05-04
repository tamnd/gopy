// Port of cpython/Python/codegen.c try-statement visitors
// (L2293-L2789). Try lowers to one of two skeletons depending on
// whether a finally body is present; the same split runs for the
// PEP 654 except* form.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitTry compiles a `try / except / else / finally`. Routes to
// visitTryFinally when a finally body is present, otherwise straight
// to visitTryExcept.
//
// CPython: Python/codegen.c:L2774 codegen_try
func (c *Compiler) visitTry(s *ast.Try) error {
	if len(s.Finalbody) > 0 {
		return c.visitTryFinally(s)
	}
	return c.visitTryExcept(s)
}

// visitTryStar compiles a `try / except* / else / finally`.
//
// CPython: Python/codegen.c:L2782 codegen_try_star
func (c *Compiler) visitTryStar(s *ast.TryStar) error {
	if len(s.Finalbody) > 0 {
		return c.visitTryStarFinally(s)
	}
	return c.visitTryStarExcept(s)
}

// visitTryFinally wraps the try-with-handlers (or bare body) in a
// SETUP_FINALLY frame, then runs the finally body twice: once on the
// success path, once via SETUP_CLEANUP for the exception path.
//
// CPython: Python/codegen.c:L2293 codegen_try_finally
func (c *Compiler) visitTryFinally(s *ast.Try) error {
	body := c.newLabel()
	end := c.newLabel()
	exit := c.newLabel()
	cleanup := c.newLabel()
	l := loc(s)

	c.addOpJump(SETUP_FINALLY, end, l)

	c.useLabel(body)
	c.pushFblock(fblockFinallyTry, body, end, s.Finalbody)

	if len(s.Handlers) > 0 {
		if err := c.visitTryExcept(s); err != nil {
			return err
		}
	} else {
		if err := c.visitStmts(s.Body); err != nil {
			return err
		}
	}
	c.addOp(POP_BLOCK, ast.Pos{})
	if err := c.popFblock(fblockFinallyTry); err != nil {
		return err
	}
	if err := c.visitStmts(s.Finalbody); err != nil {
		return err
	}
	c.addOpJump(JUMP_NO_INTERRUPT, exit, ast.Pos{})

	c.useLabel(end)
	c.addOpJump(SETUP_CLEANUP, cleanup, ast.Pos{})
	c.addOp(PUSH_EXC_INFO, ast.Pos{})
	c.pushFblock(fblockFinallyEnd, end, JumpTargetLabel{}, nil)
	if err := c.visitStmts(s.Finalbody); err != nil {
		return err
	}
	if err := c.popFblock(fblockFinallyEnd); err != nil {
		return err
	}
	c.addOpI(RERAISE, 0, ast.Pos{})

	c.useLabel(cleanup)
	c.popExceptAndReraise()

	c.useLabel(exit)
	return nil
}

// visitTryStarFinally is the except* counterpart of visitTryFinally.
//
// CPython: Python/codegen.c:L2344 codegen_try_star_finally
func (c *Compiler) visitTryStarFinally(s *ast.TryStar) error {
	body := c.newLabel()
	end := c.newLabel()
	exit := c.newLabel()
	cleanup := c.newLabel()
	l := loc(s)

	c.addOpJump(SETUP_FINALLY, end, l)

	c.useLabel(body)
	c.pushFblock(fblockFinallyTry, body, end, s.Finalbody)

	if len(s.Handlers) > 0 {
		if err := c.visitTryStarExcept(s); err != nil {
			return err
		}
	} else {
		if err := c.visitStmts(s.Body); err != nil {
			return err
		}
	}
	c.addOp(POP_BLOCK, ast.Pos{})
	if err := c.popFblock(fblockFinallyTry); err != nil {
		return err
	}
	if err := c.visitStmts(s.Finalbody); err != nil {
		return err
	}
	c.addOpJump(JUMP_NO_INTERRUPT, exit, ast.Pos{})

	c.useLabel(end)
	c.addOpJump(SETUP_CLEANUP, cleanup, ast.Pos{})
	c.addOp(PUSH_EXC_INFO, ast.Pos{})
	c.pushFblock(fblockFinallyEnd, end, JumpTargetLabel{}, nil)
	if err := c.visitStmts(s.Finalbody); err != nil {
		return err
	}
	if err := c.popFblock(fblockFinallyEnd); err != nil {
		return err
	}
	c.addOpI(RERAISE, 0, ast.Pos{})

	c.useLabel(cleanup)
	c.popExceptAndReraise()

	c.useLabel(exit)
	return nil
}

// visitTryExcept emits the standard try / except chain. Each handler
// becomes a CHECK_EXC_MATCH + body. A handler with `as name` wraps
// the body in a second SETUP_CLEANUP that clears the bound name on
// exit (matching CPython's `try / finally: name = None; del name`).
//
// CPython: Python/codegen.c:L2424 codegen_try_except
func (c *Compiler) visitTryExcept(s *ast.Try) error {
	body := c.newLabel()
	exceptLbl := c.newLabel()
	end := c.newLabel()
	cleanup := c.newLabel()
	l := loc(s)

	c.addOpJump(SETUP_FINALLY, exceptLbl, l)

	c.useLabel(body)
	c.pushFblock(fblockTryExcept, body, JumpTargetLabel{}, nil)
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	if err := c.popFblock(fblockTryExcept); err != nil {
		return err
	}
	c.addOp(POP_BLOCK, ast.Pos{})
	if len(s.Orelse) > 0 {
		if err := c.visitStmts(s.Orelse); err != nil {
			return err
		}
	}
	c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})

	c.useLabel(exceptLbl)
	c.addOpJump(SETUP_CLEANUP, cleanup, ast.Pos{})
	c.addOp(PUSH_EXC_INFO, ast.Pos{})
	c.pushFblock(fblockExceptionHandler, JumpTargetLabel{}, JumpTargetLabel{}, nil)

	n := len(s.Handlers)
	for i, h := range s.Handlers {
		eh, ok := h.(*ast.ExceptHandler)
		if !ok {
			return fmt.Errorf("compile: unknown excepthandler %T", h)
		}
		hl := loc(eh)
		if eh.Type == nil && i < n-1 {
			return fmt.Errorf("compile: default 'except:' must be last")
		}
		nextExcept := c.newLabel()
		if eh.Type != nil {
			if err := c.visitExpr(eh.Type); err != nil {
				return err
			}
			c.addOp(CHECK_EXC_MATCH, hl)
			c.addOpJump(POP_JUMP_IF_FALSE, nextExcept, hl)
		}
		if err := c.emitExceptBody(eh, end); err != nil {
			return err
		}
		c.useLabel(nextExcept)
	}
	if err := c.popFblock(fblockExceptionHandler); err != nil {
		return err
	}
	c.addOpI(RERAISE, 0, ast.Pos{})

	c.useLabel(cleanup)
	c.popExceptAndReraise()

	c.useLabel(end)
	return nil
}

// emitExceptBody writes one handler arm. The `as name` form bolts a
// second SETUP_CLEANUP around the body so that the captured name is
// always cleared on exit.
//
// CPython: Python/codegen.c codegen_try_except handler-body branch
func (c *Compiler) emitExceptBody(h *ast.ExceptHandler, end JumpTargetLabel) error {
	hl := loc(h)
	if h.Name == nil {
		cleanupBody := c.newLabel()
		c.addOp(POP_TOP, hl)
		c.useLabel(cleanupBody)
		c.pushFblock(fblockHandlerCleanup, cleanupBody, JumpTargetLabel{}, nil)
		if err := c.visitStmts(h.Body); err != nil {
			return err
		}
		if err := c.popFblock(fblockHandlerCleanup); err != nil {
			return err
		}
		c.addOp(POP_BLOCK, ast.Pos{})
		c.addOp(POP_EXCEPT, ast.Pos{})
		c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})
		return nil
	}
	cleanupEnd := c.newLabel()
	cleanupBody := c.newLabel()
	if err := c.nameOpStore(*h.Name, hl); err != nil {
		return err
	}
	c.addOpJump(SETUP_CLEANUP, cleanupEnd, hl)
	c.useLabel(cleanupBody)
	c.pushFblock(fblockHandlerCleanup, cleanupBody, JumpTargetLabel{}, *h.Name)
	if err := c.visitStmts(h.Body); err != nil {
		return err
	}
	if err := c.popFblock(fblockHandlerCleanup); err != nil {
		return err
	}
	c.addOp(POP_BLOCK, ast.Pos{})
	c.addOp(POP_BLOCK, ast.Pos{})
	c.addOp(POP_EXCEPT, ast.Pos{})
	c.addLoadConst(nil, ast.Pos{})
	if err := c.nameOpStore(*h.Name, ast.Pos{}); err != nil {
		return err
	}
	if err := c.nameOpDelete(*h.Name, ast.Pos{}); err != nil {
		return err
	}
	c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})

	c.useLabel(cleanupEnd)
	c.addLoadConst(nil, ast.Pos{})
	if err := c.nameOpStore(*h.Name, ast.Pos{}); err != nil {
		return err
	}
	if err := c.nameOpDelete(*h.Name, ast.Pos{}); err != nil {
		return err
	}
	c.addOpI(RERAISE, 1, ast.Pos{})
	return nil
}

// visitTryStarExcept emits the except* handler chain. Each handler
// runs CHECK_EG_MATCH and accumulates raised-or-unhandled exceptions
// into a list; CALL_INTRINSIC_2 PREP_RERAISE_STAR repackages the
// list into a single ExceptionGroup at the end.
//
// CPython: Python/codegen.c:L2607 codegen_try_star_except
func (c *Compiler) visitTryStarExcept(s *ast.TryStar) error {
	body := c.newLabel()
	exceptLbl := c.newLabel()
	orelse := c.newLabel()
	end := c.newLabel()
	cleanup := c.newLabel()
	reraiseStar := c.newLabel()
	l := loc(s)

	c.addOpJump(SETUP_FINALLY, exceptLbl, l)

	c.useLabel(body)
	c.pushFblock(fblockTryExcept, body, JumpTargetLabel{}, nil)
	if err := c.visitStmts(s.Body); err != nil {
		return err
	}
	if err := c.popFblock(fblockTryExcept); err != nil {
		return err
	}
	c.addOp(POP_BLOCK, ast.Pos{})
	c.addOpJump(JUMP_NO_INTERRUPT, orelse, ast.Pos{})

	c.useLabel(exceptLbl)
	c.addOpJump(SETUP_CLEANUP, cleanup, ast.Pos{})
	c.addOp(PUSH_EXC_INFO, ast.Pos{})
	c.pushFblock(fblockExceptionGroupHandler, JumpTargetLabel{}, JumpTargetLabel{}, "except handler")

	n := len(s.Handlers)
	for i, h := range s.Handlers {
		eh, ok := h.(*ast.ExceptHandler)
		if !ok {
			return fmt.Errorf("compile: unknown excepthandler %T", h)
		}
		if err := c.emitExceptStarHandler(eh, i, n, reraiseStar); err != nil {
			return err
		}
	}
	if err := c.popFblock(fblockExceptionGroupHandler); err != nil {
		return err
	}
	reraise := c.newLabel()

	c.useLabel(reraiseStar)
	c.addOpI(CALL_INTRINSIC_2, intrinsicPrepReraiseStar, ast.Pos{})
	c.addOpI(COPY, 1, ast.Pos{})
	c.addOpJump(POP_JUMP_IF_NOT_NONE, reraise, ast.Pos{})
	c.addOp(POP_TOP, ast.Pos{})
	c.addOp(POP_BLOCK, ast.Pos{})
	c.addOp(POP_EXCEPT, ast.Pos{})
	c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})

	c.useLabel(reraise)
	c.addOp(POP_BLOCK, ast.Pos{})
	c.addOpI(SWAP, 2, ast.Pos{})
	c.addOp(POP_EXCEPT, ast.Pos{})
	c.addOpI(RERAISE, 0, ast.Pos{})

	c.useLabel(cleanup)
	c.popExceptAndReraise()

	c.useLabel(orelse)
	if err := c.visitStmts(s.Orelse); err != nil {
		return err
	}

	c.useLabel(end)
	return nil
}

// emitExceptStarHandler writes one except* arm. Builds the
// raised-list bookkeeping on the first arm, runs CHECK_EG_MATCH,
// runs the body inside a SETUP_CLEANUP that clears the bound name,
// and chains to the next handler on no-match.
//
// CPython: Python/codegen.c codegen_try_star_except handler loop body
func (c *Compiler) emitExceptStarHandler(h *ast.ExceptHandler, i, n int, reraiseStar JumpTargetLabel) error {
	hl := loc(h)
	nextExcept := c.newLabel()
	exceptWithError := c.newLabel()
	noMatch := c.newLabel()
	if i == 0 {
		c.addOpI(BUILD_LIST, 0, hl)
		c.addOpI(COPY, 2, hl)
	}
	if h.Type != nil {
		if err := c.visitExpr(h.Type); err != nil {
			return err
		}
		c.addOp(CHECK_EG_MATCH, hl)
		c.addOpI(COPY, 1, hl)
		c.addOpJump(POP_JUMP_IF_NONE, noMatch, hl)
	}
	cleanupEnd := c.newLabel()
	cleanupBody := c.newLabel()
	if h.Name != nil {
		if err := c.nameOpStore(*h.Name, hl); err != nil {
			return err
		}
	} else {
		c.addOp(POP_TOP, hl)
	}
	c.addOpJump(SETUP_CLEANUP, cleanupEnd, hl)
	c.useLabel(cleanupBody)
	var datum any
	if h.Name != nil {
		datum = *h.Name
	}
	c.pushFblock(fblockHandlerCleanup, cleanupBody, JumpTargetLabel{}, datum)
	if err := c.visitStmts(h.Body); err != nil {
		return err
	}
	if err := c.popFblock(fblockHandlerCleanup); err != nil {
		return err
	}
	c.addOp(POP_BLOCK, ast.Pos{})
	if h.Name != nil {
		c.addLoadConst(nil, ast.Pos{})
		if err := c.nameOpStore(*h.Name, ast.Pos{}); err != nil {
			return err
		}
		if err := c.nameOpDelete(*h.Name, ast.Pos{}); err != nil {
			return err
		}
	}
	c.addOpJump(JUMP_NO_INTERRUPT, nextExcept, ast.Pos{})

	c.useLabel(cleanupEnd)
	if h.Name != nil {
		c.addLoadConst(nil, ast.Pos{})
		if err := c.nameOpStore(*h.Name, ast.Pos{}); err != nil {
			return err
		}
		if err := c.nameOpDelete(*h.Name, ast.Pos{}); err != nil {
			return err
		}
	}
	c.addOpI(LIST_APPEND, 3, ast.Pos{})
	c.addOp(POP_TOP, ast.Pos{})
	c.addOpJump(JUMP_NO_INTERRUPT, exceptWithError, ast.Pos{})

	c.useLabel(nextExcept)
	c.addOp(NOP, ast.Pos{})
	c.addOpJump(JUMP_NO_INTERRUPT, exceptWithError, ast.Pos{})

	c.useLabel(noMatch)
	c.addOp(POP_TOP, hl)

	c.useLabel(exceptWithError)
	if i == n-1 {
		c.addOpI(LIST_APPEND, 1, ast.Pos{})
		c.addOpJump(JUMP_NO_INTERRUPT, reraiseStar, ast.Pos{})
	}
	return nil
}
