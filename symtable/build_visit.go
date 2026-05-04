package symtable

import (
	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
)

// visitStmt dispatches to the per-kind visitor. Mirrors
// symtable_visit_stmt's switch.
//
// CPython: Python/symtable.c:L1847 symtable_visit_stmt
func (b *builder) visitStmt(s ast.Stmt) error {
	if handled, err := b.visitStmtDef(s); handled {
		return err
	}
	if handled, err := b.visitStmtControl(s); handled {
		return err
	}
	return b.visitStmtSimple(s)
}

// visitStmtDef handles statements that introduce a new binding or
// scope: def, async def, class, type alias, plain Assign / AnnAssign,
// import / global / nonlocal.
func (b *builder) visitStmtDef(s ast.Stmt) (bool, error) {
	switch n := s.(type) {
	case *ast.FunctionDef, *ast.AsyncFunctionDef:
		return true, b.visitFunctionLike(n)
	case *ast.ClassDef:
		return true, b.visitClassDef(n)
	case *ast.TypeAlias:
		return true, b.visitTypeAlias(n)
	case *ast.Assign:
		return true, b.visitAssign(n)
	case *ast.AnnAssign:
		return true, b.visitAnnAssign(n)
	case *ast.Import:
		for _, a := range n.Names {
			if err := b.visitAlias(a); err != nil {
				return true, err
			}
		}
		return true, nil
	case *ast.ImportFrom:
		for _, a := range n.Names {
			if err := b.visitAlias(a); err != nil {
				return true, err
			}
		}
		return true, b.checkImportFrom(n)
	case *ast.Global:
		return true, b.visitGlobal(n)
	case *ast.Nonlocal:
		return true, b.visitNonlocal(n)
	}
	return false, nil
}

// visitStmtControl handles control-flow statements.
func (b *builder) visitStmtControl(s ast.Stmt) (bool, error) {
	switch n := s.(type) {
	case *ast.For:
		return true, b.visitForLike(n.Target, n.Iter, n.Body, n.Orelse, false, n.Pos)
	case *ast.AsyncFor:
		return true, b.visitForLike(n.Target, n.Iter, n.Body, n.Orelse, true, n.Pos)
	case *ast.While:
		return true, b.visitWhile(n)
	case *ast.If:
		return true, b.visitIf(n)
	case *ast.Match:
		return true, b.visitMatch(n)
	case *ast.Try:
		return true, b.visitTryLike(n.Body, n.Handlers, n.Orelse, n.Finalbody)
	case *ast.TryStar:
		return true, b.visitTryLike(n.Body, n.Handlers, n.Orelse, n.Finalbody)
	case *ast.With:
		return true, b.visitWithLike(n.Items, n.Body, false, n.Pos)
	case *ast.AsyncWith:
		return true, b.visitWithLike(n.Items, n.Body, true, n.Pos)
	case *ast.Raise:
		return true, b.visitRaise(n)
	case *ast.Return:
		return true, b.visitReturn(n)
	}
	return false, nil
}

// visitStmtSimple handles leaf statements that recurse only into
// expressions.
func (b *builder) visitStmtSimple(s ast.Stmt) error {
	switch n := s.(type) {
	case *ast.Delete:
		return b.visitExprSeq(n.Targets)
	case *ast.AugAssign:
		if err := b.visitExpr(n.Target); err != nil {
			return err
		}
		return b.visitExpr(n.Value)
	case *ast.Assert:
		return b.visitAssert(n)
	case *ast.ExprStmt:
		return b.visitExpr(n.Value)
	}
	return nil
}

// funcLike captures the shared shape of FunctionDef and
// AsyncFunctionDef so visitFunctionLike doesn't need a switch in two
// places.
type funcLike struct {
	name       string
	args       *ast.Arguments
	body       ast.Seq[ast.Stmt]
	decorators ast.Seq[ast.Expr]
	returns    ast.Expr
	typeParams ast.Seq[ast.TypeParam]
	loc        ast.Pos
	isAsync    bool
}

func unpackFuncLike(s ast.Stmt) funcLike {
	switch n := s.(type) {
	case *ast.FunctionDef:
		return funcLike{n.Name, n.Args, n.Body, n.DecoratorList, n.Returns, n.TypeParams, n.Pos, false}
	case *ast.AsyncFunctionDef:
		return funcLike{n.Name, n.Args, n.Body, n.DecoratorList, n.Returns, n.TypeParams, n.Pos, true}
	}
	return funcLike{}
}

// visitFunctionLike handles both FunctionDef and AsyncFunctionDef.
// CPython repeats the body twice; we factor it once.
//
// CPython: Python/symtable.c:L1852 / L2208
func (b *builder) visitFunctionLike(s ast.Stmt) error {
	f := unpackFuncLike(s)
	if err := b.addDef(f.name, DefLocal, f.loc); err != nil {
		return err
	}
	if err := b.visitExprSeq(f.args.Defaults); err != nil {
		return err
	}
	if err := b.visitExprSeqMaybeNull(f.args.KwDefaults); err != nil {
		return err
	}
	if err := b.visitExprSeq(f.decorators); err != nil {
		return err
	}
	if len(f.typeParams) > 0 {
		if err := b.enterTypeParamBlock(f.name, s, f.typeParams, len(f.args.Defaults) > 0, hasKwOnlyDefaults(f.args), false, f.loc); err != nil {
			return err
		}
		for _, tp := range f.typeParams {
			if err := b.visitTypeParam(tp); err != nil {
				return err
			}
		}
	}
	newSte := b.makeFunctionEntry(&f, s)
	if err := b.visitAnnotations(s, f.args, f.returns, newSte); err != nil {
		return err
	}
	if err := b.enterExisting(newSte, true); err != nil {
		return err
	}
	if f.isAsync {
		newSte.Coroutine = true
	}
	if err := b.visitArguments(f.args); err != nil {
		return err
	}
	for _, st := range f.body {
		if err := b.visitStmt(st); err != nil {
			return err
		}
	}
	b.exitBlock()
	if len(f.typeParams) > 0 {
		b.exitBlock()
	}
	return nil
}

// makeFunctionEntry builds the Entry for a function body. Mirrors the
// ste_new + nested/method/docstring bookkeeping that runs immediately
// before symtable_visit_annotations in CPython.
func (b *builder) makeFunctionEntry(f *funcLike, key any) *Entry {
	b.nextID++
	ste := &Entry{
		ID:      b.nextID,
		Type:    FunctionBlock,
		Name:    f.name,
		Symbols: make(map[string]SymbolFlags),
		Loc:     f.loc,
	}
	if b.cur != nil && (b.cur.Nested || b.cur.IsFunctionLike()) {
		ste.Nested = true
	}
	if b.cur != nil && b.cur.Type == ClassBlock {
		ste.Method = true
	}
	if hasModuleDocstring(f.body) {
		ste.HasDocstring = true
	}
	b.table.Blocks[key] = ste
	return ste
}

// hasKwOnlyDefaults reports whether any kwonly parameter carries a
// default. The kw_defaults sequence is the same length as kwonlyargs;
// missing-default slots are nil.
//
// CPython: Python/symtable.c:L1801 has_kwonlydefaults
func hasKwOnlyDefaults(args *ast.Arguments) bool {
	for _, d := range args.KwDefaults {
		if d != nil {
			return true
		}
	}
	return false
}

// visitClassDef walks a class body. Type parameters wrap the class in
// a TypeParametersBlock; the class block itself is then entered.
//
// CPython: Python/symtable.c:L1904
func (b *builder) visitClassDef(c *ast.ClassDef) error {
	if err := b.addDef(c.Name, DefLocal, c.Pos); err != nil {
		return err
	}
	if err := b.visitExprSeq(c.DecoratorList); err != nil {
		return err
	}
	prevPriv := b.private
	hasTypeParams := len(c.TypeParams) > 0
	if hasTypeParams {
		if err := b.enterTypeParamBlock(c.Name, c, c.TypeParams, false, false, true, c.Pos); err != nil {
			return err
		}
		b.private = c.Name
		b.cur.MangledNames = make(map[string]struct{})
		for _, tp := range c.TypeParams {
			if err := b.visitTypeParam(tp); err != nil {
				return err
			}
		}
	}
	if err := b.visitExprSeq(c.Bases); err != nil {
		return err
	}
	if err := b.checkKeywords(c.Keywords); err != nil {
		return err
	}
	for _, k := range c.Keywords {
		if err := b.visitKeyword(k); err != nil {
			return err
		}
	}
	if err := b.enterBlock(c.Name, ClassBlock, c, c.Pos); err != nil {
		return err
	}
	b.private = c.Name
	if hasTypeParams {
		if err := b.addDef("__type_params__", DefLocal, c.Pos); err != nil {
			return err
		}
		if err := b.addDef(".type_params", Use, c.Pos); err != nil {
			return err
		}
	}
	if hasModuleDocstring(c.Body) {
		b.cur.HasDocstring = true
	}
	for _, s := range c.Body {
		if err := b.visitStmt(s); err != nil {
			return err
		}
	}
	b.exitBlock()
	if hasTypeParams {
		b.exitBlock()
	}
	b.private = prevPriv
	return nil
}

// checkKeywords rejects assignment to __debug__ via class keyword
// arguments.
//
// CPython: Python/symtable.c:L1608 check_keywords
func (b *builder) checkKeywords(keys ast.Seq[*ast.Keyword]) error {
	for _, k := range keys {
		if k.Arg != nil {
			if err := b.checkName(*k.Arg, k.Pos, ast.Store); err != nil {
				return err
			}
		}
	}
	return nil
}

// visitTypeAlias walks `type X = ...`. Generic forms wrap in a
// TypeParametersBlock; the body is always visited inside a
// TypeAliasBlock.
//
// CPython: Python/symtable.c:L1961
func (b *builder) visitTypeAlias(t *ast.TypeAlias) error {
	if err := b.visitExpr(t.Name); err != nil {
		return err
	}
	nameNode, ok := t.Name.(*ast.Name)
	if !ok {
		return nil
	}
	isInClass := b.cur.Type == ClassBlock
	hasGeneric := len(t.TypeParams) > 0
	if hasGeneric {
		if err := b.enterTypeParamBlock(nameNode.Id, t, t.TypeParams, false, false, false, t.Pos); err != nil {
			return err
		}
		for _, tp := range t.TypeParams {
			if err := b.visitTypeParam(tp); err != nil {
				return err
			}
		}
	}
	if err := b.enterBlock(nameNode.Id, TypeAliasBlock, t, t.Pos); err != nil {
		return err
	}
	b.cur.CanSeeClassScope = isInClass
	if isInClass {
		if err := b.addDef("__classdict__", Use, t.Pos); err != nil {
			return err
		}
	}
	if err := b.visitExpr(t.Value); err != nil {
		return err
	}
	b.exitBlock()
	if hasGeneric {
		b.exitBlock()
	}
	return nil
}

// visitReturn records a value-return so the analyze pass can spot
// generators that mix yield with `return value`.
//
// CPython: Python/symtable.c:L1994
func (b *builder) visitReturn(r *ast.Return) error {
	if r.Value == nil {
		return nil
	}
	if err := b.visitExpr(r.Value); err != nil {
		return err
	}
	b.cur.ReturnsValue = true
	return nil
}

// visitAssign walks plain `targets = value`. Targets visit before
// value to match CPython's order (the AST visitor relies on this for
// some pre-3.10 walrus semantics).
//
// CPython: Python/symtable.c:L2003
func (b *builder) visitAssign(a *ast.Assign) error {
	if err := b.visitExprSeq(a.Targets); err != nil {
		return err
	}
	return b.visitExpr(a.Value)
}

// visitAnnAssign records DEF_ANNOT for simple targets and rejects
// global/nonlocal annotations.
//
// CPython: Python/symtable.c:L2007
func (b *builder) visitAnnAssign(a *ast.AnnAssign) error {
	b.cur.AnnotationsUsed = true
	if name, ok := a.Target.(*ast.Name); ok {
		cur := b.lookup(name.Id)
		if cur&(DefGlobal|DefNonlocal) != 0 && b.cur != b.table.Top && a.Simple != 0 {
			msg := msgGlobalAnnot
			if cur&DefGlobal == 0 {
				msg = msgNonlocalAnnot
			}
			return errorf(b.filename, a.Pos, msg, name.Id)
		}
		if a.Simple != 0 {
			if err := b.addDef(name.Id, DefAnnot|DefLocal, name.Pos); err != nil {
				return err
			}
		} else if a.Value != nil {
			if err := b.addDef(name.Id, DefLocal, name.Pos); err != nil {
				return err
			}
		}
	} else {
		if err := b.visitExpr(a.Target); err != nil {
			return err
		}
	}
	if err := b.visitAnnotation(a.Annotation, annotationKey{a, 0}); err != nil {
		return err
	}
	if a.Value != nil {
		return b.visitExpr(a.Value)
	}
	return nil
}

// visitForLike walks both For and AsyncFor.
//
// CPython: Python/symtable.c:L2053 / L2274
func (b *builder) visitForLike(target, iter ast.Expr, body, orelse ast.Seq[ast.Stmt], isAsync bool, loc ast.Pos) error {
	if isAsync {
		b.maybeSetCoroutineForModule()
		if !b.cur.Coroutine {
			return errorf(b.filename, loc, msgAsyncForOutside)
		}
	}
	if err := b.visitExpr(target); err != nil {
		return err
	}
	if err := b.visitExpr(iter); err != nil {
		return err
	}
	prev := b.cur.InConditionalBlock
	b.cur.InConditionalBlock = true
	defer func() { b.cur.InConditionalBlock = prev }()
	if err := b.visitStmtSeq(body); err != nil {
		return err
	}
	return b.visitStmtSeq(orelse)
}

// visitWhile walks a while loop body.
func (b *builder) visitWhile(w *ast.While) error {
	if err := b.visitExpr(w.Test); err != nil {
		return err
	}
	prev := b.cur.InConditionalBlock
	b.cur.InConditionalBlock = true
	defer func() { b.cur.InConditionalBlock = prev }()
	if err := b.visitStmtSeq(w.Body); err != nil {
		return err
	}
	return b.visitStmtSeq(w.Orelse)
}

// visitIf walks an if statement.
func (b *builder) visitIf(s *ast.If) error {
	if err := b.visitExpr(s.Test); err != nil {
		return err
	}
	prev := b.cur.InConditionalBlock
	b.cur.InConditionalBlock = true
	defer func() { b.cur.InConditionalBlock = prev }()
	if err := b.visitStmtSeq(s.Body); err != nil {
		return err
	}
	return b.visitStmtSeq(s.Orelse)
}

// visitMatch walks `match subject:` cases.
func (b *builder) visitMatch(m *ast.Match) error {
	if err := b.visitExpr(m.Subject); err != nil {
		return err
	}
	prev := b.cur.InConditionalBlock
	b.cur.InConditionalBlock = true
	defer func() { b.cur.InConditionalBlock = prev }()
	for _, mc := range m.Cases {
		if err := b.visitMatchCase(mc); err != nil {
			return err
		}
	}
	return nil
}

// visitRaise walks `raise exc from cause`.
func (b *builder) visitRaise(r *ast.Raise) error {
	if r.Exc == nil {
		return nil
	}
	if err := b.visitExpr(r.Exc); err != nil {
		return err
	}
	if r.Cause != nil {
		return b.visitExpr(r.Cause)
	}
	return nil
}

// visitTryLike covers both Try and TryStar. The body, handlers,
// else, and finally clauses share the same structure.
func (b *builder) visitTryLike(body ast.Seq[ast.Stmt], handlers ast.Seq[ast.Excepthandler], orelse, finalbody ast.Seq[ast.Stmt]) error {
	prev := b.cur.InConditionalBlock
	b.cur.InConditionalBlock = true
	defer func() { b.cur.InConditionalBlock = prev }()
	if err := b.visitStmtSeq(body); err != nil {
		return err
	}
	for _, h := range handlers {
		if err := b.visitExceptHandler(h); err != nil {
			return err
		}
	}
	if err := b.visitStmtSeq(orelse); err != nil {
		return err
	}
	return b.visitStmtSeq(finalbody)
}

// visitAssert walks `assert test, msg`.
func (b *builder) visitAssert(a *ast.Assert) error {
	if err := b.visitExpr(a.Test); err != nil {
		return err
	}
	if a.Msg != nil {
		return b.visitExpr(a.Msg)
	}
	return nil
}

// visitWithLike covers With and AsyncWith.
func (b *builder) visitWithLike(items ast.Seq[*ast.Withitem], body ast.Seq[ast.Stmt], isAsync bool, loc ast.Pos) error {
	if isAsync {
		b.maybeSetCoroutineForModule()
		if !b.cur.Coroutine {
			return errorf(b.filename, loc, msgAsyncWithOutside)
		}
	}
	prev := b.cur.InConditionalBlock
	b.cur.InConditionalBlock = true
	defer func() { b.cur.InConditionalBlock = prev }()
	for _, it := range items {
		if err := b.visitWithItem(it); err != nil {
			return err
		}
	}
	return b.visitStmtSeq(body)
}

// visitGlobal records each name declared `global`. Mirrors the
// flag-validation switch in symtable_visit_stmt.
//
// CPython: Python/symtable.c:L2129
func (b *builder) visitGlobal(g *ast.Global) error {
	for _, name := range g.Names {
		cur := b.lookup(name)
		if cur&(DefParam|DefLocal|Use|DefAnnot) != 0 {
			msg := msgGlobalAfterAssign
			switch {
			case cur&DefParam != 0:
				msg = msgGlobalParam
			case cur&Use != 0:
				msg = msgGlobalAfterUse
			case cur&DefAnnot != 0:
				msg = msgGlobalAnnot
			}
			return errorf(b.filename, g.Pos, msg, name)
		}
		if err := b.addDef(name, DefGlobal, g.Pos); err != nil {
			return err
		}
		if err := b.recordDirective(name, g.Pos); err != nil {
			return err
		}
	}
	return nil
}

// visitNonlocal records each name declared `nonlocal`. The
// "no binding" check happens later in the analyze pass.
//
// CPython: Python/symtable.c:L2162
func (b *builder) visitNonlocal(n *ast.Nonlocal) error {
	for _, name := range n.Names {
		cur := b.lookup(name)
		if cur&(DefParam|DefLocal|Use|DefAnnot) != 0 {
			msg := msgNonlocalAfterAss
			switch {
			case cur&DefParam != 0:
				msg = msgNonlocalParam
			case cur&Use != 0:
				msg = msgNonlocalAfterUse
			case cur&DefAnnot != 0:
				msg = msgNonlocalAnnot
			}
			return errorf(b.filename, n.Pos, msg, name)
		}
		if err := b.addDef(name, DefNonlocal, n.Pos); err != nil {
			return err
		}
		if err := b.recordDirective(name, n.Pos); err != nil {
			return err
		}
	}
	return nil
}

// checkImportFrom rejects `from __future__ import ...` after the
// first non-future statement.
//
// CPython: Python/symtable.c:L1812 check_import_from
func (b *builder) checkImportFrom(s *ast.ImportFrom) error {
	if s.Module == nil || *s.Module != "__future__" {
		return nil
	}
	level := 0
	if s.Level != nil {
		level = *s.Level
	}
	if level != 0 {
		return nil
	}
	fut := b.future.Location
	if fut == (ast.Pos{}) || fut == ast.NoPos {
		return nil
	}
	if s.Pos.Lineno > fut.Lineno ||
		(s.Pos.Lineno == fut.EndLineno && s.Pos.ColOffset > fut.EndColOffset) {
		return errorf(b.filename, s.Pos, msgFutureLate)
	}
	return nil
}

// maybeSetCoroutineForModule promotes the module to a coroutine when
// PyCF_ALLOW_TOP_LEVEL_AWAIT is set. Currently a no-op stub.
//
// CPython: Python/symtable.c:L1839 maybe_set_ste_coroutine_for_module
func (b *builder) maybeSetCoroutineForModule() {
	if b.allowsTopLevelAwait() {
		b.cur.Coroutine = true
	}
}

// visitStmtSeq walks a sequence of statements, exiting early on the
// first error. Used in body / orelse / finally / handlers.
func (b *builder) visitStmtSeq(seq ast.Seq[ast.Stmt]) error {
	for _, s := range seq {
		if err := b.visitStmt(s); err != nil {
			return err
		}
	}
	return nil
}

// visitExprSeq walks a sequence of expressions.
func (b *builder) visitExprSeq(seq ast.Seq[ast.Expr]) error {
	for _, e := range seq {
		if err := b.visitExpr(e); err != nil {
			return err
		}
	}
	return nil
}

// visitExprSeqMaybeNull walks a sequence where individual entries may
// be nil (kw_defaults paired with kwonlyargs).
func (b *builder) visitExprSeqMaybeNull(seq ast.Seq[ast.Expr]) error {
	for _, e := range seq {
		if e == nil {
			continue
		}
		if err := b.visitExpr(e); err != nil {
			return err
		}
	}
	return nil
}

// annotationKey is the per-annotation block key. CPython distinguishes
// adjacent annotations on the same target by using the AST pointer
// plus a small offset; we mirror that with an indexed struct so each
// annotation hashes uniquely as a Go map key.
type annotationKey struct {
	parent any
	index  int
}

// futureAnnotations reports whether `from __future__ import
// annotations` is in effect.
func (b *builder) futureAnnotations() bool {
	return b.future != nil && b.future.Bits&future.Annotations != 0
}
