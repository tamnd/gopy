package symtable

import (
	"github.com/tamnd/gopy/ast"
)

// visitExpr dispatches to per-expression-kind helpers.
//
// CPython: Python/symtable.c:L2409 symtable_visit_expr
func (b *builder) visitExpr(e ast.Expr) error {
	if handled, err := b.visitExprComp(e); handled {
		return err
	}
	if handled, err := b.visitExprUnary(e); handled {
		return err
	}
	return b.visitExprLeaf(e)
}

// visitExprComp covers the comprehension family, lambda, walrus,
// generator/await constructs that introduce a new scope or have
// side-effects beyond pure operand walking.
func (b *builder) visitExprComp(e ast.Expr) (bool, error) {
	switch n := e.(type) {
	case *ast.NamedExpr:
		if err := b.raiseIfAnnotationBlock("named expression", e); err != nil {
			return true, err
		}
		return true, b.handleNamedExpr(n)
	case *ast.Lambda:
		return true, b.visitLambda(n)
	case *ast.GeneratorExp:
		return true, b.handleComprehension(n, "<genexpr>", n.Generators, n.Elt, nil, true)
	case *ast.ListComp:
		return true, b.handleComprehension(n, "<listcomp>", n.Generators, n.Elt, nil, false)
	case *ast.SetComp:
		return true, b.handleComprehension(n, "<setcomp>", n.Generators, n.Elt, nil, false)
	case *ast.DictComp:
		return true, b.handleComprehension(n, "<dictcomp>", n.Generators, n.Key, n.Value, false)
	case *ast.Yield:
		return true, b.visitYield(n.Value, e)
	case *ast.YieldFrom:
		return true, b.visitYield(n.Value, e)
	case *ast.Await:
		return true, b.visitAwait(n)
	case *ast.Call:
		return true, b.visitCall(n)
	}
	return false, nil
}

// visitExprUnary covers operator-style nodes that recurse into a
// fixed set of operand expressions.
func (b *builder) visitExprUnary(e ast.Expr) (bool, error) {
	switch n := e.(type) {
	case *ast.BoolOp:
		return true, b.visitExprSeq(n.Values)
	case *ast.BinOp:
		if err := b.visitExpr(n.Left); err != nil {
			return true, err
		}
		return true, b.visitExpr(n.Right)
	case *ast.UnaryOp:
		return true, b.visitExpr(n.Operand)
	case *ast.IfExp:
		if err := b.visitExpr(n.Test); err != nil {
			return true, err
		}
		if err := b.visitExpr(n.Body); err != nil {
			return true, err
		}
		return true, b.visitExpr(n.Orelse)
	case *ast.Dict:
		if err := b.visitExprSeqMaybeNull(n.Keys); err != nil {
			return true, err
		}
		return true, b.visitExprSeq(n.Values)
	case *ast.Set:
		return true, b.visitExprSeq(n.Elts)
	case *ast.Compare:
		if err := b.visitExpr(n.Left); err != nil {
			return true, err
		}
		return true, b.visitExprSeq(n.Comparators)
	case *ast.FormattedValue:
		return true, b.visitFormattedValue(n.Value, n.FormatSpec)
	case *ast.Interpolation:
		return true, b.visitFormattedValue(n.Value, n.FormatSpec)
	case *ast.JoinedStr:
		return true, b.visitExprSeq(n.Values)
	case *ast.TemplateStr:
		return true, b.visitExprSeq(n.Values)
	}
	return false, nil
}

// visitExprLeaf covers terminal name / attribute / index / slice and
// the literal forms.
func (b *builder) visitExprLeaf(e ast.Expr) error {
	switch n := e.(type) {
	case *ast.Constant:
		return nil
	case *ast.Attribute:
		if err := b.checkName(n.Attr, n.Pos, n.Ctx); err != nil {
			return err
		}
		return b.visitExpr(n.Value)
	case *ast.Subscript:
		if err := b.visitExpr(n.Value); err != nil {
			return err
		}
		return b.visitExpr(n.Slice)
	case *ast.Starred:
		return b.visitExpr(n.Value)
	case *ast.Slice:
		return b.visitSliceParts(n)
	case *ast.Name:
		return b.visitName(n)
	case *ast.List:
		return b.visitExprSeq(n.Elts)
	case *ast.Tuple:
		return b.visitExprSeq(n.Elts)
	}
	return nil
}

// visitFormattedValue walks the value-plus-optional-format-spec form
// shared by FormattedValue and Interpolation.
func (b *builder) visitFormattedValue(value, formatSpec ast.Expr) error {
	if err := b.visitExpr(value); err != nil {
		return err
	}
	if formatSpec != nil {
		return b.visitExpr(formatSpec)
	}
	return nil
}

// visitSliceParts walks lower / upper / step of a Slice node.
func (b *builder) visitSliceParts(n *ast.Slice) error {
	if n.Lower != nil {
		if err := b.visitExpr(n.Lower); err != nil {
			return err
		}
	}
	if n.Upper != nil {
		if err := b.visitExpr(n.Upper); err != nil {
			return err
		}
	}
	if n.Step != nil {
		return b.visitExpr(n.Step)
	}
	return nil
}

// visitName records a USE or DEF_LOCAL on the name and recognizes
// `super` to keep `__class__` in scope.
//
// CPython: Python/symtable.c:L2569
func (b *builder) visitName(n *ast.Name) error {
	if b.cur.InUnevaluatedAnnotation {
		return nil
	}
	flag := Use
	if n.Ctx != ast.Load {
		flag = DefLocal
	}
	if err := b.addDefCtx(n.Id, flag, n.Pos, n.Ctx); err != nil {
		return err
	}
	if n.Ctx == ast.Load && b.cur.IsFunctionLike() && n.Id == "super" {
		if err := b.addDef("__class__", Use, n.Pos); err != nil {
			return err
		}
	}
	return nil
}

// visitYield records a yield / yield-from. CPython rejects yield in
// the bodies of comprehensions.
//
// CPython: Python/symtable.c:L2474
func (b *builder) visitYield(value ast.Expr, source ast.Expr) error {
	if err := b.raiseIfAnnotationBlock("yield expression", source); err != nil {
		return err
	}
	if value != nil {
		if err := b.visitExpr(value); err != nil {
			return err
		}
	}
	b.cur.Generator = true
	if b.cur.Comprehension != NoComprehension {
		return b.raiseIfComprehensionBlock(source)
	}
	return nil
}

// visitAwait records an await and validates it appears in a function
// (or async function for non-comprehension contexts).
//
// CPython: Python/symtable.c:L2495
func (b *builder) visitAwait(a *ast.Await) error {
	if err := b.raiseIfAnnotationBlock("await expression", a); err != nil {
		return err
	}
	if !b.allowsTopLevelAwait() {
		if !b.cur.IsFunctionLike() {
			return errorf(b.filename, a.Pos, msgAwaitOutsideFunc)
		}
		if !b.isAsyncDef() && b.cur.Comprehension == NoComprehension {
			return errorf(b.filename, a.Pos, msgAwaitOutsideAsync)
		}
	}
	if err := b.visitExpr(a.Value); err != nil {
		return err
	}
	b.cur.Coroutine = true
	return nil
}

// visitCall walks a function-call expression.
//
// CPython: Python/symtable.c:L2520
func (b *builder) visitCall(c *ast.Call) error {
	if err := b.visitExpr(c.Func); err != nil {
		return err
	}
	if err := b.visitExprSeq(c.Args); err != nil {
		return err
	}
	if err := b.checkKeywords(c.Keywords); err != nil {
		return err
	}
	for _, k := range c.Keywords {
		if k == nil {
			continue
		}
		if err := b.visitKeyword(k); err != nil {
			return err
		}
	}
	return nil
}

// visitLambda enters a function-block child for the lambda body.
//
// CPython: Python/symtable.c:L2431
func (b *builder) visitLambda(l *ast.Lambda) error {
	if err := b.visitExprSeq(l.Args.Defaults); err != nil {
		return err
	}
	if err := b.visitExprSeqMaybeNull(l.Args.KwDefaults); err != nil {
		return err
	}
	if err := b.enterBlock("<lambda>", FunctionBlock, l, l.Pos); err != nil {
		return err
	}
	if err := b.visitArguments(l.Args); err != nil {
		return err
	}
	if err := b.visitExpr(l.Body); err != nil {
		return err
	}
	b.exitBlock()
	return nil
}

// raiseIfAnnotationBlock returns a SyntaxError if the current block
// disallows expression `name`. CPython has the same predicate covering
// AnnotationBlock, TypeVariableBlock, TypeAliasBlock, and
// TypeParametersBlock.
//
// CPython: Python/symtable.c:L3109
func (b *builder) raiseIfAnnotationBlock(name string, e ast.Expr) error {
	pos := exprPos(e)
	switch b.cur.Type {
	case AnnotationBlock:
		return errorf(b.filename, pos, msgAnnotationNotAllow, name)
	case TypeVariableBlock:
		return errorf(b.filename, pos, msgNotAllowedInTypVar, name, b.cur.ScopeInfo)
	case TypeAliasBlock:
		return errorf(b.filename, pos, msgNotAllowedInAlias, name)
	case TypeParametersBlock:
		return errorf(b.filename, pos, msgNotAllowedInParams, name)
	}
	return nil
}

// raiseIfComprehensionBlock rejects yield inside any comprehension.
//
// CPython: Python/symtable.c:L3137
func (b *builder) raiseIfComprehensionBlock(e ast.Expr) error {
	var msg string
	switch b.cur.Comprehension {
	case ListComprehension:
		msg = "'yield' inside list comprehension"
	case SetComprehension:
		msg = "'yield' inside set comprehension"
	case DictComprehension:
		msg = "'yield' inside dict comprehension"
	default:
		msg = "'yield' inside generator expression"
	}
	return errorf(b.filename, exprPos(e), "%s", msg)
}

// exprPos returns the position of an expression, or NoPos.
func exprPos(e ast.Expr) ast.Pos {
	if e == nil {
		return ast.NoPos
	}
	return e.Position()
}
