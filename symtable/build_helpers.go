package symtable

import (
	"github.com/tamnd/gopy/ast"
)

// visitArguments walks the parameter list, registering each name as
// DEF_PARAM and flagging *args / **kwargs.
//
// CPython: Python/symtable.c:L2884 symtable_visit_arguments
func (b *builder) visitArguments(a *ast.Arguments) error {
	if err := b.visitParams(a.Posonlyargs); err != nil {
		return err
	}
	if err := b.visitParams(a.Args); err != nil {
		return err
	}
	if err := b.visitParams(a.Kwonlyargs); err != nil {
		return err
	}
	if a.Vararg != nil {
		if err := b.addDef(a.Vararg.Arg, DefParam, a.Vararg.Pos); err != nil {
			return err
		}
		b.cur.Varargs = true
	}
	if a.Kwarg != nil {
		if err := b.addDef(a.Kwarg.Arg, DefParam, a.Kwarg.Pos); err != nil {
			return err
		}
		b.cur.Varkeywords = true
	}
	return nil
}

// visitParams adds each name in args as DEF_PARAM.
//
// CPython: Python/symtable.c:L2761 symtable_visit_params
func (b *builder) visitParams(args ast.Seq[*ast.Arg]) error {
	for _, a := range args {
		if err := b.addDef(a.Arg, DefParam, a.Pos); err != nil {
			return err
		}
	}
	return nil
}

// visitArgAnnotations walks each parameter's annotation expression
// inside the function's lazy AnnotationBlock.
//
// CPython: Python/symtable.c:L2828 symtable_visit_argannotations
func (b *builder) visitArgAnnotations(args ast.Seq[*ast.Arg]) error {
	for _, a := range args {
		if a.Annotation == nil {
			continue
		}
		b.cur.AnnotationsUsed = true
		if err := b.visitExpr(a.Annotation); err != nil {
			return err
		}
	}
	return nil
}

// visitAnnotations creates the AnnotationBlock for a function and
// walks every annotation inside it.
//
// CPython: Python/symtable.c:L2844 symtable_visit_annotations
func (b *builder) visitAnnotations(o ast.Stmt, a *ast.Arguments, returns ast.Expr, functionSte *Entry) error {
	_ = functionSte
	isInClass := b.cur.CanSeeClassScope
	currentType := b.cur.Type
	if err := b.enterBlock("__annotate__", AnnotationBlock, a, stmtPos(o)); err != nil {
		return err
	}
	if isInClass || currentType == ClassBlock {
		b.cur.CanSeeClassScope = true
		if err := b.addDef("__classdict__", Use, stmtPos(o)); err != nil {
			return err
		}
	}
	if err := b.visitArgAnnotations(a.Posonlyargs); err != nil {
		return err
	}
	if err := b.visitArgAnnotations(a.Args); err != nil {
		return err
	}
	if a.Vararg != nil && a.Vararg.Annotation != nil {
		b.cur.AnnotationsUsed = true
		if err := b.visitExpr(a.Vararg.Annotation); err != nil {
			return err
		}
	}
	if a.Kwarg != nil && a.Kwarg.Annotation != nil {
		b.cur.AnnotationsUsed = true
		if err := b.visitExpr(a.Kwarg.Annotation); err != nil {
			return err
		}
	}
	if err := b.visitArgAnnotations(a.Kwonlyargs); err != nil {
		return err
	}
	if returns != nil {
		b.cur.AnnotationsUsed = true
		if err := b.visitExpr(returns); err != nil {
			return err
		}
	}
	b.exitBlock()
	return nil
}

// visitAnnotation walks a per-name AnnAssign annotation. Annotations
// in function scope are unevaluated (PEP 563 / 649 hybrid) so they
// must not record name bindings.
//
// CPython: Python/symtable.c:L2775 symtable_visit_annotation
func (b *builder) visitAnnotation(annotation ast.Expr, key any) error {
	isUnevaluated := b.cur.Type == FunctionBlock
	if (b.cur.Type == ClassBlock && b.cur.InConditionalBlock || b.cur.Type == ModuleBlock) &&
		!b.cur.HasConditionalAnnotations {
		b.cur.HasConditionalAnnotations = true
		if err := b.addDef("__conditional_annotations__", Use, exprPos(annotation)); err != nil {
			return err
		}
	}
	parent := b.cur
	if parent.AnnotationBlock == nil {
		currentType := parent.Type
		if err := b.enterBlock("__annotate__", AnnotationBlock, key, exprPos(annotation)); err != nil {
			return err
		}
		parent.AnnotationBlock = b.cur
		if currentType == ClassBlock && !b.futureAnnotations() {
			b.cur.CanSeeClassScope = true
			if err := b.addDef("__classdict__", Use, exprPos(annotation)); err != nil {
				return err
			}
		}
		// When the class has conditional annotations, __conditional_annotations__
		// must be visible as a free variable inside the annotation block so that
		// emitAnnotateBody can emit LOAD_DEREF to test which indices were reached.
		// The outer class scope will have the corresponding cell (via dropClassFree
		// + stampImplicitCell). Module scope uses LOAD_GLOBAL instead, so no
		// free-var injection is needed there.
		//
		// CPython: Python/compile.c:630 (implicit cellvar cook-up)
	} else {
		if err := b.enterExisting(parent.AnnotationBlock, false); err != nil {
			return err
		}
	}
	// When the class has conditional annotations, __conditional_annotations__
	// must be visible as a free variable inside the annotation block so that
	// emitAnnotateBody can emit LOAD_DEREF to test which indices were reached.
	// The outer class scope will have the corresponding cell (via dropClassFree
	// + stampImplicitCell). Module scope uses LOAD_GLOBAL instead, so no
	// free-var injection is needed there. addDef is idempotent for USE flags.
	//
	// CPython: Python/compile.c:630 (implicit cellvar cook-up)
	if parent.HasConditionalAnnotations && parent.Type == ClassBlock {
		if _, exists := b.cur.Symbols["__conditional_annotations__"]; !exists {
			if err := b.addDef("__conditional_annotations__", Use, exprPos(annotation)); err != nil {
				return err
			}
		}
	}
	if isUnevaluated {
		b.cur.InUnevaluatedAnnotation = true
	}
	err := b.visitExpr(annotation)
	if isUnevaluated {
		b.cur.InUnevaluatedAnnotation = false
	}
	b.exitBlock()
	return err
}

// visitAlias records an `import name [as asname]` binding.
//
// CPython: Python/symtable.c:L2943 symtable_visit_alias
func (b *builder) visitAlias(a *ast.Alias) error {
	name := a.Name
	if a.Asname != nil {
		name = *a.Asname
	}
	if a.Asname == nil {
		if dot := indexDot(name); dot >= 0 {
			name = name[:dot]
		}
	}
	if name != "*" {
		return b.addDef(name, DefImport, a.Pos)
	}
	if b.cur.Type != ModuleBlock {
		return errorf(b.filename, a.Pos, msgImportStar)
	}
	return nil
}

// indexDot returns the offset of the first `.` in s, or -1.
//
// CPython: Python/symtable.c PyUnicode_FindChar(str, '.', ...)
func indexDot(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// visitExceptHandler walks `except T as name: body`.
//
// CPython: Python/symtable.c:L2910 symtable_visit_excepthandler
func (b *builder) visitExceptHandler(eh ast.Excepthandler) error {
	h, ok := eh.(*ast.ExceptHandler)
	if !ok {
		return nil
	}
	if h.Type != nil {
		if err := b.visitExpr(h.Type); err != nil {
			return err
		}
	}
	if h.Name != nil {
		if err := b.addDef(*h.Name, DefLocal, h.Pos); err != nil {
			return err
		}
	}
	return b.visitStmtSeq(h.Body)
}

// visitWithItem walks a `with ctx as target` clause.
//
// CPython: Python/symtable.c:L2922 symtable_visit_withitem
func (b *builder) visitWithItem(item *ast.Withitem) error {
	if err := b.visitExpr(item.ContextExpr); err != nil {
		return err
	}
	if item.OptionalVars != nil {
		return b.visitExpr(item.OptionalVars)
	}
	return nil
}

// visitMatchCase walks one match case (pattern, guard, body).
//
// CPython: Python/symtable.c:L2932 symtable_visit_match_case
func (b *builder) visitMatchCase(mc *ast.MatchCase) error {
	if err := b.visitPattern(mc.Pattern); err != nil {
		return err
	}
	if mc.Guard != nil {
		if err := b.visitExpr(mc.Guard); err != nil {
			return err
		}
	}
	return b.visitStmtSeq(mc.Body)
}

// visitPattern walks a structural pattern, registering capture names.
//
// CPython: Python/symtable.c:L2691 symtable_visit_pattern
func (b *builder) visitPattern(p ast.Pattern) error {
	switch n := p.(type) {
	case *ast.MatchValue:
		return b.visitExpr(n.Value)
	case *ast.MatchSingleton:
		return nil
	case *ast.MatchSequence:
		return b.visitPatternSeq(n.Patterns)
	case *ast.MatchStar:
		if n.Name != nil {
			return b.addDef(*n.Name, DefLocal, n.Pos)
		}
		return nil
	case *ast.MatchMapping:
		if err := b.visitExprSeq(n.Keys); err != nil {
			return err
		}
		if err := b.visitPatternSeq(n.Patterns); err != nil {
			return err
		}
		if n.Rest != nil {
			return b.addDef(*n.Rest, DefLocal, n.Pos)
		}
		return nil
	case *ast.MatchClass:
		if err := b.visitExpr(n.Cls); err != nil {
			return err
		}
		if err := b.visitPatternSeq(n.Patterns); err != nil {
			return err
		}
		if err := b.checkKwdPatterns(n); err != nil {
			return err
		}
		return b.visitPatternSeq(n.KwdPatterns)
	case *ast.MatchAs:
		if n.Pattern != nil {
			if err := b.visitPattern(n.Pattern); err != nil {
				return err
			}
		}
		if n.Name != nil {
			return b.addDef(*n.Name, DefLocal, n.Pos)
		}
		return nil
	case *ast.MatchOr:
		return b.visitPatternSeq(n.Patterns)
	}
	return nil
}

// visitPatternSeq walks a sequence of patterns.
//
// CPython: Python/symtable.c VISIT_SEQ(c, pattern, seq)
func (b *builder) visitPatternSeq(seq ast.Seq[ast.Pattern]) error {
	for _, p := range seq {
		if p == nil {
			continue
		}
		if err := b.visitPattern(p); err != nil {
			return err
		}
	}
	return nil
}

// checkKwdPatterns rejects assignment to __debug__ via class-pattern
// keyword binders.
//
// CPython: Python/symtable.c:L1620 check_kwd_patterns
func (b *builder) checkKwdPatterns(p *ast.MatchClass) error {
	for i, name := range p.KwdAttrs {
		loc := p.Pos
		if i < len(p.KwdPatterns) && p.KwdPatterns[i] != nil {
			loc = p.KwdPatterns[i].Position()
		}
		if err := b.checkName(name, loc, ast.Store); err != nil {
			return err
		}
	}
	return nil
}

// visitKeyword walks a keyword-argument value.
//
// CPython: Python/symtable.c:L2997 symtable_visit_keyword
func (b *builder) visitKeyword(k *ast.Keyword) error {
	if k == nil {
		return nil
	}
	return b.visitExpr(k.Value)
}

// visitTypeParam walks one TypeVar / ParamSpec / TypeVarTuple.
//
// CPython: Python/symtable.c:L2634 symtable_visit_type_param
func (b *builder) visitTypeParam(tp ast.TypeParam) error {
	switch n := tp.(type) {
	case *ast.TypeVar:
		if n.Name == "__classdict__" {
			return errorf(b.filename, n.Pos, "reserved name '%s' cannot be used for type parameter", n.Name)
		}
		if err := b.addDef(n.Name, DefTypeParam|DefLocal, n.Pos); err != nil {
			return err
		}
		var info string
		if n.Bound != nil {
			if _, ok := n.Bound.(*ast.Tuple); ok {
				info = "a TypeVar constraint"
			} else {
				info = "a TypeVar bound"
			}
		}
		if err := b.visitTypeParamSubexpr(n.Bound, n.Name, n, info, 0); err != nil {
			return err
		}
		return b.visitTypeParamSubexpr(n.DefaultValue, n.Name, n, "a TypeVar default", 1)
	case *ast.TypeVarTuple:
		if err := b.addDef(n.Name, DefTypeParam|DefLocal, n.Pos); err != nil {
			return err
		}
		return b.visitTypeParamSubexpr(n.DefaultValue, n.Name, n, "a TypeVarTuple default", 0)
	case *ast.ParamSpec:
		if err := b.addDef(n.Name, DefTypeParam|DefLocal, n.Pos); err != nil {
			return err
		}
		return b.visitTypeParamSubexpr(n.DefaultValue, n.Name, n, "a ParamSpec default", 0)
	}
	return nil
}

// visitTypeParamSubexpr runs the bound or default of a type parameter
// inside its own TypeVariableBlock so name resolution treats it like
// an inner scope.
//
// CPython: Python/symtable.c:L2597 symtable_visit_type_param_bound_or_default
func (b *builder) visitTypeParamSubexpr(e ast.Expr, name string, parent any, info string, slot int) error {
	if e == nil {
		return nil
	}
	isInClass := b.cur.CanSeeClassScope
	if err := b.enterBlock(name, TypeVariableBlock, typeParamSubexprKey{parent, slot}, exprPos(e)); err != nil {
		return err
	}
	b.cur.CanSeeClassScope = isInClass
	b.cur.ScopeInfo = info
	if isInClass {
		if err := b.addDef("__classdict__", Use, exprPos(e)); err != nil {
			return err
		}
	}
	err := b.visitExpr(e)
	b.exitBlock()
	return err
}

// typeParamSubexprKey distinguishes the bound, default, and value
// blocks of a single TypeVar so they hash to different map entries.
type typeParamSubexprKey struct {
	parent any
	slot   int
}

// enterTypeParamBlock sets up the synthetic TypeParametersBlock that
// wraps a generic def / class / type alias.
//
// CPython: Python/symtable.c:L1659 symtable_enter_type_param_block
func (b *builder) enterTypeParamBlock(name string, key any, _ ast.Seq[ast.TypeParam], hasDefaults, hasKwDefaults, isClass bool, loc ast.Pos) error {
	currentType := b.cur.Type
	if err := b.enterBlock(name, TypeParametersBlock, typeParamSubexprKey{key, -1}, loc); err != nil {
		return err
	}
	if currentType == ClassBlock {
		b.cur.CanSeeClassScope = true
		if err := b.addDef("__classdict__", Use, loc); err != nil {
			return err
		}
	}
	if isClass {
		if err := b.addDef(".type_params", DefLocal, loc); err != nil {
			return err
		}
		if err := b.addDef(".type_params", Use, loc); err != nil {
			return err
		}
		if err := b.addDef(".generic_base", DefLocal, loc); err != nil {
			return err
		}
		if err := b.addDef(".generic_base", Use, loc); err != nil {
			return err
		}
	}
	if hasDefaults {
		if err := b.addDef(".defaults", DefParam, loc); err != nil {
			return err
		}
	}
	if hasKwDefaults {
		if err := b.addDef(".kwdefaults", DefParam, loc); err != nil {
			return err
		}
	}
	return nil
}

// stmtPos returns the source location of a statement, or NoPos.
//
// CPython: Python/symtable.c LOCATION(node) macro for Stmt
func stmtPos(s ast.Stmt) ast.Pos {
	if s == nil {
		return ast.NoPos
	}
	return s.Position()
}
