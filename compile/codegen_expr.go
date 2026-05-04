// Port of cpython/Python/codegen.c expression visitors (L5172+).
//
// This file holds the dispatch and the leaf cases (Constant, Name).
// Other expression kinds (BoolOp, BinOp, Compare, Call, Lambda,
// comprehensions, etc.) live in sibling files.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitExpr dispatches on the concrete Expr type. Every kind must be
// handled or rejected with a clear error. The dispatch is split across
// two helpers (visitExprPrimary covers leaves + operators + containers
// + calls; visitExprMisc covers walrus / yield / await / f-strings) to
// keep each switch under the cyclomatic complexity threshold.
//
// CPython: Python/codegen.c:L5172 codegen_visit_expr
func (c *Compiler) visitExpr(e ast.Expr) error {
	if handled, err := c.visitExprPrimary(e); handled {
		return err
	}
	if handled, err := c.visitExprMisc(e); handled {
		return err
	}
	return fmt.Errorf("compile: expr kind %T not yet supported", e)
}

// visitExprPrimary handles leaves (Constant, Name), operators
// (BoolOp / BinOp / UnaryOp / Compare / IfExp), Lambda, container
// builders (List / Tuple / Set / Dict), member access (Attribute /
// Subscript / Slice), and Call. Split out of the dispatch switch to
// keep cyclomatic complexity below the project threshold.
//
// CPython: Python/codegen.c:L5172 codegen_visit_expr (first half)
func (c *Compiler) visitExprPrimary(e ast.Expr) (bool, error) {
	switch n := e.(type) {
	case *ast.Constant:
		c.visitConstant(n)
		return true, nil
	case *ast.Name:
		return true, c.visitName(n)
	case *ast.Lambda:
		return true, c.visitLambda(n)
	case *ast.BoolOp:
		return true, c.visitBoolOp(n)
	case *ast.BinOp:
		return true, c.visitBinOp(n)
	case *ast.UnaryOp:
		return true, c.visitUnaryOp(n)
	case *ast.Compare:
		return true, c.visitCompare(n)
	case *ast.IfExp:
		return true, c.visitIfExp(n)
	case *ast.List:
		return true, c.visitList(n)
	case *ast.Tuple:
		return true, c.visitTuple(n)
	case *ast.Set:
		return true, c.visitSet(n)
	case *ast.Dict:
		return true, c.visitDict(n)
	case *ast.Attribute:
		return true, c.visitAttribute(n)
	case *ast.Subscript:
		return true, c.visitSubscript(n)
	case *ast.Slice:
		return true, c.visitSlice(n)
	case *ast.Call:
		return true, c.visitCall(n)
	}
	return false, nil
}

// visitExprMisc handles walrus, yield / yield from, await, and the
// f-string nodes (JoinedStr / FormattedValue). Split out of the
// dispatch switch to keep cyclomatic complexity below the threshold.
//
// CPython: Python/codegen.c:L5172 codegen_visit_expr (second half)
func (c *Compiler) visitExprMisc(e ast.Expr) (bool, error) {
	switch n := e.(type) {
	case *ast.NamedExpr:
		return true, c.visitNamedExpr(n)
	case *ast.Yield:
		return true, c.visitYield(n)
	case *ast.YieldFrom:
		return true, c.visitYieldFrom(n)
	case *ast.Await:
		return true, c.visitAwait(n)
	case *ast.JoinedStr:
		return true, c.visitJoinedStr(n)
	case *ast.FormattedValue:
		return true, c.visitFormattedValue(n)
	case *ast.ListComp:
		return true, c.visitListComp(n)
	case *ast.SetComp:
		return true, c.visitSetComp(n)
	case *ast.DictComp:
		return true, c.visitDictComp(n)
	case *ast.GeneratorExp:
		return true, c.visitGeneratorExp(n)
	}
	return false, nil
}

// visitConstant emits LOAD_CONST. CPython's constant folding has
// already collapsed most of these by the time we get here; the
// remaining work is the dedup + LOAD_CONST emit.
//
// CPython: Python/codegen.c codegen_visit_expr Constant case
func (c *Compiler) visitConstant(e *ast.Constant) {
	c.addLoadConst(e.Value, loc(e))
}

// visitName routes Load context to nameOpLoad. Store and Del
// contexts are emitted by the parent (assignTo / deleteFrom) which
// call nameOpStore / nameOpDelete directly.
//
// CPython: Python/codegen.c:L3186 codegen_nameop
func (c *Compiler) visitName(e *ast.Name) error {
	switch e.Ctx {
	case ast.Load:
		return c.nameOpLoad(e.Id, loc(e))
	case ast.Store:
		return c.nameOpStore(e.Id, loc(e))
	case ast.Del:
		return c.nameOpDelete(e.Id, loc(e))
	}
	return fmt.Errorf("compile: Name with unknown context %v", e.Ctx)
}
