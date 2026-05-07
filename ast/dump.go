// CPython parity for ast.dump. The string we emit must match
// `ast.dump(node, annotate_fields=True, include_attributes=False,
// indent=None, show_empty=False)` byte for byte. Differential tests
// in parser/parity_test.go run python3 against the same source and
// fail when the strings diverge, so this is the parser's correctness
// gate at the AST level.
//
// CPython: Lib/ast.py:109 ast.dump

package ast

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Dump returns a textual representation of node matching CPython's
// ast.dump(node) with the documented defaults.
func Dump(node any) string {
	s, _ := dumpFormat(node)
	return s
}

// dumpFormat mirrors Lib/ast.py:127 _format(node, level=0) under
// indent=None (so prefix and sep are empty / ", "), annotate_fields=True,
// show_empty=False, include_attributes=False.
func dumpFormat(v any) (string, bool) {
	if v == nil {
		return "None", true
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return "None", true
		}
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Slice {
		// Bytes literals (`b'...'`) land here as []byte. CPython
		// renders them via repr(bytes), not as a list of ints, so
		// route them through dumpRepr before the generic slice path.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return reprBytes(rv.Bytes()), true
		}
		if rv.Len() == 0 {
			return "[]", true
		}
		parts := make([]string, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			s, _ := dumpFormat(rv.Index(i).Interface())
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", false
	}

	if rv.Kind() == reflect.Struct {
		return dumpStruct(rv)
	}

	return dumpRepr(rv.Interface()), true
}

// dumpStruct walks an AST node's declared fields (skipping the trailing
// `Pos` source-location field) in declared order and emits the
// `ClassName(field=value, ...)` form CPython prints.
func dumpStruct(rv reflect.Value) (string, bool) {
	name := dumpClassName(rv.Type())
	var args []string
	allsimple := true
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		if ft.Name == "Pos" {
			continue
		}
		fv := rv.Field(i)

		asdlName := asdlFieldName(t.Name(), ft.Name)

		if isOptional(t.Name(), ft.Name) && isNilLike(fv) {
			continue
		}
		if fv.Kind() == reflect.Slice && fv.Len() == 0 {
			continue
		}

		s, simple := dumpFormat(fv.Interface())
		allsimple = allsimple && simple
		args = append(args, asdlName+"="+s)
	}
	if allsimple && len(args) <= 3 {
		return name + "(" + strings.Join(args, ", ") + ")", len(args) == 0
	}
	return name + "(" + strings.Join(args, ", ") + ")", false
}

// isNilLike checks whether a reflect.Value is a None-equivalent: nil
// pointer, nil interface, nil map, or zero pointer-shaped value. This
// gates the "skip optional field" logic from CPython
// (`value is None and getattr(cls, name, ...) is None`).
func isNilLike(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// dumpClassName returns the asdl class name for a Go AST type. Most
// types map by identity; the product types (arguments, arg, keyword,
// alias, withitem, match_case, comprehension) are lowercased.
func dumpClassName(t reflect.Type) string {
	switch t.Name() {
	case "Arguments":
		return "arguments"
	case "Arg":
		return "arg"
	case "Keyword":
		return "keyword"
	case "Alias":
		return "alias"
	case "Withitem":
		return "withitem"
	case "MatchCase":
		return "match_case"
	case "Comprehension":
		return "comprehension"
	case "TypeIgnoreNode":
		return "TypeIgnore"
	case "ExprStmt":
		// gopy renames Expr (the stmt wrapper) to ExprStmt to avoid
		// clashing with the expr interface; CPython calls it Expr.
		return "Expr"
	}
	return t.Name()
}

// asdlFieldName converts a Go struct field name to its asdl name. The
// rule is "lowercase first letter, insert underscore before any
// subsequent uppercase". A small list of fields where CPython uses no
// underscore (posonlyargs, kwonlyargs) is special-cased.
func asdlFieldName(structName, fieldName string) string {
	if special, ok := asdlFieldNameOverrides[structKey{structName, fieldName}]; ok {
		return special
	}
	if special, ok := asdlFieldNameByFieldOnly[fieldName]; ok {
		return special
	}
	var b strings.Builder
	for i, r := range fieldName {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

type structKey struct {
	struct_, field string
}

var asdlFieldNameOverrides = map[structKey]string{
	{"ClassDef", "Bases"}: "bases",
	{"Withitem", "ContextExpr"}:  "context_expr",
	{"Withitem", "OptionalVars"}: "optional_vars",
}

var asdlFieldNameByFieldOnly = map[string]string{
	"Posonlyargs": "posonlyargs",
	"Kwonlyargs":  "kwonlyargs",
}

// isOptional reports whether asdl marks a field as `?` (Optional).
// CPython's dump skips Optional fields whose value is None; required
// fields render as "field=None" instead. The table mirrors
// Parser/Python.asdl line by line.
func isOptional(structName, fieldName string) bool {
	if optionalFields[structKey{structName, fieldName}] {
		return true
	}
	return false
}

// optionalFields enumerates every asdl `?` field. Generated from
// Parser/Python.asdl by hand; the asdl is small enough that
// hand-curation beats parsing it at build time.
var optionalFields = map[structKey]bool{
	{"FunctionDef", "Returns"}:          true,
	{"FunctionDef", "TypeComment"}:      true,
	{"AsyncFunctionDef", "Returns"}:     true,
	{"AsyncFunctionDef", "TypeComment"}: true,
	{"Return", "Value"}:                 true,
	{"Assign", "TypeComment"}:           true,
	{"AnnAssign", "Value"}:              true,
	{"For", "TypeComment"}:              true,
	{"AsyncFor", "TypeComment"}:         true,
	{"With", "TypeComment"}:             true,
	{"AsyncWith", "TypeComment"}:        true,
	{"Raise", "Exc"}:                    true,
	{"Raise", "Cause"}:                  true,
	{"Assert", "Msg"}:                   true,
	{"ImportFrom", "Module"}:            true,
	{"ImportFrom", "Level"}:             true,
	{"Yield", "Value"}:                  true,
	{"YieldFrom", "Value"}:              true,
	{"FormattedValue", "FormatSpec"}:    true,
	{"Interpolation", "FormatSpec"}:     true,
	{"Constant", "Kind"}:                true,
	{"Slice", "Lower"}:                  true,
	{"Slice", "Upper"}:                  true,
	{"Slice", "Step"}:                   true,
	{"ExceptHandler", "Type"}:           true,
	{"ExceptHandler", "Name"}:           true,
	{"Arguments", "Vararg"}:             true,
	{"Arguments", "Kwarg"}:              true,
	{"Arg", "Annotation"}:               true,
	{"Arg", "TypeComment"}:              true,
	{"Keyword", "Arg"}:                  true,
	{"Alias", "Asname"}:                 true,
	{"Withitem", "OptionalVars"}:        true,
	{"MatchCase", "Guard"}:              true,
	{"MatchMapping", "Rest"}:            true,
	{"MatchStar", "Name"}:               true,
	{"MatchAs", "Pattern"}:              true,
	{"MatchAs", "Name"}:                 true,
	{"TypeVar", "Bound"}:                true,
	{"TypeVar", "DefaultValue"}:         true,
	{"ParamSpec", "DefaultValue"}:       true,
	{"TypeVarTuple", "DefaultValue"}:    true,
}

// dumpRepr is Python repr() for the leaf values that show up in
// AST fields and Constant.Value: identifier strings, literal strings,
// bytes, ints (including arbitrary-precision), floats, complex, bool,
// None, plus the empty-singleton enums (Load, Store, Add, Mod, Eq, ...).
func dumpRepr(v any) string {
	if v == nil {
		return "None"
	}
	switch x := v.(type) {
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int8, int16, int32, int64:
		return fmt.Sprintf("%d", x)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32:
		return reprFloat(float64(x))
	case float64:
		return reprFloat(x)
	case complex64:
		return reprComplex(complex128(x))
	case complex128:
		return reprComplex(x)
	case string:
		return reprString(x)
	case []byte:
		return reprBytes(x)
	case *string:
		if x == nil {
			return "None"
		}
		return reprString(*x)
	case *int:
		if x == nil {
			return "None"
		}
		return strconv.Itoa(*x)
	case *big.Int:
		return x.String()
	case ExprContext:
		return x.String() + "()"
	case Boolop:
		return x.String() + "()"
	case Operator:
		return operatorName(x) + "()"
	case Unaryop:
		return x.String() + "()"
	case Cmpop:
		return cmpopName(x) + "()"
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return "None"
	}
	return fmt.Sprintf("%v", v)
}

// operatorName returns the asdl name for an Operator value. gopy's
// Operator.String() returns "ModOperator" for the `%` operator to
// avoid clashing with ast.Mod (a different type); CPython calls it
// "Mod", so we override here.
func operatorName(op Operator) string {
	if op == ModOperator {
		return "Mod"
	}
	return op.String()
}

// cmpopName mirrors operatorName for Cmpop. gopy uses the asdl names
// directly so the default String() is fine; this is a hook in case
// later renames force divergence.
func cmpopName(c Cmpop) string {
	return c.String()
}

// reprFloat ports Python's repr(float). Python uses the shortest
// round-tripping representation and always emits a decimal point or
// 'e' so the value lexes as a float.
func reprFloat(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// reprComplex ports Python's repr(complex). Real component is
// suppressed when zero; sign of the imaginary component appears as
// '+' or '-'.
func reprComplex(c complex128) string {
	r := real(c)
	im := imag(c)
	if r == 0 && !math.Signbit(r) {
		return reprFloat(im) + "j"
	}
	sign := "+"
	if im < 0 || math.Signbit(im) {
		sign = "-"
		im = math.Abs(im)
	}
	return "(" + reprFloat(r) + sign + reprFloat(im) + "j)"
}

// reprString ports Python's repr(str). Picks the quote style that
// minimises escapes (single by default, double if the body contains
// a single quote and no double), then escapes \\ and the chosen quote
// plus the standard non-printable escapes.
func reprString(s string) string {
	hasSingle := strings.ContainsRune(s, '\'')
	hasDouble := strings.ContainsRune(s, '"')
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case rune(quote):
			b.WriteByte('\\')
			b.WriteByte(quote)
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\x%02x", r)
			} else if r < 0x80 {
				b.WriteRune(r)
			} else if unicode.IsPrint(r) {
				b.WriteRune(r)
			} else {
				if r < 0x10000 {
					fmt.Fprintf(&b, "\\u%04x", r)
				} else {
					fmt.Fprintf(&b, "\\U%08x", r)
				}
			}
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// reprBytes ports Python's repr(bytes). Same quote-pick logic as
// reprString but each byte is treated as a Latin-1 code point.
func reprBytes(bs []byte) string {
	hasSingle := false
	hasDouble := false
	for _, b := range bs {
		if b == '\'' {
			hasSingle = true
		} else if b == '"' {
			hasDouble = true
		}
	}
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte('b')
	b.WriteByte(quote)
	for _, c := range bs {
		switch {
		case c == '\\':
			b.WriteString("\\\\")
		case c == quote:
			b.WriteByte('\\')
			b.WriteByte(quote)
		case c == '\n':
			b.WriteString("\\n")
		case c == '\r':
			b.WriteString("\\r")
		case c == '\t':
			b.WriteString("\\t")
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, "\\x%02x", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// utf8Sentinel keeps the `unicode/utf8` import live for any future
// surrogate handling we add to reprString.
var _ = utf8.RuneSelf
