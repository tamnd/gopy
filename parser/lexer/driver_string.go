// CPython: Parser/tokenizer/string_tokenizer.c and
// Parser/tokenizer/utf8_tokenizer.c. Both drivers load the entire
// source up front; the difference is that string_tokenizer runs PEP
// 263 encoding detection (decoding to UTF-8 in place) while
// utf8_tokenizer trusts the caller. gopy collapses the two into one
// driver because Go strings are already UTF-8.
//
// Function map (string_tokenizer.c → gopy):
//
//	tok_underflow_string         → FromBytes underflow returning false
//	_PyTokenizer_FromString      → FromString / FromBytes (encoding path)
//
// Function map (utf8_tokenizer.c → gopy):
//
//	_PyTokenizer_FromUTF8        → FromString / FromBytes (utf-8 fast path)

package lexer

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/codecs"
)

// decodeErrorMessage formats a codec failure into the SyntaxError text
// CPython produces when the cookie-driven decode raises. The cookie
// path is reached during tokenizer init, so the SyntaxError args[0]
// is the bare str() of the UnicodeDecodeError, no "(unicode error)"
// prefix. The prefix only appears when the decode error is raised
// later inside the parser via _Pypegen_raise_decode_error; the string
// driver never reaches that branch because tokenizer init already
// returned NULL.
//
// CPython: Parser/pegen_errors.c:13 _PyPegen_raise_tokenizer_init_error
func decodeErrorMessage(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "UnicodeDecodeError: ")
	msg = strings.TrimPrefix(msg, "UnicodeEncodeError: ")
	return msg
}

// nonUTF8ErrorMessage renders the SyntaxError text CPython emits when
// a non-utf-8 byte appears in source that has no PEP 263 cookie.
// Matches the upstream format byte-for-byte (modulo the "in file %s"
// fragment which only appears when the tokenizer carries a filename;
// the bytes-source path here does not).
//
// CPython: Parser/tokenizer/helpers.c:529 _PyTokenizer_syntaxerror_known_range
// (Non-UTF-8 code starting with '\x%.2x'%s%V on line %i, but no
// encoding declared; see https://peps.python.org/pep-0263/ for details)
func nonUTF8ErrorMessage(bad byte, lineno int) string {
	return fmt.Sprintf(
		"Non-UTF-8 code starting with '\\x%02x' on line %d, "+
			"but no encoding declared; "+
			"see https://peps.python.org/pep-0263/ for details",
		bad, lineno,
	)
}

// FromString builds a State for a source that is already canonical
// UTF-8 (callers that pass a Python str). Mirrors CPython's
// _PyTokenizer_FromUTF8 path: BOM stripping and PEP 263 cookie
// detection are skipped because the source is presumed pre-decoded.
// The per-line UTF-8 validation still runs so a Go string containing
// invalid UTF-8 bytes surfaces the Non-UTF-8 SyntaxError at the right
// line.
//
// CPython: Parser/tokenizer/utf8_tokenizer.c:11 _PyTokenizer_FromUTF8
// (compile() routes here when PyCF_IGNORE_COOKIE is set in
// Parser/pegen.c:1051).
func FromString(src string, mode Mode) *State {
	s := newState()
	buf := []byte(src)
	if line, bad, ok := ValidateUTF8(buf); !ok {
		s.lineno = line
		s.recordErrorWithText(
			nonUTF8ErrorMessage(bad, line),
			nthLine(buf, line),
		)
		s.done = eEncoding
	}
	buf = TranslateNewlines(buf, mode == ModeFile)
	s.encoding = "utf-8"
	s.buf = buf
	s.cur = 0
	s.inp = len(buf)
	s.end = len(buf)
	s.mode = mode
	s.lineno = 1
	s.firstLine = 1
	s.col = 0
	s.lineStart = 0
	s.underflow = func(*State) bool { return false }
	return s
}

// FromBytes is the byte-slice variant. The caller hands ownership of
// the slice to the lexer; we still grow when the FSM needs more room
// but for in-memory drivers that's a no-op since cur never reaches
// inp past the original length.
//
// CPython: Parser/tokenizer/utf8_tokenizer.c:11 _PyTokenizer_FromUTF8
func FromBytes(src []byte, mode Mode) *State {
	s := newState()
	hadBOM := false
	// Strip a UTF-8 BOM. PEP 263 says the BOM signature is treated as
	// declaring UTF-8 encoding; conflicting `coding:` cookies are
	// flagged here so the parser surfaces a SyntaxError.
	// CPython: Parser/tokenizer/helpers.c:265 check_bom
	if len(src) >= 3 && src[0] == 0xef && src[1] == 0xbb && src[2] == 0xbf {
		src = src[3:]
		s.encoding = "utf-8"
		hadBOM = true
	}
	cookie, cookieLine := detectEncodingCookieAt(src)
	nonUTF8Cookie := false
	if name := cookie; name != "" {
		// CPython: Parser/tokenizer/helpers.c:425 BOM vs cookie mismatch
		if hadBOM && !isUTF8Name(name) {
			s.lineno = cookieLine
			s.recordErrorWithText(
				"encoding problem: "+name+" with BOM",
				nthLine(src, cookieLine),
			)
			s.done = eEncoding
		} else if !hadBOM {
			s.encoding = name
			if !isUTF8Name(name) {
				nonUTF8Cookie = true
				decoded, _, err := codecs.Decode(src, name, "strict")
				if err != nil {
					s.lineno = cookieLine
					s.recordErrorWithText(
						decodeErrorMessage(err),
						nthLine(src, cookieLine),
					)
					s.done = eEncoding
				} else {
					src = []byte(decoded)
				}
			}
		}
	}
	// CPython runs _PyTokenizer_ensure_utf8 on the source whenever the
	// declared encoding is utf-8 (default, BOM, or explicit cookie). The
	// non-utf-8 cookie path skips validation because the codec decode
	// already produced canonical utf-8 above.
	//
	// CPython: Parser/tokenizer/string_tokenizer.c:108 ensure_utf8 call
	if s.err == nil && !nonUTF8Cookie {
		if line, bad, ok := ValidateUTF8(src); !ok {
			s.lineno = line
			s.recordErrorWithText(
				nonUTF8ErrorMessage(bad, line),
				nthLine(src, line),
			)
			s.done = eEncoding
		}
	}
	// CPython: Parser/lexer/lexer.c:89 contains_null_bytes
	// CPython runs the null-byte scan per-line inside tok_nextc after
	// every refill. gopy loads the whole source upfront in FromBytes so
	// the equivalent check is a single pre-scan that finds the offending
	// line and records the canonical SyntaxError.
	if s.err == nil {
		if line, ok := firstNullByteLine(src); ok {
			s.lineno = line
			s.recordErrorWithText(
				"source code cannot contain null bytes",
				nthLine(src, line),
			)
			s.done = eSyntax
		}
	}
	// CPython: Parser/pegen.c:1048 exec_input = start_rule == Py_file_input
	src = TranslateNewlines(src, mode == ModeFile)
	s.buf = src
	s.cur = 0
	s.inp = len(src)
	s.end = len(src)
	s.mode = mode
	s.lineno = 1
	s.firstLine = 1
	s.col = 0
	s.lineStart = 0
	s.underflow = func(*State) bool { return false }
	return s
}

// firstNullByteLine returns the 1-based line number of the first NUL
// byte in src plus ok=true; if src has no NUL, ok is false. Mirrors the
// per-line tok_nextc scan but in one pass since gopy buffers upfront.
//
// CPython: Parser/lexer/lexer.c:53 contains_null_bytes
func firstNullByteLine(src []byte) (int, bool) {
	line := 1
	for _, b := range src {
		if b == 0 {
			return line, true
		}
		if b == '\n' {
			line++
		}
	}
	return 0, false
}
