// Source preprocessing: PEP 263 encoding cookie detection and
// newline normalization. CPython does both inside its tokenizer
// drivers (Parser/tokenizer/string_tokenizer.c and helpers.c). gopy
// keeps the surface narrow and pure so the in-memory and file
// drivers can share it.

package lexer

import (
	"bytes"
	"strings"
)

// cookieFileBufsize matches CPython's BUFSIZ default (8192) for file
// reads. The file driver peeks twice this many bytes so a two-line
// header that has a cookie inside either line is fully scanned.
//
// CPython: Parser/tokenizer/file_tokenizer.c:379 (PyMem_Malloc(BUFSIZ))
const cookieFileBufsize = 8192

// DetectEncodingCookie scans the first two physical lines of src
// for a PEP 263 `coding:` declaration and returns the encoding
// name, or "" when no cookie is present. The scan covers the full
// line; CPython does the same in get_coding_spec by stepping
// through `size - 6` bytes. Lines may end with \n, \r\n, or \r;
// the function is newline-flavor agnostic.
//
// Mirrors CPython's decoding_state machine: the cookie may only
// appear on a line that is blank or comment-only. Once a line
// containing actual code is seen the search stops, so a `coding:`
// comment after the first statement is ignored just like in
// CPython. A line that follows a backslash-continued line carries
// `tok->cont_line == 1` in CPython and CPython skips the cookie
// scan on it (helpers.c:392); gopy applies the same skip via
// contLine tracking.
//
// CPython: Parser/tokenizer/helpers.c:388 _PyTokenizer_check_coding_spec
func DetectEncodingCookie(src []byte) string {
	name, _ := detectEncodingCookieAt(src)
	return name
}

// detectEncodingCookieAt is the line-tracking sibling of
// DetectEncodingCookie. The returned line is 1-based and identifies the
// physical source line the cookie sat on (1 or 2), or 0 when no cookie
// was found.
//
// CPython: Parser/tokenizer/helpers.c:388 _PyTokenizer_check_coding_spec
// (tok->lineno is incremented by tok_underflow_string before this runs)
func detectEncodingCookieAt(src []byte) (string, int) {
	rest := src
	contLine := false
	for line := 0; line < 2 && len(rest) > 0; line++ {
		end := lineEnd(rest)
		head := rest[:end]
		if !contLine {
			if name := matchCodingCookie(head); name != "" {
				return name, line + 1
			}
			if lineHasCode(head) {
				return "", 0
			}
		}
		contLine = len(head) > 0 && head[len(head)-1] == '\\'
		rest = skipNewline(rest, end)
	}
	return "", 0
}

// lineHasCode reports whether line contains a non-whitespace byte
// before any `#`. CPython's check_coding_spec uses this to decide
// whether the decoding state should transition to STATE_NORMAL,
// halting further cookie scans.
//
// CPython: Parser/tokenizer/helpers.c:401 the post-get_coding_spec loop
func lineHasCode(line []byte) bool {
	for _, c := range line {
		if c == '#' || c == '\n' || c == '\r' {
			return false
		}
		// CPython treats space, tab, and form-feed (\014) as
		// indentation-only bytes; anything else flips the line
		// into "has code" territory.
		if c != ' ' && c != '\t' && c != '\014' {
			return true
		}
	}
	return false
}

// matchCodingCookie picks the encoding name out of one line if it
// looks like a PEP 263 cookie. The line must start with a `#`
// (after optional whitespace) and contain `coding[:=]\s*<name>`
// where `<name>` is a run of letters, digits, `-`, `_`, or `.`.
func matchCodingCookie(line []byte) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || line[i] != '#' {
		return ""
	}
	// Find the `coding` keyword anywhere on the line.
	rest := line[i:]
	idx := bytes.Index(rest, []byte("coding"))
	if idx < 0 {
		return ""
	}
	rest = rest[idx+len("coding"):]
	if len(rest) == 0 {
		return ""
	}
	if rest[0] != ':' && rest[0] != '=' {
		return ""
	}
	rest = rest[1:]
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	end := 0
	for end < len(rest) && isCodingNameByte(rest[end]) {
		end++
	}
	if end == 0 {
		return ""
	}
	return getNormalName(string(rest[:end]))
}

// getNormalName folds the utf-8 / latin-1 family aliases CPython
// short-circuits in get_normal_name. It only looks at the first 12
// bytes of s; everything past that is ignored, so names like
// "iso-8859-1-xxxxx..." normalise to "iso-8859-1" and the codec
// lookup succeeds.
//
// CPython: Parser/tokenizer/helpers.c:305 get_normal_name
func getNormalName(s string) string {
	var buf [13]byte
	n := 0
	for n < 12 && n < len(s) {
		c := s[n]
		if c == '_' {
			buf[n] = '-'
		} else if c >= 'A' && c <= 'Z' {
			buf[n] = c + 'a' - 'A'
		} else {
			buf[n] = c
		}
		n++
	}
	prefix := string(buf[:n])
	switch {
	case prefix == "utf-8" || strings.HasPrefix(prefix, "utf-8-"):
		return "utf-8"
	case prefix == "latin-1" || prefix == "iso-8859-1" || prefix == "iso-latin-1":
		return "iso-8859-1"
	case strings.HasPrefix(prefix, "latin-1-") || strings.HasPrefix(prefix, "iso-8859-1-") || strings.HasPrefix(prefix, "iso-latin-1-"):
		return "iso-8859-1"
	}
	return s
}

func isCodingNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_' || c == '.':
		return true
	}
	return false
}

// nthLine returns the n-th physical line (1-based) of src, stripped of
// any trailing newline. Returns "" when n is out of range or src is
// empty. Used at the BOM/cookie error boundary to populate the
// SyntaxError text field, since the lexer FSM has not yet ingested
// these bytes when the error fires.
//
// CPython: Parser/tokenizer/helpers.c (SyntaxError text is copied from
// the offending line in the source buffer).
func nthLine(src []byte, n int) string {
	if n <= 0 {
		return ""
	}
	rest := src
	for line := 1; line <= n; line++ {
		end := lineEnd(rest)
		if line == n {
			return string(rest[:end])
		}
		next := skipNewline(rest, end)
		if next == nil {
			return ""
		}
		rest = next
	}
	return ""
}

func lineEnd(src []byte) int {
	for i, c := range src {
		if c == '\n' || c == '\r' {
			return i
		}
	}
	return len(src)
}

func skipNewline(src []byte, at int) []byte {
	if at >= len(src) {
		return nil
	}
	if src[at] == '\r' {
		if at+1 < len(src) && src[at+1] == '\n' {
			return src[at+2:]
		}
		return src[at+1:]
	}
	return src[at+1:]
}

// CheckBOMCookieConflict reports the CPython error text when the
// source begins with a UTF-8 BOM but the PEP 263 cookie names a
// non-utf-8 encoding. Returns the empty string when there is no
// conflict (no BOM, no cookie, or cookie says utf-8 / utf8 / U8).
//
// CPython: Parser/tokenizer/helpers.c:425 check_coding_spec
// (the encoding-vs-cookie comparison arm)
func CheckBOMCookieConflict(src []byte) string {
	if len(src) < 3 || src[0] != 0xef || src[1] != 0xbb || src[2] != 0xbf {
		return ""
	}
	name := DetectEncodingCookie(src[3:])
	if name == "" {
		return ""
	}
	if isUTF8Name(name) {
		return ""
	}
	return "encoding problem: " + name + " with BOM"
}

func isUTF8Name(name string) bool {
	switch normalizeEncodingName(name) {
	case "utf8", "utf-8", "u8":
		return true
	}
	return false
}

func normalizeEncodingName(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

// ValidateUTF8 walks src and returns the 1-based line number and
// offending byte at the first non-UTF-8 sequence, plus ok=false.
// When src is valid UTF-8 ok is true. The line count tracks \n, \r,
// and \r\n the same way the lexer does so the reported line matches
// the source the user sees in their editor.
//
// CPython: Parser/tokenizer/helpers.c:506 _PyTokenizer_ensure_utf8
// (the tok_check_bom / decoding_fgets pair raises a SyntaxError on
// the first non-UTF-8 byte when no PEP 263 cookie names a different
// encoding).
func ValidateUTF8(src []byte) (line int, bad byte, ok bool) {
	line = 1
	i := 0
	for i < len(src) {
		c := src[i]
		if c < 0x80 {
			if c == '\n' {
				line++
				i++
				continue
			}
			if c == '\r' {
				line++
				i++
				if i < len(src) && src[i] == '\n' {
					i++
				}
				continue
			}
			i++
			continue
		}
		size := validUTF8(src[i:])
		if size == 0 {
			return line, c, false
		}
		i += size
	}
	return 0, 0, true
}

// validUTF8 returns the length of the UTF-8 sequence at s, or 0 if
// s does not start a valid sequence. The function rejects every
// byte sequence stringlib/codecs.h:utf8_decode also rejects:
// overlong encodings (0xC0/0xC1, 0xE0 with byte2 < 0xA0, 0xF0 with
// byte2 < 0x90), surrogates (0xED with byte2 >= 0xA0 produces
// D800-DFFF), and overflow past U+10FFFF (0xF4 with byte2 >= 0x90,
// 0xF5+ leading bytes). Continuation bytes must lie in 0x80-0xBF.
//
// CPython: Parser/tokenizer/helpers.c:446 valid_utf8
func validUTF8(s []byte) int {
	if len(s) == 0 {
		return 0
	}
	c := s[0]
	expected := 0
	switch {
	case c < 0x80:
		return 1
	case c < 0xE0:
		if c < 0xC2 {
			// \x80-\xBF is a continuation byte; \xC0-\xC1 would
			// be an overlong encoding of 0000-007F.
			return 0
		}
		expected = 1
	case c < 0xF0:
		if len(s) < 2 {
			return 0
		}
		if c == 0xE0 && s[1] < 0xA0 {
			// Overlong \xE0\x80\x80-\xE0\x9F\xBF for 0000-07FF.
			return 0
		}
		if c == 0xED && s[1] >= 0xA0 {
			// Surrogates D800-DFFF. See Unicode 5.2 table 3-7
			// and RFC 3629.
			return 0
		}
		expected = 2
	case c < 0xF5:
		if len(s) < 2 {
			return 0
		}
		var overlongOrOverflow bool
		if s[1] < 0x90 {
			overlongOrOverflow = c == 0xF0
		} else {
			overlongOrOverflow = c == 0xF4
		}
		if overlongOrOverflow {
			// 0xF0\x80\x80\x80-0xF0\x8F\xBF\xBF would re-encode
			// 0000-FFFF; 0xF4\x90+ would overflow past U+10FFFF.
			return 0
		}
		expected = 3
	default:
		// 0xF5-0xFF: no valid 4-byte sequence starts here.
		return 0
	}
	length := expected + 1
	if len(s) < length {
		return 0
	}
	for i := 1; i <= expected; i++ {
		if s[i] < 0x80 || s[i] >= 0xC0 {
			return 0
		}
	}
	return length
}

// TranslateNewlines is the gopy port of CPython's
// _PyTokenizer_translate_newlines. It folds CRLF and bare CR into LF
// (so the FSM treats newline as a single byte) and, when execInput is
// true, appends a trailing LF when the source does not already end in
// one. The trailing-newline injection is what file-input mode relies on
// so the final statement's NEWLINE token closes the suite.
//
// CPython: Parser/tokenizer/helpers.c:215 _PyTokenizer_translate_newlines
func TranslateNewlines(src []byte, execInput bool) []byte {
	needsFold := bytes.IndexByte(src, '\r') >= 0
	needsTrailingNL := execInput && len(src) > 0 && src[len(src)-1] != '\n'
	if !needsFold && !needsTrailingNL {
		return src
	}
	out := make([]byte, 0, len(src)+1)
	for i := 0; i < len(src); i++ {
		if src[i] == '\r' {
			out = append(out, '\n')
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			continue
		}
		out = append(out, src[i])
	}
	if execInput && len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out
}

// NormalizeNewlines is the no-injection form (execInput=false). Kept
// for callers and tests that only need the CRLF fold.
//
// CPython: Parser/tokenizer/helpers.c:215 _PyTokenizer_translate_newlines
// (with exec_input == 0)
func NormalizeNewlines(src []byte) []byte {
	return TranslateNewlines(src, false)
}
