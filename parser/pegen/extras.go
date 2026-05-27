// CPython: Parser/action_helpers.c. Helpers that build small
// pieces of the AST (key/value extraction, decorated function def,
// numeric-literal kind enforcement, string concatenation). Kept
// separate from actions.go so the shape ops there stay easy to
// scan.

package pegen

import (
	"strings"

	"github.com/tamnd/gopy/ast"
	perrors "github.com/tamnd/gopy/parser/errors"
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

// ConcatenateStrings folds a sequence of adjacent string / fstring
// pieces into a single AST node. The shape mirrors CPython exactly:
//
//   - all-plain unicode pieces collapse to one Constant string;
//   - all-bytes pieces collapse to one Constant []byte;
//   - mixing bytes with anything non-bytes pins the parser's error
//     indicator (the caller surfaces "cannot mix bytes and nonbytes
//     literals");
//   - any f-string piece flips the result into a JoinedStr whose
//     values are the flattened-and-folded element list.
//
// EXTRA position arguments are dropped at the action-translator layer,
// so the assembled nodes use ast.NoPos.
//
// CPython: Parser/action_helpers.c:1860 _PyPegen_concatenate_strings
func ConcatenateStrings(p *Parser, parts []ast.Expr) ast.Expr {
	n := len(parts)
	if n == 0 {
		return nil
	}
	fStringFound := false
	unicodeFound := false
	bytesFound := false
	for _, elem := range parts {
		switch e := elem.(type) {
		case *ast.Constant:
			if _, isBytes := e.Value.([]byte); isBytes {
				bytesFound = true
			} else {
				unicodeFound = true
			}
		case *ast.JoinedStr:
			fStringFound = true
		case *ast.TemplateStr:
			// python.gram routes TemplateStr through
			// _PyPegen_concatenate_tstrings; reaching this branch
			// means a grammar bug, not user input.
			return nil
		default:
			fStringFound = true
		}
	}
	if (unicodeFound || fStringFound) && bytesFound {
		// CPython: Parser/action_helpers.c:1897
		// _PyPegen_concatenate_strings raises the SyntaxError directly
		// via RAISE_SYNTAX_ERROR before bailing, so the parser surface
		// gets the specific message rather than the generic "invalid
		// syntax" the empty errorIndicator path produces.
		p.RaiseSyntaxError("%s", perrors.MsgMixedBytesLiterals)
		return nil
	}
	if !fStringFound {
		if n == 1 {
			return parts[0]
		}
		if bytesFound {
			return buildConcatenatedBytes(parts)
		}
		return buildConcatenatedUnicode(parts)
	}
	return buildConcatenatedJoinedStr(parts)
}

// buildConcatenatedBytes folds an all-bytes part list into a single
// Constant whose Value is the concatenated []byte. The kind of the
// first piece propagates, mirroring the unicode path even though
// bytes literals never carry a u/b kind annotation in practice.
//
// CPython: Parser/action_helpers.c:1610 _build_concatenated_bytes
func buildConcatenatedBytes(parts []ast.Expr) ast.Expr {
	var kind *string
	if c, ok := parts[0].(*ast.Constant); ok {
		kind = c.Kind
	}
	var out []byte
	for _, elem := range parts {
		c, ok := elem.(*ast.Constant)
		if !ok {
			return nil
		}
		b, _ := c.Value.([]byte)
		out = append(out, b...)
	}
	return &ast.Constant{Value: out, Kind: kind, Pos: ast.NoPos}
}

// buildConcatenatedUnicode folds adjacent plain-string Constants. The
// kind of the first piece wins ("u'a' 'b'" -> u"ab", "'a' u'b'" ->
// "ab").
//
// CPython: Parser/action_helpers.c:1636 _build_concatenated_unicode
func buildConcatenatedUnicode(parts []ast.Expr) ast.Expr {
	first, ok := parts[0].(*ast.Constant)
	if !ok {
		return nil
	}
	kind := first.Kind
	var sb strings.Builder
	for _, elem := range parts {
		c, ok := elem.(*ast.Constant)
		if !ok {
			return nil
		}
		s, _ := c.Value.(string)
		sb.WriteString(s)
	}
	return &ast.Constant{Value: sb.String(), Kind: kind, Pos: ast.NoPos}
}

// buildConcatenatedStr flattens nested JoinedStr / TemplateStr
// children into a single value list, then folds runs of adjacent
// Constant pieces and drops empty Constants. The fold preserves the
// kind of the run's first piece, matching unicode-concat semantics.
//
// CPython: Parser/action_helpers.c:1682 _build_concatenated_str
func buildConcatenatedStr(parts []ast.Expr) []ast.Expr {
	var flattened []ast.Expr
	for _, elem := range parts {
		switch e := elem.(type) {
		case *ast.JoinedStr:
			flattened = append(flattened, e.Values...)
		case *ast.TemplateStr:
			flattened = append(flattened, e.Values...)
		default:
			flattened = append(flattened, elem)
		}
	}
	var values []ast.Expr
	for i := 0; i < len(flattened); i++ {
		elem := flattened[i]
		if c, ok := elem.(*ast.Constant); ok {
			if i+1 < len(flattened) {
				if _, isConst := flattened[i+1].(*ast.Constant); isConst {
					firstElem := c
					kind := firstElem.Kind
					startPos := firstElem.Pos
					var sb strings.Builder
					var lastPos ast.Pos
					j := i
					for ; j < len(flattened); j++ {
						cc, ok := flattened[j].(*ast.Constant)
						if !ok {
							break
						}
						if s, ok := cc.Value.(string); ok {
							sb.WriteString(s)
						}
						lastPos = cc.Pos
					}
					i = j - 1
					// Span: start of first piece, end of last piece.
					// CPython: Parser/action_helpers.c:1715 _build_concatenated_str
					foldedPos := ast.Pos{
						Lineno:       startPos.Lineno,
						ColOffset:    startPos.ColOffset,
						EndLineno:    lastPos.EndLineno,
						EndColOffset: lastPos.EndColOffset,
					}
					if startPos.Lineno <= 0 {
						foldedPos = ast.NoPos
					}
					folded := &ast.Constant{Value: sb.String(), Kind: kind, Pos: foldedPos}
					elem = folded
					c = folded
				}
			}
			if s, ok := c.Value.(string); ok && s == "" {
				continue
			}
		}
		values = append(values, elem)
	}
	return values
}

// buildConcatenatedJoinedStr wraps the folded value list in a
// JoinedStr.
//
// CPython: Parser/action_helpers.c:1838 _build_concatenated_joined_str
func buildConcatenatedJoinedStr(parts []ast.Expr) ast.Expr {
	return &ast.JoinedStr{Values: buildConcatenatedStr(parts), Pos: ast.NoPos}
}

// FunctionDefDecorators returns a fresh FunctionDef / AsyncFunctionDef
// equivalent to fn but with decorators stamped in. CPython rebuilds
// the node so the original (decorator-free) value the function_def_raw
// rule produced stays untouched in the arena.
//
// CPython: Parser/action_helpers.c:727 _PyPegen_function_def_decorators
func FunctionDefDecorators(p *Parser, decorators []ast.Expr, fn ast.Stmt) ast.Stmt {
	_ = p
	switch v := fn.(type) {
	case *ast.AsyncFunctionDef:
		return &ast.AsyncFunctionDef{
			Name:          v.Name,
			Args:          v.Args,
			Body:          v.Body,
			DecoratorList: decorators,
			Returns:       v.Returns,
			TypeComment:   v.TypeComment,
			TypeParams:    v.TypeParams,
			Pos:           v.Pos,
		}
	case *ast.FunctionDef:
		return &ast.FunctionDef{
			Name:          v.Name,
			Args:          v.Args,
			Body:          v.Body,
			DecoratorList: decorators,
			Returns:       v.Returns,
			TypeComment:   v.TypeComment,
			TypeParams:    v.TypeParams,
			Pos:           v.Pos,
		}
	}
	return fn
}

// ClassDefDecorators returns a fresh ClassDef equivalent to classDef
// but with decorators stamped in.
//
// CPython: Parser/action_helpers.c:756 _PyPegen_class_def_decorators
func ClassDefDecorators(p *Parser, decorators []ast.Expr, classDef ast.Stmt) ast.Stmt {
	_ = p
	v, ok := classDef.(*ast.ClassDef)
	if !ok {
		return classDef
	}
	return &ast.ClassDef{
		Name:          v.Name,
		Bases:         v.Bases,
		Keywords:      v.Keywords,
		Body:          v.Body,
		DecoratorList: decorators,
		TypeParams:    v.TypeParams,
		Pos:           v.Pos,
	}
}
