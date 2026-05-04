// CPython: Parser/tokenizer/string_tokenizer.c and
// Parser/tokenizer/utf8_tokenizer.c. Both drivers load the entire
// source up front; the difference is that string_tokenizer runs PEP
// 263 encoding detection (decoding to UTF-8 in place) while
// utf8_tokenizer trusts the caller. gopy collapses the two into one
// driver because Go strings are already UTF-8.

package lexer

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
	// Strip a UTF-8 BOM. PEP 263 says the BOM signature is treated as
	// declaring UTF-8 encoding; conflicting `coding:` cookies are
	// flagged by the action_helpers layer, not here.
	if len(src) >= 3 && src[0] == 0xef && src[1] == 0xbb && src[2] == 0xbf {
		src = src[3:]
		s.encoding = "utf-8"
	}
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
