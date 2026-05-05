// Hand-written action helpers that the generated parser calls into.
// The generator's stub emitter excludes the names defined here; the
// real implementations win and surface typed AST nodes from the
// untyped any results the per-rule parsers return.
//
// Each helper does the type assertions and builds the corresponding
// ast.* node. Where the upstream rule chain has not yet been typed
// (the surrounding expression rules still wrap their results as
// []any{...}), the helper returns placeholderMatched so the alt
// counts as matched without producing a typed node. Dispatch
// surfaces ErrParserNotImplemented for those cases, the gate test
// skips, and the typed-path coverage grows as the rule chain types up.
//
// CPython: Parser/action_helpers.c, Parser/pegen.c

package pegen

import (
	"strconv"
	"strings"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/tokenize"
)

// argAt returns args[i] or nil if i is out of range. The action
// translator emits variadic call sites; trailing optional bindings
// can be missing.
func argAt(args []any, i int) any {
	if i < 0 || i >= len(args) {
		return nil
	}
	return args[i]
}

// asExpr unwraps any layers of []any wrapping until it lands on an
// ast.Expr or runs out of structure. Many expression rules currently
// wrap their inner result as []any{e} or []any{e, rest}; the helpers
// pull the typed value back out. Returns nil if no Expr is found.
func asExpr(v any) ast.Expr {
	for {
		switch x := v.(type) {
		case nil:
			return nil
		case ast.Expr:
			return x
		case []any:
			if len(x) == 0 {
				return nil
			}
			v = x[0]
		default:
			return nil
		}
	}
}

// asStmt is the Stmt counterpart to asExpr.
func asStmt(v any) ast.Stmt {
	for {
		switch x := v.(type) {
		case nil:
			return nil
		case ast.Stmt:
			return x
		case []any:
			if len(x) == 0 {
				return nil
			}
			v = x[0]
		default:
			return nil
		}
	}
}

// flattenStmts walks v recursively and collects every ast.Stmt it
// finds. The shape coming out of statements / loop1 / gather rules
// nests deeply ([]any of []any of ...) so a flat traversal beats
// trying to enumerate every wrapper layer.
func flattenStmts(v any) []ast.Stmt {
	var out []ast.Stmt
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
			return
		case ast.Stmt:
			out = append(out, t)
		case ast.Seq[ast.Stmt]:
			out = append(out, t...)
		case []ast.Stmt:
			out = append(out, t...)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// flattenTokens collects every *Token in v, in order.
func flattenTokens(v any) []*Token {
	var out []*Token
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
			return
		case *Token:
			out = append(out, t)
		case []*Token:
			out = append(out, t...)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// stmtSeqOf coerces v into a Seq[Stmt] by recursively flattening.
// CPython: Parser/action_helpers.c the stmt-seq plumbing.
func stmtSeqOf(v any) ast.Seq[ast.Stmt] {
	stmts := flattenStmts(v)
	if len(stmts) == 0 {
		return nil
	}
	return ast.Seq[ast.Stmt](stmts)
}

// actionPgenMakeModule builds a Module from the optional statements
// list. Variadic shape mirrors the generated stub signature; the
// call site passes (p, p, a) where the inner p is the C-action's
// explicit p arg and a is the statements slice (or nil).
//
// CPython: Parser/pegen.c:1203 _PyPegen_make_module
func actionPgenMakeModule(p *Parser, args ...any) any {
	_ = p
	body := stmtSeqOf(argAt(args, 1))
	return &ast.Module{Body: body}
}

// actionPgenInteractiveExit returns the sentinel CPython uses to end
// interactive mode on a bare ENDMARKER. The Go side returns nil here
// so Dispatch's retry loop falls through.
//
// CPython: Parser/pegen.c:1244 _PyPegen_interactive_exit
func actionPgenInteractiveExit(p *Parser, args ...any) any {
	_ = p
	_ = args
	return nil
}

func actionAstPass(p *Parser, args ...any) any {
	_ = p
	_ = args
	return &ast.Pass{Pos: ast.NoPos}
}

func actionAstBreak(p *Parser, args ...any) any {
	_ = p
	_ = args
	return &ast.Break{Pos: ast.NoPos}
}

func actionAstContinue(p *Parser, args ...any) any {
	_ = p
	_ = args
	return &ast.Continue{Pos: ast.NoPos}
}

// actionAstReturn builds Return. Args: (value).
func actionAstReturn(p *Parser, args ...any) any {
	_ = p
	return &ast.Return{Value: asExpr(argAt(args, 0)), Pos: ast.NoPos}
}

// actionAstRaise builds Raise. Args: (exc, cause). cause may be nil.
func actionAstRaise(p *Parser, args ...any) any {
	_ = p
	return &ast.Raise{
		Exc:   asExpr(argAt(args, 0)),
		Cause: asExpr(argAt(args, 1)),
		Pos:   ast.NoPos,
	}
}

// actionAstAssert builds Assert. Args: (test, msg).
func actionAstAssert(p *Parser, args ...any) any {
	_ = p
	test := asExpr(argAt(args, 0))
	if test == nil {
		return placeholderMatched
	}
	return &ast.Assert{Test: test, Msg: asExpr(argAt(args, 1)), Pos: ast.NoPos}
}

// actionAstDelete builds Delete. Args: (targets-seq).
func actionAstDelete(p *Parser, args ...any) any {
	_ = p
	targets := exprSeqOf(argAt(args, 0))
	if len(targets) == 0 {
		return placeholderMatched
	}
	for i := range targets {
		targets[i] = SetExprContext(targets[i], ast.Del)
	}
	return &ast.Delete{Targets: targets, Pos: ast.NoPos}
}

// actionAstExpr builds ExprStmt. Args: (expr).
func actionAstExpr(p *Parser, args ...any) any {
	_ = p
	v := asExpr(argAt(args, 0))
	if v == nil {
		return placeholderMatched
	}
	return &ast.ExprStmt{Value: v, Pos: ast.NoPos}
}

// actionAstYield builds Yield. Args: (value).
func actionAstYield(p *Parser, args ...any) any {
	_ = p
	return &ast.Yield{Value: asExpr(argAt(args, 0)), Pos: ast.NoPos}
}

// actionAstYieldFrom builds YieldFrom. Args: (value).
func actionAstYieldFrom(p *Parser, args ...any) any {
	_ = p
	v := asExpr(argAt(args, 0))
	if v == nil {
		return placeholderMatched
	}
	return &ast.YieldFrom{Value: v, Pos: ast.NoPos}
}

// actionAstImport builds Import. Args: (names: dotted_as_names).
//
// CPython: Parser/Python.asdl Import(alias* names)
func actionAstImport(p *Parser, args ...any) any {
	_ = p
	names := aliasSeqOf(argAt(args, 0))
	if len(names) == 0 {
		return placeholderMatched
	}
	return &ast.Import{Names: names, Pos: ast.NoPos}
}

// actionAstImportFrom builds ImportFrom. Args: (module, names, level).
func actionAstImportFrom(p *Parser, args ...any) any {
	_ = p
	mod := nameOf(argAt(args, 0))
	names := aliasSeqOf(argAt(args, 1))
	lvl := intOf(argAt(args, 2))
	if len(names) == 0 && mod == nil {
		return placeholderMatched
	}
	out := &ast.ImportFrom{Names: names, Pos: ast.NoPos}
	if mod != nil {
		s := *mod
		out.Module = &s
	}
	if lvl != nil {
		out.Level = lvl
	}
	return out
}

// actionAstIf builds If. Args: (test, body, elif/else).
func actionAstIf(p *Parser, args ...any) any {
	_ = p
	test := asExpr(argAt(args, 0))
	body := stmtSeqOf(argAt(args, 1))
	orelse := stmtSeqOf(argAt(args, 2))
	if test == nil || len(body) == 0 {
		return placeholderMatched
	}
	return &ast.If{Test: test, Body: body, Orelse: orelse, Pos: ast.NoPos}
}

// actionAstWhile builds While. Args: (test, body, else).
func actionAstWhile(p *Parser, args ...any) any {
	_ = p
	test := asExpr(argAt(args, 0))
	body := stmtSeqOf(argAt(args, 1))
	orelse := stmtSeqOf(argAt(args, 2))
	if test == nil || len(body) == 0 {
		return placeholderMatched
	}
	return &ast.While{Test: test, Body: body, Orelse: orelse, Pos: ast.NoPos}
}

// actionAstIfExp builds IfExp. Args: (body, test, orelse).
func actionAstIfExp(p *Parser, args ...any) any {
	_ = p
	body := asExpr(argAt(args, 0))
	test := asExpr(argAt(args, 1))
	orelse := asExpr(argAt(args, 2))
	if body == nil || test == nil || orelse == nil {
		return placeholderMatched
	}
	return &ast.IfExp{Test: test, Body: body, Orelse: orelse, Pos: ast.NoPos}
}

// actionAstTry builds Try. Args: (body, handlers, orelse, finalbody).
func actionAstTry(p *Parser, args ...any) any {
	_ = p
	body := stmtSeqOf(argAt(args, 0))
	handlers := exceptHandlerSeqOf(argAt(args, 1))
	orelse := stmtSeqOf(argAt(args, 2))
	finally := stmtSeqOf(argAt(args, 3))
	if len(body) == 0 {
		return placeholderMatched
	}
	return &ast.Try{Body: body, Handlers: handlers, Orelse: orelse, Finalbody: finally, Pos: ast.NoPos}
}

// actionAstExceptHandler builds ExceptHandler. Args: (type, name, body).
func actionAstExceptHandler(p *Parser, args ...any) any {
	_ = p
	body := stmtSeqOf(argAt(args, 2))
	if len(body) == 0 {
		return placeholderMatched
	}
	out := &ast.ExceptHandler{Type: asExpr(argAt(args, 0)), Body: body, Pos: ast.NoPos}
	if n := nameOf(argAt(args, 1)); n != nil {
		out.Name = n
	}
	return out
}

// actionAstSlice builds Slice. Args: (lower, upper, step).
func actionAstSlice(p *Parser, args ...any) any {
	_ = p
	return &ast.Slice{
		Lower: asExpr(argAt(args, 0)),
		Upper: asExpr(argAt(args, 1)),
		Step:  asExpr(argAt(args, 2)),
		Pos:   ast.NoPos,
	}
}

// actionAstSet builds Set. Args: (elts).
func actionAstSet(p *Parser, args ...any) any {
	_ = p
	elts := exprSeqOf(argAt(args, 0))
	return &ast.Set{Elts: elts, Pos: ast.NoPos}
}

// actionAstListComp builds ListComp. Args: (elt, generators).
func actionAstListComp(p *Parser, args ...any) any {
	_ = p
	elt := asExpr(argAt(args, 0))
	gens := comprehensionSeqOf(argAt(args, 1))
	if elt == nil || len(gens) == 0 {
		return placeholderMatched
	}
	return &ast.ListComp{Elt: elt, Generators: gens, Pos: ast.NoPos}
}

// actionAstSetComp builds SetComp.
func actionAstSetComp(p *Parser, args ...any) any {
	_ = p
	elt := asExpr(argAt(args, 0))
	gens := comprehensionSeqOf(argAt(args, 1))
	if elt == nil || len(gens) == 0 {
		return placeholderMatched
	}
	return &ast.SetComp{Elt: elt, Generators: gens, Pos: ast.NoPos}
}

// actionAstGeneratorExp builds GeneratorExp.
func actionAstGeneratorExp(p *Parser, args ...any) any {
	_ = p
	elt := asExpr(argAt(args, 0))
	gens := comprehensionSeqOf(argAt(args, 1))
	if elt == nil || len(gens) == 0 {
		return placeholderMatched
	}
	return &ast.GeneratorExp{Elt: elt, Generators: gens, Pos: ast.NoPos}
}

// Match patterns: each helper builds the matching ast.* pattern. The
// upstream pattern rules still produce []any wrappers in many cases
// so the helpers fall back to placeholderMatched when shape is wrong.

func actionAstMatchValue(p *Parser, args ...any) any {
	_ = p
	v := asExpr(argAt(args, 0))
	if v == nil {
		return placeholderMatched
	}
	return &ast.MatchValue{Value: v, Pos: ast.NoPos}
}

func actionAstMatchSequence(p *Parser, args ...any) any {
	_ = p
	pats := patternSeqOf(argAt(args, 0))
	return &ast.MatchSequence{Patterns: pats, Pos: ast.NoPos}
}

func actionAstMatchMapping(p *Parser, args ...any) any {
	_ = p
	keys := exprSeqOf(argAt(args, 0))
	pats := patternSeqOf(argAt(args, 1))
	rest := nameOf(argAt(args, 2))
	out := &ast.MatchMapping{Keys: keys, Patterns: pats, Pos: ast.NoPos}
	if rest != nil {
		out.Rest = rest
	}
	return out
}

func actionAstMatchClass(p *Parser, args ...any) any {
	_ = p
	cls := asExpr(argAt(args, 0))
	if cls == nil {
		return placeholderMatched
	}
	return &ast.MatchClass{
		Cls:         cls,
		Patterns:    patternSeqOf(argAt(args, 1)),
		KwdAttrs:    stringSeqOf(argAt(args, 2)),
		KwdPatterns: patternSeqOf(argAt(args, 3)),
		Pos:         ast.NoPos,
	}
}

func actionAstMatchStar(p *Parser, args ...any) any {
	_ = p
	out := &ast.MatchStar{Pos: ast.NoPos}
	if n := nameOf(argAt(args, 0)); n != nil {
		out.Name = n
	}
	return out
}

func actionAstMatchAs(p *Parser, args ...any) any {
	_ = p
	out := &ast.MatchAs{Pos: ast.NoPos}
	if pat := patternOf(argAt(args, 0)); pat != nil {
		out.Pattern = pat
	}
	if n := nameOf(argAt(args, 1)); n != nil {
		out.Name = n
	}
	return out
}

// SeqCountDots / SingletonSeq / SeqInsertInFront / JoinNamesWithDot
// are direct ports of the helpers in actions.go, with the (p, p, ...)
// variadic call shape the generator emits.
//
// CPython: Parser/action_helpers.c

func actionPgenSeqCountDots(p *Parser, args ...any) any {
	_ = p
	return SeqCountDots(flattenTokens(argAt(args, 1)))
}

func actionPgenSingletonSeq(p *Parser, args ...any) any {
	_ = p
	return []any{argAt(args, 1)}
}

func actionPgenSeqInsertInFront(p *Parser, args ...any) any {
	_ = p
	first := argAt(args, 1)
	rest := argAt(args, 2)
	out := []any{first}
	if r, ok := rest.([]any); ok {
		out = append(out, r...)
	} else if rest != nil {
		out = append(out, rest)
	}
	return out
}

func actionPgenJoinNamesWithDot(p *Parser, args ...any) any {
	_ = p
	a, b := argAt(args, 1), argAt(args, 2)
	an := identString(a)
	bn := identString(b)
	if an == "" || bn == "" {
		return placeholderMatched
	}
	return &ast.Name{Id: an + "." + bn, Ctx: ast.Load, Pos: ast.NoPos}
}

func actionPgenJoinSequences(p *Parser, args ...any) any {
	_ = p
	a := argAt(args, 1)
	b := argAt(args, 2)
	out := []any{}
	if x, ok := a.([]any); ok {
		out = append(out, x...)
	} else if a != nil {
		out = append(out, a)
	}
	if x, ok := b.([]any); ok {
		out = append(out, x...)
	} else if b != nil {
		out = append(out, b)
	}
	return out
}

func actionPgenGetExprName(p *Parser, args ...any) any {
	_ = p
	if e := asExpr(argAt(args, 1)); e != nil {
		return GetExprName(e)
	}
	return "expression"
}

// actionPgenKeyValuePair builds a (key, value) pair the dict / dict
// pattern collectors flatten. CPython uses a KeyValuePair struct;
// the Go side carries it as a 2-element []any so the existing
// flatten helpers see the elements without a new type.
func actionPgenKeyValuePair(p *Parser, args ...any) any {
	_ = p
	return [2]any{argAt(args, 1), argAt(args, 2)}
}

func actionPgenKeyPatternPair(p *Parser, args ...any) any {
	_ = p
	return [2]any{argAt(args, 1), argAt(args, 2)}
}

// actionPgenKeywordOrStarred wraps the (value, is_keyword) shape the
// call-arg collector reads. The gather later splits them.
func actionPgenKeywordOrStarred(p *Parser, args ...any) any {
	_ = p
	return []any{argAt(args, 1), argAt(args, 2)}
}

// actionPgenNameDefaultPair pairs a parameter with its default
// expression. The function-def builder consumes the pairs.
func actionPgenNameDefaultPair(p *Parser, args ...any) any {
	_ = p
	return []any{argAt(args, 1), argAt(args, 2), argAt(args, 3)}
}

// actionPgenConstantFromToken builds a numeric Constant.
//
// CPython: Parser/action_helpers.c:583 _PyPegen_constant_from_token
func actionPgenConstantFromToken(p *Parser, args ...any) any {
	_ = p
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	v, ok := parseNumberLiteral(string(t.Bytes))
	if !ok {
		return placeholderMatched
	}
	return &ast.Constant{Value: v, Pos: ast.NoPos}
}

// actionPgenConstantFromString builds a string-literal Constant.
//
// CPython: Parser/action_helpers.c:601 _PyPegen_constant_from_string
func actionPgenConstantFromString(p *Parser, args ...any) any {
	_ = p
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	s, ok := decodeStringToken(string(t.Bytes))
	if !ok {
		return placeholderMatched
	}
	return &ast.Constant{Value: s, Pos: ast.NoPos}
}

func actionPgenDecodedConstantFromToken(p *Parser, args ...any) any {
	return actionPgenConstantFromString(p, args...)
}

func actionPgenEnsureImaginary(p *Parser, args ...any) any {
	_ = p
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	s := string(t.Bytes)
	if !strings.HasSuffix(s, "j") && !strings.HasSuffix(s, "J") {
		return placeholderMatched
	}
	v, ok := parseNumberLiteral(s)
	if !ok {
		return placeholderMatched
	}
	return &ast.Constant{Value: v, Pos: ast.NoPos}
}

func actionPgenEnsureReal(p *Parser, args ...any) any {
	_ = p
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	s := string(t.Bytes)
	if strings.HasSuffix(s, "j") || strings.HasSuffix(s, "J") {
		return placeholderMatched
	}
	v, ok := parseNumberLiteral(s)
	if !ok {
		return placeholderMatched
	}
	return &ast.Constant{Value: v, Pos: ast.NoPos}
}

// The remaining pgen helpers cover string-formatting, function/class
// build, and call-arg shaping. Each requires substantial scaffolding
// (FormattedValue conversions, Arguments grouping, decorators); they
// stay as placeholderMatched until the typed expression chain lands
// so the gate test continues to skip those shapes cleanly rather
// than mis-typing.

func actionPgenFormattedValue(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenInterpolation(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenConcatenateStrings(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenConcatenateTstrings(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenCheckFstringConversion(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenAddTypeCommentToArg(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenArgumentsParsingError(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenClassDefDecorators(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenFunctionDefDecorators(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenCollectCallSeqs(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenMakeArguments(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenNonparenGenexpInCall(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

func actionPgenStarEtc(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

// --- shape helpers ---

func exprSeqOf(v any) ast.Seq[ast.Expr] {
	var out []ast.Expr
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case ast.Expr:
			out = append(out, t)
		case ast.Seq[ast.Expr]:
			out = append(out, t...)
		case []ast.Expr:
			out = append(out, t...)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[ast.Expr](out)
}

func aliasSeqOf(v any) ast.Seq[*ast.Alias] {
	var out []*ast.Alias
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *ast.Alias:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[*ast.Alias](out)
}

func exceptHandlerSeqOf(v any) ast.Seq[ast.Excepthandler] {
	var out []ast.Excepthandler
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case ast.Excepthandler:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[ast.Excepthandler](out)
}

func comprehensionSeqOf(v any) ast.Seq[*ast.Comprehension] {
	var out []*ast.Comprehension
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *ast.Comprehension:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[*ast.Comprehension](out)
}

func patternSeqOf(v any) ast.Seq[ast.Pattern] {
	var out []ast.Pattern
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case ast.Pattern:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[ast.Pattern](out)
}

func patternOf(v any) ast.Pattern {
	for {
		switch x := v.(type) {
		case nil:
			return nil
		case ast.Pattern:
			return x
		case []any:
			if len(x) == 0 {
				return nil
			}
			v = x[0]
		default:
			return nil
		}
	}
}

func stringSeqOf(v any) ast.Seq[string] {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case string:
			out = append(out, t)
		case *Token:
			if t != nil {
				out = append(out, string(t.Bytes))
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[string](out)
}

// nameOf returns the identifier text for a NAME token or *ast.Name.
func nameOf(v any) *string {
	switch x := v.(type) {
	case nil:
		return nil
	case *Token:
		if x == nil || x.Type != tokenize.NAME {
			return nil
		}
		s := string(x.Bytes)
		return &s
	case *ast.Name:
		s := x.Id
		return &s
	case string:
		s := x
		return &s
	case []any:
		if len(x) == 0 {
			return nil
		}
		return nameOf(x[0])
	}
	return nil
}

// identString returns the text of a NAME token or *ast.Name; "" on
// any other shape.
func identString(v any) string {
	if s := nameOf(v); s != nil {
		return *s
	}
	return ""
}

// intOf extracts an int from an int-typed action result. CPython
// uses C ints for shift counts and module levels; the action emitter
// passes them as native ints.
func intOf(v any) *int {
	switch x := v.(type) {
	case nil:
		return nil
	case int:
		n := x
		return &n
	}
	return nil
}

// parseNumberLiteral turns a NUMBER token's text into the Go value
// CPython's PyAST_Num would land on: int (math/big when oversized,
// kept as int64 for now), float64, or complex128 for the j-suffixed
// form. Returns ok=false if the literal does not parse cleanly.
//
// CPython: Parser/string_parser.c parsenumber
func parseNumberLiteral(s string) (any, bool) {
	if s == "" {
		return nil, false
	}
	clean := strings.ReplaceAll(s, "_", "")
	last := clean[len(clean)-1]
	if last == 'j' || last == 'J' {
		f, err := strconv.ParseFloat(clean[:len(clean)-1], 64)
		if err != nil {
			return nil, false
		}
		return complex(0, f), true
	}
	if strings.ContainsAny(clean, ".eE") && !strings.HasPrefix(clean, "0x") && !strings.HasPrefix(clean, "0X") {
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return nil, false
		}
		return f, true
	}
	base := 10
	body := clean
	switch {
	case strings.HasPrefix(body, "0x"), strings.HasPrefix(body, "0X"):
		base = 16
		body = body[2:]
	case strings.HasPrefix(body, "0o"), strings.HasPrefix(body, "0O"):
		base = 8
		body = body[2:]
	case strings.HasPrefix(body, "0b"), strings.HasPrefix(body, "0B"):
		base = 2
		body = body[2:]
	}
	n, err := strconv.ParseInt(body, base, 64)
	if err != nil {
		return nil, false
	}
	return n, true
}

// decodeStringToken strips quote/prefix wrapping and decodes escapes.
// v0.6 ships a minimal decoder; the parser/string package is the
// real path and will replace this once the action surface is wired
// to it.
func decodeStringToken(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	for len(s) > 0 {
		c := s[0]
		if c == '\'' || c == '"' {
			break
		}
		if c == 'b' || c == 'B' || c == 'r' || c == 'R' || c == 'u' || c == 'U' || c == 'f' || c == 'F' || c == 't' || c == 'T' {
			s = s[1:]
			continue
		}
		return "", false
	}
	if len(s) < 2 {
		return "", false
	}
	q := s[0]
	if q != s[len(s)-1] {
		return "", false
	}
	body := s[1 : len(s)-1]
	if len(body) >= 4 && body[:2] == strings.Repeat(string(q), 2) && body[len(body)-2:] == strings.Repeat(string(q), 2) {
		body = body[2 : len(body)-2]
	}
	return body, true
}
