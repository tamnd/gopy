// CPython: Parser/action_helpers.c:1167 _PyPegen_get_invalid_target
// plus the surrounding error helpers (_PyPegen_arguments_parsing_error,
// _PyPegen_nonparen_genexp_in_call). These are the second-pass
// helpers the generated parser reaches for once the first-pass
// match fails: they walk the partial AST to find the offending
// child and produce a precise diagnostic.
//
// The full second-pass invalid_rules grammar is generated alongside
// the parser table; this file holds the helpers that the generated
// invalid_rules.go calls into.

package errors

import "github.com/tamnd/gopy/ast"

// TargetKind tags the assignment-target context so the diagnostic
// helper knows which sub-expressions are legal.
//
// CPython: Parser/pegen.h TARGETS_TYPE
type TargetKind int

// Target kinds. Star targets are LHS of `=`; del targets are after
// `del`; for targets sit between `for ... in`.
const (
	StarTargets TargetKind = iota
	DelTargets
	ForTargets
)

// FindInvalidTarget walks an expression that was used as an
// assignment target and returns the first sub-expression that is
// not a legal target. Returns nil when the whole tree is valid.
//
// CPython: Parser/action_helpers.c:1167 _PyPegen_get_invalid_target
func FindInvalidTarget(e ast.Expr, kind TargetKind) ast.Expr {
	if e == nil {
		return nil
	}
	switch v := e.(type) {
	case *ast.List:
		for _, child := range v.Elts {
			if bad := FindInvalidTarget(child, kind); bad != nil {
				return bad
			}
		}
		return nil
	case *ast.Tuple:
		for _, child := range v.Elts {
			if bad := FindInvalidTarget(child, kind); bad != nil {
				return bad
			}
		}
		return nil
	case *ast.Starred:
		if kind == DelTargets {
			return e
		}
		return FindInvalidTarget(v.Value, kind)
	case *ast.Compare:
		if kind == ForTargets && len(v.Ops) > 0 && v.Ops[0] == ast.In {
			return FindInvalidTarget(v.Left, kind)
		}
		return e
	case *ast.Name, *ast.Subscript, *ast.Attribute:
		return nil
	default:
		return e
	}
}

// ArgumentsParsingMsg returns the message the second-pass invalid
// rule emits when arguments are mis-ordered. The choice between
// the two phrasings is driven by whether any kwarg in the call is
// `**kwargs` (kwarg unpacking) versus a plain `name=value`.
//
// CPython: Parser/action_helpers.c:1224 _PyPegen_arguments_parsing_error
func ArgumentsParsingMsg(call *ast.Call) string {
	for _, kw := range call.Keywords {
		if kw.Arg == nil {
			return "positional argument follows keyword argument unpacking"
		}
	}
	return "positional argument follows keyword argument"
}

// NonparenGenexpInCallMsg is the diagnostic the parser raises when
// a generator expression is used as the only positional argument in
// a call without surrounding parentheses, e.g. `f(x for x in y, 1)`.
//
// CPython: Parser/action_helpers.c:1243 _PyPegen_nonparen_genexp_in_call
const NonparenGenexpInCallMsg = "Generator expression must be parenthesized"
