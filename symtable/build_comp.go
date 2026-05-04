package symtable

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// handleNamedExpr walks a walrus expression `(target := value)`.
// Inside a comprehension, the target retargets the enclosing function
// or module scope (PEP 572).
//
// CPython: Python/symtable.c:L2391 symtable_handle_namedexpr
func (b *builder) handleNamedExpr(n *ast.NamedExpr) error {
	if b.cur.CompIterExpr > 0 {
		return errorf(b.filename, n.Pos, msgNamedExprIterExpr)
	}
	if b.cur.Comprehension != NoComprehension {
		target, ok := n.Target.(*ast.Name)
		if !ok {
			return fmt.Errorf("symtable: walrus target must be Name")
		}
		if err := b.extendNamedExprScope(target, n); err != nil {
			return err
		}
	}
	if err := b.visitExpr(n.Value); err != nil {
		return err
	}
	return b.visitExpr(n.Target)
}

// extendNamedExprScope walks the block stack from innermost outward
// and writes the walrus target into the nearest enclosing scope that
// can host a binding. Comprehension scopes are skipped (after a
// conflict check); ClassBlock / TypeAliasBlock / TypeParametersBlock /
// TypeVariableBlock are rejected.
//
// CPython: Python/symtable.c:L2293 symtable_extend_namedexpr_scope
func (b *builder) extendNamedExprScope(target *ast.Name, e *ast.NamedExpr) error {
	name := target.Id
	for i := len(b.stack) - 1; i >= 0; i-- {
		ste := b.stack[i]
		if ste.Comprehension != NoComprehension {
			cur := b.lookupEntry(ste, name)
			if cur&DefCompIter != 0 && cur&DefLocal != 0 {
				return errorf(b.filename, e.Pos, msgNamedExprConflict, name)
			}
			continue
		}
		if ste.Type == FunctionBlock {
			cur := b.lookupEntry(ste, name)
			flag := DefNonlocal
			if cur&DefGlobal != 0 {
				flag = DefGlobal
			}
			if err := b.addDef(name, flag, e.Pos); err != nil {
				return err
			}
			if err := b.recordDirective(name, e.Pos); err != nil {
				return err
			}
			return b.addDefHelper(name, DefLocal, ste, e.Pos)
		}
		if ste.Type == ModuleBlock {
			if err := b.addDef(name, DefGlobal, e.Pos); err != nil {
				return err
			}
			if err := b.recordDirective(name, e.Pos); err != nil {
				return err
			}
			return b.addDefHelper(name, DefGlobal, ste, e.Pos)
		}
		switch ste.Type {
		case ClassBlock:
			return errorf(b.filename, e.Pos, msgNamedExprComp)
		case TypeParametersBlock:
			return errorf(b.filename, e.Pos, msgNamedExprParam)
		case TypeAliasBlock:
			return errorf(b.filename, e.Pos, msgNamedExprAlias)
		case TypeVariableBlock:
			return errorf(b.filename, e.Pos, msgNamedExprBound)
		}
	}
	return fmt.Errorf("symtable: extendNamedExprScope: no enclosing function/module/class block")
}

// handleComprehension walks a list / set / dict comprehension or
// generator expression. The outermost iterable is evaluated in the
// current scope; the rest of the comprehension runs in a new
// FunctionBlock with an implicit `.0` parameter that receives the
// outer iterable.
//
// CPython: Python/symtable.c:L3005 symtable_handle_comprehension
func (b *builder) handleComprehension(e ast.Expr, scopeName string, generators ast.Seq[*ast.Comprehension], elt, value ast.Expr, isGenerator bool) error {
	if len(generators) == 0 {
		return nil
	}
	outermost := generators[0]
	b.cur.CompIterExpr++
	err := b.visitExpr(outermost.Iter)
	b.cur.CompIterExpr--
	if err != nil {
		return err
	}
	if err := b.enterBlock(scopeName, FunctionBlock, e, exprPos(e)); err != nil {
		return err
	}
	b.cur.Comprehension = comprehensionTypeFor(e)
	if outermost.IsAsync != 0 {
		b.cur.Coroutine = true
	}
	if err := b.runComprehensionBody(outermost, generators, elt, value); err != nil {
		b.exitBlock()
		return err
	}
	b.cur.Generator = isGenerator
	isAsync := b.cur.Coroutine && !isGenerator
	b.exitBlock()
	if isAsync && !b.isAsyncDef() && b.cur.Comprehension == NoComprehension && !b.allowsTopLevelAwait() {
		return errorf(b.filename, exprPos(e), msgAsyncCompOutside)
	}
	if isAsync {
		b.cur.Coroutine = true
	}
	return nil
}

// runComprehensionBody walks every part of the comprehension that
// must execute inside the new FunctionBlock: implicit .0 parameter,
// the outermost target / ifs, the trailing generator clauses, and
// finally the value / element expressions.
//
// CPython: Python/symtable.c symtable_handle_comprehension body half
func (b *builder) runComprehensionBody(outermost *ast.Comprehension, generators ast.Seq[*ast.Comprehension], elt, value ast.Expr) error {
	if err := b.implicitArg(0); err != nil {
		return err
	}
	b.cur.CompIterTarget = true
	err := b.visitExpr(outermost.Target)
	b.cur.CompIterTarget = false
	if err != nil {
		return err
	}
	if err := b.visitExprSeq(outermost.Ifs); err != nil {
		return err
	}
	for i := 1; i < len(generators); i++ {
		if err := b.visitComprehensionIter(generators[i]); err != nil {
			return err
		}
	}
	if value != nil {
		if err := b.visitExpr(value); err != nil {
			return err
		}
	}
	return b.visitExpr(elt)
}

// comprehensionTypeFor maps an AST comprehension node to the matching
// ComprehensionType tag.
//
// CPython: Python/symtable.c comprehension-kind switch in
// symtable_handle_comprehension
func comprehensionTypeFor(e ast.Expr) ComprehensionType {
	switch e.(type) {
	case *ast.ListComp:
		return ListComprehension
	case *ast.SetComp:
		return SetComprehension
	case *ast.DictComp:
		return DictComprehension
	}
	return GeneratorExpression
}

// visitComprehensionIter walks a non-outermost generator clause.
//
// CPython: Python/symtable.c:L2980 symtable_visit_comprehension
func (b *builder) visitComprehensionIter(c *ast.Comprehension) error {
	b.cur.CompIterTarget = true
	if err := b.visitExpr(c.Target); err != nil {
		b.cur.CompIterTarget = false
		return err
	}
	b.cur.CompIterTarget = false
	b.cur.CompIterExpr++
	if err := b.visitExpr(c.Iter); err != nil {
		b.cur.CompIterExpr--
		return err
	}
	b.cur.CompIterExpr--
	if err := b.visitExprSeq(c.Ifs); err != nil {
		return err
	}
	if c.IsAsync != 0 {
		b.cur.Coroutine = true
	}
	return nil
}

// implicitArg adds a synthetic positional parameter named `.<pos>`,
// used for the outermost iterable of a comprehension.
//
// CPython: Python/symtable.c:L2747 symtable_implicit_arg
func (b *builder) implicitArg(pos int) error {
	name := fmt.Sprintf(".%d", pos)
	return b.addDef(name, DefParam, b.cur.Loc)
}
