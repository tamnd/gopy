// Port of cpython/Python/codegen.c statement visitors (L2991-L3166).
//
// Per-kind visitors live alongside in codegen_stmt_*.go. This file
// holds the dispatch and the simple stmts (Pass, ExprStmt, Return,
// Assign, etc.).

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
	body := c.consumeDocstring(m.Body)
	if bodyHasAnnotations(body) {
		c.addOp(SETUP_ANNOTATIONS, ast.Pos{Lineno: 1})
	}
	if err := c.visitStmts(body); err != nil {
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

// bodyHasAnnotations reports whether body contains an AnnAssign whose
// target is a bare Name. SETUP_ANNOTATIONS is only worth emitting when
// at least one such assignment will populate __annotations__. Module
// and class bodies don't recurse into nested defs / classes (those have
// their own scope and __annotations__), so a shallow scan over the
// top-level statements matches CPython's ste_annotations_used flag.
//
// CPython: Python/symtable.c sets ste_annotations_used when an
// AnnAssign with a Name target is visited at module / class scope.
func bodyHasAnnotations(body ast.Seq[ast.Stmt]) bool {
	for _, s := range body {
		if a, ok := s.(*ast.AnnAssign); ok {
			if _, ok := a.Target.(*ast.Name); ok {
				return true
			}
		}
	}
	return false
}

// consumeDocstring inspects body[0] for a bare string literal. When
// found, it pins the string at the current unit's first const slot,
// sets the CoHasDocstring flag, and returns the body with that first
// statement skipped. Callers that want CPython-style __doc__ exposure
// must invoke this before the first const-emitting opcode so the
// docstring lands at index 0.
//
// CPython: Python/codegen.c:L868 codegen_body (the
// PyUnicode_CheckExact / CO_HAS_DOCSTRING block at body entry)
func (c *Compiler) consumeDocstring(body ast.Seq[ast.Stmt]) ast.Seq[ast.Stmt] {
	if len(body) == 0 {
		return body
	}
	es, ok := body[0].(*ast.ExprStmt)
	if !ok {
		return body
	}
	con, ok := es.Value.(*ast.Constant)
	if !ok {
		return body
	}
	s, ok := con.Value.(string)
	if !ok {
		return body
	}
	// Pin the docstring at consts[0] and set the flag. Callers must
	// have just opened the unit so no other const has been registered.
	c.constIndex(s)
	c.unit().Flags |= CoHasDocstring
	return body[1:]
}

// visitStmt dispatches on the concrete Stmt type. Every stmt kind
// must be handled or rejected with a clear error. Mirrors
// codegen_visit_stmt.
//
// CPython: Python/codegen.c:L2991 codegen_visit_stmt
func (c *Compiler) visitStmt(s ast.Stmt) error {
	if handled, err := c.visitStmtSimple(s); handled {
		return err
	}
	if handled, err := c.visitStmtBlock(s); handled {
		return err
	}
	return fmt.Errorf("compile: stmt kind %T not yet supported", s)
}

// visitStmtSimple handles the leaf statements: Pass, ExprStmt,
// Return, Assign, AugAssign, AnnAssign, Delete, Raise, Assert,
// Import, ImportFrom, Global, Nonlocal. Split out of the dispatch
// switch to keep cyclomatic complexity below the threshold.
//
// CPython: Python/codegen.c codegen_visit_stmt (leaf cases)
func (c *Compiler) visitStmtSimple(s ast.Stmt) (bool, error) {
	switch n := s.(type) {
	case *ast.Pass:
		c.visitPass(n)
		return true, nil
	case *ast.ExprStmt:
		return true, c.visitExprStmt(n)
	case *ast.Return:
		return true, c.visitReturn(n)
	case *ast.Assign:
		return true, c.visitAssign(n)
	case *ast.AugAssign:
		return true, c.visitAugAssign(n)
	case *ast.AnnAssign:
		return true, c.visitAnnAssign(n)
	case *ast.Delete:
		return true, c.visitDelete(n)
	case *ast.Raise:
		return true, c.visitRaise(n)
	case *ast.Assert:
		return true, c.visitAssert(n)
	case *ast.Import:
		return true, c.visitImport(n)
	case *ast.ImportFrom:
		return true, c.visitImportFrom(n)
	case *ast.Global, *ast.Nonlocal:
		// Already lifted by symtable.
		return true, nil
	}
	return false, nil
}

// visitStmtBlock handles compound statements: control flow (If, For,
// While, Break, Continue) and the def-like nodes (FunctionDef,
// AsyncFunctionDef, ClassDef). Split out of the dispatch switch to
// keep cyclomatic complexity below the threshold.
//
// CPython: Python/codegen.c codegen_visit_stmt (compound cases)
func (c *Compiler) visitStmtBlock(s ast.Stmt) (bool, error) {
	switch n := s.(type) {
	case *ast.If:
		return true, c.visitIf(n)
	case *ast.While:
		return true, c.visitWhile(n)
	case *ast.For:
		return true, c.visitFor(n)
	case *ast.Break:
		return true, c.visitBreak(n)
	case *ast.Continue:
		return true, c.visitContinue(n)
	case *ast.FunctionDef:
		return true, c.visitFunctionDef(n)
	case *ast.AsyncFunctionDef:
		return true, c.visitAsyncFunctionDef(n)
	case *ast.ClassDef:
		return true, c.visitClassDef(n)
	case *ast.With:
		return true, c.visitWith(n)
	case *ast.AsyncWith:
		return true, c.visitAsyncWith(n)
	case *ast.Try:
		return true, c.visitTry(n)
	case *ast.TryStar:
		return true, c.visitTryStar(n)
	case *ast.Match:
		return true, c.visitMatch(n)
	case *ast.TypeAlias:
		return true, c.visitTypeAlias(n)
	}
	return false, nil
}

// visitPass emits nothing. CPython does emit a NOP if the optimisation
// level is below 2 so debuggers can break on the line; the flowgraph
// removes redundant NOPs in pass-1.
//
// CPython: Python/codegen.c codegen_visit_stmt Pass case
func (c *Compiler) visitPass(s *ast.Pass) {
	c.addOp(NOP, loc(s))
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
// The value to store must already be on top of the stack; the helper
// picks the right opcode by target kind:
//
//	Name       -> STORE_FAST / STORE_NAME / STORE_DEREF / STORE_GLOBAL
//	Attribute  -> evaluate object; STORE_ATTR
//	Subscript  -> evaluate object + index; STORE_SUBSCR
//	Tuple/List -> UNPACK_SEQUENCE n (or UNPACK_EX before|after) then
//	              recurse into the elements
//	Starred    -> unwrap to inner target (only legal inside Tuple/List
//	              unpack)
//
// CPython: Python/codegen.c codegen_nameop with ctx=Store, plus the
// per-target Tuple / List / Starred / Attribute / Subscript paths.
func (c *Compiler) assignTo(target ast.Expr, l ast.Pos) error {
	switch t := target.(type) {
	case *ast.Name:
		return c.nameOpStore(t.Id, l)
	case *ast.Attribute:
		return c.visitAttribute(t)
	case *ast.Subscript:
		return c.visitSubscript(t)
	case *ast.Tuple:
		return c.assignToSequence(t.Elts, l)
	case *ast.List:
		return c.assignToSequence(t.Elts, l)
	case *ast.Starred:
		// A bare Starred outside a Tuple/List target is a syntax
		// error caught earlier; if we get here we just store into the
		// inner target.
		return c.assignTo(t.Value, l)
	}
	return fmt.Errorf("compile: assign target %T not supported", target)
}

// assignToSequence emits UNPACK_SEQUENCE / UNPACK_EX for tuple- or
// list-target assignments, then walks the elements and emits each
// element's store sequence in order (UNPACK pushes the elements with
// the first one on top of the stack).
//
// CPython: Python/codegen.c codegen_unpack_helper
func (c *Compiler) assignToSequence(elts ast.Seq[ast.Expr], l ast.Pos) error {
	n := len(elts)
	starIdx := -1
	for i, e := range elts {
		if _, ok := e.(*ast.Starred); ok {
			if starIdx >= 0 {
				return fmt.Errorf("compile: multiple starred expressions in assignment")
			}
			starIdx = i
		}
	}
	if starIdx < 0 {
		c.addOpI(UNPACK_SEQUENCE, int32(n), l)
	} else {
		countBefore := starIdx
		countAfter := n - starIdx - 1
		// CPython packs (after << 8) | before into the oparg.
		c.addOpI(UNPACK_EX, int32((countAfter<<8)|countBefore), l)
	}
	for _, e := range elts {
		if err := c.assignTo(e, l); err != nil {
			return err
		}
	}
	return nil
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
