// Port of cpython/Python/codegen.c codegen_jump_if (L1884-1976).
// Recursive helper that emits a conditional jump for a boolean test.
// Special-cases UnaryOp(Not, ...), BoolOp short-circuit, IfExp, and
// chained Compare; falls back to TO_BOOL + POP_JUMP_IF_X otherwise.

package compile

import "github.com/tamnd/gopy/ast"

// codegenJumpIf evaluates test, normalises the top of stack with
// TO_BOOL, and emits POP_JUMP_IF_TRUE or POP_JUMP_IF_FALSE to next.
// cond == true jumps when the test is true; cond == false jumps when
// the test is false (the if-statement's "skip body if test is
// false" case).
//
// CPython: Python/codegen.c:1884 codegen_jump_if
//
// 1:1 port of CPython's codegen_jump_if; the switch is one flat
// dispatch on expr kind to keep parity with the C source.
//
//nolint:gocognit,gocyclo // tracks the C function shape
func (c *Compiler) codegenJumpIf(test ast.Expr, next JumpTargetLabel, cond bool) error {
	switch e := test.(type) {
	case *ast.UnaryOp:
		if e.Op == ast.Not {
			return c.codegenJumpIf(e.Operand, next, !cond)
		}
		// fall through to general
	case *ast.BoolOp:
		values := e.Values
		n := len(values) - 1
		if n < 0 {
			break
		}
		cond2 := e.Op == ast.Or
		next2 := next
		if cond2 != cond {
			next2 = c.newLabel()
		}
		for i := range n {
			if err := c.codegenJumpIf(values[i], next2, cond2); err != nil {
				return err
			}
		}
		if err := c.codegenJumpIf(values[n], next, cond); err != nil {
			return err
		}
		if next2 != next {
			c.useLabel(next2)
		}
		return nil
	case *ast.IfExp:
		end := c.newLabel()
		next2 := c.newLabel()
		if err := c.codegenJumpIf(e.Test, next2, false); err != nil {
			return err
		}
		if err := c.codegenJumpIf(e.Body, next, cond); err != nil {
			return err
		}
		c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})
		c.useLabel(next2)
		if err := c.codegenJumpIf(e.Orelse, next, cond); err != nil {
			return err
		}
		c.useLabel(end)
		return nil
	case *ast.Compare:
		n := len(e.Ops) - 1
		if n > 0 {
			le := loc(e)
			cleanup := c.newLabel()
			if err := c.visitExpr(e.Left); err != nil {
				return err
			}
			for i := range n {
				if err := c.visitExpr(e.Comparators[i]); err != nil {
					return err
				}
				c.addOpI(SWAP, 2, le)
				c.addOpI(COPY, 2, le)
				c.emitCmpOp(e.Ops[i], le)
				c.addOp(TO_BOOL, le)
				c.addOpJump(POP_JUMP_IF_FALSE, cleanup, le)
			}
			if err := c.visitExpr(e.Comparators[n]); err != nil {
				return err
			}
			c.emitCmpOp(e.Ops[n], le)
			c.addOp(TO_BOOL, le)
			op := POP_JUMP_IF_FALSE
			if cond {
				op = POP_JUMP_IF_TRUE
			}
			c.addOpJump(op, next, le)
			end := c.newLabel()
			c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})
			c.useLabel(cleanup)
			c.addOp(POP_TOP, le)
			if !cond {
				c.addOpJump(JUMP_NO_INTERRUPT, next, ast.Pos{})
			}
			c.useLabel(end)
			return nil
		}
		// fall through to general
	}

	// general implementation
	le := loc(test)
	if err := c.visitExpr(test); err != nil {
		return err
	}
	c.addOp(TO_BOOL, le)
	op := POP_JUMP_IF_FALSE
	if cond {
		op = POP_JUMP_IF_TRUE
	}
	c.addOpJump(op, next, le)
	return nil
}
