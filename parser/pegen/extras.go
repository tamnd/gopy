// CPython: Parser/action_helpers.c. Helpers that build small
// pieces of the AST (key/value extraction, decorated function def,
// numeric-literal kind enforcement, string concatenation). Kept
// separate from actions.go so the shape ops there stay easy to
// scan.

package pegen

import (
	"strings"

	"github.com/tamnd/gopy/ast"
)

// GetKeys flattens the key side of a list of (key, value) pairs.
//
// CPython: Parser/action_helpers.c:354 _PyPegen_get_keys
func GetKeys(pairs []KeyValuePair) []ast.Expr {
	out := make([]ast.Expr, len(pairs))
	for i, p := range pairs {
		out[i] = p.Key
	}
	return out
}

// GetValues flattens the value side of a (key, value) pair list.
//
// CPython: Parser/action_helpers.c:370 _PyPegen_get_values
func GetValues(pairs []KeyValuePair) []ast.Expr {
	out := make([]ast.Expr, len(pairs))
	for i, p := range pairs {
		out[i] = p.Value
	}
	return out
}

// KeyValuePair is the parser-side struct that carries a (key, value)
// pair through dict literal and dict-comprehension rules.
//
// CPython: Parser/pegen.h:96 KeyValuePair
type KeyValuePair struct {
	Key, Value ast.Expr
}

// EnsureReal mirrors the runtime check that the imaginary side of
// a complex-literal sum (`1 + 2j`) is a real numeric Constant. It
// returns nil and sets the error indicator if the check fails.
//
// CPython: Parser/action_helpers.c:853 _PyPegen_ensure_real
func (p *Parser) EnsureReal(e ast.Expr) ast.Expr {
	c, ok := e.(*ast.Constant)
	if !ok || isComplex(c.Value) {
		p.errorIndicator = true
		return nil
	}
	return e
}

// EnsureImaginary is the matching check for the imaginary side.
//
// CPython: Parser/action_helpers.c:843 _PyPegen_ensure_imaginary
func (p *Parser) EnsureImaginary(e ast.Expr) ast.Expr {
	c, ok := e.(*ast.Constant)
	if !ok || !isComplex(c.Value) {
		p.errorIndicator = true
		return nil
	}
	return e
}

func isComplex(v any) bool {
	_, ok := v.(complex128)
	return ok
}

// AugOp wraps an Operator value so the rule action that builds an
// AugAssign can carry the operator separately from the target /
// value pair before assembling the statement.
//
// CPython: Parser/action_helpers.c:716 _PyPegen_augoperator
func AugOp(op ast.Operator) ast.Operator { return op }

// ConcatenateStrings joins adjacent Constant string nodes into
// one. The C source flattens at the AST level; the Go version
// returns a single Constant when every piece is a plain string,
// otherwise it leaves the slice for the JoinedStr builder to
// handle.
//
// CPython: Parser/action_helpers.c:1860 _PyPegen_concatenate_strings
func ConcatenateStrings(parts []ast.Expr) ast.Expr {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if joined, ok := tryJoinPlainConstants(parts); ok {
		return joined
	}
	return &ast.JoinedStr{Values: parts}
}

func tryJoinPlainConstants(parts []ast.Expr) (ast.Expr, bool) {
	var sb strings.Builder
	first := parts[0]
	pos := first.Position()
	for _, p := range parts {
		c, ok := p.(*ast.Constant)
		if !ok {
			return nil, false
		}
		s, ok := c.Value.(string)
		if !ok {
			return nil, false
		}
		sb.WriteString(s)
	}
	return &ast.Constant{Value: sb.String(), Pos: pos}, true
}

// FunctionDefDecorators returns a copy of fn with decorators stamped
// in. The C source rebuilds the FunctionDef / AsyncFunctionDef node
// to keep arena ownership clean; gopy mutates in place because the
// node already lives in the parser's value heap.
//
// CPython: Parser/action_helpers.c:727 _PyPegen_function_def_decorators
func FunctionDefDecorators(decorators []ast.Expr, fn ast.Stmt) ast.Stmt {
	switch v := fn.(type) {
	case *ast.FunctionDef:
		v.DecoratorList = decorators
		return v
	case *ast.AsyncFunctionDef:
		v.DecoratorList = decorators
		return v
	case *ast.ClassDef:
		v.DecoratorList = decorators
		return v
	}
	return fn
}
