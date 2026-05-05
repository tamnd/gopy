// CPython: Parser/action_helpers.c. Helpers that build the
// `arguments` AST node and split a flat call-argument list into
// positional and keyword sides.

package pegen

import "github.com/tamnd/gopy/ast"

// NameDefaultPair is the (arg, default) tuple a regular parameter
// rule emits. nameDefaultPair is the constructor; the parser action
// wraps the (arg, value, type-comment) triple into a fresh pair.
//
// CPython: Parser/pegen.h:111 NameDefaultPair
type NameDefaultPair struct {
	Arg   *ast.Arg
	Value ast.Expr
}

// nameDefaultPair allocates a NameDefaultPair and runs the optional
// type-comment token through addTypeCommentToArg before storing it.
//
// CPython: Parser/action_helpers.c:430 _PyPegen_name_default_pair
func nameDefaultPair(p *Parser, arg *ast.Arg, value ast.Expr, tc *Token) *NameDefaultPair {
	a := addTypeCommentToArg(p, arg, tc)
	if a == nil {
		return nil
	}
	return &NameDefaultPair{Arg: a, Value: value}
}

// SlashWithDefault carries the (plain-names, names-with-defaults)
// pair the slash_with_default parameter rule emits. The bundle holds
// position-only parameters; the surrounding makeArguments folds it
// into Arguments.posonlyargs / Arguments.defaults.
//
// CPython: Parser/pegen.h:116 SlashWithDefault
type SlashWithDefault struct {
	PlainNames        []*ast.Arg
	NamesWithDefaults []*NameDefaultPair
}

// slashWithDefault allocates a fresh SlashWithDefault.
//
// CPython: Parser/action_helpers.c:447 _PyPegen_slash_with_default
func slashWithDefault(p *Parser, plainNames []*ast.Arg, namesWithDefaults []*NameDefaultPair) *SlashWithDefault {
	_ = p
	return &SlashWithDefault{PlainNames: plainNames, NamesWithDefaults: namesWithDefaults}
}

// StarEtc holds the (*vararg, kwonlyargs, **kwarg) tail of a
// parameter list. kwonlyargs is a slice of NameDefaultPair so that a
// kwonly arg can carry its own default.
//
// CPython: Parser/pegen.h:121 StarEtc
type StarEtc struct {
	Vararg     *ast.Arg
	Kwonlyargs []*NameDefaultPair
	Kwarg      *ast.Arg
}

// starEtc allocates a fresh StarEtc.
//
// CPython: Parser/action_helpers.c:460 _PyPegen_star_etc
func starEtc(p *Parser, vararg *ast.Arg, kwonlyargs []*NameDefaultPair, kwarg *ast.Arg) *StarEtc {
	_ = p
	return &StarEtc{Vararg: vararg, Kwonlyargs: kwonlyargs, Kwarg: kwarg}
}

// getNames extracts the .Arg field from each NameDefaultPair.
//
// CPython: Parser/action_helpers.c:493 _get_names
func getNames(p *Parser, namesWithDefaults []*NameDefaultPair) []*ast.Arg {
	_ = p
	out := make([]*ast.Arg, len(namesWithDefaults))
	for i, pair := range namesWithDefaults {
		out[i] = pair.Arg
	}
	return out
}

// getDefaults extracts the .Value field from each NameDefaultPair.
// Pairs without a default contribute nil; CPython relies on the
// surrounding rules to never call getDefaults on a sequence that
// mixes default-bearing and default-free pairs in a way that would
// confuse the asdl_seq.
//
// CPython: Parser/action_helpers.c:508 _get_defaults
func getDefaults(p *Parser, namesWithDefaults []*NameDefaultPair) []ast.Expr {
	_ = p
	out := make([]ast.Expr, len(namesWithDefaults))
	for i, pair := range namesWithDefaults {
		out[i] = pair.Value
	}
	return out
}

// makePosonlyargs picks the position-only argument list. CPython has
// three branches: slash-without-default, slash-with-default, or
// neither (empty list).
//
// CPython: Parser/action_helpers.c:523 _make_posonlyargs
func makePosonlyargs(p *Parser, slashWithoutDefault []*ast.Arg, slashWithDefault *SlashWithDefault) []*ast.Arg {
	if slashWithoutDefault != nil {
		return slashWithoutDefault
	}
	if slashWithDefault != nil {
		names := getNames(p, slashWithDefault.NamesWithDefaults)
		out := make([]*ast.Arg, 0, len(slashWithDefault.PlainNames)+len(names))
		out = append(out, slashWithDefault.PlainNames...)
		out = append(out, names...)
		return out
	}
	return []*ast.Arg{}
}

// makePosargs picks the regular positional arguments. The CPython
// branch with `plainNames != nil && namesWithDefault == nil` carries
// an `assert(0)` because the grammar never reaches it; the Go port
// preserves the dead branch so future grammar changes still match.
//
// CPython: Parser/action_helpers.c:548 _make_posargs
func makePosargs(p *Parser, plainNames []*ast.Arg, namesWithDefault []*NameDefaultPair) []*ast.Arg {
	if namesWithDefault != nil {
		if plainNames != nil {
			names := getNames(p, namesWithDefault)
			out := make([]*ast.Arg, 0, len(plainNames)+len(names))
			out = append(out, plainNames...)
			out = append(out, names...)
			return out
		}
		return getNames(p, namesWithDefault)
	}
	if plainNames != nil {
		return plainNames
	}
	return []*ast.Arg{}
}

// makePosdefaults picks the positional-default expression list.
//
// CPython: Parser/action_helpers.c:581 _make_posdefaults
func makePosdefaults(p *Parser, slashWithDefault *SlashWithDefault, namesWithDefault []*NameDefaultPair) []ast.Expr {
	if slashWithDefault != nil && namesWithDefault != nil {
		swd := getDefaults(p, slashWithDefault.NamesWithDefaults)
		nwd := getDefaults(p, namesWithDefault)
		out := make([]ast.Expr, 0, len(swd)+len(nwd))
		out = append(out, swd...)
		out = append(out, nwd...)
		return out
	}
	if slashWithDefault == nil && namesWithDefault != nil {
		return getDefaults(p, namesWithDefault)
	}
	if slashWithDefault != nil && namesWithDefault == nil {
		return getDefaults(p, slashWithDefault.NamesWithDefaults)
	}
	return []ast.Expr{}
}

// makeKwargs lifts the keyword-only arg/default pair out of starEtc.
// Returns (kwonlyargs, kwdefaults) as parallel slices; both are
// non-nil and possibly empty.
//
// CPython: Parser/action_helpers.c:613 _make_kwargs
func makeKwargs(p *Parser, se *StarEtc) ([]*ast.Arg, []ast.Expr) {
	if se != nil && se.Kwonlyargs != nil {
		return getNames(p, se.Kwonlyargs), getDefaults(p, se.Kwonlyargs)
	}
	return []*ast.Arg{}, []ast.Expr{}
}

// MakeArguments folds the parameter-list bundle into a single
// Arguments node.
//
// CPython: Parser/action_helpers.c:643 _PyPegen_make_arguments
func MakeArguments(
	p *Parser,
	slashWithoutDefault []*ast.Arg,
	slashWithDefault *SlashWithDefault,
	plainNames []*ast.Arg,
	namesWithDefault []*NameDefaultPair,
	se *StarEtc,
) *ast.Arguments {
	posonlyargs := makePosonlyargs(p, slashWithoutDefault, slashWithDefault)
	posargs := makePosargs(p, plainNames, namesWithDefault)
	posdefaults := makePosdefaults(p, slashWithDefault, namesWithDefault)
	var vararg *ast.Arg
	if se != nil {
		vararg = se.Vararg
	}
	kwonlyargs, kwdefaults := makeKwargs(p, se)
	var kwarg *ast.Arg
	if se != nil {
		kwarg = se.Kwarg
	}
	return &ast.Arguments{
		Posonlyargs: ast.Seq[*ast.Arg](posonlyargs),
		Args:        ast.Seq[*ast.Arg](posargs),
		Vararg:      vararg,
		Kwonlyargs:  ast.Seq[*ast.Arg](kwonlyargs),
		KwDefaults:  ast.Seq[ast.Expr](kwdefaults),
		Kwarg:       kwarg,
		Defaults:    ast.Seq[ast.Expr](posdefaults),
	}
}

// EmptyArguments returns the all-empty Arguments node CPython lifts
// when a function has no parameter list.
//
// CPython: Parser/action_helpers.c:686 _PyPegen_empty_arguments
func EmptyArguments(p *Parser) *ast.Arguments {
	_ = p
	return &ast.Arguments{
		Posonlyargs: ast.Seq[*ast.Arg]{},
		Args:        ast.Seq[*ast.Arg]{},
		Kwonlyargs:  ast.Seq[*ast.Arg]{},
		KwDefaults:  ast.Seq[ast.Expr]{},
		Defaults:    ast.Seq[ast.Expr]{},
	}
}

// addTypeCommentToArg copies arg with TypeComment populated when tc
// is non-nil. Returns the input unchanged when there is no type
// comment to attach.
//
// CPython: Parser/action_helpers.c:903 _PyPegen_add_type_comment_to_arg
func addTypeCommentToArg(p *Parser, a *ast.Arg, tc *Token) *ast.Arg {
	_ = p
	if tc == nil {
		return a
	}
	tco := string(tc.Bytes)
	out := *a
	out.TypeComment = &tco
	return &out
}

// KeywordOrStarred is the union the kwarg_or_starred /
// kwarg_or_double_starred rules produce: each entry carries either a
// positional / *args expression (IsKeyword false) or a (name, value)
// keyword (IsKeyword true). The collect-call-seqs path splits the
// flat list back into the asdl_expr_seq / asdl_keyword_seq pair Call
// expects.
//
// CPython: Parser/pegen.h:91 KeywordOrStarred
type KeywordOrStarred struct {
	Element   any
	IsKeyword bool
}

// keywordOrStarred allocates a fresh KeywordOrStarred carrying
// element with the given is_keyword flag.
//
// CPython: Parser/action_helpers.c:769 _PyPegen_keyword_or_starred
func keywordOrStarred(p *Parser, element any, isKeyword bool) *KeywordOrStarred {
	_ = p
	return &KeywordOrStarred{Element: element, IsKeyword: isKeyword}
}

// seqNumberOfStarredExprs returns the count of non-keyword entries.
//
// CPython: Parser/action_helpers.c:783 _seq_number_of_starred_exprs
func seqNumberOfStarredExprs(seq []*KeywordOrStarred) int {
	n := 0
	for _, k := range seq {
		if !k.IsKeyword {
			n++
		}
	}
	return n
}

// seqExtractStarredExprs lifts the starred-expression elements out of
// a KeywordOrStarred slice. Returns nil when there are none, matching
// CPython's `new_len == 0` early return.
//
// CPython: Parser/action_helpers.c:797 _PyPegen_seq_extract_starred_exprs
func seqExtractStarredExprs(p *Parser, seq []*KeywordOrStarred) []ast.Expr {
	_ = p
	newLen := seqNumberOfStarredExprs(seq)
	if newLen == 0 {
		return nil
	}
	out := make([]ast.Expr, 0, newLen)
	for _, k := range seq {
		if !k.IsKeyword {
			if e, ok := k.Element.(ast.Expr); ok {
				out = append(out, e)
			}
		}
	}
	return out
}

// seqDeleteStarredExprs returns a new sequence containing only the
// keyword entries. CPython returns NULL when none survive; the Go
// port returns nil to mirror that.
//
// CPython: Parser/action_helpers.c:820 _PyPegen_seq_delete_starred_exprs
func seqDeleteStarredExprs(p *Parser, seq []*KeywordOrStarred) []*ast.Keyword {
	_ = p
	newLen := len(seq) - seqNumberOfStarredExprs(seq)
	if newLen == 0 {
		return nil
	}
	out := make([]*ast.Keyword, 0, newLen)
	for _, k := range seq {
		if k.IsKeyword {
			if kw, ok := k.Element.(*ast.Keyword); ok {
				out = append(out, kw)
			}
		}
	}
	return out
}

// dummyName is the sentinel Name CPython uses as the function side of
// the Call carrier produced by collect_call_seqs / the kwargs alt of
// the args rule. The surrounding primary rule replaces this name with
// the actual callee.
//
// CPython: Parser/action_helpers.c:11 _PyPegen_dummy_name
func dummyName(p *Parser) ast.Expr {
	_ = p
	return &ast.Name{Id: "", Ctx: ast.Load, Pos: ast.NoPos}
}

// joinSequences concatenates two slices into a fresh one. Mirrors the
// asdl_seq concat pattern CPython uses to glue the two halves of a
// kwargs alt or to merge positional/positional-default lists.
//
// CPython: Parser/action_helpers.c:472 _PyPegen_join_sequences
func joinSequences(p *Parser, a, b []*KeywordOrStarred) []*KeywordOrStarred {
	_ = p
	out := make([]*KeywordOrStarred, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// CollectCallSeqs builds the dummy-Call carrier the args rule
// returns. The surrounding primary alt extracts .Args and .Keywords
// and rebuilds the real Call with the actual callee.
//
//	args[expr_ty]:
//	    | a=','.starred_or_expr+ b=[',' k=kwargs {k}] {
//	        _PyPegen_collect_call_seqs(p, a, b, EXTRA) }
//
// b is nil when the source has no kwargs tail. When present, it is a
// flat sequence of *KeywordOrStarred whose starred elements append to
// the positional list and whose keyword elements become the keyword
// list.
//
// CPython: Parser/action_helpers.c:1129 _PyPegen_collect_call_seqs
func CollectCallSeqs(p *Parser, a []ast.Expr, b []*KeywordOrStarred) ast.Expr {
	if b == nil {
		return &ast.Call{
			Func:     dummyName(p),
			Args:     ast.Seq[ast.Expr](a),
			Keywords: nil,
			Pos:      ast.NoPos,
		}
	}
	starreds := seqExtractStarredExprs(p, b)
	keywords := seqDeleteStarredExprs(p, b)
	totalLen := len(a) + len(starreds)
	args := make([]ast.Expr, 0, totalLen)
	args = append(args, a...)
	args = append(args, starreds...)
	return &ast.Call{
		Func:     dummyName(p),
		Args:     ast.Seq[ast.Expr](args),
		Keywords: ast.Seq[*ast.Keyword](keywords),
		Pos:      ast.NoPos,
	}
}
