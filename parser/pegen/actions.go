// CPython: Parser/action_helpers.c. The generated parser table calls
// these helpers from rule actions. They are mechanical translations
// of the C helpers, with one twist: where the C source juggles
// asdl_seq* with element-typed wrappers (asdl_expr_seq, asdl_stmt_seq),
// gopy uses native Go slices and lets the type parameter on each
// helper carry the element type.
//
// This file holds the shape helpers (sequence ops, name joins,
// expr-context stamping) that do not depend on full AST builder
// surface. The AST-builder helpers (MakeArguments, FunctionDef
// with decorators, JoinedStr assembly) land alongside the generated
// parser when parser_gen lands.

package pegen

import (
	"strings"

	"github.com/tamnd/gopy/ast"
)

// SingletonSeq wraps one element into a one-element slice. The
// generated parser uses this to lift a single rule result into the
// list shape that the next rule expects.
//
// CPython: Parser/action_helpers.c:80 _PyPegen_singleton_seq
func SingletonSeq[T any](a T) []T { return []T{a} }

// SeqInsertInFront returns a new slice with a prepended to seq. The
// C source allocates a fresh asdl_seq; the Go version makes a copy
// for the same reason (caller may still hold seq).
//
// CPython: Parser/action_helpers.c:97 _PyPegen_seq_insert_in_front
func SeqInsertInFront[T any](a T, seq []T) []T {
	out := make([]T, 0, len(seq)+1)
	out = append(out, a)
	out = append(out, seq...)
	return out
}

// SeqAppendToEnd returns a new slice with a appended to seq.
//
// CPython: Parser/action_helpers.c:114 _PyPegen_seq_append_to_end
func SeqAppendToEnd[T any](seq []T, a T) []T {
	out := make([]T, 0, len(seq)+1)
	out = append(out, seq...)
	out = append(out, a)
	return out
}

// SeqFlatten concatenates a slice of slices.
//
// CPython: Parser/action_helpers.c:131 _PyPegen_seq_flatten
func SeqFlatten[T any](seqs [][]T) []T {
	n := 0
	for _, s := range seqs {
		n += len(s)
	}
	out := make([]T, 0, n)
	for _, s := range seqs {
		out = append(out, s...)
	}
	return out
}

// SeqFirstItem returns seq[0] or the zero value if empty.
//
// CPython: Parser/action_helpers.c:172 _PyPegen_seq_first_item
func SeqFirstItem[T any](seq []T) T {
	var zero T
	if len(seq) == 0 {
		return zero
	}
	return seq[0]
}

// SeqLastItem returns the last item or the zero value if empty.
//
// CPython: Parser/action_helpers.c:163 _PyPegen_seq_last_item
func SeqLastItem[T any](seq []T) T {
	var zero T
	if len(seq) == 0 {
		return zero
	}
	return seq[len(seq)-1]
}

// seqLastAny returns the last element of a sequence-shaped action
// result. Rule-result sequences arrive as `[]any` from the generator;
// individual AST nodes are passed through untouched so the caller can
// chain extractPos on either form.
//
// CPython: Parser/pegen.h:265 PyPegen_last_item macro
func seqLastAny(v any) any {
	for {
		switch x := v.(type) {
		case nil:
			return nil
		case []any:
			if len(x) == 0 {
				return nil
			}
			v = x[len(x)-1]
		default:
			return v
		}
	}
}

// SeqCountDots counts the leading DOT/ELLIPSIS tokens that the
// `from ... import` rule accepts. ELLIPSIS counts as three dots.
//
// CPython: Parser/action_helpers.c:198 _PyPegen_seq_count_dots
func SeqCountDots(seq []*Token) int {
	n := 0
	for _, t := range seq {
		switch t.Type {
		case 23: // DOT, see tokenize/types_gen.go
			n++
		case 52: // ELLIPSIS
			n += 3
		}
	}
	return n
}

// JoinNamesWithDot turns two ast.Name expressions into "a.b". The
// generated parser chains this for dotted import names.
//
// CPython: Parser/action_helpers.c:181 _PyPegen_join_names_with_dot
func JoinNamesWithDot(first, second *ast.Name) string {
	return first.Id + "." + second.Id
}

// JoinIDsWithDot is the n-ary form, used by the import-name rule.
// CPython composes by calling join_names_with_dot in a fold; gopy
// hands the slice over directly because Go has no asdl_seq.
//
// CPython: Parser/action_helpers.c:181 (n-ary fold pattern)
func JoinIDsWithDot(ids []string) string { return strings.Join(ids, ".") }

// SetExprContext returns a copy of expr with ctx as the context. Name,
// Tuple, List, Subscript, Attribute and Starred are rewritten; every
// other expression kind is returned unchanged. Tuple and List recurse
// through their element list via setSeqContext; Starred recurses into
// its inner value.
//
// CPython: Parser/action_helpers.c:309 _PyPegen_set_expr_context
func SetExprContext(p *Parser, expr ast.Expr, ctx ast.ExprContext) ast.Expr {
	switch v := expr.(type) {
	case *ast.Name:
		return setNameContext(p, v, ctx)
	case *ast.Tuple:
		return setTupleContext(p, v, ctx)
	case *ast.List:
		return setListContext(p, v, ctx)
	case *ast.Subscript:
		return setSubscriptContext(p, v, ctx)
	case *ast.Attribute:
		return setAttributeContext(p, v, ctx)
	case *ast.Starred:
		return setStarredContext(p, v, ctx)
	}
	return expr
}

// setSeqContext walks every element of seq and rebuilds the slice with
// each element rewritten through SetExprContext.
//
// CPython: Parser/action_helpers.c:243 _set_seq_context
func setSeqContext(p *Parser, seq []ast.Expr, ctx ast.ExprContext) []ast.Expr {
	if len(seq) == 0 {
		return nil
	}
	out := make([]ast.Expr, len(seq))
	for i, e := range seq {
		out[i] = SetExprContext(p, e, ctx)
	}
	return out
}

// setNameContext returns a fresh Name carrying ctx.
//
// CPython: Parser/action_helpers.c:262 _set_name_context
func setNameContext(p *Parser, e *ast.Name, ctx ast.ExprContext) ast.Expr {
	_ = p
	return &ast.Name{Id: e.Id, Ctx: ctx, Pos: e.Pos}
}

// setTupleContext returns a fresh Tuple whose elements have been
// rewritten with ctx.
//
// CPython: Parser/action_helpers.c:268 _set_tuple_context
func setTupleContext(p *Parser, e *ast.Tuple, ctx ast.ExprContext) ast.Expr {
	return &ast.Tuple{Elts: setSeqContext(p, e.Elts, ctx), Ctx: ctx, Pos: e.Pos}
}

// setListContext is the List counterpart of setTupleContext.
//
// CPython: Parser/action_helpers.c:277 _set_list_context
func setListContext(p *Parser, e *ast.List, ctx ast.ExprContext) ast.Expr {
	return &ast.List{Elts: setSeqContext(p, e.Elts, ctx), Ctx: ctx, Pos: e.Pos}
}

// setSubscriptContext returns a fresh Subscript carrying ctx; the
// value and slice are reused unchanged because subscript ctx never
// recurses in CPython.
//
// CPython: Parser/action_helpers.c:286 _set_subscript_context
func setSubscriptContext(p *Parser, e *ast.Subscript, ctx ast.ExprContext) ast.Expr {
	_ = p
	return &ast.Subscript{Value: e.Value, Slice: e.Slice, Ctx: ctx, Pos: e.Pos}
}

// setAttributeContext returns a fresh Attribute carrying ctx; the
// value/attr pair is reused unchanged.
//
// CPython: Parser/action_helpers.c:293 _set_attribute_context
func setAttributeContext(p *Parser, e *ast.Attribute, ctx ast.ExprContext) ast.Expr {
	_ = p
	return &ast.Attribute{Value: e.Value, Attr: e.Attr, Ctx: ctx, Pos: e.Pos}
}

// setStarredContext returns a fresh Starred whose inner value has
// been rewritten through SetExprContext.
//
// CPython: Parser/action_helpers.c:300 _set_starred_context
func setStarredContext(p *Parser, e *ast.Starred, ctx ast.ExprContext) ast.Expr {
	return &ast.Starred{Value: SetExprContext(p, e.Value, ctx), Ctx: ctx, Pos: e.Pos}
}

// GetExprName returns the human-readable phrase used in
// "cannot assign to %s" diagnostics for an expression. The set of
// returned strings matches CPython's `_PyPegen_get_expr_name` switch
// arm-for-arm so messages from `invalid_*` rules read identically.
//
// CPython: Parser/action_helpers.c:1043 _PyPegen_get_expr_name
func GetExprName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Attribute:
		return "attribute"
	case *ast.Subscript:
		return "subscript"
	case *ast.Starred:
		return "starred"
	case *ast.Name:
		return "name"
	case *ast.List:
		return "list"
	case *ast.Tuple:
		return "tuple"
	case *ast.Lambda:
		return "lambda"
	case *ast.Call:
		return "function call"
	case *ast.BoolOp, *ast.BinOp, *ast.UnaryOp:
		return "expression"
	case *ast.GeneratorExp:
		return "generator expression"
	case *ast.Yield, *ast.YieldFrom:
		return "yield expression"
	case *ast.Await:
		return "await expression"
	case *ast.ListComp:
		return "list comprehension"
	case *ast.SetComp:
		return "set comprehension"
	case *ast.DictComp:
		return "dict comprehension"
	case *ast.Dict:
		return "dict literal"
	case *ast.Set:
		return "set display"
	case *ast.JoinedStr, *ast.FormattedValue:
		return "f-string expression"
	case *ast.TemplateStr, *ast.Interpolation:
		return "t-string expression"
	case *ast.Constant:
		switch x := v.Value.(type) {
		case nil:
			return "None"
		case bool:
			if x {
				return "True"
			}
			return "False"
		default:
			return "literal"
		}
	case *ast.Compare:
		return "comparison"
	case *ast.IfExp:
		return "conditional expression"
	case *ast.NamedExpr:
		return "named expression"
	}
	return "expression"
}
