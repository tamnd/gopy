// Grammar AST. Mirrors cpython/Tools/peg_generator/pegen/grammar.py.
// Each Go type stands in for one Python class so the upstream
// generator algorithms translate one-to-one.
//
// CPython: Tools/peg_generator/pegen/grammar.py

package main

// Grammar is the root: an ordered list of rules plus the @meta
// directives at the top of the file (@trailer, @subheader, etc).
type Grammar struct {
	Rules []*Rule
	Metas []Meta
}

// Meta is one @name [value] directive.
type Meta struct {
	Name  string
	Value string // empty when the directive has no value
}

// Rule is one named grammar production. Type is the bracketed
// return type (e.g. "stmt_ty"), empty when omitted. Memo flags the
// (memo) annotation. LeftRecursive and Leader are stamped later by
// the analyser, not by the parser.
type Rule struct {
	Name          string
	Type          string
	RHS           *RHS
	Memo          bool
	LeftRecursive bool
	Leader        bool
}

// RHS is a sequence of alternatives joined by '|'.
type RHS struct {
	Alts []*Alt
}

// Alt is one alternative: a list of items plus an optional action.
// Icut is the index of the cut operator within Items, or -1.
type Alt struct {
	Items  []*NamedItem
	Action string
	Icut   int
}

// NamedItem wraps an Item with an optional binding name and type
// annotation. The metagrammar is "NAME annotation '=' item" so both
// can be set.
type NamedItem struct {
	Name string
	Item Item
	Type string
}

// Item is the metagrammar's item disjunction. Concrete kinds:
// NameLeaf, StringLeaf, Group, Opt, Repeat0, Repeat1, Gather,
// Forced, PositiveLookahead, NegativeLookahead, Cut, *RHS.
type Item interface {
	itemKind()
}

// NameLeaf is a bare identifier reference. It might name another
// rule or a token kind; the resolver decides later.
type NameLeaf struct{ Value string }

// StringLeaf is a quoted literal. Single-quoted strings in the
// grammar are hard keywords ('return'); double-quoted strings are
// soft keywords ("match"). Value carries the surrounding quotes.
type StringLeaf struct{ Value string }

// Group is a parenthesised sub-expression.
type Group struct{ RHS *RHS }

// Opt is `e?` or `[e]`. Both forms reduce to this node.
type Opt struct{ Item Item }

// Repeat0 is `e*`.
type Repeat0 struct{ Item Item }

// Repeat1 is `e+`.
type Repeat1 struct{ Item Item }

// Gather is `s.e+`: one or more `e` separated by `s`.
type Gather struct{ Sep, Node Item }

// Forced is `&&e`: like an expect, but raises a SyntaxError on miss
// instead of backtracking.
type Forced struct{ Node Item }

// PositiveLookahead is `&e`.
type PositiveLookahead struct{ Node Item }

// NegativeLookahead is `!e`.
type NegativeLookahead struct{ Node Item }

// Cut is the `~` operator: commit to the current alt.
type Cut struct{}

func (*NameLeaf) itemKind()          {}
func (*StringLeaf) itemKind()        {}
func (*Group) itemKind()             {}
func (*Opt) itemKind()               {}
func (*Repeat0) itemKind()           {}
func (*Repeat1) itemKind()           {}
func (*Gather) itemKind()            {}
func (*Forced) itemKind()            {}
func (*PositiveLookahead) itemKind() {}
func (*NegativeLookahead) itemKind() {}
func (*Cut) itemKind()               {}
func (*RHS) itemKind()               {}
