// CPython: Parser/action_helpers.c. Helpers that build the
// `arguments` AST node and split a flat call-argument list into
// positional and keyword sides.

package pegen

import "github.com/tamnd/gopy/ast"

// SlashWithDefault carries one half of the position-only / regular
// split a function-def signature can express with a `/`. The C source
// uses two separate fields on the rule's value union; gopy keeps a
// small struct so the rule action can pass them as one value.
//
// CPython: Parser/pegen.h:111 SlashWithDefault
type SlashWithDefault struct {
	PlainNames []*ast.Arg
	Defaults   []ast.Expr // matches len(NamesWithDefaults)
	Names      []*ast.Arg // names that pair with Defaults
}

// StarEtc is the (vararg, kwonly, kwarg) bundle. Same shape reason.
//
// CPython: Parser/pegen.h:118 StarEtc
type StarEtc struct {
	Vararg     *ast.Arg
	Kwonlyargs []*ast.Arg
	KwDefaults []ast.Expr
	Kwarg      *ast.Arg
}

// EmptyArguments returns the zero-value Arguments node. Rules that
// see an empty parameter list lift this into the FunctionDef.
//
// CPython: Parser/action_helpers.c:1014 _PyPegen_empty_arguments
func EmptyArguments() *ast.Arguments {
	return &ast.Arguments{}
}

// MakeArguments stitches the four sub-bundles a parameter list rule
// produces into a single Arguments node. Any of the pointers can be
// nil if that section was absent in the source.
//
// CPython: Parser/action_helpers.c:1042 _PyPegen_make_arguments
func MakeArguments(
	posOnly *SlashWithDefault,
	posOnlyDefaults []ast.Expr,
	plain []*ast.Arg,
	plainDefaults []*NameDefaultPair,
	starEtc *StarEtc,
) *ast.Arguments {
	a := &ast.Arguments{}
	if posOnly != nil {
		a.Posonlyargs = append(append(ast.Seq[*ast.Arg]{}, posOnly.PlainNames...), posOnly.Names...)
		a.Defaults = append(a.Defaults, posOnly.Defaults...)
	}
	a.Defaults = append(a.Defaults, posOnlyDefaults...)

	a.Args = append(a.Args, plain...)
	for _, p := range plainDefaults {
		a.Args = append(a.Args, p.Arg)
		if p.Value != nil {
			a.Defaults = append(a.Defaults, p.Value)
		}
	}

	if starEtc != nil {
		a.Vararg = starEtc.Vararg
		a.Kwonlyargs = append(a.Kwonlyargs, starEtc.Kwonlyargs...)
		a.KwDefaults = append(a.KwDefaults, starEtc.KwDefaults...)
		a.Kwarg = starEtc.Kwarg
	}
	return a
}

// NameDefaultPair is the (name, default) tuple a regular parameter
// rule emits before MakeArguments folds it into Arguments.
//
// CPython: Parser/pegen.h:104 NameDefaultPair
type NameDefaultPair struct {
	Arg   *ast.Arg
	Value ast.Expr // nil when the parameter has no default
}

// KeywordOrStarred is the union the call-arguments rule produces:
// each entry is either a positional / *args expression (IsKeyword
// false) or a (name, value) keyword (IsKeyword true).
//
// CPython: Parser/pegen.h:91 KeywordOrStarred
type KeywordOrStarred struct {
	Element   any // ast.Expr or *ast.Keyword
	IsKeyword bool
}

// SplitKeywordOrStarred reorders a flat KeywordOrStarred list into
// the (positional, keyword) pair the Call node expects. The C
// source preserves source order within each side.
//
// CPython: Parser/action_helpers.c:444 _PyPegen_seq_extract_starred_exprs
// CPython: Parser/action_helpers.c:475 _PyPegen_seq_delete_starred_exprs
func SplitKeywordOrStarred(items []KeywordOrStarred) (args []ast.Expr, kws []*ast.Keyword) {
	for _, it := range items {
		if it.IsKeyword {
			if kw, ok := it.Element.(*ast.Keyword); ok {
				kws = append(kws, kw)
			}
			continue
		}
		if e, ok := it.Element.(ast.Expr); ok {
			args = append(args, e)
		}
	}
	return args, kws
}

// CollectCallSeqs is the ergonomic wrapper rules call when they
// already have positional and keyword sides built.
//
// CPython: Parser/action_helpers.c:431 _PyPegen_collect_call_seqs
func CollectCallSeqs(args []ast.Expr, kws []*ast.Keyword) (
	[]ast.Expr, []*ast.Keyword,
) {
	return args, kws
}
