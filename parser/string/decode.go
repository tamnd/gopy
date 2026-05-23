// CPython: Objects/unicodeobject.c
// _PyUnicode_DecodeUnicodeEscapeInternal and Objects/bytesobject.c
// _PyBytes_DecodeEscape. The two functions share a structure but
// differ on the unicode escapes (\xNN, \uNNNN, \UNNNNNNNN,
// \N{name}) which only the unicode form accepts.

package string

import (
	"fmt"
	"unicode/utf8"
)

// DecodeError carries the byte-position pair that
// _PyUnicode_DecodeUnicodeEscapeInternal2 records on its
// PyUnicodeDecodeError so _Pypegen_raise_decode_error can format the
// codec-style prefix the parser surface needs.
//
// CPython: Objects/unicodeobject.c:6854 raise on "unicodeescape"
type DecodeError struct {
	Reason string
	Start  int
	End    int
}

func (e *DecodeError) Error() string {
	return e.Reason
}

// newDecodeError constructs a DecodeError that mirrors the
// PyUnicodeDecodeError raised inside the C decoder.
func newDecodeError(reason string, start, end int) *DecodeError {
	return &DecodeError{Reason: reason, Start: start, End: end}
}

// decodeUnicodeEscapes walks s and expands the standard Python
// escape sequences. The returned string is the decoded text in
// UTF-8 form. Unknown escape sequences are kept verbatim and
// reported via the warnings slice so the caller can surface them
// as SyntaxWarning text without aborting decoding.
//
// CPython: Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscapeInternal
func decodeUnicodeEscapes(s []byte) (text string, warnings []string, err error) {
	var out []byte
	var warns []string
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		escStart := i
		i++
		if i >= len(s) {
			return "", nil, newDecodeError("\\ at end of string", escStart, i)
		}
		c = s[i]
		i++
		switch c {
		case '\n':
			// line continuation: backslash-newline drops both
		case '\\':
			out = append(out, '\\')
		case '\'':
			out = append(out, '\'')
		case '"':
			out = append(out, '"')
		case 'a':
			out = append(out, 0x07)
		case 'b':
			out = append(out, 0x08)
		case 'f':
			out = append(out, 0x0c)
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, 0x0b)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// CPython: Objects/unicodeobject.c:6713 octal escape.
			// Octal value is a Unicode ordinal: emit as a codepoint,
			// not as a raw byte (raw bytes >=0x80 corrupt UTF-8).
			// Values > 0o377 are reported as invalid escapes, but
			// the codepoint is still emitted.
			val := int(c - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				val = val*8 + int(s[i]-'0')
				i++
			}
			if val > 0o377 {
				warns = append(warns, fmt.Sprintf("invalid octal escape sequence '\\%o'", val))
			}
			out = utf8.AppendRune(out, rune(val))
		case 'x':
			if i+2 > len(s) {
				return "", nil, newDecodeError("truncated \\xXX escape", escStart, len(s))
			}
			v, err := parseHex(s[i : i+2])
			if err != nil {
				return "", nil, newDecodeError("truncated \\xXX escape", escStart, i+2)
			}
			out = utf8.AppendRune(out, rune(v))
			i += 2
		case 'u':
			if i+4 > len(s) {
				return "", nil, newDecodeError("truncated \\uXXXX escape", escStart, len(s))
			}
			v, err := parseHex(s[i : i+4])
			if err != nil {
				return "", nil, newDecodeError("truncated \\uXXXX escape", escStart, i+4)
			}
			out = utf8.AppendRune(out, rune(v))
			i += 4
		case 'U':
			if i+8 > len(s) {
				return "", nil, newDecodeError("truncated \\UXXXXXXXX escape", escStart, len(s))
			}
			v, err := parseHex(s[i : i+8])
			if err != nil {
				return "", nil, newDecodeError("truncated \\UXXXXXXXX escape", escStart, i+8)
			}
			if v > 0x10FFFF {
				return "", nil, newDecodeError("illegal Unicode character", escStart, i+8)
			}
			out = utf8.AppendRune(out, rune(v))
			i += 8
		case 'N':
			// CPython: Objects/unicodeobject.c:6791 \N{name} expansion.
			// The matching errors live at unicodeobject.c:6801 ("malformed
			// \N character escape") and :6826 ("unknown Unicode character
			// name"); both are surfaced through PyUnicodeDecodeError so
			// _Pypegen_raise_decode_error can format the codec prefix.
			if i >= len(s) || s[i] != '{' {
				return "", nil, newDecodeError("malformed \\N character escape", escStart, i)
			}
			j := i + 1
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j >= len(s) {
				return "", nil, newDecodeError("malformed \\N character escape", escStart, j)
			}
			name := string(s[i+1 : j])
			if name == "" {
				return "", nil, newDecodeError("malformed \\N character escape", escStart, j+1)
			}
			expanded, ok := NameLookup(name)
			if !ok {
				return "", nil, newDecodeError("unknown Unicode character name", escStart, j+1)
			}
			out = append(out, expanded...)
			i = j + 1
		default:
			// PEP 414 keeps the backslash in the output for
			// unrecognized escapes; CPython 3.14 also emits a
			// SyntaxWarning so the user can spot a typo.
			out = append(out, '\\', c)
			warns = append(warns, fmt.Sprintf("invalid escape sequence '\\%c'", c))
		}
	}
	if !utf8.Valid(out) {
		return "", nil, fmt.Errorf("invalid utf-8 in string literal")
	}
	return string(out), warns, nil
}

// decodeBytesEscapes is the bytes form. The unicode escapes are
// rejected here because they have no meaning in a byte literal.
//
// CPython: Objects/bytesobject.c _PyBytes_DecodeEscape
func decodeBytesEscapes(s []byte) (out []byte, warnings []string, err error) {
	var warns []string
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			return nil, nil, fmt.Errorf("Trailing \\ in string") //nolint:staticcheck // Mirror CPython's exact error text.
		}
		c = s[i]
		i++
		switch c {
		case '\n':
		case '\\':
			out = append(out, '\\')
		case '\'':
			out = append(out, '\'')
		case '"':
			out = append(out, '"')
		case 'a':
			out = append(out, 0x07)
		case 'b':
			out = append(out, 0x08)
		case 'f':
			out = append(out, 0x0c)
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, 0x0b)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			val := int(c - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				val = val*8 + int(s[i]-'0')
				i++
			}
			out = append(out, byte(val&0xff))
		case 'x':
			if i+2 > len(s) {
				return nil, nil, fmt.Errorf("truncated \\xXX escape")
			}
			v, err := parseHex(s[i : i+2])
			if err != nil {
				return nil, nil, err
			}
			out = append(out, byte(v))
			i += 2
		default:
			out = append(out, '\\', c)
			warns = append(warns, fmt.Sprintf("invalid escape sequence '\\%c'", c))
		}
	}
	return out, warns, nil
}

func parseHex(s []byte) (int, error) {
	v := 0
	for _, c := range s {
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex digit %q", c)
		}
		v = v<<4 | d
	}
	return v, nil
}
