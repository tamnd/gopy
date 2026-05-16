// Source preprocessing: PEP 263 encoding cookie detection and
// newline normalization. CPython does both inside its tokenizer
// drivers (Parser/tokenizer/string_tokenizer.c and helpers.c). gopy
// keeps the surface narrow and pure so the in-memory and file
// drivers can share it.

package lexer

import "bytes"

// codingCookieMax bounds how many bytes of the first two lines we
// scan when looking for the PEP 263 cookie. CPython caps the scan
// at the line length; gopy uses a hard 256-byte ceiling so a
// pathological one-line file does not turn into a 1MB regex run.
//
// CPython: Parser/tokenizer/helpers.c:165 check_coding_spec
const codingCookieMax = 256

// DetectEncodingCookie scans the first two physical lines of src
// for a PEP 263 `coding:` declaration and returns the encoding
// name, or "" when no cookie is present. The scan stops at byte
// codingCookieMax of each line. Lines may end with \n, \r\n, or
// \r; the function is newline-flavor agnostic.
//
// Mirrors CPython's decoding_state machine: the cookie may only
// appear on a line that is blank or comment-only. Once a line
// containing actual code is seen the search stops, so a `coding:`
// comment after the first statement is ignored just like in
// CPython.
//
// CPython: Parser/tokenizer/helpers.c:388 _PyTokenizer_check_coding_spec
func DetectEncodingCookie(src []byte) string {
	rest := src
	for line := 0; line < 2 && len(rest) > 0; line++ {
		end := lineEnd(rest)
		head := rest[:end]
		scan := head
		if len(scan) > codingCookieMax {
			scan = scan[:codingCookieMax]
		}
		if name := matchCodingCookie(scan); name != "" {
			return name
		}
		if lineHasCode(scan) {
			return ""
		}
		rest = skipNewline(rest, end)
	}
	return ""
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
	return string(rest[:end])
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
// CPython: Parser/tokenizer/helpers.c:332 ensure_utf8 (the tok_check_bom
// / decoding_fgets pair raises a SyntaxError on the first non-UTF-8
// byte when no PEP 263 cookie names a different encoding).
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
		size := utf8Size(c)
		if size == 0 || i+size > len(src) {
			return line, c, false
		}
		for k := 1; k < size; k++ {
			if src[i+k]&0xc0 != 0x80 {
				return line, c, false
			}
		}
		i += size
	}
	return 0, 0, true
}

// utf8Size returns the length of a UTF-8 sequence whose leading byte
// is c, or 0 if c is not a valid leading byte. CPython's stb_lookup
// table; we keep the masks inline because there are only four cases.
func utf8Size(c byte) int {
	switch {
	case c&0xe0 == 0xc0:
		return 2
	case c&0xf0 == 0xe0:
		return 3
	case c&0xf8 == 0xf0:
		return 4
	}
	return 0
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
