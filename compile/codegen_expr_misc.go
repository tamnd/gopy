// Port of cpython/Python/codegen.c expression visitors for the
// remaining kinds: NamedExpr, Yield, YieldFrom, Await, JoinedStr,
// FormattedValue, Starred (load context errors).

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitNamedExpr handles `target := value`. It evaluates value,
// duplicates it (so the walrus expression itself returns it), and
// stores it into target.
//
// CPython: Python/codegen.c NamedExpr case in visit_expr
func (c *Compiler) visitNamedExpr(e *ast.NamedExpr) error {
	if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	c.addOpI(COPY, 1, loc(e))
	return c.assignTo(e.Target, loc(e))
}

// visitYield emits YIELD_VALUE. Bare `yield` puts None on the stack.
// The enclosing scope must be function-like; module / class scope
// rejects yield with a SyntaxError at compile time.
//
// CPython: Python/codegen.c:5223 codegen_yield
func (c *Compiler) visitYield(e *ast.Yield) error {
	l := loc(e)
	if c.scope == nil || !c.scope.IsFunctionLike() {
		return c.errorAt(l, "'yield' outside function")
	}
	if e.Value == nil {
		c.addLoadConst(nil, l)
	} else if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	c.addOpI(YIELD_VALUE, 0, l)
	return nil
}

// visitYieldFrom lowers `yield from x` to GET_YIELD_FROM_ITER plus a
// SEND loop that drives the inner iterator.
//
// CPython: Python/codegen.c:5235 codegen_add_yield_from
func (c *Compiler) visitYieldFrom(e *ast.YieldFrom) error {
	if c.scope == nil || !c.scope.IsFunctionLike() {
		return c.errorAt(loc(e), "'yield from' outside function")
	}
	if c.scope.Coroutine {
		return c.errorAt(loc(e), "'yield from' inside async function")
	}
	if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	c.addOp(GET_YIELD_FROM_ITER, loc(e))
	c.addLoadConst(nil, loc(e))
	loop := c.newLabel()
	end := c.newLabel()
	c.useLabel(loop)
	c.addOpJump(SEND, end, loc(e))
	c.addOpI(YIELD_VALUE, 0, loc(e))
	c.addOpI(RESUME, 2, loc(e))
	c.addOpJump(JUMP_BACKWARD, loop, loc(e))
	c.useLabel(end)
	c.addOp(END_SEND, loc(e))
	return nil
}

// visitAwait emits the await sequence. CPython lowers this to
// GET_AWAITABLE 0 / LOAD_CONST None / SEND / YIELD / RESUME 3 loop.
//
// CPython: Python/codegen.c Await case in visit_expr
func (c *Compiler) visitAwait(e *ast.Await) error {
	if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	c.addOpI(GET_AWAITABLE, 0, loc(e))
	c.addLoadConst(nil, loc(e))
	loop := c.newLabel()
	end := c.newLabel()
	c.useLabel(loop)
	c.addOpJump(SEND, end, loc(e))
	c.addOpI(YIELD_VALUE, 1, loc(e))
	c.addOpI(RESUME, 3, loc(e))
	c.addOpJump(JUMP_BACKWARD, loop, loc(e))
	c.useLabel(end)
	c.addOp(END_SEND, loc(e))
	return nil
}

// visitTemplateStr lowers a t-string (PEP 750). Values alternate between
// Constant string parts and Interpolation nodes. The compiler pushes a
// strings tuple and an interpolations tuple then emits BUILD_TEMPLATE.
// Interpolations are not yet supported; attempting them returns an error.
//
// CPython: Python/codegen.c codegen_templatestr
func (c *Compiler) visitTemplateStr(e *ast.TemplateStr) error {
	var strParts []any
	var interps []any
	for i := 0; i < e.Values.Len(); i++ {
		v := e.Values.Get(i)
		switch n := v.(type) {
		case *ast.Constant:
			if s, ok := n.Value.(string); ok {
				strParts = append(strParts, s)
			}
		case *ast.Interpolation:
			_ = n
			if len(strParts) == len(interps) {
				strParts = append(strParts, "")
			}
			interps = append(interps, nil)
		}
	}
	if len(strParts) == len(interps) {
		strParts = append(strParts, "")
	}
	if len(interps) > 0 {
		return fmt.Errorf("compile: t-string interpolations not yet supported")
	}
	c.addLoadConst(tupleOf(strParts), loc(e))
	c.addLoadConst(tupleOf(interps), loc(e))
	c.addOp(BUILD_TEMPLATE, loc(e))
	return nil
}

// visitJoinedStr lowers an f-string. Each Value is either a string
// literal (Constant) emitted as LOAD_CONST or a FormattedValue node
// emitted via visitFormattedValue. After the parts are stacked,
// BUILD_STRING joins them.
//
// CPython: Python/codegen.c:L4104 codegen_joinedstr
func (c *Compiler) visitJoinedStr(e *ast.JoinedStr) error {
	for _, v := range e.Values {
		if err := c.visitExpr(v); err != nil {
			return err
		}
	}
	c.addOpI(BUILD_STRING, int32(len(e.Values)), loc(e))
	return nil
}

// visitFormattedValue lowers `{value!c:spec}`. The conversion field
// applies !s / !r / !a via CONVERT_VALUE; FORMAT_SIMPLE handles the
// common "no spec" path; FORMAT_WITH_SPEC takes a format spec on top.
//
// CPython: Python/codegen.c:L4165 codegen_formatted_value
func (c *Compiler) visitFormattedValue(e *ast.FormattedValue) error {
	if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	if e.Conversion > 0 {
		conv, err := convertKind(e.Conversion)
		if err != nil {
			return err
		}
		c.addOpI(CONVERT_VALUE, conv, loc(e))
	}
	if e.FormatSpec != nil {
		if err := c.visitExpr(e.FormatSpec); err != nil {
			return err
		}
		c.addOp(FORMAT_WITH_SPEC, loc(e))
	} else {
		c.addOp(FORMAT_SIMPLE, loc(e))
	}
	return nil
}

// convertKind maps Conversion field values to CONVERT_VALUE oparg.
// CPython: Python/codegen.c uses ASCII chars; we map the same set.
func convertKind(conv int) (int32, error) {
	switch conv {
	case 's':
		return 1, nil
	case 'r':
		return 2, nil
	case 'a':
		return 3, nil
	}
	return 0, fmt.Errorf("compile: unknown FormattedValue conversion %d", conv)
}
