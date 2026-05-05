// CPython: Parser/string_parser.c PEP 750 t-string section.
// Same brace-balanced scan as f-strings; emits the identical
// Segment shape so the parser pegen layer can reuse the assembly
// path with a TemplateStr/Interpolation builder instead of
// JoinedStr/FormattedValue.

package string

// ScanTString is the t-string brace scanner. The lexer rules and
// segment shape are identical to f-strings; the difference is in
// the AST node the parser builds afterward.
//
// CPython: Parser/string_parser.c (PEP 750 t-string handling)
func ScanTString(body string) ([]Segment, error) {
	return ScanFString(body)
}
