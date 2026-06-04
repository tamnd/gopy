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
	docstring, docLoc, hasDoc := moduleDocstring(m.Body)
	body := c.consumeDocstring(m.Body)
	// CPython codegen_body emits LOAD_CONST <docstring> / STORE_NAME
	// __doc__ at module entry whenever the symtable flagged a
	// docstring. consumeDocstring above pins the string at consts[0];
	// here we surface it on the module as __doc__ so attribute access
	// matches CPython.
	//
	// CPython: Python/codegen.c:895 codegen_body (ADDOP_LOAD_CONST +
	// codegen_nameop(__doc__, Store))
	if hasDoc {
		pool := poolNames
		c.addLoadConst(docstring, docLoc)
		c.addOpName(STORE_NAME, &pool, "__doc__", ast.Pos{Lineno: -1})
	}
	// PEP 649: module annotations are deferred. visitAnnAssign records
	// each annotation into the unit's DeferredAnnotations slice.
	// After the body, annotation setup code (__conditional_annotations__
	// + __annotate__) is emitted into a separate stash sequence that
	// cfgFromSequence prepends at the start of the body, so __annotate__
	// is available before any body statement executes. This matches
	// CPython's _PyCompile_StartAnnotationSetup /
	// _PyCompile_EndAnnotationSetup stash.
	//
	// CPython: Python/compile.c _PyCompile_StartAnnotationSetup (L739)
	// CPython: Python/flowgraph.c:3946 _PyCfg_FromInstructionSequence
	if err := c.visitStmts(body); err != nil {
		return err
	}
	if err := c.stashAnnotationCode(ast.Pos{Lineno: 1}); err != nil {
		return err
	}
	c.addReturnNoneIfMissing(ast.Pos{Lineno: -1})
	return nil
}

// visitInteractive is the REPL form. Every expression statement at
// module nest level (including those nested inside `with`, `if`,
// `for`, ... bodies) emits PRINT_EXPR so the interactive shell sees
// the result via sys.displayhook. visitExprStmt consults
// c.interactive plus the unit-stack depth to gate the opcode;
// nested function / class bodies push the depth past the module
// level and revert to plain POP_TOP.
//
// CPython: Python/codegen.c codegen_body branch is_interactive=true
// CPython: Python/codegen.c codegen_stmt_expr (c->c_interactive &&
// c->c_nestlevel <= 1)
func (c *Compiler) visitInteractive(m *ast.Interactive) error {
	prev := c.interactive
	c.interactive = true
	defer func() { c.interactive = prev }()
	for _, s := range m.Body {
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

// moduleDocstring extracts the leading bare-string docstring (if any)
// without touching the unit's const pool. The module emitter uses it
// to capture the docstring value and source location before
// consumeDocstring runs, so it can emit the matching LOAD_CONST +
// STORE_NAME __doc__ pair at module entry.
//
// CPython: Python/codegen.c:883 codegen_body (_PyAST_GetDocString +
// LOC(st->v.Expr.value))
func moduleDocstring(body ast.Seq[ast.Stmt]) (string, ast.Pos, bool) {
	if len(body) == 0 {
		return "", ast.Pos{}, false
	}
	es, ok := body[0].(*ast.ExprStmt)
	if !ok {
		return "", ast.Pos{}, false
	}
	con, ok := es.Value.(*ast.Constant)
	if !ok {
		return "", ast.Pos{}, false
	}
	s, ok := con.Value.(string)
	if !ok {
		return "", ast.Pos{}, false
	}
	return s, loc(con), true
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
	case *ast.AsyncFor:
		return true, c.visitAsyncFor(n)
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
	// REPL single-mode: print the value at the module nest level so
	// sys.displayhook fires for top-level expressions even inside
	// compound bodies (`with`, `if`, ...). Nested function / class
	// scopes push the unit stack past 1 and stay on POP_TOP.
	//
	// CPython: Python/codegen.c codegen_stmt_expr (c->c_interactive &&
	// c->c_nestlevel <= 1 emits CALL_INTRINSIC_1 INTRINSIC_PRINT)
	if c.interactive && len(c.units) <= 1 {
		// CALL_INTRINSIC_1 INTRINSIC_PRINT consumes the value and
		// leaves the result of sys.displayhook (None) on the stack.
		// Pop it so the body's net stack effect matches the plain
		// POP_TOP form.
		//
		// CPython: Python/codegen.c codegen_stmt_expr (PRINT_EXPR
		// branch emits the intrinsic followed by POP_TOP)
		c.addOpI(CALL_INTRINSIC_1, intrinsicPrint, loc(s))
		c.addOp(POP_TOP, loc(s))
		return nil
	}
	c.addOp(POP_TOP, loc(s))
	return nil
}

// visitReturn emits LOAD_CONST None for bare return, otherwise
// evaluates the value, then RETURN_VALUE. Generators handle return
// differently (RETURN_VALUE in a generator raises StopIteration with
// the value); that path lands when generators land.
//
// CPython: Python/codegen.c:2191 codegen_return
func (c *Compiler) visitReturn(s *ast.Return) error {
	l := loc(s)
	if c.scope == nil || !c.scope.IsFunctionLike() {
		return c.errorAt(l, "'return' outside function")
	}
	// `return value` inside an async generator (async def that also
	// yields) is forbidden, mirroring CPython's codegen_return check.
	//
	// CPython: Python/codegen.c:2201 codegen_return
	if s.Value != nil && c.scope.Coroutine && c.scope.Generator {
		return c.errorAt(l, "'return' with value in async generator")
	}
	_, valueIsConst := s.Value.(*ast.Constant)
	preserveTOS := s.Value != nil && !valueIsConst

	if preserveTOS {
		if err := c.visitExpr(s.Value); err != nil {
			return err
		}
	} else if s.Value != nil {
		l = loc(s.Value)
		c.addOp(NOP, l)
	}
	if s.Value == nil || loc(s.Value).Lineno != loc(s).Lineno {
		l = loc(s)
		c.addOp(NOP, l)
	}
	c.unwindForReturn(preserveTOS, l)
	if s.Value == nil {
		c.addLoadConst(nil, l)
	} else if !preserveTOS {
		c.addLoadConst(s.Value.(*ast.Constant).Value, l)
	}
	c.addOp(RETURN_VALUE, l)
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
		if err := c.assignTo(target, loc(target)); err != nil {
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
		// A bare Starred outside a Tuple/List target is invalid: only
		// `a, *b = x` or `[*b] = x` shapes are legal. CPython's
		// codegen_visit_expr Starred(Store) branch raises here too.
		//
		// CPython: Python/codegen.c:5301 codegen_visit_expr (Starred_kind)
		return c.errorAt(l, "starred assignment target must be in a list or tuple")
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
				return c.errorAt(l, "multiple starred expressions in assignment")
			}
			starIdx = i
		}
	}
	if starIdx < 0 {
		c.addOpI(UNPACK_SEQUENCE, int32(n), l)
	} else {
		countBefore := starIdx
		countAfter := n - starIdx - 1
		// The oparg packs (countAfter << 8) | countBefore, so each half
		// must fit in 8 / 24 bits. CPython raises before emitting the
		// instruction; otherwise the runtime hits an invalid CFG.
		//
		// CPython: Python/codegen.c:3398 unpack_helper
		if countBefore >= (1<<8) || countAfter >= (1<<24) {
			return c.errorAt(l, "too many expressions in star-unpacking assignment")
		}
		c.addOpI(UNPACK_EX, int32((countAfter<<8)|countBefore), l)
	}
	for _, e := range elts {
		inner := e
		if star, ok := e.(*ast.Starred); ok {
			inner = star.Value
		}
		if err := c.assignTo(inner, loc(inner)); err != nil {
			return err
		}
	}
	return nil
}

// addReturnNoneIfMissing unconditionally emits LOAD_CONST None /
// RETURN_VALUE at the current sequence tail. The CPython routine notes
// "this also ensures that no jump target offsets are out of bounds":
// every labeled position past the last codegen-visible statement (for
// example the merge label after an if-then with an early return) needs
// at least one instruction to bind to, so the if-statement's
// post-merge label resolves inside the sequence instead of past its
// end.
//
// CPython: Python/codegen.c:6475 _PyCodegen_AddReturnAtEnd
func (c *Compiler) addReturnNoneIfMissing(l ast.Pos) {
	c.addLoadConst(nil, l)
	c.addOp(RETURN_VALUE, l)
}
