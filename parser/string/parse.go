// Package string ports cpython/Parser/string_parser.c. The parser
// hands a string token (with surrounding quotes and any b/r/u
// prefix still attached) to ParseString, which returns the decoded
// payload plus the prefix flags the AST builder needs.
//
// Escape decoding mirrors pycore_unicodeobject.c
// _PyUnicode_DecodeUnicodeEscapeInternal and pycore_bytesobject.c
// _PyBytes_DecodeEscape. The full table is implemented here rather
// than re-exported from the runtime so the parser stays
// self-contained.
//
// CPython: Parser/string_parser.c
package string

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Result is the decoded payload of a single string token. Bytes is
// set when IsBytes is true; otherwise Text holds the decoded
// unicode body.
//
// CPython: Parser/string_parser.h:11 result type (folded into one
// shape on the Go side)
type Result struct {
	Text    string
	Bytes   []byte
	IsBytes bool
	IsRaw   bool
	// IsFString is set on the per-segment result the f-string
	// scanner emits, and on the folded result when at least one
	// part was an f-string. IsTString is the matching flag for
	// t-strings. Concat uses these to enforce the
	// no-implicit-mixing rule in CPython 3.14.
	IsFString bool
	IsTString bool
	// Warnings carries SyntaxWarning text (one per unknown escape)
	// that the caller should surface separately. Empty when the
	// literal contained no flagged escapes.
	//
	// CPython: Objects/unicodeobject.c emits PyExc_SyntaxWarning
	// from _PyUnicode_DecodeUnicodeEscapeInternal.
	Warnings []string
}

// errBadInternalCall mirrors PyErr_BadInternalCall: the caller fed
// us a token whose shape does not match what the lexer is supposed
// to emit. We promote it to a normal error here.
//
// CPython: Parser/string_parser.c PyErr_BadInternalCall callers
var errBadInternalCall = errors.New("string parser: bad internal call")

// ParseString decodes a single string-literal token. tok is the
// raw bytes the lexer emitted, including prefix and quotes.
//
// CPython: Parser/string_parser.c:253 _PyPegen_parse_string
func ParseString(tok []byte) (Result, error) {
	s := tok
	var bytesMode, rawMode bool
	for len(s) > 0 {
		switch s[0] {
		case 'b', 'B':
			bytesMode = true
		case 'u', 'U':
			// no-op flag; carried for compatibility
		case 'r', 'R':
			rawMode = true
		default:
			goto stripped
		}
		s = s[1:]
	}
stripped:
	if len(s) < 2 {
		return Result{}, errBadInternalCall
	}
	quote := s[0]
	if quote != '\'' && quote != '"' {
		return Result{}, errBadInternalCall
	}
	if s[len(s)-1] != quote {
		return Result{}, errBadInternalCall
	}
	s = s[1 : len(s)-1]
	if len(s) >= 2 && len(tok) >= 6 && tok[len(tok)-2] == quote && tok[len(tok)-3] == quote {
		// Triple-quoted: we already trimmed one quote off each side;
		// trim the remaining two.
		if len(s) < 4 || s[0] != quote || s[1] != quote ||
			s[len(s)-1] != quote || s[len(s)-2] != quote {
			return Result{}, errBadInternalCall
		}
		s = s[2 : len(s)-2]
	}

	// rawmode shortcut: no escape decoding needed when raw or when
	// the body has no backslash. This mirrors the C source's
	// "Avoid invoking escape decoding routines if possible" guard.
	noEscape := rawMode || !hasBackslash(s)

	if bytesMode {
		for _, c := range s {
			if c >= 0x80 {
				return Result{}, fmt.Errorf("bytes can only contain ASCII literal characters")
			}
		}
		out := s
		var warns []string
		if !noEscape {
			b, w, err := decodeBytesEscapes(s)
			if err != nil {
				return Result{}, err
			}
			out = b
			warns = w
		}
		return Result{
			Bytes:    append([]byte(nil), out...),
			IsBytes:  true,
			IsRaw:    rawMode,
			Warnings: warns,
		}, nil
	}

	if noEscape {
		if !utf8.Valid(s) {
			return Result{}, fmt.Errorf("invalid utf-8 in string literal")
		}
		return Result{Text: string(s), IsRaw: rawMode}, nil
	}
	text, warns, err := decodeUnicodeEscapes(s)
	if err != nil {
		return Result{}, err
	}
	return Result{Text: text, Warnings: warns}, nil
}

func hasBackslash(b []byte) bool { return strings.IndexByte(string(b), '\\') >= 0 }
