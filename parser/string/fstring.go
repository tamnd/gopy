// CPython: Parser/string_parser.c f-string section.
// Brace-balanced scanner that splits a literal body into runs of
// plain text and {expression} segments. Doubled `{{` and `}}` are
// emitted as literal single braces.

package string

import "fmt"

// SegKind tags a Segment.
type SegKind int

// Segment kinds.
const (
	SegLiteral SegKind = iota
	SegExpr
)

// Segment is one piece of an f-string body. Literal segments carry
// the decoded plain text; expression segments carry the raw text
// inside the braces (without the braces themselves) plus the
// optional conversion character and format-spec body.
type Segment struct {
	Kind       SegKind
	Literal    string
	ExprText   string
	Conversion byte // 0 when absent
	FormatSpec string
	IsDebug    bool
}

// ScanFString walks body and emits Segments. body is the post-prefix
// inner text, e.g. for `f'a{x!r:0.2f}'` the input is `a{x!r:0.2f}`.
//
// CPython: Parser/string_parser.c:455 fstring_find_literal_and_field
func ScanFString(body string) ([]Segment, error) {
	var out []Segment
	var lit []byte
	i := 0
	for i < len(body) {
		c := body[i]
		switch c {
		case '{':
			if i+1 < len(body) && body[i+1] == '{' {
				lit = append(lit, '{')
				i += 2
				continue
			}
			if len(lit) > 0 {
				out = append(out, Segment{Kind: SegLiteral, Literal: string(lit)})
				lit = lit[:0]
			}
			seg, n, err := scanExprSegment(body[i:])
			if err != nil {
				return nil, err
			}
			out = append(out, seg)
			i += n
		case '}':
			if i+1 < len(body) && body[i+1] == '}' {
				lit = append(lit, '}')
				i += 2
				continue
			}
			return nil, fmt.Errorf("f-string: single '}' is not allowed")
		default:
			lit = append(lit, c)
			i++
		}
	}
	if len(lit) > 0 {
		out = append(out, Segment{Kind: SegLiteral, Literal: string(lit)})
	}
	return out, nil
}

// scanExprSegment consumes from the opening `{` and returns the
// Segment plus the number of bytes consumed including the closing
// `}`. The expression body is brace-balanced; nested `{...}` for
// format specs is handled inline.
func scanExprSegment(s string) (Segment, int, error) {
	if s[0] != '{' {
		return Segment{}, 0, fmt.Errorf("internal: scanExprSegment not at brace")
	}
	depth := 1
	i := 1
	exprEnd := -1
	conv := byte(0)
	debug := false
	formatStart := -1
	for i < len(s) {
		c := s[i]
		switch c {
		case '{':
			depth++
			i++
		case '}':
			depth--
			if depth == 0 {
				if exprEnd < 0 {
					exprEnd = i
				}
				expr := s[1:exprEnd]
				format := ""
				if formatStart >= 0 {
					format = s[formatStart+1 : i]
				}
				return Segment{
					Kind:       SegExpr,
					ExprText:   expr,
					Conversion: conv,
					FormatSpec: format,
					IsDebug:    debug,
				}, i + 1, nil
			}
			i++
		case '!':
			if depth == 1 && exprEnd < 0 && i+1 < len(s) && isConv(s[i+1]) &&
				(i+2 >= len(s) || s[i+2] == '}' || s[i+2] == ':') {
				exprEnd = i
				conv = s[i+1]
				i += 2
				continue
			}
			i++
		case ':':
			if depth == 1 && formatStart < 0 {
				if exprEnd < 0 {
					exprEnd = i
				}
				formatStart = i
				i++
				continue
			}
			i++
		case '=':
			if depth == 1 && exprEnd < 0 && (i+1 >= len(s) || s[i+1] == '}' || s[i+1] == '!' || s[i+1] == ':') {
				debug = true
				exprEnd = i
				i++
				continue
			}
			i++
		default:
			i++
		}
	}
	return Segment{}, 0, fmt.Errorf("f-string: expecting '}'")
}

func isConv(c byte) bool { return c == 'r' || c == 's' || c == 'a' }
