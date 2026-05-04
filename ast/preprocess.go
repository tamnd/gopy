// Port of cpython/Python/ast_preprocess.c. The walker (`*preprocessor`)
// mirrors `astfold_*` and `fold_*` in the C file; helper functions
// such as `parseFormatSpec`, `negateConst`, `combineConst` and
// `toComplex` port the small constant-folding utilities in the same
// file. Pure-Go helpers (`hasStarred`, fmt-cursor primitives) have no
// individual CPython counterpart and live alongside the ports.

package ast

import (
	"fmt"
	"strconv"
	"strings"
)

// Warning is a non-fatal preprocess diagnostic. PEP 765 emits one of
// these for each `return`/`break`/`continue` that escapes a `finally`
// block. CPython spells them via _PyErr_EmitSyntaxWarning.
//
// CPython: Python/ast_preprocess.c control_flow_in_finally_warning
type Warning struct {
	Msg      string
	Filename string
	Pos      Pos
}

// Error formats the warning as a SyntaxWarning-style message.
//
// CPython: Python/ast_preprocess.c PyErr_FormatSyntaxWarningObject
func (w Warning) Error() string {
	return fmt.Sprintf("%s:%d:%d: SyntaxWarning: %s",
		w.Filename, w.Pos.Lineno, w.Pos.ColOffset+1, w.Msg)
}

// CoFutureAnnotations is the CO_FUTURE_ANNOTATIONS bit (PEP 563).
// Mirrors the value used by future.Annotations so the preprocess pass
// can be told to skip annotation expressions without taking a
// future-package dependency (which would import this package
// transitively).
//
// CPython: Include/cpython/code.h CO_FUTURE_ANNOTATIONS
const CoFutureAnnotations uint32 = 0x1000000

// PreprocessOptions controls which preprocess passes run. Mirrors the
// _PyASTPreprocessState flag bag.
//
// CPython: Python/ast_preprocess.c _PyASTPreprocessState
type PreprocessOptions struct {
	Filename        string
	OptimizeLevel   int    // -1 default, 0 = no, 1 = -O, 2 = -OO.
	FFFeatures      uint32 // future feature bits (e.g. CO_FUTURE_ANNOTATIONS).
	EnableWarnings  bool   // false under -W ignore.
	SyntaxCheckOnly bool   // true suppresses mutating folds.
}

// Preprocess walks mod, emits PEP 765 control-flow-in-finally warnings,
// and applies the small set of constant folds CPython 3.14 keeps in the
// AST layer (printf-format folding, `__debug__` substitution, MatchValue
// numeric folding, docstring removal at -OO).
//
// CPython: Python/ast_preprocess.c _PyAST_Preprocess
func Preprocess(mod Mod, opt PreprocessOptions) []Warning {
	p := &preprocessor{opt: opt}
	switch m := mod.(type) {
	case *Module:
		p.foldBody(&m.Body)
	case *Interactive:
		for i := 0; i < m.Body.Len(); i++ {
			p.foldStmt(m.Body.Get(i))
		}
	case *Expression:
		m.Body = p.foldExpr(m.Body)
	case *FunctionType:
		// CPython: FunctionType_kind does not participate in folding.
	}
	return p.warnings
}

type cfFrame struct {
	inFinally bool
	inFuncDef bool
	inLoop    bool
}

type preprocessor struct {
	opt      PreprocessOptions
	stack    []cfFrame
	warnings []Warning
}

func (p *preprocessor) push(f cfFrame) { p.stack = append(p.stack, f) }
func (p *preprocessor) pop()           { p.stack = p.stack[:len(p.stack)-1] }

func (p *preprocessor) skipAnnotations() bool {
	return p.opt.FFFeatures&CoFutureAnnotations != 0
}

// foldBody walks a stmt body and applies docstring removal under -OO
// plus the leading-string-rewrap rule.
//
// CPython: Python/ast_preprocess.c astfold_body
func (p *preprocessor) foldBody(bodyPtr *Seq[Stmt]) {
	body := *bodyPtr
	hadDoc := body.Len() > 0 && IsDocString(body.Get(0))
	if hadDoc && p.opt.OptimizeLevel >= 2 && !p.opt.SyntaxCheckOnly {
		removeDocstring(bodyPtr)
		body = *bodyPtr
		hadDoc = false
	}
	for i := 0; i < body.Len(); i++ {
		p.foldStmt(body.Get(i))
	}
	if !hadDoc && body.Len() > 0 && IsDocString(body.Get(0)) {
		es := body.Get(0).(*ExprStmt)
		values := NewSeq[Expr](1)
		values.Set(0, es.Value)
		es.Value = &JoinedStr{Values: values, Pos: es.Pos}
	}
}

// removeDocstring drops the leading docstring; if it was the only
// statement, the slot is replaced with `Pass` so the body stays
// non-empty.
//
// CPython: Python/ast_preprocess.c remove_docstring
func removeDocstring(bodyPtr *Seq[Stmt]) {
	body := *bodyPtr
	ds := body.Get(0).(*ExprStmt)
	if body.Len() == 1 {
		body.Set(0, &Pass{Pos: Pos{
			Lineno:       ds.Pos.Lineno,
			ColOffset:    ds.Pos.ColOffset,
			EndLineno:    ds.Pos.Lineno,
			EndColOffset: ds.Pos.ColOffset + 4,
		}})
		return
	}
	*bodyPtr = body[1:]
}

// foldStmt visits one statement, descending into children and updating
// cf-context. Mirrors astfold_stmt's per-kind switch.
//
// CPython: Python/ast_preprocess.c astfold_stmt
func (p *preprocessor) foldStmt(s Stmt) {
	if p.foldStmtFunc(s) {
		return
	}
	if p.foldStmtAssign(s) {
		return
	}
	if p.foldStmtControl(s) {
		return
	}
	p.foldStmtMisc(s)
}

func (p *preprocessor) foldStmtFunc(s Stmt) bool {
	switch n := s.(type) {
	case *FunctionDef:
		p.foldTypeParams(n.TypeParams)
		p.foldArguments(n.Args)
		p.push(cfFrame{inFuncDef: true})
		p.foldBody(&n.Body)
		p.pop()
		p.foldExprSeq(n.DecoratorList)
		if !p.skipAnnotations() {
			n.Returns = p.foldExpr(n.Returns)
		}
	case *AsyncFunctionDef:
		p.foldTypeParams(n.TypeParams)
		p.foldArguments(n.Args)
		p.push(cfFrame{inFuncDef: true})
		p.foldBody(&n.Body)
		p.pop()
		p.foldExprSeq(n.DecoratorList)
		if !p.skipAnnotations() {
			n.Returns = p.foldExpr(n.Returns)
		}
	case *ClassDef:
		p.foldTypeParams(n.TypeParams)
		p.foldExprSeq(n.Bases)
		for i := 0; i < n.Keywords.Len(); i++ {
			k := n.Keywords.Get(i)
			k.Value = p.foldExpr(k.Value)
		}
		p.foldBody(&n.Body)
		p.foldExprSeq(n.DecoratorList)
	default:
		return false
	}
	return true
}

func (p *preprocessor) foldStmtAssign(s Stmt) bool {
	switch n := s.(type) {
	case *Return:
		p.checkFinallyExit("return", n.Position())
		n.Value = p.foldExpr(n.Value)
	case *Delete:
		p.foldExprSeq(n.Targets)
	case *Assign:
		p.foldExprSeq(n.Targets)
		n.Value = p.foldExpr(n.Value)
	case *AugAssign:
		n.Target = p.foldExpr(n.Target)
		n.Value = p.foldExpr(n.Value)
	case *AnnAssign:
		n.Target = p.foldExpr(n.Target)
		if !p.skipAnnotations() {
			n.Annotation = p.foldExpr(n.Annotation)
		}
		n.Value = p.foldExpr(n.Value)
	case *TypeAlias:
		n.Name = p.foldExpr(n.Name)
		p.foldTypeParams(n.TypeParams)
		n.Value = p.foldExpr(n.Value)
	default:
		return false
	}
	return true
}

func (p *preprocessor) foldStmtControl(s Stmt) bool {
	switch n := s.(type) {
	case *For:
		n.Target = p.foldExpr(n.Target)
		n.Iter = p.foldExpr(n.Iter)
		p.foldLoopBody(n.Body, n.Orelse)
	case *AsyncFor:
		n.Target = p.foldExpr(n.Target)
		n.Iter = p.foldExpr(n.Iter)
		p.foldLoopBody(n.Body, n.Orelse)
	case *While:
		n.Test = p.foldExpr(n.Test)
		p.foldLoopBody(n.Body, n.Orelse)
	case *If:
		n.Test = p.foldExpr(n.Test)
		p.foldStmtSeq(n.Body)
		p.foldStmtSeq(n.Orelse)
	case *Try:
		p.foldTry(n.Body, n.Handlers, n.Orelse, n.Finalbody)
	case *TryStar:
		p.foldTry(n.Body, n.Handlers, n.Orelse, n.Finalbody)
	default:
		return false
	}
	return true
}

func (p *preprocessor) foldStmtMisc(s Stmt) {
	switch n := s.(type) {
	case *With:
		p.foldWithItems(n.Items)
		p.foldStmtSeq(n.Body)
	case *AsyncWith:
		p.foldWithItems(n.Items)
		p.foldStmtSeq(n.Body)
	case *Raise:
		n.Exc = p.foldExpr(n.Exc)
		n.Cause = p.foldExpr(n.Cause)
	case *Assert:
		n.Test = p.foldExpr(n.Test)
		n.Msg = p.foldExpr(n.Msg)
	case *ExprStmt:
		n.Value = p.foldExpr(n.Value)
	case *Match:
		p.foldMatch(n)
	case *Break:
		p.checkLoopExit("break", n.Position())
	case *Continue:
		p.checkLoopExit("continue", n.Position())
	}
}

func (p *preprocessor) foldStmtSeq(body Seq[Stmt]) {
	for i := 0; i < body.Len(); i++ {
		p.foldStmt(body.Get(i))
	}
}

func (p *preprocessor) foldLoopBody(body, orelse Seq[Stmt]) {
	p.push(cfFrame{inLoop: true})
	p.foldStmtSeq(body)
	p.pop()
	p.foldStmtSeq(orelse)
}

func (p *preprocessor) foldMatch(n *Match) {
	n.Subject = p.foldExpr(n.Subject)
	for i := 0; i < n.Cases.Len(); i++ {
		c := n.Cases.Get(i)
		p.foldPattern(c.Pattern)
		c.Guard = p.foldExpr(c.Guard)
		p.foldStmtSeq(c.Body)
	}
}

func (p *preprocessor) foldTry(body Seq[Stmt], handlers Seq[Excepthandler], orelse, finalbody Seq[Stmt]) {
	p.foldStmtSeq(body)
	for i := 0; i < handlers.Len(); i++ {
		if h, ok := handlers.Get(i).(*ExceptHandler); ok {
			h.Type = p.foldExpr(h.Type)
			p.foldStmtSeq(h.Body)
		}
	}
	p.foldStmtSeq(orelse)
	p.push(cfFrame{inFinally: true})
	p.foldStmtSeq(finalbody)
	p.pop()
}

func (p *preprocessor) foldWithItems(items Seq[*Withitem]) {
	for i := 0; i < items.Len(); i++ {
		w := items.Get(i)
		w.ContextExpr = p.foldExpr(w.ContextExpr)
		w.OptionalVars = p.foldExpr(w.OptionalVars)
	}
}

func (p *preprocessor) foldArguments(a *Arguments) {
	if a == nil {
		return
	}
	for i := 0; i < a.Posonlyargs.Len(); i++ {
		p.foldArg(a.Posonlyargs.Get(i))
	}
	for i := 0; i < a.Args.Len(); i++ {
		p.foldArg(a.Args.Get(i))
	}
	if a.Vararg != nil {
		p.foldArg(a.Vararg)
	}
	for i := 0; i < a.Kwonlyargs.Len(); i++ {
		p.foldArg(a.Kwonlyargs.Get(i))
	}
	p.foldExprSeq(a.KwDefaults)
	if a.Kwarg != nil {
		p.foldArg(a.Kwarg)
	}
	p.foldExprSeq(a.Defaults)
}

func (p *preprocessor) foldArg(a *Arg) {
	if !p.skipAnnotations() {
		a.Annotation = p.foldExpr(a.Annotation)
	}
}

func (p *preprocessor) foldTypeParams(tps Seq[TypeParam]) {
	for i := 0; i < tps.Len(); i++ {
		switch t := tps.Get(i).(type) {
		case *TypeVar:
			t.Bound = p.foldExpr(t.Bound)
			t.DefaultValue = p.foldExpr(t.DefaultValue)
		case *ParamSpec:
			t.DefaultValue = p.foldExpr(t.DefaultValue)
		case *TypeVarTuple:
			t.DefaultValue = p.foldExpr(t.DefaultValue)
		}
	}
}

func (p *preprocessor) foldExprSeq(seq Seq[Expr]) {
	for i := 0; i < seq.Len(); i++ {
		seq.Set(i, p.foldExpr(seq.Get(i)))
	}
}

// foldExpr recursively folds an expression. Returns the (possibly
// replaced) node; callers must reassign the result.
//
// CPython: Python/ast_preprocess.c astfold_expr
func (p *preprocessor) foldExpr(e Expr) Expr {
	if e == nil {
		return nil
	}
	if r, handled := p.foldExprOps(e); handled {
		return r
	}
	if r, handled := p.foldExprColl(e); handled {
		return r
	}
	if r, handled := p.foldExprStrings(e); handled {
		return r
	}
	return p.foldExprAtoms(e)
}

func (p *preprocessor) foldExprOps(e Expr) (Expr, bool) {
	switch n := e.(type) {
	case *BoolOp:
		p.foldExprSeq(n.Values)
	case *BinOp:
		n.Left = p.foldExpr(n.Left)
		n.Right = p.foldExpr(n.Right)
		return p.foldBinop(n), true
	case *UnaryOp:
		n.Operand = p.foldExpr(n.Operand)
	case *Lambda:
		p.foldArguments(n.Args)
		n.Body = p.foldExpr(n.Body)
	case *IfExp:
		n.Test = p.foldExpr(n.Test)
		n.Body = p.foldExpr(n.Body)
		n.Orelse = p.foldExpr(n.Orelse)
	case *Compare:
		n.Left = p.foldExpr(n.Left)
		p.foldExprSeq(n.Comparators)
	case *NamedExpr:
		n.Value = p.foldExpr(n.Value)
	default:
		return nil, false
	}
	return e, true
}

func (p *preprocessor) foldExprColl(e Expr) (Expr, bool) {
	switch n := e.(type) {
	case *Dict:
		p.foldExprSeq(n.Keys)
		p.foldExprSeq(n.Values)
	case *Set:
		p.foldExprSeq(n.Elts)
	case *ListComp:
		n.Elt = p.foldExpr(n.Elt)
		p.foldComprehensions(n.Generators)
	case *SetComp:
		n.Elt = p.foldExpr(n.Elt)
		p.foldComprehensions(n.Generators)
	case *DictComp:
		n.Key = p.foldExpr(n.Key)
		n.Value = p.foldExpr(n.Value)
		p.foldComprehensions(n.Generators)
	case *GeneratorExp:
		n.Elt = p.foldExpr(n.Elt)
		p.foldComprehensions(n.Generators)
	case *List:
		p.foldExprSeq(n.Elts)
	case *Tuple:
		p.foldExprSeq(n.Elts)
	case *Call:
		n.Func = p.foldExpr(n.Func)
		p.foldExprSeq(n.Args)
		for i := 0; i < n.Keywords.Len(); i++ {
			k := n.Keywords.Get(i)
			k.Value = p.foldExpr(k.Value)
		}
	default:
		return nil, false
	}
	return e, true
}

func (p *preprocessor) foldExprStrings(e Expr) (Expr, bool) {
	switch n := e.(type) {
	case *FormattedValue:
		n.Value = p.foldExpr(n.Value)
		n.FormatSpec = p.foldExpr(n.FormatSpec)
	case *Interpolation:
		n.Value = p.foldExpr(n.Value)
		n.FormatSpec = p.foldExpr(n.FormatSpec)
	case *JoinedStr:
		p.foldExprSeq(n.Values)
	case *TemplateStr:
		p.foldExprSeq(n.Values)
	default:
		return nil, false
	}
	return e, true
}

func (p *preprocessor) foldExprAtoms(e Expr) Expr {
	switch n := e.(type) {
	case *Await:
		n.Value = p.foldExpr(n.Value)
	case *Yield:
		n.Value = p.foldExpr(n.Value)
	case *YieldFrom:
		n.Value = p.foldExpr(n.Value)
	case *Attribute:
		n.Value = p.foldExpr(n.Value)
	case *Subscript:
		n.Value = p.foldExpr(n.Value)
		n.Slice = p.foldExpr(n.Slice)
	case *Starred:
		n.Value = p.foldExpr(n.Value)
	case *Slice:
		n.Lower = p.foldExpr(n.Lower)
		n.Upper = p.foldExpr(n.Upper)
		n.Step = p.foldExpr(n.Step)
	case *Name:
		return p.foldName(n)
	case *Constant:
		// already constant
	}
	return e
}

func (p *preprocessor) foldComprehensions(gens Seq[*Comprehension]) {
	for i := 0; i < gens.Len(); i++ {
		c := gens.Get(i)
		c.Target = p.foldExpr(c.Target)
		c.Iter = p.foldExpr(c.Iter)
		p.foldExprSeq(c.Ifs)
	}
}

// foldName replaces `__debug__` with a Constant tied to the optimize
// level. CPython does this in astfold_expr's Name_kind branch.
func (p *preprocessor) foldName(n *Name) Expr {
	if p.opt.SyntaxCheckOnly {
		return n
	}
	if n.Ctx != Load || n.Id != "__debug__" {
		return n
	}
	return &Constant{Value: p.opt.OptimizeLevel == 0, Pos: n.Pos}
}

// foldPattern walks a match pattern. The relevant fold is on
// MatchValue and MatchMapping keys: USub of a Constant, and Add/Sub
// of all-Constant operands collapse so user patterns like
// `case -1:` and `case 1+2j:` still work.
//
// CPython: Python/ast_preprocess.c astfold_pattern + fold_const_match_patterns
func (p *preprocessor) foldPattern(pat Pattern) {
	switch n := pat.(type) {
	case *MatchValue:
		n.Value = p.foldMatchPattern(n.Value)
	case *MatchSingleton:
		// no subexpressions
	case *MatchSequence:
		for i := 0; i < n.Patterns.Len(); i++ {
			p.foldPattern(n.Patterns.Get(i))
		}
	case *MatchMapping:
		for i := 0; i < n.Keys.Len(); i++ {
			n.Keys.Set(i, p.foldMatchPattern(n.Keys.Get(i)))
		}
		for i := 0; i < n.Patterns.Len(); i++ {
			p.foldPattern(n.Patterns.Get(i))
		}
	case *MatchClass:
		n.Cls = p.foldExpr(n.Cls)
		for i := 0; i < n.Patterns.Len(); i++ {
			p.foldPattern(n.Patterns.Get(i))
		}
		for i := 0; i < n.KwdPatterns.Len(); i++ {
			p.foldPattern(n.KwdPatterns.Get(i))
		}
	case *MatchStar:
		// no subexpressions
	case *MatchAs:
		if n.Pattern != nil {
			p.foldPattern(n.Pattern)
		}
	case *MatchOr:
		for i := 0; i < n.Patterns.Len(); i++ {
			p.foldPattern(n.Patterns.Get(i))
		}
	}
}

// foldMatchPattern collapses USub of Constant and Add/Sub of all-
// Constant operands. Mirrors fold_const_match_patterns. Only descends
// when not in syntax-check-only mode (no mutation otherwise).
func (p *preprocessor) foldMatchPattern(e Expr) Expr {
	if e == nil || p.opt.SyntaxCheckOnly {
		return e
	}
	switch n := e.(type) {
	case *UnaryOp:
		if n.Op != USub {
			return e
		}
		c, ok := n.Operand.(*Constant)
		if !ok {
			return e
		}
		v, ok := negateConst(c.Value)
		if !ok {
			return e
		}
		return &Constant{Value: v, Pos: c.Pos}
	case *BinOp:
		if n.Op != Add && n.Op != Sub {
			return e
		}
		rc, ok := n.Right.(*Constant)
		if !ok {
			return e
		}
		n.Left = p.foldMatchPattern(n.Left)
		lc, ok := n.Left.(*Constant)
		if !ok {
			return e
		}
		v, ok := combineConst(lc.Value, rc.Value, n.Op == Sub)
		if !ok {
			return e
		}
		return &Constant{Value: v, Pos: lc.Pos}
	}
	return e
}

// foldBinop handles the only BinOp fold ast_preprocess.c keeps: the
// printf-style `string % tuple` collapse to a `JoinedStr`. Other BinOp
// folds happen in flowgraph LOAD_CONST chain folding.
//
// CPython: Python/ast_preprocess.c fold_binop
func (p *preprocessor) foldBinop(n *BinOp) Expr {
	if p.opt.SyntaxCheckOnly {
		return n
	}
	if n.Op != ModOperator {
		return n
	}
	lc, ok := n.Left.(*Constant)
	if !ok {
		return n
	}
	s, ok := lc.Value.(string)
	if !ok {
		return n
	}
	rt, ok := n.Right.(*Tuple)
	if !ok {
		return n
	}
	if hasStarred(rt.Elts) {
		return n
	}
	js, ok := optimizeFormat(s, rt.Elts, n.Pos)
	if !ok {
		return n
	}
	return js
}

func hasStarred(elts Seq[Expr]) bool {
	for i := 0; i < elts.Len(); i++ {
		if _, ok := elts.Get(i).(*Starred); ok {
			return true
		}
	}
	return false
}

// optimizeFormat parses the format string and, when every spec is
// supported, returns a JoinedStr that renders the same value at
// runtime via f-string-style assembly. Returns ok=false when an
// unsupported spec is encountered.
//
// CPython: Python/ast_preprocess.c optimize_format
func optimizeFormat(s string, elts Seq[Expr], pos Pos) (*JoinedStr, bool) {
	values := NewSeq[Expr](0)
	pp := 0
	cnt := 0
	for {
		if lit, ok := parseLiteral(s, &pp); ok {
			values = append(values, &Constant{Value: lit, Pos: pos})
		}
		if pp >= len(s) {
			break
		}
		if cnt >= elts.Len() || s[pp] != '%' {
			return nil, false
		}
		pp++
		fv, ok := parseFormat(s, &pp, elts.Get(cnt), pos)
		cnt++
		if !ok {
			return nil, false
		}
		values = append(values, fv)
	}
	if cnt < elts.Len() {
		return nil, false
	}
	return &JoinedStr{Values: values, Pos: pos}, true
}

// parseLiteral returns the literal segment starting at *pp, advancing
// *pp past it. `%%` collapses to `%`. Returns ok=false when nothing was
// consumed (immediate `%` spec).
//
// CPython: Python/ast_preprocess.c parse_literal
func parseLiteral(s string, pp *int) (string, bool) {
	start := *pp
	pos := *pp
	hasPercents := false
	for pos < len(s) {
		if s[pos] != '%' {
			pos++
			continue
		}
		if pos+1 < len(s) && s[pos+1] == '%' {
			hasPercents = true
			pos += 2
			continue
		}
		break
	}
	*pp = pos
	if pos == start {
		return "", false
	}
	out := s[start:pos]
	if hasPercents {
		out = strings.ReplaceAll(out, "%%", "%")
	}
	return out, true
}

const maxDigits = 3

// parseFormat parses a single `%`-conversion spec at s[*pp:] and
// returns a FormattedValue wrapping arg with the equivalent format
// spec. Mirrors simple_format_arg_parse + parse_format.
//
// CPython: Python/ast_preprocess.c simple_format_arg_parse, parse_format
func parseFormat(s string, pp *int, arg Expr, pos Pos) (Expr, bool) {
	r, ok := parseFormatSpec(s, pp)
	if !ok {
		return nil, false
	}
	if r.spec != 's' && r.spec != 'r' && r.spec != 'a' {
		return nil, false
	}
	var b strings.Builder
	if r.flags&fLjust == 0 && r.width > 0 {
		b.WriteByte('>')
	}
	if r.width >= 0 {
		b.WriteString(strconv.Itoa(r.width))
	}
	if r.prec >= 0 {
		b.WriteByte('.')
		b.WriteString(strconv.Itoa(r.prec))
	}
	var formatSpec Expr
	if b.Len() > 0 {
		values := NewSeq[Expr](1)
		values.Set(0, &Constant{Value: b.String(), Pos: pos})
		formatSpec = &JoinedStr{Values: values, Pos: pos}
	}
	return &FormattedValue{
		Value:      arg,
		Conversion: r.spec,
		FormatSpec: formatSpec,
		Pos:        pos,
	}, true
}

// flag bits mirror pycore_format.h F_*; only F_LJUST is observable in
// our supported subset, but the parser still recognizes the others
// (and conservatively bails on unsupported specifiers).
const (
	fLjust = 1 << iota
	fSign
	fBlank
	fAlt
	fZero
)

type formatSpec struct {
	flags, width, prec, spec int
}

// parseFormatSpec is a thin shim around fmtCursor that reads flag
// bits, optional width, optional precision, and the conversion code
// from s[*pp:], advancing *pp on success. Returns ok=false on
// unexpected EOF or malformed numeric runs.
func parseFormatSpec(s string, pp *int) (formatSpec, bool) {
	c := &fmtCursor{s: s, pos: *pp}
	r := formatSpec{width: -1, prec: -1}
	ch, ok := c.next()
	if !ok {
		return r, false
	}
	ch, ok = parseFormatFlags(c, &r, ch)
	if !ok {
		return r, false
	}
	ch, ok = parseFormatWidth(c, &r, ch)
	if !ok {
		return r, false
	}
	ch, ok = parseFormatPrec(c, &r, ch)
	if !ok {
		return r, false
	}
	r.spec = int(ch)
	*pp = c.pos
	return r, true
}

type fmtCursor struct {
	s   string
	pos int
}

func (c *fmtCursor) next() (byte, bool) {
	if c.pos >= len(c.s) {
		return 0, false
	}
	ch := c.s[c.pos]
	c.pos++
	return ch, true
}

func parseFormatFlags(c *fmtCursor, r *formatSpec, ch byte) (byte, bool) {
	for {
		bit, ok := flagBit(ch)
		if !ok {
			return ch, true
		}
		r.flags |= bit
		next, ok := c.next()
		if !ok {
			return 0, false
		}
		ch = next
	}
}

func flagBit(ch byte) (int, bool) {
	switch ch {
	case '-':
		return fLjust, true
	case '+':
		return fSign, true
	case ' ':
		return fBlank, true
	case '#':
		return fAlt, true
	case '0':
		return fZero, true
	}
	return 0, false
}

func parseFormatWidth(c *fmtCursor, r *formatSpec, ch byte) (byte, bool) {
	if ch < '0' || ch > '9' {
		return ch, true
	}
	r.width = 0
	return parseDigits(c, ch, &r.width)
}

func parseFormatPrec(c *fmtCursor, r *formatSpec, ch byte) (byte, bool) {
	if ch != '.' {
		return ch, true
	}
	next, ok := c.next()
	if !ok {
		return 0, false
	}
	r.prec = 0
	if next < '0' || next > '9' {
		return next, true
	}
	return parseDigits(c, next, &r.prec)
}

// parseDigits reads up to maxDigits-1 base-10 digits into *out and
// returns the first non-digit character (or false on EOF / overflow).
func parseDigits(c *fmtCursor, ch byte, out *int) (byte, bool) {
	digits := 0
	for ch >= '0' && ch <= '9' {
		*out = *out*10 + int(ch-'0')
		next, ok := c.next()
		if !ok {
			return 0, false
		}
		digits++
		if digits >= maxDigits {
			return 0, false
		}
		ch = next
	}
	return ch, true
}

// negateConst returns -v for the const kinds CPython supports (int,
// float, complex). Mirrors PyNumber_Negative on the supported subset.
func negateConst(v any) (any, bool) {
	switch x := v.(type) {
	case int:
		return -x, true
	case int64:
		return -x, true
	case float64:
		return -x, true
	case complex128:
		return -x, true
	}
	return nil, false
}

// combineConst returns lhs+rhs (sub=false) or lhs-rhs (sub=true) for
// numeric constants. Mirrors PyNumber_Add / PyNumber_Subtract on the
// match-pattern subset.
func combineConst(lhs, rhs any, sub bool) (any, bool) {
	if l, lf := toComplex(lhs); lf {
		if r, rf := toComplex(rhs); rf {
			if sub {
				return l - r, true
			}
			return l + r, true
		}
	}
	return nil, false
}

func toComplex(v any) (complex128, bool) {
	switch x := v.(type) {
	case int:
		return complex(float64(x), 0), true
	case int64:
		return complex(float64(x), 0), true
	case float64:
		return complex(x, 0), true
	case complex128:
		return x, true
	}
	return 0, false
}

// checkFinallyExit mirrors before_return: a `return` is flagged if any
// enclosing finally block is active *before* an enclosing funcdef.
func (p *preprocessor) checkFinallyExit(kw string, pos Pos) {
	if !p.opt.EnableWarnings {
		return
	}
	for i := len(p.stack) - 1; i >= 0; i-- {
		f := p.stack[i]
		if f.inFuncDef {
			return
		}
		if f.inFinally {
			p.emit(kw, pos)
			return
		}
	}
}

// checkLoopExit mirrors before_loop_exit: a `break`/`continue` is
// flagged if any enclosing finally is active before an enclosing loop.
func (p *preprocessor) checkLoopExit(kw string, pos Pos) {
	if !p.opt.EnableWarnings {
		return
	}
	for i := len(p.stack) - 1; i >= 0; i-- {
		f := p.stack[i]
		if f.inLoop {
			return
		}
		if f.inFinally {
			p.emit(kw, pos)
			return
		}
	}
}

func (p *preprocessor) emit(kw string, pos Pos) {
	p.warnings = append(p.warnings, Warning{
		Msg:      fmt.Sprintf("'%s' in a 'finally' block", kw),
		Filename: p.opt.Filename,
		Pos:      pos,
	})
}
