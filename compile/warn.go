package compile

import (
	"fmt"
	"reflect"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/objects" //nolint:depguard // needed for Object type check in inferLiteralType
)

// WarnHook is the package-level drain for compile-time SyntaxWarning
// diagnostics. It is nil until module/_warnings sets it via init().
// The compiler calls warnAt, which forwards the warning through this
// hook to PyErr_WarnExplicit. If the hook returns an error (meaning
// the warning was elevated to SyntaxError by the filter), the caller
// propagates it as a compile error.
//
// CPython: Python/compile.c:237 _PyCompile_Warn
// (calls PyErr_WarnExplicitObject, returns ERROR on failure)
var WarnHook func(category, message, filename string, lineno int) error

// assertTupleWarning emits "assertion is always true, perhaps remove
// parentheses?" when the assert test is a non-empty tuple literal or a
// non-empty constant tuple. This fires before the optimize-out check so
// the warning is always seen.
//
// CPython: Python/codegen.c:2932 codegen_assert (tuple-check block)
func (c *Compiler) assertTupleWarning(s *ast.Assert) error {
	var isTuple bool
	switch t := s.Test.(type) {
	case *ast.Tuple:
		isTuple = len(t.Elts) > 0
	case *ast.Constant:
		// constant-folded tuple: t.Value is a Go []any or objects.Tuple
		if t.Value != nil {
			rv := reflect.ValueOf(t.Value)
			if rv.Kind() == reflect.Slice && rv.Len() > 0 {
				isTuple = true
			}
		}
	}
	if !isTuple {
		return nil
	}
	return c.warnAt(s.Test.Position(), "assertion is always true, perhaps remove parentheses?")
}

// checkCompare emits a SyntaxWarning when `is` or `is not` is used
// with a non-singleton literal operand (int, str, tuple, etc.). Named
// singletons (None, True, False, Ellipsis) are always safe.
//
// CPython: Python/codegen.c:1811 codegen_check_compare
func (c *Compiler) checkCompare(e *ast.Compare) error {
	left := isArg(e.Left)
	leftExpr := e.Left
	for i, op := range e.Ops {
		right := isArg(e.Comparators[i])
		if op == ast.Is || op == ast.IsNot {
			if !right || !left {
				var msg string
				if op == ast.Is {
					msg = "\"is\" with '%s' literal. Did you mean \"==\"?"
				} else {
					msg = "\"is not\" with '%s' literal. Did you mean \"!=\"?"
				}
				// The literal side is whichever operand returned false from isArg.
				var literal ast.Expr
				if !left {
					literal = leftExpr
				} else {
					literal = e.Comparators[i]
				}
				typeName := inferLiteralType(literal)
				return c.warnAt(loc(e), msg, typeName)
			}
		}
		left = right
		leftExpr = e.Comparators[i]
	}
	return nil
}

// isArg returns false when the expression is a constant that is NOT
// one of the named singletons (None, True, False, Ellipsis), i.e. it
// is a literal that should trigger the "is with literal" warning.
//
// CPython: Python/codegen.c:1790 check_is_arg
func isArg(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Tuple:
		for _, elt := range t.Elts {
			if _, ok := elt.(*ast.Constant); !ok {
				return true
			}
		}
		// all-constant tuple is a literal → not a safe is-arg
		return len(t.Elts) == 0
	case *ast.Constant:
		// Named singletons are safe; everything else warns.
		v := t.Value
		if v == nil {
			return true // None singleton
		}
		if _, ok := v.(bool); ok {
			return true // True / False
		}
		if _, ok := v.(ast.EllipsisType); ok {
			return true // Ellipsis
		}
		return false
	}
	return true
}

// inferLiteralType returns the type name for a literal expression,
// used in "is with literal" and "missed comma" warning messages.
// Returns "" for expressions with no statically-known type (variables,
// attribute loads, calls, etc.), mirroring CPython infer_type returning NULL.
//
// CPython: Python/codegen.c:3593 infer_type
func inferLiteralType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Tuple:
		return "tuple"
	case *ast.List, *ast.ListComp:
		return "list"
	case *ast.Dict, *ast.DictComp:
		return "dict"
	case *ast.Set, *ast.SetComp:
		return "set"
	case *ast.GeneratorExp:
		return "generator"
	case *ast.Lambda:
		return "function"
	case *ast.TemplateStr, *ast.Interpolation:
		return "string.templatelib.Template"
	case *ast.JoinedStr, *ast.FormattedValue:
		return "str"
	case *ast.Constant:
		if t.Value == nil {
			return "NoneType"
		}
		switch t.Value.(type) {
		case int64:
			return "int"
		case float64:
			return "float"
		case complex128:
			return "complex"
		case string:
			return "str"
		case []byte:
			return "bytes"
		case bool:
			return "bool"
		case ast.EllipsisType:
			return "ellipsis"
		}
		if obj, ok := t.Value.(objects.Object); ok {
			return obj.Type().Name
		}
		return fmt.Sprintf("%T", t.Value)
	}
	return ""
}

// checkCaller emits a SyntaxWarning when the function position of a
// call is a non-callable literal (tuple, list, dict, set, etc.),
// typically indicating a missed comma.
//
// CPython: Python/codegen.c:3625 check_caller
func (c *Compiler) checkCaller(e ast.Expr) error {
	switch e.(type) {
	case *ast.Constant, *ast.Tuple, *ast.List, *ast.ListComp,
		*ast.Dict, *ast.DictComp, *ast.Set, *ast.SetComp,
		*ast.GeneratorExp, *ast.TemplateStr, *ast.Interpolation,
		*ast.JoinedStr, *ast.FormattedValue:
		typeName := inferLiteralType(e)
		return c.warnAt(loc(e), "'%s' object is not callable; perhaps you missed a comma?", typeName)
	}
	return nil
}

// checkSubscripter emits a SyntaxWarning when the object of a
// subscript operation is a type that is not subscriptable
// (set, generator, lambda, template, or non-str/non-bytes constant),
// typically indicating a missed comma.
//
// CPython: Python/codegen.c:3652 check_subscripter
func (c *Compiler) checkSubscripter(e ast.Expr) error {
	switch t := e.(type) {
	case *ast.Constant:
		v := t.Value
		// None, Ellipsis, int (including bool), float, complex are not subscriptable.
		// CPython: PyLong_Check covers bool since bool is int subclass.
		if v == nil {
			break
		}
		switch v.(type) {
		case ast.EllipsisType, int64, float64, complex128, bool:
			break
		default:
			// str, bytes, tuple constants are subscriptable — no warning.
			return nil
		}
	case *ast.Set, *ast.SetComp,
		*ast.GeneratorExp,
		*ast.TemplateStr, *ast.Interpolation,
		*ast.Lambda:
		// intentionally falls through to warn
	default:
		return nil
	}
	typeName := inferLiteralType(e)
	return c.warnAt(loc(e), "'%s' object is not subscriptable; perhaps you missed a comma?", typeName)
}

// checkIndex emits a SyntaxWarning when the index of a subscript is
// not an integer or slice type but the object is a sequence that only
// accepts integer indices (list, tuple, str constant, etc.).
//
// CPython: Python/codegen.c:3683 check_index
func (c *Compiler) checkIndex(obj ast.Expr, idx ast.Expr) error {
	idxType := inferLiteralType(idx)
	// Unknown type (variables, calls, etc.) and int/slice indices are safe.
	if idxType == "" || idxType == "int" || idxType == "bool" {
		return nil
	}
	// Slice AST nodes have no infer_type entry → NULL → safe.
	if _, ok := idx.(*ast.Slice); ok {
		return nil
	}
	// Warn only for objects that require integer indices.
	var objType string
	switch t := obj.(type) {
	case *ast.Tuple, *ast.List, *ast.ListComp,
		*ast.JoinedStr, *ast.FormattedValue:
		objType = inferLiteralType(obj)
	case *ast.Constant:
		if t.Value == nil {
			return nil
		}
		switch t.Value.(type) {
		case string, []byte:
			objType = inferLiteralType(obj)
		default:
			// int, float, etc. constants are handled by checkSubscripter, not here.
			return nil
		}
	default:
		return nil
	}
	return c.warnAt(loc(obj), "%s indices must be integers or slices, not %s; perhaps you missed a comma?", objType, idxType)
}

// warnAt emits a SyntaxWarning at pos via WarnHook. When WarnHook is
// nil (e.g. in unit tests without _warnings loaded) the warning is
// silently dropped. When the warnings filter elevates the warning to
// an error, warnAt converts it to a *SyntaxError so the VM's prefix
// table maps it to PyExc_SyntaxError.
//
// CPython: Python/compile.c:237 _PyCompile_Warn
func (c *Compiler) warnAt(pos ast.Pos, format string, args ...any) error {
	// Suppressed while compiling the exception-path copy of a finally
	// body, so a warning there is not emitted a second time.
	// CPython: Python/compile.c:1213 _PyCompile_Warn (c_disable_warning)
	if c.disableWarning > 0 {
		return nil
	}
	if WarnHook == nil {
		return nil
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	lineno := pos.Lineno
	if lineno <= 0 {
		lineno = 1
	}
	if err := WarnHook("SyntaxWarning", msg, c.Filename, lineno); err != nil {
		// The filter elevated the warning to an error.
		// Use the clamped lineno so SyntaxError.lineno is never None.
		// CPython: Python/compile.c:251 _PyCompile_Warn (SyntaxWarning → SyntaxError)
		adjustedPos := pos
		adjustedPos.Lineno = lineno
		return &SyntaxError{Msg: msg, Filename: c.Filename, Pos: adjustedPos}
	}
	return nil
}
