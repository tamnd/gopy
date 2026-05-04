// Port of cpython/Python/codegen.c statement visitors (L2991-L3166).
// Spec: notes/Spec/1600/1626_gopy_codegen.md
//
// Per-kind visitors live alongside in codegen_stmt_*.go. This file
// holds the dispatch and the simple stmts (Pass, ExprStmt, Return,
// Assign, etc.). Larger constructs (functions, classes, control
// flow, with, match, try) get their own files in later steps.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitModule emits the module's top-level body. CPython treats the
// module as a function with no args that runs once, so it ends with
// LOAD_CONST None / RETURN_VALUE if the body does not already.
//
// CPython: Python/codegen.c:L868 codegen_body called from
// _PyCodegen_Module
func (c *Compiler) visitModule(m *ast.Module) error {
	if err := c.visitStmts(m.Body); err != nil {
		return err
	}
	c.addReturnNoneIfMissing(ast.Pos{Lineno: -1})
	return nil
}

// visitInteractive is the REPL form. Each top-level expression is
// printed via PRINT_EXPR; everything else is the same as Module.
//
// CPython: Python/codegen.c codegen_body branch is_interactive=true
func (c *Compiler) visitInteractive(m *ast.Interactive) error {
	for _, s := range m.Body {
		if es, ok := s.(*ast.ExprStmt); ok {
			if err := c.visitExpr(es.Value); err != nil {
				return err
			}
			// PRINT_EXPR was folded into CALL_INTRINSIC_1 with
			// intrinsic id 1 (INTRINSIC_PRINT) in 3.12+.
			//
			// CPython: Include/internal/pycore_intrinsics.h
			// INTRINSIC_PRINT
			c.addOpI(CALL_INTRINSIC_1, intrinsicPrint, loc(es))
			continue
		}
		if err := c.visitStmt(s); err != nil {
			return err
		}
	}
	c.addReturnNoneIfMissing(ast.Pos{Lineno: -1})
	return nil
}

// visitExpressionMod is the eval-mode form: a single expression that
// becomes the return value.
//
// CPython: Python/codegen.c _PyCodegen_Expression
func (c *Compiler) visitExpressionMod(m *ast.Expression) error {
	if err := c.visitExpr(m.Body); err != nil {
		return err
	}
	c.addOp(RETURN_VALUE, loc(m.Body))
	return nil
}

// visitStmts walks a body. CPython: codegen_body lifts the docstring
// out of the loop; we follow the same shape so docstring handling
// has one well-known location.
//
// CPython: Python/codegen.c:L868 codegen_body
func (c *Compiler) visitStmts(stmts ast.Seq[ast.Stmt]) error {
	for _, s := range stmts {
		if err := c.visitStmt(s); err != nil {
			return err
		}
	}
	return nil
}

// visitStmt dispatches on the concrete Stmt type. Every stmt kind
// must be handled or rejected with a clear error. Mirrors
// codegen_visit_stmt.
//
// CPython: Python/codegen.c:L2991 codegen_visit_stmt
func (c *Compiler) visitStmt(s ast.Stmt) error {
	switch n := s.(type) {
	case *ast.Pass:
		return c.visitPass(n)
	case *ast.ExprStmt:
		return c.visitExprStmt(n)
	case *ast.Return:
		return c.visitReturn(n)
	case *ast.Assign:
		return c.visitAssign(n)
	case *ast.If:
		return c.visitIf(n)
	case *ast.While:
		return c.visitWhile(n)
	case *ast.For:
		return c.visitFor(n)
	case *ast.Break:
		return c.visitBreak(n)
	case *ast.Continue:
		return c.visitContinue(n)
	case *ast.FunctionDef:
		return c.visitFunctionDef(n)
	case *ast.AsyncFunctionDef:
		return c.visitAsyncFunctionDef(n)
	case *ast.Delete:
		return c.visitDelete(n)
	case *ast.AugAssign:
		return c.visitAugAssign(n)
	case *ast.AnnAssign:
		return c.visitAnnAssign(n)
	case *ast.Raise:
		return c.visitRaise(n)
	case *ast.Assert:
		return c.visitAssert(n)
	case *ast.Import:
		return c.visitImport(n)
	case *ast.ImportFrom:
		return c.visitImportFrom(n)
	case *ast.Global, *ast.Nonlocal:
		// Already lifted by symtable.
		return nil
	}
	return fmt.Errorf("compile: stmt kind %T not yet supported", s)
}

// visitPass emits nothing. CPython does emit a NOP if the optimisation
// level is below 2 so debuggers can break on the line; the flowgraph
// removes redundant NOPs in pass-1.
//
// CPython: Python/codegen.c codegen_visit_stmt Pass case
func (c *Compiler) visitPass(s *ast.Pass) error {
	c.addOp(NOP, loc(s))
	return nil
}

// visitExprStmt evaluates the expression and discards the result.
// Bare-string docstrings at the top of a body are not popped: they
// land in co_consts[0] and the wrapping CO_HAS_DOCSTRING flag tells
// the runtime to surface them as __doc__.
//
// CPython: Python/codegen.c:L2962 codegen_stmt_expr
func (c *Compiler) visitExprStmt(s *ast.ExprStmt) error {
	if err := c.visitExpr(s.Value); err != nil {
		return err
	}
	c.addOp(POP_TOP, loc(s))
	return nil
}

// visitReturn emits LOAD_CONST None for bare return, otherwise
// evaluates the value, then RETURN_VALUE. Generators handle return
// differently (RETURN_VALUE in a generator raises StopIteration with
// the value); that path lands when generators land.
//
// CPython: Python/codegen.c:L2191 codegen_return
func (c *Compiler) visitReturn(s *ast.Return) error {
	if s.Value == nil {
		c.addLoadConst(nil, loc(s))
	} else {
		if err := c.visitExpr(s.Value); err != nil {
			return err
		}
	}
	c.addOp(RETURN_VALUE, loc(s))
	return nil
}

// visitAssign handles `a = expr` and tuple-unpack `a, b = expr`. The
// value is evaluated once; for multiple targets the value is
// duplicated with COPY for each extra target. Then each target gets
// its own store sequence.
//
// CPython: Python/codegen.c codegen_visit_stmt Assign case
func (c *Compiler) visitAssign(s *ast.Assign) error {
	if err := c.visitExpr(s.Value); err != nil {
		return err
	}
	for i := len(s.Targets) - 1; i > 0; i-- {
		c.addOpI(COPY, 1, loc(s))
	}
	for _, target := range s.Targets {
		if err := c.assignTo(target, loc(s)); err != nil {
			return err
		}
	}
	return nil
}

// assignTo emits the store side of an assignment for one target.
// Only Name targets are supported in the skeleton; tuple/list/star
// unpack and attribute/subscript stores land alongside the
// per-visitor work in later steps.
//
// CPython: Python/codegen.c codegen_nameop with ctx=Store, plus the
// per-target Tuple / List / Starred / Attribute / Subscript paths.
func (c *Compiler) assignTo(target ast.Expr, l ast.Pos) error {
	if t, ok := target.(*ast.Name); ok {
		return c.nameOpStore(t.Id, l)
	}
	return fmt.Errorf("compile: assign target %T not yet supported", target)
}

// addReturnNoneIfMissing emits LOAD_CONST None / RETURN_VALUE if the
// last instruction is not already a RETURN_VALUE. Modules and bare
// function bodies need this fall-through.
//
// CPython: Python/codegen.c:L6473 _PyCodegen_AddReturnAtEnd
func (c *Compiler) addReturnNoneIfMissing(l ast.Pos) {
	seq := c.seq()
	if n := len(seq.Instrs); n > 0 && seq.Instrs[n-1].Op == RETURN_VALUE {
		return
	}
	c.addLoadConst(nil, l)
	c.addOp(RETURN_VALUE, l)
}
