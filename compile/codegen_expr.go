// Port of cpython/Python/codegen.c expression visitors (L5172+).
// Spec: notes/Spec/1600/1626_gopy_codegen.md
//
// This file holds the dispatch and the leaf cases (Constant, Name).
// Other expression kinds (BoolOp, BinOp, Compare, Call, Lambda,
// comprehensions, etc.) get their own files in later steps.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitExpr dispatches on the concrete Expr type. Every kind must be
// handled or rejected with a clear error.
//
// CPython: Python/codegen.c:L5172 codegen_visit_expr
func (c *Compiler) visitExpr(e ast.Expr) error {
	switch n := e.(type) {
	case *ast.Constant:
		return c.visitConstant(n)
	case *ast.Name:
		return c.visitName(n)
	}
	return fmt.Errorf("compile: expr kind %T not yet supported", e)
}

// visitConstant emits LOAD_CONST. CPython's constant folding has
// already collapsed most of these by the time we get here; the
// remaining work is the dedup + LOAD_CONST emit.
//
// CPython: Python/codegen.c codegen_visit_expr Constant case
func (c *Compiler) visitConstant(e *ast.Constant) error {
	c.addLoadConst(e.Value, loc(e))
	return nil
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
