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
// CPython: Parser/tokenizer/helpers.c:165 check_coding_spec
func DetectEncodingCookie(src []byte) string {
	rest := src
	for line := 0; line < 2 && len(rest) > 0; line++ {
		end := lineEnd(rest)
		head := rest[:end]
		if len(head) > codingCookieMax {
			head = head[:codingCookieMax]
		}
		if name := matchCodingCookie(head); name != "" {
			return name
		}
		rest = skipNewline(rest, end)
	}
	return ""
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
// CPython: Parser/tokenizer/helpers.c:223 check_bom
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
	return "encoding declaration in Unicode string"
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

// NormalizeNewlines folds \r\n and bare \r into \n so the FSM can
// treat newline as a single byte. CPython does the same fold in
// the file driver before handing lines to the scanner.
//
// CPython: Parser/tokenizer/file_tokenizer.c:118 translate_newlines
func NormalizeNewlines(src []byte) []byte {
	if bytes.IndexByte(src, '\r') < 0 {
		return src
	}
	out := make([]byte, 0, len(src))
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
	return out
}
