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

// decodeUnicodeEscapes walks s and expands the standard Python
// escape sequences. The returned string is the decoded text in
// UTF-8 form.
//
// CPython: Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscapeInternal
func decodeUnicodeEscapes(s []byte) (string, error) {
	var out []byte
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
			return "", fmt.Errorf("Trailing \\ in string") //nolint:staticcheck // Mirror CPython's exact error text.
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
			val := int(c - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				val = val*8 + int(s[i]-'0')
				i++
			}
			if val < 0x100 {
				out = append(out, byte(val))
			} else {
				out = utf8.AppendRune(out, rune(val))
			}
		case 'x':
			if i+2 > len(s) {
				return "", fmt.Errorf("truncated \\xXX escape")
			}
			v, err := parseHex(s[i : i+2])
			if err != nil {
				return "", err
			}
			out = append(out, byte(v))
			i += 2
		case 'u':
			if i+4 > len(s) {
				return "", fmt.Errorf("truncated \\uXXXX escape")
			}
			v, err := parseHex(s[i : i+4])
			if err != nil {
				return "", err
			}
			out = utf8.AppendRune(out, rune(v))
			i += 4
		case 'U':
			if i+8 > len(s) {
				return "", fmt.Errorf("truncated \\UXXXXXXXX escape")
			}
			v, err := parseHex(s[i : i+8])
			if err != nil {
				return "", err
			}
			if v > 0x10FFFF {
				return "", fmt.Errorf("illegal Unicode character in \\U escape")
			}
			out = utf8.AppendRune(out, rune(v))
			i += 8
		default:
			// PEP 414 keeps the backslash in the output for
			// unrecognized escapes (with a DeprecationWarning the
			// caller surfaces separately).
			out = append(out, '\\', c)
		}
	}
	if !utf8.Valid(out) {
		return "", fmt.Errorf("invalid utf-8 in string literal")
	}
	return string(out), nil
}

// decodeBytesEscapes is the bytes form. The unicode escapes are
// rejected here because they have no meaning in a byte literal.
//
// CPython: Objects/bytesobject.c _PyBytes_DecodeEscape
func decodeBytesEscapes(s []byte) ([]byte, error) {
	var out []byte
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
			return nil, fmt.Errorf("Trailing \\ in string") //nolint:staticcheck // Mirror CPython's exact error text.
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
				return nil, fmt.Errorf("truncated \\xXX escape")
			}
			v, err := parseHex(s[i : i+2])
			if err != nil {
				return nil, err
			}
			out = append(out, byte(v))
			i += 2
		default:
			out = append(out, '\\', c)
		}
	}
	return out, nil
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
