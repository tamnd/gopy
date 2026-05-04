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

// SetExprContext walks a Name / Tuple / List / Starred / Attribute /
// Subscript tree and stamps each node's expr_context to ctx. Used
// for assignment targets (Store) and del statements (Del).
//
// CPython: Parser/action_helpers.c:323 _PyPegen_set_expr_context
func SetExprContext(e ast.Expr, ctx ast.ExprContext) ast.Expr {
	switch v := e.(type) {
	case *ast.Name:
		v.Ctx = ctx
	case *ast.Attribute:
		v.Ctx = ctx
	case *ast.Subscript:
		v.Ctx = ctx
	case *ast.Starred:
		v.Ctx = ctx
		v.Value = SetExprContext(v.Value, ctx)
	case *ast.Tuple:
		v.Ctx = ctx
		for i, el := range v.Elts {
			v.Elts[i] = SetExprContext(el, ctx)
		}
	case *ast.List:
		v.Ctx = ctx
		for i, el := range v.Elts {
			v.Elts[i] = SetExprContext(el, ctx)
		}
	}
	return e
}

// GetExprName returns the human-readable phrase used in
// "cannot assign to %s" diagnostics for an expression.
//
// CPython: Parser/action_helpers.c:1259 _PyPegen_get_expr_name
func GetExprName(e ast.Expr) string {
	switch v := e.(type) {
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
	case *ast.Name:
		return "Name"
	case *ast.Attribute:
		return "attribute"
	case *ast.Subscript:
		return "subscript"
	case *ast.Starred:
		return "starred"
	case *ast.List:
		return "list"
	case *ast.Tuple:
		return "tuple"
	case *ast.Call:
		return "function call"
	case *ast.JoinedStr, *ast.FormattedValue:
		return "f-string expression"
	case *ast.TemplateStr, *ast.Interpolation:
		return "t-string expression"
	}
	return "expression"
}
