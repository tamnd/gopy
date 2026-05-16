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

	"github.com/tamnd/gopy/codecs"
)

// nonUTF8ErrorMessage renders the SyntaxError text CPython emits when
// a non-utf-8 byte appears in source that has no PEP 263 cookie. The
// test_utf8source gate only checks 'utf-8' (lowercased) is present;
// the rest of the wording mirrors CPython so users see a familiar
// message.
//
// CPython: Parser/tokenizer/helpers.c:332 ensure_utf8 error_ret arm
func nonUTF8ErrorMessage(bad byte) string {
	return fmt.Sprintf(
		"Non-UTF-8 code starting with '\\x%02x' but no encoding declared; "+
			"see https://peps.python.org/pep-0263/ for details",
		bad,
	)
}

// FromString builds a State that tokenises the given source. The
// driver loads the whole buffer up front; underflow returns false on
// the next refill request, matching the C source's
// _PyTokenizer_FromUTF8 / _PyTokenizer_FromString behavior after the
// final line lands.
//
// CPython: Parser/tokenizer/utf8_tokenizer.c:11 _PyTokenizer_FromUTF8
// (and Parser/tokenizer/string_tokenizer.c:106 _PyTokenizer_FromString)
func FromString(src string, mode Mode) *State {
	return FromBytes([]byte(src), mode)
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
	cookie := DetectEncodingCookie(src)
	if cookie == "" && !hadBOM {
		// No encoding declaration and no BOM: source defaults to UTF-8.
		// CPython raises SyntaxError at the offending byte naming the
		// 'utf-8' default so the user knows to add a coding cookie.
		// CPython: Parser/tokenizer/helpers.c:332 ensure_utf8
		if line, bad, ok := ValidateUTF8(src); !ok {
			s.lineno = line
			s.recordError(nonUTF8ErrorMessage(bad))
			s.done = eEncoding
		}
	}
	if name := cookie; name != "" {
		// CPython: Parser/tokenizer/helpers.c:425 BOM vs cookie mismatch
		if hadBOM && !isUTF8Name(name) {
			s.recordError("encoding problem: " + name + " with BOM")
			s.done = eEncoding
		} else if !hadBOM {
			s.encoding = name
			if !isUTF8Name(name) {
				decoded, _, err := codecs.Decode(src, name, "strict")
				if err != nil {
					s.recordError("encoding problem: " + name)
					s.done = eEncoding
				} else {
					src = []byte(decoded)
				}
			}
		}
	}
	src = NormalizeNewlines(src)
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
