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
	"errors"
	"math/big"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/ast"
	perrors "github.com/tamnd/gopy/parser/errors"
	stringparse "github.com/tamnd/gopy/parser/string"
	"github.com/tamnd/gopy/token"
)

// pyConstantSentinel tags Py_True / Py_False / Py_None / Py_Ellipsis
// references in grammar actions. The translator emits one of these
// sentinels for the bare `Py_True` etc. identifiers; actionAstConstant
// turns the sentinel into the corresponding ast.Constant value.
type pyConstantSentinel int

const (
	pyTrueSentinel pyConstantSentinel = iota + 1
	pyFalseSentinel
	pyNoneSentinel
	pyEllipsisSentinel
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
//
// Bare NAME / NUMBER / STRING / FSTRING tokens get the implicit
// conversion CPython's c_generator inserts when a rule's [type] is
// expr_ty: NAME becomes ast.Name with Load ctx, NUMBER becomes a
// numeric ast.Constant, single-string tokens become a string
// ast.Constant. Rules that need a different ctx (Store / Del) call
// SetExprContext on the result via _PyPegen_set_expr_context.
func asExpr(v any) ast.Expr {
	for {
		switch x := v.(type) {
		case nil:
			return nil
		case ast.Expr:
			return x
		case *Token:
			if x == nil {
				return nil
			}
			return tokenToExpr(x)
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

// tokenToExpr maps a NAME / NUMBER / STRING token onto the ast.Expr
// node CPython implicitly creates when the surrounding rule expects
// an expr_ty. Anything else returns nil so callers fall through to
// placeholderMatched.
func tokenToExpr(t *Token) ast.Expr {
	pos := tokenPos(t)
	switch t.Type {
	case token.NAME:
		return &ast.Name{Id: string(t.Bytes), Ctx: ast.Load, Pos: pos}
	case token.NUMBER:
		if v, ok := parseNumberLiteral(string(t.Bytes)); ok {
			return &ast.Constant{Value: v, Pos: pos}
		}
	case token.STRING:
		if s, ok := decodeStringToken(string(t.Bytes)); ok {
			return &ast.Constant{Value: s, Pos: pos}
		}
	}
	return nil
}

// tokenPos lifts a Token's location into the AST Pos shape.
func tokenPos(t *Token) ast.Pos {
	if t == nil {
		return ast.NoPos
	}
	return ast.Pos{
		Lineno:       t.Lineno,
		ColOffset:    t.ColOff,
		EndLineno:    t.EndLine,
		EndColOffset: t.EndCol,
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

// actionAstExpression builds the Expression mod root used by ModeEval.
// Args: (body) where body is the single expression evaluated.
//
// CPython: Python/Python-ast.c:_PyAST_Expression
func actionAstExpression(p *Parser, args ...any) any {
	_ = p
	body := asExpr(argAt(args, 0))
	if body == nil {
		return placeholderMatched
	}
	return &ast.Expression{Body: body}
}

// actionAstInteractive builds the Interactive mod root used by
// ModeSingle. Args: (body) where body is the statement list.
//
// CPython: Python/Python-ast.c:_PyAST_Interactive
func actionAstInteractive(p *Parser, args ...any) any {
	_ = p
	body := stmtSeqOf(argAt(args, 0))
	return &ast.Interactive{Body: body}
}

// actionAstFunctionType builds the FunctionType mod root for
// `(argtypes) -> returns` source under ModeFunctionType. Args:
// (argtypes, returns).
//
// CPython: Python/Python-ast.c:_PyAST_FunctionType
func actionAstFunctionType(p *Parser, args ...any) any {
	_ = p
	argtypes := exprSeqOf(argAt(args, 0))
	returns := asExpr(argAt(args, 1))
	if returns == nil {
		return placeholderMatched
	}
	return &ast.FunctionType{Argtypes: argtypes, Returns: returns}
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
		targets[i] = SetExprContext(p, targets[i], ast.Del)
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

// actionAstIfExp builds IfExp. Args: (test, body, orelse). CPython
// emits the call as _PyAST_IfExp(b, a, c) where the grammar binds
// a=body 'if' b=test 'else' c=orelse.
func actionAstIfExp(p *Parser, args ...any) any {
	_ = p
	test := asExpr(argAt(args, 0))
	body := asExpr(argAt(args, 1))
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

// actionAstDictComp builds a DictComp. Args: (key, value, generators).
//
// CPython: Parser/Python.asdl DictComp(expr key, expr value, comprehension* generators)
func actionAstDictComp(p *Parser, args ...any) any {
	_ = p
	key := asExpr(argAt(args, 0))
	value := asExpr(argAt(args, 1))
	gens := comprehensionSeqOf(argAt(args, 2))
	if key == nil || value == nil || len(gens) == 0 {
		return placeholderMatched
	}
	return &ast.DictComp{Key: key, Value: value, Generators: gens, Pos: ast.NoPos}
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

// actionAstMatchOr materializes a MatchOr from the gather-rule output.
// The grammar wraps this in a `len == 1 ? get(0) : MatchOr(...)` ternary
// so this helper always receives a sequence with at least two members;
// the collapse case is handled by seqLenAny / seqGetAny in the
// translated ternary.
//
// CPython: Parser/Python.asdl MatchOr(pattern* patterns)
func actionAstMatchOr(p *Parser, args ...any) any {
	_ = p
	seq := patternSeqOf(argAt(args, 0))
	if len(seq) == 0 {
		return placeholderMatched
	}
	return &ast.MatchOr{Patterns: seq, Pos: ast.NoPos}
}

// SeqCountDots / SingletonSeq / SeqInsertInFront / JoinNamesWithDot
// are direct ports of the helpers in actions.go, with the (p, p, ...)
// variadic call shape the generator emits.
//
// CPython: Parser/action_helpers.c

func actionPgenSeqCountDots(p *Parser, args ...any) any {
	_ = p
	// _PyPegen_seq_count_dots(seq) takes no explicit Parser*, so the
	// translator emits a single positional arg: args[0] is the seq.
	return SeqCountDots(flattenTokens(argAt(args, 0)))
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

// actionPgenJoinSequences concatenates the two halves of the kwargs
// alt 0 (kwarg_or_starred+ ',' kwarg_or_double_starred+) into one
// flat *KeywordOrStarred sequence.
//
// CPython: Parser/action_helpers.c:472 _PyPegen_join_sequences
func actionPgenJoinSequences(p *Parser, args ...any) any {
	a := kwargOrStarredSeqOf(argAt(args, 1))
	b := kwargOrStarredSeqOf(argAt(args, 2))
	return joinSequences(p, a, b)
}

func actionPgenGetExprName(p *Parser, args ...any) any {
	_ = p
	if e := asExpr(argAt(args, 0)); e != nil {
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

// actionPgenKeywordOrStarred wraps the (element, is_keyword) pair the
// kwarg_or_starred and kwarg_or_double_starred rules emit.
//
// CPython: Parser/action_helpers.c:769 _PyPegen_keyword_or_starred
func actionPgenKeywordOrStarred(p *Parser, args ...any) any {
	element := argAt(args, 1)
	flag := intOf(argAt(args, 2))
	isKeyword := flag != nil && *flag != 0
	return keywordOrStarred(p, element, isKeyword)
}

// actionAstArg builds an *ast.Arg from (name, annotation, type_comment).
// Mirrors the C grammar's _PyAST_arg constructor invocation.
//
// CPython: Python/Python-ast.c:8534 _PyAST_arg
func actionAstArg(p *Parser, args ...any) any {
	_ = p
	name, _ := argAt(args, 0).(string)
	if name == "" {
		return placeholderMatched
	}
	var annotation ast.Expr
	if v := argAt(args, 1); v != nil {
		annotation = asExpr(v)
	}
	tc := decodeTypeComment(argAt(args, 2))
	return &ast.Arg{Arg: name, Annotation: annotation, TypeComment: tc, Pos: ast.NoPos}
}

// actionPgenNameDefaultPair wires the param-with-default rule into the
// name_default_pair port.
//
// CPython: Parser/action_helpers.c:430 _PyPegen_name_default_pair
func actionPgenNameDefaultPair(p *Parser, args ...any) any {
	a, _ := argAt(args, 1).(*ast.Arg)
	value := asExpr(argAt(args, 2))
	tc, _ := argAt(args, 3).(*Token)
	if a == nil {
		return placeholderMatched
	}
	out := nameDefaultPair(p, a, value, tc)
	if out == nil {
		return placeholderMatched
	}
	return out
}

// actionPgenConstantFromToken builds a Constant from the raw bytes of
// an FSTRING_MIDDLE / TSTRING_MIDDLE token. The bytes are the literal
// text between interpolations (with `{{` / `}}` already collapsed by
// the tokenizer); CPython feeds them through PyUnicode_FromString
// without escape decoding.
//
// CPython: Parser/action_helpers.c:1431 _PyPegen_constant_from_token
func actionPgenConstantFromToken(p *Parser, args ...any) any {
	_ = p
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	return &ast.Constant{Value: string(t.Bytes), Pos: ast.NoPos}
}

// actionPgenConstantFromString builds a string-literal Constant. The
// `b` prefix routes through []byte so the Constant.Value matches
// CPython's `bytes` type instead of `str`.
//
// CPython: Parser/action_helpers.c:601 _PyPegen_constant_from_string
func actionPgenConstantFromString(p *Parser, args ...any) any {
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	body, isBytes, ok, derr := decodeStringTokenTaggedErr(string(t.Bytes))
	if !ok {
		if derr != nil && p != nil {
			p.RaiseSyntaxErrorKnownLocation(perrors.Pos{
				Lineno:  t.Lineno,
				ColOff:  t.ColOff,
				EndLine: t.EndLine,
				EndCol:  t.EndCol,
			}, "%s", derr.Error())
		}
		return placeholderMatched
	}
	if isBytes {
		return &ast.Constant{Value: []byte(body), Pos: ast.NoPos}
	}
	return &ast.Constant{Value: body, Pos: ast.NoPos}
}

// actionPgenDecodedConstantFromToken builds a Constant from
// FSTRING_MIDDLE bytes that came from a format-spec body. CPython
// peeks the surrounding tokenizer mode to find out whether the outer
// f-string is raw and routes the bytes through _PyPegen_decode_string.
//
// CPython: Parser/action_helpers.c:1404 _PyPegen_decoded_constant_from_token
func actionPgenDecodedConstantFromToken(p *Parser, args ...any) any {
	t, ok := argAt(args, 1).(*Token)
	if !ok || t == nil {
		return placeholderMatched
	}
	isRaw := false
	if p != nil && p.tok != nil && p.tok.InsideFString() {
		isRaw = p.tok.CurrentFStringRaw()
	}
	text, _, err := stringparse.DecodeFStringPart(isRaw, string(t.Bytes))
	if err != nil {
		return placeholderMatched
	}
	return &ast.Constant{Value: text, Pos: ast.NoPos}
}

// actionPgenEnsureImaginary validates that exp is a Constant carrying
// a complex value. The complex_number grammar rule requires the right
// operand to be an imaginary literal; anything else (a real, an
// integer) raises a SyntaxError at the operand's location so the
// parser fails fast rather than letting codegen do shape checks.
//
// CPython: Parser/action_helpers.c:843 _PyPegen_ensure_imaginary
func actionPgenEnsureImaginary(p *Parser, args ...any) any {
	exp := asExpr(argAt(args, 1))
	c, ok := exp.(*ast.Constant)
	if !ok {
		raiseEnsureNumberError(p, exp, "imaginary number required in complex literal")
		return placeholderMatched
	}
	if _, isComplex := c.Value.(complex128); !isComplex {
		raiseEnsureNumberError(p, c, "imaginary number required in complex literal")
		return placeholderMatched
	}
	return c
}

// actionPgenEnsureReal validates that exp is a Constant carrying a
// non-complex numeric value. The complex_number grammar rule requires
// the left operand to be a real literal (signed_real_number); a
// complex literal here raises the symmetric SyntaxError.
//
// CPython: Parser/action_helpers.c:853 _PyPegen_ensure_real
func actionPgenEnsureReal(p *Parser, args ...any) any {
	exp := asExpr(argAt(args, 1))
	c, ok := exp.(*ast.Constant)
	if !ok {
		raiseEnsureNumberError(p, exp, "real number required in complex literal")
		return placeholderMatched
	}
	if _, isComplex := c.Value.(complex128); isComplex {
		raiseEnsureNumberError(p, c, "real number required in complex literal")
		return placeholderMatched
	}
	return c
}

// raiseEnsureNumberError reports a SyntaxError at the expression's
// span. ensureReal / ensureImaginary share this path so the location
// arithmetic stays in one place.
func raiseEnsureNumberError(p *Parser, exp ast.Expr, msg string) {
	if p == nil || exp == nil {
		return
	}
	pos := exp.Position()
	p.RaiseSyntaxErrorKnownLocation(perrors.Pos{
		Lineno:  pos.Lineno,
		ColOff:  pos.ColOffset,
		EndLine: pos.EndLineno,
		EndCol:  pos.EndColOffset,
	}, "%s", msg)
}

// The remaining pgen helpers cover string-formatting, function/class
// build, and call-arg shaping. Each requires substantial scaffolding
// (FormattedValue conversions, Arguments grouping, decorators); they
// stay as placeholderMatched until the typed expression chain lands
// so the gate test continues to skip those shapes cleanly rather
// than mis-typing.

// actionPgenFormattedValue ports `_PyPegen_formatted_value`. Args:
// (p, expression, debug_token, conversion_result, format_result, rbrace).
// For the no-debug path we just build a FormattedValue. The debug-text
// shim that wraps the value into a JoinedStr is intentionally
// minimal until the parser exposes the metadata we'd need.
//
// CPython: Parser/action_helpers.c:1564 _PyPegen_formatted_value
func actionPgenFormattedValue(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 1))
	if value == nil {
		return placeholderMatched
	}
	debug, _ := argAt(args, 2).(*Token)
	conv := fstringConversionChar(argAt(args, 3))
	format := asExpr(argAt(args, 4))
	if conv == 0 && debug != nil && format == nil {
		conv = 'r'
	} else if conv == 0 {
		conv = -1
	}
	return &ast.FormattedValue{
		Value:      value,
		Conversion: conv,
		FormatSpec: format,
		Pos:        ast.NoPos,
	}
}

// fstringConversionChar pulls the character code out of whatever
// actionPgenCheckFstringConversion returned. The conversion token in
// f'{y!r}' is a single-letter NAME ('s', 'r', or 'a'); store the
// rune as an int. nil means "no conversion specified".
func fstringConversionChar(v any) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case rune:
		return int(t)
	case *Token:
		if t == nil || len(t.Bytes) == 0 {
			return 0
		}
		return int(t.Bytes[0])
	case *ast.Name:
		if t == nil || t.Id == "" {
			return 0
		}
		return int(t.Id[0])
	}
	return 0
}

// actionPgenInterpolation builds an Interpolation node for a t-string
// {expr!conv:fmt} chunk. Call shape:
//
//	(p, p, expression, debug_expr_token, conversion, format, rbrace)
//
// CPython captures the source text of `expr` via set_ftstring_expr, so
// it is reachable as the closing brace token's metadata.
//
// CPython: Parser/action_helpers.c:1508 _PyPegen_interpolation (PEP 750)
func actionPgenInterpolation(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 1))
	if value == nil {
		return placeholderMatched
	}
	debug, _ := argAt(args, 2).(*Token)
	conv := fstringConversionChar(argAt(args, 3))
	format := asExpr(argAt(args, 4))
	rbrace, _ := argAt(args, 5).(*Token)
	if conv == 0 && debug != nil && format == nil {
		conv = 'r'
	} else if conv == 0 {
		conv = -1
	}
	var exprstr string
	if rbrace != nil && len(rbrace.Metadata) > 0 {
		exprstr = string(rbrace.Metadata)
	}
	interp := &ast.Interpolation{
		Value:      value,
		Str:        stripInterpolationExpr(exprstr),
		Conversion: conv,
		FormatSpec: format,
		Pos:        ast.NoPos,
	}
	if debug == nil {
		return interp
	}
	return &ast.JoinedStr{
		Values: []ast.Expr{
			&ast.Constant{Value: exprstr, Pos: ast.NoPos},
			interp,
		},
		Pos: ast.NoPos,
	}
}

// stripInterpolationExpr drops trailing whitespace and '=' from the
// expression source. Used so the Interpolation.str field carries the
// pre-'=' expression while the debug Constant keeps the full text.
//
// CPython: Parser/action_helpers.c:1491 _strip_interpolation_expr
func stripInterpolationExpr(s string) string {
	end := len(s)
	for end > 0 {
		c := s[end-1]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '=' {
			end--
			continue
		}
		break
	}
	return s[:end]
}

// actionPgenConcatenateStrings dispatches the `strings` rule's
// (fstring|string)+ alt into the concatenation port. The action
// translator emits this call as `(p, p, a)`; argAt(args, 1) is the
// asdl_expr_seq* the loop returned.
//
// CPython: Parser/action_helpers.c:1860 _PyPegen_concatenate_strings
func actionPgenConcatenateStrings(p *Parser, args ...any) any {
	parts := exprSeqOf(argAt(args, 1))
	if len(parts) == 0 {
		return placeholderMatched
	}
	out := ConcatenateStrings(p, []ast.Expr(parts))
	if out == nil {
		return placeholderMatched
	}
	return out
}

// actionPgenTemplateStr builds a TemplateStr from the tstring tokens.
// Call shape: (p, p, TSTRING_START, loop0_66_result, TSTRING_END).
// Arg[2] is the _loop0_66 result: []any of Constant / Interpolation /
// JoinedStr(debug) exprs.
//
// CPython splits JoinedStr nodes (which only arise from debug
// interpolations) into their [Constant, Interpolation] pair so the
// constant text sits in the TemplateStr.values stream rather than
// inside a nested JoinedStr. Empty Constants from TSTRING_MIDDLE
// tokens between adjacent {expr} / closing quote get dropped here so
// they do not flow into BUILD_TEMPLATE.
//
// CPython: Parser/action_helpers.c:1300 _get_resized_exprs
// CPython: Parser/action_helpers.c:1387 _PyPegen_template_str
func actionPgenTemplateStr(p *Parser, args ...any) any {
	_ = p
	parts := exprSeqOf(argAt(args, 2))
	out := make([]ast.Expr, 0, len(parts))
	for _, item := range parts {
		if js, ok := item.(*ast.JoinedStr); ok && len(js.Values) == 2 {
			out = append(out, js.Values[0], js.Values[1])
			continue
		}
		if c, ok := item.(*ast.Constant); ok {
			if s, isStr := c.Value.(string); isStr && s == "" {
				continue
			}
		}
		out = append(out, item)
	}
	return TemplateStrFromValues(out)
}

// actionPgenConcatenateTstrings joins one or more TemplateStr nodes
// produced by a tstring+ loop into a single TemplateStr, folding runs
// of adjacent Constants and dropping empty ones. Arg[1] is the loop
// result ([]any of *ast.TemplateStr).
//
// CPython: Parser/action_helpers.c:1849 _PyPegen_concatenate_tstrings
func actionPgenConcatenateTstrings(p *Parser, args ...any) any {
	_ = p
	items := exprSeqOf(argAt(args, 1))
	parts := make([]ast.Expr, 0, len(items))
	for _, item := range items {
		parts = append(parts, item)
	}
	return TemplateStrFromValues(buildConcatenatedStr(parts))
}

// actionPgenCheckFstringConversion validates the conversion specifier
// after `!` in an f-string or t-string interpolation. The conversion
// must be a single-character Name whose id is one of 's', 'r', or 'a',
// and it must sit immediately after the `!` (no whitespace allowed).
// Violations turn into SyntaxError at the conv's span, so the lexer's
// generic Unrecognized conversion character SystemError never reaches
// user code. Returns the conv Name on success so downstream
// FormattedValue construction can read its single-character id via
// fstringConversionChar.
//
// CPython: Parser/action_helpers.c:966 _PyPegen_check_fstring_conversion
func actionPgenCheckFstringConversion(p *Parser, args ...any) any {
	convToken, _ := argAt(args, 1).(*Token)
	conv := asExpr(argAt(args, 2))
	if conv == nil {
		return placeholderMatched
	}
	name, ok := conv.(*ast.Name)
	if !ok {
		return placeholderMatched
	}
	prefix := byte('f')
	if p != nil && p.tok != nil {
		prefix = p.tok.CurrentFStringPrefixChar()
	}
	convPos := name.Position()
	// CPython rejects whitespace between `!` and the conversion char by
	// comparing the `!` token's end_col_offset to the conv name's
	// col_offset on the same line. The range carrying both spans pins
	// the caret at the gap.
	//
	// CPython: Parser/action_helpers.c:968 RAISE_SYNTAX_ERROR_KNOWN_RANGE
	if convToken != nil && (convToken.EndLine != convPos.Lineno || convToken.EndCol != convPos.ColOffset) {
		p.RaiseSyntaxErrorKnownLocation(perrors.Pos{
			Lineno:  convToken.Lineno,
			ColOff:  convToken.ColOff,
			EndLine: convPos.EndLineno,
			EndCol:  convPos.EndColOffset,
		}, "%c-string: conversion type must come right after the exclamation mark", prefix)
		return placeholderMatched
	}
	id := name.Id
	if len(id) != 1 || (id[0] != 's' && id[0] != 'r' && id[0] != 'a') {
		// CPython renders the bad id with %R, which for a str object
		// emits Python's repr (single-quoted ASCII for short ids).
		// Build the same shape by hand: %q would pick double quotes
		// and diverge from CPython's caret message.
		//
		// CPython: Parser/action_helpers.c:979 RAISE_SYNTAX_ERROR_KNOWN_LOCATION
		p.RaiseSyntaxErrorKnownLocation(perrors.Pos{
			Lineno:  convPos.Lineno,
			ColOff:  convPos.ColOffset,
			EndLine: convPos.EndLineno,
			EndCol:  convPos.EndColOffset,
		}, "%c-string: invalid conversion character '%s': expected 's', 'r', or 'a'", prefix, id)
		return placeholderMatched
	}
	return name
}

// actionPgenAddTypeCommentToArg wires the param-with-type-comment alt
// into the add_type_comment_to_arg port.
//
// CPython: Parser/action_helpers.c:903 _PyPegen_add_type_comment_to_arg
func actionPgenAddTypeCommentToArg(p *Parser, args ...any) any {
	a, _ := argAt(args, 1).(*ast.Arg)
	tc, _ := argAt(args, 2).(*Token)
	if a == nil {
		return placeholderMatched
	}
	out := addTypeCommentToArg(p, a, tc)
	if out == nil {
		return placeholderMatched
	}
	return out
}

// actionPgenArgumentsParsingError raises the second-pass diagnostic
// for a call whose argument list mixes positionals after kwargs (or
// after a **kwargs unpacking). args = (p, callExpr); we inspect the
// keywords list to pick the longer "unpacking" phrasing when any
// keyword has a nil Arg (i.e., **expr).
//
// CPython: Parser/action_helpers.c:1224 _PyPegen_arguments_parsing_error
func actionPgenArgumentsParsingError(p *Parser, args ...any) any {
	msg := "positional argument follows keyword argument"
	if call, ok := asExpr(argAt(args, 1)).(*ast.Call); ok {
		for _, kw := range call.Keywords {
			if kw.Arg == nil {
				msg = "positional argument follows keyword argument unpacking"
				break
			}
		}
	}
	return raiseAction(p, "RAISE_SYNTAX_ERROR", msg)
}

// actionPgenClassDefDecorators stamps decorators onto a ClassDef built
// by class_def_raw.
//
// CPython: Parser/action_helpers.c:756 _PyPegen_class_def_decorators
func actionPgenClassDefDecorators(p *Parser, args ...any) any {
	dec := exprSeqOf(argAt(args, 1))
	cd, ok := argAt(args, 2).(ast.Stmt)
	if !ok {
		return placeholderMatched
	}
	return ClassDefDecorators(p, []ast.Expr(dec), cd)
}

// actionPgenFunctionDefDecorators stamps decorators onto a FunctionDef
// or AsyncFunctionDef built by function_def_raw.
//
// CPython: Parser/action_helpers.c:727 _PyPegen_function_def_decorators
func actionPgenFunctionDefDecorators(p *Parser, args ...any) any {
	dec := exprSeqOf(argAt(args, 1))
	fn, ok := argAt(args, 2).(ast.Stmt)
	if !ok {
		return placeholderMatched
	}
	return FunctionDefDecorators(p, []ast.Expr(dec), fn)
}

// actionPgenCollectCallSeqs forwards the args rule's positional list
// `a` and optional kwargs tail `b` into the CollectCallSeqs port.
//
// CPython: Parser/action_helpers.c:1129 _PyPegen_collect_call_seqs
func actionPgenCollectCallSeqs(p *Parser, args ...any) any {
	pos := exprSeqOf(argAt(args, 1))
	kw := kwargOrStarredSeqOf(argAt(args, 2))
	out := CollectCallSeqs(p, []ast.Expr(pos), kw)
	if out == nil {
		return placeholderMatched
	}
	return out
}

// actionPgenSeqExtractStarredExprs lifts starred positional values out
// of a kwargs-only args alt. The args rule's alt 1 wraps the result as
// the args of a dummy Call carrier so the surrounding primary rule can
// pull positional and keyword sequences off it.
//
// CPython: Parser/action_helpers.c:797 _PyPegen_seq_extract_starred_exprs
func actionPgenSeqExtractStarredExprs(p *Parser, args ...any) any {
	seq := kwargOrStarredSeqOf(argAt(args, 1))
	return seqExtractStarredExprs(p, seq)
}

// actionPgenSeqDeleteStarredExprs returns the keyword-only entries of
// a kwargs-only args alt, used as the keywords of the dummy Call
// carrier produced by the args rule's alt 1.
//
// CPython: Parser/action_helpers.c:820 _PyPegen_seq_delete_starred_exprs
func actionPgenSeqDeleteStarredExprs(p *Parser, args ...any) any {
	seq := kwargOrStarredSeqOf(argAt(args, 1))
	return seqDeleteStarredExprs(p, seq)
}

// actionAstKeyword builds an *ast.Keyword from (arg, value). arg is
// the keyword identifier string (or nil for `**value`); value is the
// expression on the right-hand side.
//
// CPython: Python/Python-ast.c _PyAST_keyword
func actionAstKeyword(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 1))
	if value == nil {
		return placeholderMatched
	}
	var arg *string
	switch v := argAt(args, 0).(type) {
	case nil:
	case string:
		if v != "" {
			s := v
			arg = &s
		}
	case *string:
		arg = v
	}
	return &ast.Keyword{Arg: arg, Value: value, Pos: ast.NoPos}
}

// actionPgenDummyName surfaces the placeholder Name CPython uses as
// the function side of the Call carrier emitted by the args rule's
// alt 1 and by collect_call_seqs. The surrounding primary rule swaps
// this name out for the real callee.
//
// CPython: Parser/action_helpers.c:11 _PyPegen_dummy_name
func actionPgenDummyName(p *Parser, args ...any) any {
	_ = args
	return dummyName(p)
}

// argSeqOf walks v and collects *ast.Arg entries. The parameters
// rules return *ast.Arg values from `param`-shaped alts; gather/loop
// alts wrap them in []any. Returns nil when no arg is present so the
// _make_* helpers see the same NULL signal CPython relies on.
func argSeqOf(v any) []*ast.Arg {
	var out []*ast.Arg
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *ast.Arg:
			out = append(out, t)
		case []*ast.Arg:
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
	return out
}

// nameDefaultPairSeqOf walks v and collects *NameDefaultPair entries.
func nameDefaultPairSeqOf(v any) []*NameDefaultPair {
	var out []*NameDefaultPair
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *NameDefaultPair:
			out = append(out, t)
		case []*NameDefaultPair:
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
	return out
}

// kwargOrStarredSeqOf walks v and collects *KeywordOrStarred entries.
// The `kwargs` rule produces a flat sequence of these via gather +
// loop; the action_translator wraps the gather output as nested
// []any, so a recursive flatten is required.
func kwargOrStarredSeqOf(v any) []*KeywordOrStarred {
	var out []*KeywordOrStarred
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *KeywordOrStarred:
			out = append(out, t)
		case []*KeywordOrStarred:
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

// actionPgenMakeArguments folds the parameter-list bundle into a
// single Arguments node.
//
// CPython: Parser/action_helpers.c:643 _PyPegen_make_arguments
func actionPgenMakeArguments(p *Parser, args ...any) any {
	slashWithoutDefaultArg := argSeqOf(argAt(args, 1))
	swd, _ := argAt(args, 2).(*SlashWithDefault)
	plainNames := argSeqOf(argAt(args, 3))
	namesWithDefault := nameDefaultPairSeqOf(argAt(args, 4))
	se, _ := argAt(args, 5).(*StarEtc)
	out := MakeArguments(p, slashWithoutDefaultArg, swd, plainNames, namesWithDefault, se)
	if out == nil {
		return placeholderMatched
	}
	return out
}

func actionPgenNonparenGenexpInCall(p *Parser, args ...any) any {
	_ = p
	_ = args
	return placeholderMatched
}

// actionPgenStarEtc bundles the (*vararg, kwonlyargs, **kwarg) tail of
// a parameter list.
//
// CPython: Parser/action_helpers.c:460 _PyPegen_star_etc
func actionPgenStarEtc(p *Parser, args ...any) any {
	vararg, _ := argAt(args, 1).(*ast.Arg)
	kwonly := nameDefaultPairSeqOf(argAt(args, 2))
	kwarg, _ := argAt(args, 3).(*ast.Arg)
	out := starEtc(p, vararg, kwonly, kwarg)
	if out == nil {
		return placeholderMatched
	}
	return out
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
		case ast.Seq[ast.Pattern]:
			out = append(out, t...)
		case []ast.Pattern:
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
		case ast.Seq[string]:
			out = append(out, t...)
		case []string:
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
	return ast.Seq[string](out)
}

// nameOf returns the identifier text for a NAME token or *ast.Name.
func nameOf(v any) *string {
	switch x := v.(type) {
	case nil:
		return nil
	case *Token:
		if x == nil || x.Type != token.NAME {
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
		if err != nil && !isFloatRange(err) {
			return nil, false
		}
		return complex(0, f), true
	}
	if strings.ContainsAny(clean, ".eE") && !strings.HasPrefix(clean, "0x") && !strings.HasPrefix(clean, "0X") {
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil && !isFloatRange(err) {
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
	if n, err := strconv.ParseInt(body, base, 64); err == nil {
		return n, true
	}
	// Out of int64 range: lift to *big.Int. CPython routes this through
	// PyLong_FromString (Parser/string_parser.c parsenumber), which is
	// arbitrary-precision; the validator accepts *big.Int.
	bi, ok := new(big.Int).SetString(body, base)
	if !ok {
		return nil, false
	}
	return bi, true
}

// isFloatRange reports whether err from strconv.ParseFloat indicates an
// out-of-range value (returns +Inf or -Inf in that case). CPython
// silently accepts overflows: `1e1000` evaluates to inf at parse time,
// matching the C strtod default rounding to infinity, so the lexer must
// accept the same surface.
//
// CPython: Parser/string_parser.c:1280 parsenumber_raw (PyOS_string_to_double accepts overflow)
func isFloatRange(err error) bool {
	var ne *strconv.NumError
	if !errors.As(err, &ne) {
		return false
	}
	return errors.Is(ne.Err, strconv.ErrRange)
}

// decodeStringToken strips quote/prefix wrapping and decodes escapes
// by delegating to parser/string. Escape decoding (e.g. "\n" -> 0x0A)
// goes through the full CPython-compatible path.
//
// CPython: Parser/string_parser.c:253 _PyPegen_parse_string
func decodeStringToken(s string) (string, bool) {
	body, _, ok := decodeStringTokenTagged(s)
	return body, ok
}

// decodeStringTokenTagged returns the decoded body together with a
// flag that says whether the source had a bytes prefix (b/B). bytes
// literals are still returned in their decoded form; the caller wraps
// them in []byte so Constant.Value matches CPython's `bytes` vs `str`
// distinction.
//
// CPython: Parser/string_parser.c:253 _PyPegen_parse_string
func decodeStringTokenTagged(s string) (string, bool, bool) {
	body, isBytes, ok, _ := decodeStringTokenTaggedErr(s)
	return body, isBytes, ok
}

// decodeStringTokenTaggedErr is the underlying form that surfaces the
// decode error (e.g. unknown \N{NAME}) so callers with a Parser handle
// can pin a SyntaxError at the token. decodeStringToken /
// decodeStringTokenTagged keep swallowing the error for callers that
// only have a Token, but in practice the explicit-constant path in
// actionPgenConstantFromString routes through this variant.
//
// CPython: Parser/string_parser.c:253 _PyPegen_parse_string
func decodeStringTokenTaggedErr(s string) (string, bool, bool, error) {
	if len(s) < 2 {
		return "", false, false, nil
	}
	res, err := stringparse.ParseString([]byte(s))
	if err != nil {
		return "", false, false, err
	}
	if res.IsBytes {
		return string(res.Bytes), true, true, nil
	}
	return res.Text, false, true, nil
}

// withitemSeqOf walks v and collects the *ast.Withitem values found
// inside any-wrappers. Used by actionAstWith.
func withitemSeqOf(v any) ast.Seq[*ast.Withitem] {
	var out []*ast.Withitem
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *ast.Withitem:
			out = append(out, t)
		case []*ast.Withitem:
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
	return ast.Seq[*ast.Withitem](out)
}

// actionAstAssign builds an Assign. Args: (targets, value, type_comment).
//
// CPython: Parser/Python.asdl Assign(expr* targets, expr value, ...)
func actionAstAssign(p *Parser, args ...any) any {
	_ = p
	targets := exprSeqOf(argAt(args, 0))
	value := asExpr(argAt(args, 1))
	if len(targets) == 0 || value == nil {
		return placeholderMatched
	}
	tc := decodeTypeComment(argAt(args, 2))
	return &ast.Assign{Targets: targets, Value: value, TypeComment: tc, Pos: ast.NoPos}
}

// actionAstAugAssign builds an AugAssign. Args: (target, op, value).
//
// CPython: Parser/Python.asdl AugAssign(expr target, operator op, expr value)
func actionAstAugAssign(p *Parser, args ...any) any {
	_ = p
	target := asExpr(argAt(args, 0))
	op, ok := argAt(args, 1).(ast.Operator)
	value := asExpr(argAt(args, 2))
	if target == nil || !ok || value == nil {
		return placeholderMatched
	}
	return &ast.AugAssign{Target: target, Op: op, Value: value, Pos: ast.NoPos}
}

// actionAstAnnAssign builds an AnnAssign. Args: (target, annotation, value, simple).
//
// CPython: Parser/Python.asdl AnnAssign(expr target, expr annotation, expr? value, int simple)
func actionAstAnnAssign(p *Parser, args ...any) any {
	_ = p
	target := asExpr(argAt(args, 0))
	annotation := asExpr(argAt(args, 1))
	value := asExpr(argAt(args, 2))
	simple := intOf(argAt(args, 3))
	if target == nil || annotation == nil {
		return placeholderMatched
	}
	isSimple := 0
	if simple != nil {
		isSimple = *simple
	}
	return &ast.AnnAssign{
		Target:     target,
		Annotation: annotation,
		Value:      value,
		Simple:     isSimple,
		Pos:        ast.NoPos,
	}
}

// actionAstBinOp builds a BinOp. Args: (left, op, right).
func actionAstBinOp(p *Parser, args ...any) any {
	_ = p
	left := asExpr(argAt(args, 0))
	op, ok := argAt(args, 1).(ast.Operator)
	right := asExpr(argAt(args, 2))
	if left == nil || !ok || right == nil {
		return placeholderMatched
	}
	return &ast.BinOp{Left: left, Op: op, Right: right, Pos: ast.NoPos}
}

// actionAstBoolOp builds a BoolOp. Args: (op, values).
func actionAstBoolOp(p *Parser, args ...any) any {
	_ = p
	op, ok := argAt(args, 0).(ast.Boolop)
	values := exprSeqOf(argAt(args, 1))
	if !ok || len(values) == 0 {
		return placeholderMatched
	}
	return &ast.BoolOp{Op: op, Values: values, Pos: ast.NoPos}
}

// actionAstUnaryOp builds a UnaryOp. Args: (op, operand).
func actionAstUnaryOp(p *Parser, args ...any) any {
	_ = p
	op, ok := argAt(args, 0).(ast.Unaryop)
	operand := asExpr(argAt(args, 1))
	if !ok || operand == nil {
		return placeholderMatched
	}
	return &ast.UnaryOp{Op: op, Operand: operand, Pos: ast.NoPos}
}

// actionAstCompare builds a Compare. Args: (left, ops, comparators).
// CPython's grammar splits the cmpop_pair list into parallel ops /
// exprs slices via _PyPegen_get_cmpops / _PyPegen_get_exprs before
// calling the constructor; we accept either the precomputed split
// or the original pair list (older shape) so the rule keeps working
// either way.
func actionAstCompare(p *Parser, args ...any) any {
	_ = p
	left := asExpr(argAt(args, 0))
	if left == nil {
		return placeholderMatched
	}
	ops, opsOK := argAt(args, 1).(ast.Seq[ast.Cmpop])
	rhs, rhsOK := argAt(args, 2).(ast.Seq[ast.Expr])
	if opsOK && rhsOK && len(ops) > 0 {
		return &ast.Compare{Left: left, Ops: ops, Comparators: rhs, Pos: ast.NoPos}
	}
	cmps := flattenCmpopExprPairs(argAt(args, 1))
	if len(cmps) == 0 {
		return placeholderMatched
	}
	ops = make(ast.Seq[ast.Cmpop], 0, len(cmps))
	rhs = make(ast.Seq[ast.Expr], 0, len(cmps))
	for _, pair := range cmps {
		ops = append(ops, pair.Op)
		rhs = append(rhs, pair.Expr)
	}
	return &ast.Compare{Left: left, Ops: ops, Comparators: rhs, Pos: ast.NoPos}
}

// actionAstNamedExpr builds a NamedExpr. Args: (target, value).
func actionAstNamedExpr(p *Parser, args ...any) any {
	_ = p
	target := asExpr(argAt(args, 0))
	value := asExpr(argAt(args, 1))
	if target == nil || value == nil {
		return placeholderMatched
	}
	return &ast.NamedExpr{Target: target, Value: value, Pos: ast.NoPos}
}

// actionAstAwait builds an Await. Args: (value).
func actionAstAwait(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 0))
	if value == nil {
		return placeholderMatched
	}
	return &ast.Await{Value: value, Pos: ast.NoPos}
}

// actionAstName builds a Name. Args: (id, ctx).
func actionAstName(p *Parser, args ...any) any {
	_ = p
	id := identString(argAt(args, 0))
	ctx, ok := argAt(args, 1).(ast.ExprContext)
	if id == "" || !ok {
		return placeholderMatched
	}
	return &ast.Name{Id: id, Ctx: ctx, Pos: ast.NoPos}
}

// actionAstAttribute builds an Attribute. Args: (value, attr, ctx).
func actionAstAttribute(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 0))
	attr := identString(argAt(args, 1))
	ctx, ok := argAt(args, 2).(ast.ExprContext)
	if value == nil || attr == "" || !ok {
		return placeholderMatched
	}
	return &ast.Attribute{Value: value, Attr: attr, Ctx: ctx, Pos: ast.NoPos}
}

// actionAstSubscript builds a Subscript. Args: (value, slice, ctx).
func actionAstSubscript(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 0))
	sl := asExpr(argAt(args, 1))
	ctx, ok := argAt(args, 2).(ast.ExprContext)
	if value == nil || sl == nil || !ok {
		return placeholderMatched
	}
	return &ast.Subscript{Value: value, Slice: sl, Ctx: ctx, Pos: ast.NoPos}
}

// actionAstStarred builds a Starred. Args: (value, ctx).
func actionAstStarred(p *Parser, args ...any) any {
	_ = p
	value := asExpr(argAt(args, 0))
	ctx, ok := argAt(args, 1).(ast.ExprContext)
	if value == nil || !ok {
		return placeholderMatched
	}
	return &ast.Starred{Value: value, Ctx: ctx, Pos: ast.NoPos}
}

// actionAstTuple builds a Tuple. Args: (elts, ctx).
func actionAstTuple(p *Parser, args ...any) any {
	_ = p
	elts := exprSeqOf(argAt(args, 0))
	ctx, ok := argAt(args, 1).(ast.ExprContext)
	if !ok {
		return placeholderMatched
	}
	return &ast.Tuple{Elts: elts, Ctx: ctx, Pos: ast.NoPos}
}

// actionAstList builds a List. Args: (elts, ctx).
func actionAstList(p *Parser, args ...any) any {
	_ = p
	elts := exprSeqOf(argAt(args, 0))
	ctx, ok := argAt(args, 1).(ast.ExprContext)
	if !ok {
		return placeholderMatched
	}
	return &ast.List{Elts: elts, Ctx: ctx, Pos: ast.NoPos}
}

// actionAstDict builds a Dict. Args: (keys, values).
func actionAstDict(p *Parser, args ...any) any {
	_ = p
	keys := exprSeqOf(argAt(args, 0))
	values := exprSeqOf(argAt(args, 1))
	return &ast.Dict{Keys: keys, Values: values, Pos: ast.NoPos}
}

// actionAstCall builds a Call. Args: (func, args, keywords).
func actionAstCall(p *Parser, args ...any) any {
	_ = p
	fn := asExpr(argAt(args, 0))
	if fn == nil {
		return placeholderMatched
	}
	callArgs := exprSeqOf(argAt(args, 1))
	kws := keywordSeqOf(argAt(args, 2))
	return &ast.Call{Func: fn, Args: callArgs, Keywords: kws, Pos: ast.NoPos}
}

// actionAstConstant builds a Constant. Args: (value, kind).
func actionAstConstant(p *Parser, args ...any) any {
	_ = p
	v := constantValue(argAt(args, 0))
	if v == nil && argAt(args, 0) != pyNoneSentinel {
		return placeholderMatched
	}
	var kind *string
	if k, ok := argAt(args, 1).(*string); ok {
		kind = k
	}
	return &ast.Constant{Value: v, Kind: kind, Pos: ast.NoPos}
}

// constantValue maps a translated argument value (raw token, sentinel,
// or already-typed Go value) onto the Python value the AST should
// carry. Returns nil if the value is unrecognized; the caller treats
// the explicit pyNoneSentinel case specially because nil is also the
// "no value" return.
func constantValue(v any) any {
	switch t := v.(type) {
	case pyConstantSentinel:
		switch t {
		case pyTrueSentinel:
			return true
		case pyFalseSentinel:
			return false
		case pyNoneSentinel:
			return nil
		case pyEllipsisSentinel:
			return ast.Ellipsis
		}
	case *Token:
		if t == nil {
			return nil
		}
		switch t.Type {
		case token.NUMBER:
			if v, ok := parseNumberLiteral(string(t.Bytes)); ok {
				return v
			}
		case token.STRING:
			if s, ok := decodeStringToken(string(t.Bytes)); ok {
				return s
			}
		}
	case bool, int64, float64, complex128, string:
		return t
	}
	return nil
}

// actionAstFor builds a For. Args: (target, iter, body, orelse, type_comment).
func actionAstFor(p *Parser, args ...any) any {
	_ = p
	target := asExpr(argAt(args, 0))
	iter := asExpr(argAt(args, 1))
	body := stmtSeqOf(argAt(args, 2))
	orelse := stmtSeqOf(argAt(args, 3))
	if target == nil || iter == nil || len(body) == 0 {
		return placeholderMatched
	}
	tc := decodeTypeComment(argAt(args, 4))
	return &ast.For{
		Target: target, Iter: iter, Body: body, Orelse: orelse,
		TypeComment: tc, Pos: ast.NoPos,
	}
}

// actionAstAsyncFor mirrors actionAstFor but returns AsyncFor.
func actionAstAsyncFor(p *Parser, args ...any) any {
	_ = p
	target := asExpr(argAt(args, 0))
	iter := asExpr(argAt(args, 1))
	body := stmtSeqOf(argAt(args, 2))
	orelse := stmtSeqOf(argAt(args, 3))
	if target == nil || iter == nil || len(body) == 0 {
		return placeholderMatched
	}
	tc := decodeTypeComment(argAt(args, 4))
	return &ast.AsyncFor{
		Target: target, Iter: iter, Body: body, Orelse: orelse,
		TypeComment: tc, Pos: ast.NoPos,
	}
}

// actionAstWith builds a With. Args: (items, body, type_comment).
func actionAstWith(p *Parser, args ...any) any {
	_ = p
	items := withitemSeqOf(argAt(args, 0))
	body := stmtSeqOf(argAt(args, 1))
	if len(items) == 0 || len(body) == 0 {
		return placeholderMatched
	}
	tc := decodeTypeComment(argAt(args, 2))
	return &ast.With{Items: items, Body: body, TypeComment: tc, Pos: ast.NoPos}
}

// actionAstAsyncWith mirrors actionAstWith but returns AsyncWith.
func actionAstAsyncWith(p *Parser, args ...any) any {
	_ = p
	items := withitemSeqOf(argAt(args, 0))
	body := stmtSeqOf(argAt(args, 1))
	if len(items) == 0 || len(body) == 0 {
		return placeholderMatched
	}
	tc := decodeTypeComment(argAt(args, 2))
	return &ast.AsyncWith{Items: items, Body: body, TypeComment: tc, Pos: ast.NoPos}
}

// actionAstMatchSingleton builds a MatchSingleton. Args: (value).
func actionAstMatchSingleton(p *Parser, args ...any) any {
	_ = p
	v := constantValue(argAt(args, 0))
	if v == nil && argAt(args, 0) != pyNoneSentinel {
		return placeholderMatched
	}
	return &ast.MatchSingleton{Value: v, Pos: ast.NoPos}
}

// keywordSeqOf walks v and collects *ast.Keyword values.
func keywordSeqOf(v any) ast.Seq[*ast.Keyword] {
	var out []*ast.Keyword
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *ast.Keyword:
			out = append(out, t)
		case ast.Seq[*ast.Keyword]:
			out = append(out, t...)
		case []*ast.Keyword:
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
	return ast.Seq[*ast.Keyword](out)
}

// cmpopExprPair pairs a comparison operator with its right-hand
// operand. The Compare AST node stores them as parallel slices, but
// the grammar produces them paired.
type cmpopExprPair struct {
	Op   ast.Cmpop
	Expr ast.Expr
}

// flattenCmpopExprPairs walks v and collects every cmpopExprPair.
func flattenCmpopExprPairs(v any) []cmpopExprPair {
	var out []cmpopExprPair
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case cmpopExprPair:
			out = append(out, t)
		case []cmpopExprPair:
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

// actionPgenAugoperator wraps an ast.Operator for the augassign rule.
// CPython returns an AugOperator helper struct so that b->kind reads
// out the operator. The translator now drops the field selector, so
// this helper just returns the operator unchanged.
//
// CPython: Parser/action_helpers.c _PyPegen_augoperator
func actionPgenAugoperator(p *Parser, args ...any) any {
	_ = p
	// Pgen helpers receive the parser explicitly as args[0]; the
	// operator the rule passes lives at args[1].
	if op, ok := argAt(args, 1).(ast.Operator); ok {
		return op
	}
	return placeholderMatched
}

// actionPgenCmpopExprPair pairs a comparison operator with its
// right-hand expression so actionAstCompare can split them apart.
//
// CPython: Parser/action_helpers.c _PyPegen_cmpop_expr_pair
func actionPgenCmpopExprPair(p *Parser, args ...any) any {
	_ = p
	// CPython spelling is `_PyPegen_cmpop_expr_pair(p, op, expr)`, so
	// the action translator emits the parser explicitly as args[0].
	op, ok := argAt(args, 1).(ast.Cmpop)
	expr := asExpr(argAt(args, 2))
	if !ok || expr == nil {
		return placeholderMatched
	}
	return cmpopExprPair{Op: op, Expr: expr}
}

// actionPgenSetExprContext is the dispatch shim the generator emits
// for `_PyPegen_set_expr_context(p, expr, ctx)`. The action translator
// passes (p_action, p_explicit, expr, ctx); we forward to the
// SetExprContext port.
//
// CPython: Parser/action_helpers.c:309 _PyPegen_set_expr_context
func actionPgenSetExprContext(p *Parser, args ...any) any {
	expr := asExpr(argAt(args, 1))
	if expr == nil {
		return placeholderMatched
	}
	ctx, ok := argAt(args, 2).(ast.ExprContext)
	if !ok {
		return expr
	}
	return SetExprContext(p, expr, ctx)
}

// actionPgenSeqFlatten flattens a nested any-shape into a single
// Seq[Stmt]. Used by the `statements` rule whose body is
// `_PyPegen_seq_flatten(p, a)` over the +-repetition of `statement`.
//
// CPython: Parser/action_helpers.c:33 _PyPegen_seq_flatten
func actionPgenSeqFlatten(p *Parser, args ...any) any {
	_ = p
	return stmtSeqOf(argAt(args, 1))
}

// actionPgenSeqAppendToEnd appends `item` to the end of `seq`. The
// grammar uses it to glue a singleton onto the tail of a +-repetition
// when collecting non-empty statement sequences.
//
// CPython: Parser/action_helpers.c _PyPegen_seq_append_to_end
func actionPgenSeqAppendToEnd(p *Parser, args ...any) any {
	_ = p
	seq := stmtSeqOf(argAt(args, 1))
	item := asStmt(argAt(args, 2))
	if item == nil {
		return seq
	}
	out := make([]ast.Stmt, 0, len(seq)+1)
	out = append(out, seq...)
	out = append(out, item)
	return ast.Seq[ast.Stmt](out)
}

// actionPgenRegisterStmts annotates the parser's current statement
// sequence for the interactive REPL. A no-op for the file path; the
// helper just returns its input unchanged.
//
// CPython: Parser/action_helpers.c _PyPegen_register_stmts
func actionPgenRegisterStmts(p *Parser, args ...any) any {
	_ = p
	return argAt(args, 1)
}

// actionPgenSlashWithDefault carries the (plain-names, names-with-defaults)
// pair the slash_with_default rule emits for position-only parameters.
//
// CPython: Parser/action_helpers.c:447 _PyPegen_slash_with_default
func actionPgenSlashWithDefault(p *Parser, args ...any) any {
	plainNames := argSeqOf(argAt(args, 1))
	nwd := nameDefaultPairSeqOf(argAt(args, 2))
	out := slashWithDefault(p, plainNames, nwd)
	if out == nil {
		return placeholderMatched
	}
	return out
}

// actionPgenSetupFullFormatSpec wraps a format-spec body (from
// `:spec*` in fstring_full_format_spec) into the expression that
// FormattedValue.format_spec carries. CPython filters out empty
// Constant nodes and either returns a JoinedStr or concatenates the
// surviving parts; the result is then wrapped in a ResultTokenWithMetadata.
// We return the bare expression since the FormatSpec slot reads an Expr.
//
// CPython: Parser/action_helpers.c:990 _PyPegen_setup_full_format_spec
func actionPgenSetupFullFormatSpec(p *Parser, args ...any) any {
	_ = p
	spec := joinedStrValues(argAt(args, 2))
	filtered := make([]ast.Expr, 0, len(spec))
	for _, item := range spec {
		if c, ok := item.(*ast.Constant); ok {
			if s, isStr := c.Value.(string); isStr && s == "" {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	n := len(filtered)
	if n == 0 {
		return &ast.JoinedStr{Values: filtered, Pos: ast.NoPos}
	}
	if n == 1 {
		if _, ok := filtered[0].(*ast.Constant); ok {
			return &ast.JoinedStr{Values: filtered, Pos: ast.NoPos}
		}
	}
	concat := ConcatenateStrings(p, filtered)
	if concat == nil {
		return &ast.JoinedStr{Values: filtered, Pos: ast.NoPos}
	}
	return concat
}

// actionPgenJoinedStr ports `_PyPegen_joined_str`. Args:
// (p, fstring_start_token, raw_expressions, fstring_end_token). The
// FSTRING_START token's bytes carry the prefix (e.g. `f'`, `rf"`); a
// case-insensitive `r` flips is_raw. _get_resized_exprs walks the
// raw_expressions seq, decodes the escape sequences in each Constant
// segment through _PyPegen_decode_fstring_part, drops empty results,
// and flattens the 2-element debug-mode JoinedStr that
// _PyPegen_formatted_value emits.
//
// CPython: Parser/action_helpers.c:1396 _PyPegen_joined_str
// CPython: Parser/action_helpers.c:1301 _get_resized_exprs
func actionPgenJoinedStr(p *Parser, args ...any) any {
	_ = p
	start, _ := argAt(args, 1).(*Token)
	values := joinedStrValues(argAt(args, 2))
	resized, err := resizeFStringExprs(start, values)
	if err != nil {
		return placeholderMatched
	}
	return &ast.JoinedStr{Values: resized, Pos: ast.NoPos}
}

// resizeFStringExprs is the post-pass _PyPegen_joined_str runs over
// the parsed raw_expressions. It decodes literal Constants (so `\n`
// in `f', line {n}\n'` becomes a newline), inlines the debug
// JoinedStr the FormattedValue arm wraps around `expr=` text, and
// drops empty Constants the lexer emits for adjacent escape runs.
//
// CPython: Parser/action_helpers.c:1301 _get_resized_exprs
func resizeFStringExprs(start *Token, raw []ast.Expr) ([]ast.Expr, error) {
	isRaw := fstringStartIsRaw(start)
	out := make([]ast.Expr, 0, len(raw))
	for _, item := range raw {
		if joined, ok := item.(*ast.JoinedStr); ok {
			out = append(out, joined.Values...)
			continue
		}
		c, ok := item.(*ast.Constant)
		if !ok {
			out = append(out, item)
			continue
		}
		s, isStr := c.Value.(string)
		if !isStr {
			out = append(out, item)
			continue
		}
		decoded, _, err := stringparse.DecodeFStringPart(isRaw, s)
		if err != nil {
			return nil, err
		}
		if decoded == "" {
			continue
		}
		out = append(out, &ast.Constant{Value: decoded, Pos: c.Pos})
	}
	return out, nil
}

// fstringStartIsRaw scans the FSTRING_START token bytes for an `r` or
// `R` prefix character. CPython does the same with strpbrk in
// _get_resized_exprs.
//
// CPython: Parser/action_helpers.c:1322 strpbrk(quote_str, "rR")
func fstringStartIsRaw(t *Token) bool {
	if t == nil {
		return false
	}
	for _, b := range t.Bytes {
		if b == 'r' || b == 'R' {
			return true
		}
		if b == '\'' || b == '"' {
			return false
		}
	}
	return false
}

// joinedStrValues coerces a raw_expressions seq into the Values slot
// on an ast.JoinedStr. The grammar produces a []any (loop result)
// containing FormattedValue / Constant / placeholder values; we
// strip the placeholders and keep the rest in order.
func joinedStrValues(v any) ast.Seq[ast.Expr] {
	var out []ast.Expr
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case ast.Expr:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		case []ast.Expr:
			out = append(out, t...)
		case ast.Seq[ast.Expr]:
			out = append(out, t...)
		}
	}
	walk(v)
	if len(out) == 0 {
		return nil
	}
	return ast.Seq[ast.Expr](out)
}

// decodeTypeComment turns an optional TYPE_COMMENT token into the
// stored string pointer. Nil token → nil pointer (no comment).
func decodeTypeComment(v any) *string {
	if v == nil {
		return nil
	}
	if t, ok := v.(*Token); ok && t != nil {
		s := strings.TrimSpace(string(t.Bytes))
		if s == "" {
			return nil
		}
		return &s
	}
	return nil
}

// matchedOr lifts a default PEG action ("return the lone binding")
// across a legitimately-nil sub-rule result. CPython distinguishes
// "alt matched all items" (success, action may produce NULL) from
// "alt failed" (reset and try the next alt) via the comma operator
// in C; gopy's generator collapses both into "result != nil". When
// an alt's lone binding is itself an optional sub-rule that returned
// nil, this helper substitutes placeholderMatched so the alt counts
// as success and the consumed tokens stay consumed.
//
// CPython: Parser/parser.c (e.g. _tmp_26_rule's `_res = z` followed
// by `goto done` — the alt succeeds even when z is NULL).
func matchedOr(v any) any {
	if v == nil {
		return placeholderMatched
	}
	return v
}

// posSetter is implemented by every AST node that carries a source
// location (ast/nodes_gen.go SetPos). The pegen layer assigns the
// span covered by an alt to the constructed node so PEP 657 location
// emission has a real position to encode.
type posSetter interface {
	Position() ast.Pos
	SetPos(ast.Pos)
}

// withSpan stamps the source span covering tokens [startMark, p.mark)
// onto v if v is an AST node whose Pos is still NoPos. The action
// helpers themselves still construct nodes with Pos: ast.NoPos; this
// wrapper is the gopy equivalent of CPython's EXTRA macro applied at
// every alt's action-call site.
//
// Leaf helpers (nameFromToken, numberToken) already write the token
// span into Pos. withSpan leaves those alone.
//
// CPython: Tools/peg_generator/pegen/c_generator.py EXTRA
func withSpan(p *Parser, startMark int, v any) any {
	if v == nil || v == placeholderMatched {
		return v
	}
	// kwarg_or_starred and kwarg_or_double_starred wrap the constructed
	// Keyword in a KeywordOrStarred carrier. CPython applies EXTRA to
	// the Keyword itself inside _PyAST_keyword; here the carrier sits
	// between withSpan and the node that holds the Pos, so peel it
	// open and stamp the inner Keyword directly.
	//
	// CPython: Grammar/python.gram:1081 kwarg_or_starred (EXTRA passed
	// to _PyAST_keyword, not the outer keyword_or_starred wrapper).
	if k, ok := v.(*KeywordOrStarred); ok && k != nil {
		if kw, ok := k.Element.(*ast.Keyword); ok && kw != nil && kw.Pos == ast.NoPos {
			kw.Pos = p.Span(startMark)
		}
		return v
	}
	n, ok := v.(posSetter)
	if !ok {
		return v
	}
	if n.Position() != ast.NoPos {
		return v
	}
	n.SetPos(p.Span(startMark))
	return v
}

// truthy is the C-style truthiness check the action-body translator
// emits for ternary conditions. Mirrors the implicit `!= 0` /
// `!= NULL` check that C uses on pointers, ints, and bool-like
// expressions inside `cond ? a : b`.
func truthy(v any) bool {
	if v == nil {
		return false
	}
	if v == placeholderMatched {
		// Alt matched but its action produced NULL (e.g. empty
		// `()` arguments). The C grammar treats NULL as "no
		// value", so report truthy=false here too.
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case *Token:
		return x != nil
	case []any:
		return len(x) > 0
	}
	return true
}

// nameIDOf extracts the identifier text from a NAME token or a
// Name expression. Mirrors the C grammar's `n->v.Name.id` accessor.
// Returns "" when neither shape applies; callers route on that.
func nameIDOf(v any) string {
	switch x := v.(type) {
	case *Token:
		if x == nil {
			return ""
		}
		return string(x.Bytes)
	case *ast.Name:
		if x == nil {
			return ""
		}
		return x.Id
	}
	return ""
}

// callArgsOf returns the positional-arg slice on a Call expression.
// Mirrors `n->v.Call.args`.
func callArgsOf(v any) ast.Seq[ast.Expr] {
	if c, ok := v.(*ast.Call); ok && c != nil {
		return c.Args
	}
	return nil
}

// callKwOf returns the keyword-arg slice on a Call expression.
// Mirrors `n->v.Call.keywords`.
func callKwOf(v any) ast.Seq[*ast.Keyword] {
	if c, ok := v.(*ast.Call); ok && c != nil {
		return c.Keywords
	}
	return nil
}

// kvKey / kvValue project the columns out of a [2]any pair the
// KeyValuePair / KeyPatternPair / NameDefaultPair helpers stamp out.
// Mirrors `pair->key` / `pair->value` (also `pair->name` /
// `pair->pattern`) in the C grammar.
func kvKey(v any) any {
	if p, ok := v.([2]any); ok {
		return p[0]
	}
	return nil
}

func kvValue(v any) any {
	if p, ok := v.([2]any); ok {
		return p[1]
	}
	return nil
}

// argumentsOf coerces an action arg into *ast.Arguments. The grammar
// passes either a real Arguments value (when params matched) or the
// fallback empty arguments produced by actionPgenEmptyArguments.
// `params` arrives wrapped in []any{*ast.Arguments} via the surrounding
// rule alt, so unwrap one level when we see that shape.
func argumentsOf(v any) *ast.Arguments {
	switch x := v.(type) {
	case nil:
		return &ast.Arguments{}
	case *ast.Arguments:
		if x == nil {
			return &ast.Arguments{}
		}
		return x
	case []any:
		for _, e := range x {
			if a, ok := e.(*ast.Arguments); ok && a != nil {
				return a
			}
		}
	}
	return &ast.Arguments{}
}

// exprOptional returns nil when v is nil-shaped, else asExpr(v).
// Used for optional return-annotation / decorator slots that the C
// grammar passes through unchanged from a possibly-empty alt.
func exprOptional(v any) ast.Expr {
	if isNilResult(v) {
		return nil
	}
	return asExpr(v)
}

// typeParamSeqOf coerces v into Seq[ast.TypeParam] for the optional
// type_params slot. Today the rule chain returns []any of TypeParam
// values when present; nil otherwise.
func typeParamSeqOf(v any) ast.Seq[ast.TypeParam] {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case ast.Seq[ast.TypeParam]:
		return x
	case []ast.TypeParam:
		return ast.Seq[ast.TypeParam](x)
	case []any:
		var out []ast.TypeParam
		for _, e := range x {
			if tp, ok := e.(ast.TypeParam); ok {
				out = append(out, tp)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return ast.Seq[ast.TypeParam](out)
	}
	return nil
}

// actionAstFunctionDef builds FunctionDef. Args from the translator:
// (name, args, body, decorators, returns, type_comment, type_params).
//
// CPython: Parser/Python.asdl FunctionDef
func actionAstFunctionDef(p *Parser, args ...any) any {
	_ = p
	name, _ := argAt(args, 0).(string)
	if name == "" {
		return placeholderMatched
	}
	body := stmtSeqOf(argAt(args, 2))
	if len(body) == 0 {
		return placeholderMatched
	}
	return &ast.FunctionDef{
		Name:          name,
		Args:          argumentsOf(argAt(args, 1)),
		Body:          body,
		DecoratorList: exprSeqOf(argAt(args, 3)),
		Returns:       exprOptional(argAt(args, 4)),
		TypeComment:   decodeTypeComment(argAt(args, 5)),
		TypeParams:    typeParamSeqOf(argAt(args, 6)),
		Pos:           ast.NoPos,
	}
}

// actionAstClassDef builds ClassDef. Args from the translator:
// (name, bases, keywords, body, decorators, type_params).
// decorators arrive as nil here; actionPgenClassDefDecorators stamps
// them later when the surrounding rule sees a decorator list.
//
// CPython: Parser/Python.asdl ClassDef
func actionAstClassDef(p *Parser, args ...any) any {
	_ = p
	name, _ := argAt(args, 0).(string)
	if name == "" {
		return placeholderMatched
	}
	body := stmtSeqOf(argAt(args, 3))
	if len(body) == 0 {
		return placeholderMatched
	}
	return &ast.ClassDef{
		Name:          name,
		Bases:         exprSeqOf(argAt(args, 1)),
		Keywords:      keywordSeqOf(argAt(args, 2)),
		Body:          body,
		DecoratorList: exprSeqOf(argAt(args, 4)),
		TypeParams:    typeParamSeqOf(argAt(args, 5)),
		Pos:           ast.NoPos,
	}
}

// actionAstAsyncFunctionDef builds AsyncFunctionDef with the same
// arg shape as actionAstFunctionDef.
//
// CPython: Parser/Python.asdl AsyncFunctionDef
func actionAstAsyncFunctionDef(p *Parser, args ...any) any {
	_ = p
	name, _ := argAt(args, 0).(string)
	if name == "" {
		return placeholderMatched
	}
	body := stmtSeqOf(argAt(args, 2))
	if len(body) == 0 {
		return placeholderMatched
	}
	return &ast.AsyncFunctionDef{
		Name:          name,
		Args:          argumentsOf(argAt(args, 1)),
		Body:          body,
		DecoratorList: exprSeqOf(argAt(args, 3)),
		Returns:       exprOptional(argAt(args, 4)),
		TypeComment:   decodeTypeComment(argAt(args, 5)),
		TypeParams:    typeParamSeqOf(argAt(args, 6)),
		Pos:           ast.NoPos,
	}
}

// actionPgenEmptyArguments returns the all-empty Arguments value used
// when a def has no parenthesised parameters.
//
// CPython: Parser/action_helpers.c:686 _PyPegen_empty_arguments
func actionPgenEmptyArguments(p *Parser, args ...any) any {
	_ = args
	return EmptyArguments(p)
}

// actionAstComprehension builds a single comprehension clause used by
// list/set/dict comps and generator expressions. Args after stripping
// `p->arena`: (target, iter, ifs, is_async).
//
// CPython: Parser/Python.asdl comprehension; constructor in
// Python/Python-ast.c:_PyAST_comprehension.
func actionAstComprehension(p *Parser, args ...any) any {
	_ = p
	target := asExpr(argAt(args, 0))
	iter := asExpr(argAt(args, 1))
	ifs := exprSeqOf(argAt(args, 2))
	isAsync := 0
	if v, ok := argAt(args, 3).(int); ok {
		isAsync = v
	}
	if target == nil || iter == nil {
		return placeholderMatched
	}
	return &ast.Comprehension{Target: target, Iter: iter, Ifs: ifs, IsAsync: isAsync}
}

// actionAstAlias builds an alias entry for import / from-import.
// Args: (name_id, asname_id_or_nil).
//
// CPython: Parser/Python.asdl alias(identifier name, identifier? asname)
func actionAstAlias(p *Parser, args ...any) any {
	_ = p
	name := identString(argAt(args, 0))
	if name == "" {
		return placeholderMatched
	}
	out := &ast.Alias{Name: name, Pos: ast.NoPos}
	if as := identString(argAt(args, 1)); as != "" {
		out.Asname = &as
	}
	return out
}

// actionAstGlobal builds Global. Args: (names_seq).
//
// CPython: Parser/Python.asdl Global(identifier* names)
func actionAstGlobal(p *Parser, args ...any) any {
	_ = p
	names := stringSeqOf(argAt(args, 0))
	if len(names) == 0 {
		return placeholderMatched
	}
	return &ast.Global{Names: names, Pos: ast.NoPos}
}

// actionAstNonlocal builds Nonlocal. Args: (names_seq).
//
// CPython: Parser/Python.asdl Nonlocal(identifier* names)
func actionAstNonlocal(p *Parser, args ...any) any {
	_ = p
	names := stringSeqOf(argAt(args, 0))
	if len(names) == 0 {
		return placeholderMatched
	}
	return &ast.Nonlocal{Names: names, Pos: ast.NoPos}
}

// actionAstLambda builds Lambda. Args: (args, body).
//
// CPython: Parser/Python.asdl Lambda(arguments args, expr body)
func actionAstLambda(p *Parser, args ...any) any {
	_ = p
	a := argumentsOf(argAt(args, 0))
	body := asExpr(argAt(args, 1))
	if body == nil {
		return placeholderMatched
	}
	return &ast.Lambda{Args: a, Body: body, Pos: ast.NoPos}
}

// actionAstWithitem builds withitem. Args: (context_expr, optional_vars).
//
// CPython: Parser/Python.asdl withitem(expr context_expr, expr? optional_vars)
func actionAstWithitem(p *Parser, args ...any) any {
	_ = p
	ctx := asExpr(argAt(args, 0))
	if ctx == nil {
		return placeholderMatched
	}
	return &ast.Withitem{ContextExpr: ctx, OptionalVars: asExpr(argAt(args, 1))}
}

// actionAstMatch builds Match. Args: (subject, cases).
//
// CPython: Parser/Python.asdl Match(expr subject, match_case* cases)
func actionAstMatch(p *Parser, args ...any) any {
	_ = p
	subject := asExpr(argAt(args, 0))
	cases := matchCaseSeqOf(argAt(args, 1))
	if subject == nil || len(cases) == 0 {
		return placeholderMatched
	}
	return &ast.Match{Subject: subject, Cases: cases, Pos: ast.NoPos}
}

// actionAstMatchCase builds match_case. Args: (pattern, guard, body).
//
// CPython: Parser/Python.asdl match_case(pattern pattern, expr? guard, stmt* body)
func actionAstMatchCase(p *Parser, args ...any) any {
	_ = p
	pat := patternOf(argAt(args, 0))
	body := stmtSeqOf(argAt(args, 2))
	if pat == nil || len(body) == 0 {
		return placeholderMatched
	}
	return &ast.MatchCase{Pattern: pat, Guard: asExpr(argAt(args, 1)), Body: body}
}

// actionAstTryStar builds TryStar. Args: (body, handlers, orelse, finalbody).
//
// CPython: Parser/Python.asdl TryStar(stmt* body, excepthandler* handlers,
//
//	stmt* orelse, stmt* finalbody)
func actionAstTryStar(p *Parser, args ...any) any {
	_ = p
	body := stmtSeqOf(argAt(args, 0))
	if len(body) == 0 {
		return placeholderMatched
	}
	return &ast.TryStar{
		Body:      body,
		Handlers:  exceptHandlerSeqOf(argAt(args, 1)),
		Orelse:    stmtSeqOf(argAt(args, 2)),
		Finalbody: stmtSeqOf(argAt(args, 3)),
		Pos:       ast.NoPos,
	}
}

// actionAstTypeAlias builds TypeAlias. Args: (name_expr, type_params, value).
//
// CPython: Parser/Python.asdl TypeAlias(expr name, type_param* type_params, expr value)
func actionAstTypeAlias(p *Parser, args ...any) any {
	_ = p
	name := asExpr(argAt(args, 0))
	value := asExpr(argAt(args, 2))
	if name == nil || value == nil {
		return placeholderMatched
	}
	return &ast.TypeAlias{
		Name:       name,
		TypeParams: typeParamSeqOf(argAt(args, 1)),
		Value:      value,
		Pos:        ast.NoPos,
	}
}

// actionAstTypeVar builds TypeVar. Args: (name_id, bound, default).
//
// CPython: Parser/Python.asdl TypeVar(identifier name, expr? bound, expr? default_value)
func actionAstTypeVar(p *Parser, args ...any) any {
	_ = p
	name := identString(argAt(args, 0))
	if name == "" {
		return placeholderMatched
	}
	return &ast.TypeVar{
		Name:         name,
		Bound:        exprOptional(argAt(args, 1)),
		DefaultValue: exprOptional(argAt(args, 2)),
		Pos:          ast.NoPos,
	}
}

// actionAstTypeVarTuple builds TypeVarTuple. Args: (name_id, default).
//
// CPython: Parser/Python.asdl TypeVarTuple(identifier name, expr? default_value)
func actionAstTypeVarTuple(p *Parser, args ...any) any {
	_ = p
	name := identString(argAt(args, 0))
	if name == "" {
		return placeholderMatched
	}
	return &ast.TypeVarTuple{
		Name:         name,
		DefaultValue: exprOptional(argAt(args, 1)),
		Pos:          ast.NoPos,
	}
}

// actionAstParamSpec builds ParamSpec. Args: (name_id, default).
//
// CPython: Parser/Python.asdl ParamSpec(identifier name, expr? default_value)
func actionAstParamSpec(p *Parser, args ...any) any {
	_ = p
	name := identString(argAt(args, 0))
	if name == "" {
		return placeholderMatched
	}
	return &ast.ParamSpec{
		Name:         name,
		DefaultValue: exprOptional(argAt(args, 1)),
		Pos:          ast.NoPos,
	}
}

// actionPgenMapNamesToIDs extracts the identifier text from a
// sequence of NAME tokens or *ast.Name expressions.
//
// CPython: Parser/action_helpers.c:444 _PyPegen_map_names_to_ids
func actionPgenMapNamesToIDs(p *Parser, args ...any) any {
	_ = p
	v := argAt(args, 1)
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *Token:
			if t != nil && t.Type == token.NAME {
				out = append(out, string(t.Bytes))
			}
		case *ast.Name:
			if t != nil {
				out = append(out, t.Id)
			}
		case ast.Expr:
			if n, ok := t.(*ast.Name); ok && n != nil {
				out = append(out, n.Id)
			}
		case string:
			out = append(out, t)
		case ast.Seq[ast.Expr]:
			for _, e := range t {
				walk(e)
			}
		case []ast.Expr:
			for _, e := range t {
				walk(e)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	if len(out) == 0 {
		return ast.Seq[string]{}
	}
	return ast.Seq[string](out)
}

// actionPgenAliasForStar builds the alias entry for `from X import *`.
// CPython spells the asname as "*" and leaves asname unset.
//
// CPython: Parser/action_helpers.c _PyPegen_alias_for_star
func actionPgenAliasForStar(p *Parser, args ...any) any {
	_ = p
	_ = args
	return &ast.Alias{Name: "*", Pos: ast.NoPos}
}

// actionPgenGetCmpops returns the operator slice from a list of
// cmpopExprPair, mirroring `_PyPegen_get_cmpops` which projects out
// the cmpop column.
//
// CPython: Parser/action_helpers.c _PyPegen_get_cmpops
func actionPgenGetCmpops(p *Parser, args ...any) any {
	_ = p
	pairs := flattenCmpopExprPairs(argAt(args, 1))
	out := make(ast.Seq[ast.Cmpop], 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, pr.Op)
	}
	return out
}

// actionPgenGetExprs returns the rhs-expression slice from a list of
// cmpopExprPair, mirroring `_PyPegen_get_exprs`.
//
// CPython: Parser/action_helpers.c _PyPegen_get_exprs
func actionPgenGetExprs(p *Parser, args ...any) any {
	_ = p
	pairs := flattenCmpopExprPairs(argAt(args, 1))
	out := make(ast.Seq[ast.Expr], 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, pr.Expr)
	}
	return out
}

// actionPgenGetKeys returns the key column from a list of [2]any
// (key, value) pairs produced by actionPgenKeyValuePair.
//
// CPython: Parser/action_helpers.c _PyPegen_get_keys
func actionPgenGetKeys(p *Parser, args ...any) any {
	_ = p
	pairs := flattenKVPairs(argAt(args, 1))
	out := make(ast.Seq[ast.Expr], 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, asExpr(pr[0]))
	}
	return out
}

// actionPgenGetValues returns the value column from a list of [2]any
// (key, value) pairs.
//
// CPython: Parser/action_helpers.c _PyPegen_get_values
func actionPgenGetValues(p *Parser, args ...any) any {
	_ = p
	pairs := flattenKVPairs(argAt(args, 1))
	out := make(ast.Seq[ast.Expr], 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, asExpr(pr[1]))
	}
	return out
}

// actionPgenGetPatternKeys returns the key column from a list of
// [2]any (key, pattern) pairs from actionPgenKeyPatternPair.
//
// CPython: Parser/action_helpers.c _PyPegen_get_pattern_keys
func actionPgenGetPatternKeys(p *Parser, args ...any) any {
	_ = p
	pairs := flattenKVPairs(argAt(args, 1))
	out := make(ast.Seq[ast.Expr], 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, asExpr(pr[0]))
	}
	return out
}

// actionPgenGetPatterns returns the pattern column from a list of
// [2]any (key, pattern) pairs.
//
// CPython: Parser/action_helpers.c:415 _PyPegen_get_patterns
func actionPgenGetPatterns(p *Parser, args ...any) any {
	_ = p
	pairs := flattenKVPairs(argAt(args, 1))
	out := make(ast.Seq[ast.Pattern], 0, len(pairs))
	for _, pr := range pairs {
		out = append(out, patternOf(pr[1]))
	}
	return out
}

// flattenKVPairs walks v (possibly nested []any) and collects every
// [2]any entry produced by KeyValuePair / KeyPatternPair. Pairs are
// the smallest object the rule emits so flattening through one level
// is enough.
func flattenKVPairs(v any) [][2]any {
	var out [][2]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case [2]any:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// matchCaseSeqOf coerces v into Seq[*ast.MatchCase]. The cases rule
// returns a +-repetition wrapped in []any.
func matchCaseSeqOf(v any) ast.Seq[*ast.MatchCase] {
	var out []*ast.MatchCase
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
		case *ast.MatchCase:
			if t != nil {
				out = append(out, t)
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
	return ast.Seq[*ast.MatchCase](out)
}

// actionPgenCheckedFutureImport mirrors `_PyPegen_checked_future_import`.
// CPython also flips the parser's PARSE_BARRY_AS_BDFL flag when a
// `from __future__ import barry_as_FLUFL` is seen; that flag is not
// yet plumbed through the Go parser, so we build the ImportFrom and
// leave the flag handling for when feature_version / future flags
// are wired up.
//
// CPython: Parser/action_helpers.c:1921 _PyPegen_checked_future_import
func actionPgenCheckedFutureImport(p *Parser, args ...any) any {
	_ = p
	mod := nameOf(argAt(args, 1))
	names := aliasSeqOf(argAt(args, 2))
	lvl := intOf(argAt(args, 3))
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

// positioner is anything that exposes an ast.Pos. Both ast.Stmt and
// ast.Expr interfaces require Position(); ast.Keyword and the type
// params likewise expose one.
type positioner interface{ Position() ast.Pos }

// extractPos peels the variadic wrapper layers a translated action
// emits and returns the underlying source-span. Handles tokens,
// typed AST nodes, and pre-built position quads.
func extractPos(v any) ast.Pos {
	for {
		switch x := v.(type) {
		case nil:
			return ast.NoPos
		case *Token:
			return tokenPos(x)
		case ast.Pos:
			return x
		case positioner:
			return x.Position()
		case []any:
			if len(x) == 0 {
				return ast.NoPos
			}
			v = x[0]
		default:
			return ast.NoPos
		}
	}
}

// errPosFromAst converts an ast.Pos to the perrors.Pos shape used by
// the pinned SyntaxError record.
func errPosFromAst(p ast.Pos) perrors.Pos {
	return perrors.Pos{
		Lineno:  p.Lineno,
		ColOff:  p.ColOffset,
		EndLine: p.EndLineno,
		EndCol:  p.EndColOffset,
	}
}

// peekTokenPos returns the perrors.Pos of the next token, or the zero
// value if the buffer is empty.
func peekTokenPos(p *Parser) perrors.Pos {
	t := p.Peek()
	if t == nil {
		return perrors.Pos{}
	}
	return perrors.Pos{
		Lineno:  t.Lineno,
		ColOff:  t.ColOff,
		EndLine: t.EndLine,
		EndCol:  t.EndCol,
	}
}

// normalizeFormat rewrites CPython printf conversions that don't exist
// in Go: %U (PyUnicode) becomes %s. The translator passes Go strings
// for the corresponding format arguments, so a plain %s round-trips
// the value.
func normalizeFormat(s string) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}
	return strings.ReplaceAll(s, "%U", "%s")
}

// raiseAction is the runtime fan-out for the RAISE_SYNTAX_ERROR_*
// macros the action translator emits. `kind` selects the position
// style; `args` holds the optional start/end nodes followed by the
// printf-style format and its arguments.
//
// Returns nil so the calling alt records a parse failure. The
// SyntaxError is pinned on the Parser via the existing record path
// so SetSyntaxError surfaces it after Dispatch.
//
// CPython: Parser/pegen.h RAISE_SYNTAX_ERROR / RAISE_ERROR_KNOWN_LOCATION
func raiseAction(p *Parser, kind string, args ...any) any {
	if p == nil {
		return nil
	}
	var (
		pos     perrors.Pos
		msg     string
		fmtArgs []any
	)
	switch kind {
	case "RAISE_SYNTAX_ERROR_KNOWN_RANGE":
		start := extractPos(argAt(args, 0))
		end := extractPos(argAt(args, 1))
		pos = perrors.Pos{
			Lineno:  start.Lineno,
			ColOff:  start.ColOffset,
			EndLine: end.EndLineno,
			EndCol:  end.EndColOffset,
		}
		msg, _ = argAt(args, 2).(string)
		if len(args) > 3 {
			fmtArgs = args[3:]
		}
	case "RAISE_SYNTAX_ERROR_KNOWN_LOCATION", "RAISE_SYNTAX_ERROR_INVALID_TARGET":
		pos = errPosFromAst(extractPos(argAt(args, 0)))
		msg, _ = argAt(args, 1).(string)
		if len(args) > 2 {
			fmtArgs = args[2:]
		}
	case "RAISE_SYNTAX_ERROR_STARTING_FROM":
		start := extractPos(argAt(args, 0))
		peek := peekTokenPos(p)
		pos = perrors.Pos{
			Lineno:  start.Lineno,
			ColOff:  start.ColOffset,
			EndLine: peek.EndLine,
			EndCol:  peek.EndCol,
		}
		msg, _ = argAt(args, 1).(string)
		if len(args) > 2 {
			fmtArgs = args[2:]
		}
	case "RAISE_SYNTAX_ERROR_ON_NEXT_TOKEN":
		pos = peekTokenPos(p)
		msg, _ = argAt(args, 0).(string)
		if len(args) > 1 {
			fmtArgs = args[1:]
		}
	default: // RAISE_SYNTAX_ERROR
		pos = peekTokenPos(p)
		msg, _ = argAt(args, 0).(string)
		if len(args) > 1 {
			fmtArgs = args[1:]
		}
	}
	if msg == "" {
		msg = "invalid syntax"
	}
	msg = normalizeFormat(msg)
	if len(fmtArgs) > 0 {
		p.RaiseSyntaxErrorKnownLocation(pos, msg, fmtArgs...)
	} else {
		p.RaiseSyntaxErrorKnownLocation(pos, "%s", msg)
	}
	return nil
}
