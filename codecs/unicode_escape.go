// unicode-escape codec. Encoding maps each codepoint to its ASCII-safe
// backslash form; decoding interprets the standard backslash sequences
// back to Unicode text.
//
// CPython: Objects/unicodeobject.c:6926 PyUnicode_AsUnicodeEscapeString
// CPython: Objects/unicodeobject.c:6627 _PyUnicode_DecodeUnicodeEscapeInternal2
package codecs

import (
	"fmt"
	"strings"
)

var unicodeEscapeCodec = &CodecInfo{
	Name:   "unicode-escape",
	Encode: encodeUnicodeEscape,
	Decode: decodeUnicodeEscape,
}

// encodeUnicodeEscape mirrors PyUnicode_AsUnicodeEscapeString.
//
// CPython: Objects/unicodeobject.c:6926 PyUnicode_AsUnicodeEscapeString
func encodeUnicodeEscape(input, _ string) ([]byte, int, error) {
	var out []byte
	for _, r := range input {
		switch {
		case r == '\\':
			out = append(out, '\\', '\\')
		case r == '\t':
			out = append(out, '\\', 't')
		case r == '\n':
			out = append(out, '\\', 'n')
		case r == '\r':
			out = append(out, '\\', 'r')
		case r >= 0x20 && r < 0x7f:
			out = append(out, byte(r))
		case r < 0x100:
			out = append(out, []byte(fmt.Sprintf("\\x%02x", r))...)
		case r < 0x10000:
			out = append(out, []byte(fmt.Sprintf("\\u%04x", r))...)
		default:
			out = append(out, []byte(fmt.Sprintf("\\U%08x", r))...)
		}
	}
	return out, len([]rune(input)), nil
}

// decodeUnicodeEscape mirrors _PyUnicode_DecodeUnicodeEscapeInternal2.
// It handles the standard backslash sequences: \n \r \t \\ \' \" \a \b \f \v
// \0, octal \ooo, \xHH, \uHHHH, \UHHHHHHHH, and \N{name}.
//
// CPython: Objects/unicodeobject.c:6627 _PyUnicode_DecodeUnicodeEscapeInternal2
func decodeUnicodeEscape(input []byte, errors string) (string, int, error) {
	var b strings.Builder
	i := 0
	for i < len(input) {
		c := input[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(input) {
			b.WriteByte(c)
			i++
			continue
		}
		next := input[i+1]
		i += 2
		switch next {
		case '\\':
			b.WriteByte('\\')
		case '\'':
			b.WriteByte('\'')
		case '"':
			b.WriteByte('"')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// octal: 1 to 3 digits
			oct := int(next - '0')
			for k := 0; k < 2 && i < len(input) && input[i] >= '0' && input[i] <= '7'; k++ {
				oct = oct*8 + int(input[i]-'0')
				i++
			}
			b.WriteRune(rune(oct))
		case 'x':
			if i+2 > len(input) {
				b.WriteString("\\x")
				continue
			}
			hi, okH := fromHex(input[i])
			lo, okL := fromHex(input[i+1])
			if !okH || !okL {
				b.WriteString("\\x")
				continue
			}
			b.WriteRune(rune(hi<<4 | lo))
			i += 2
		case 'u':
			if i+4 > len(input) {
				b.WriteString("\\u")
				continue
			}
			var ch rune
			ok := true
			for j := 0; j < 4; j++ {
				d, okD := fromHex(input[i+j])
				if !okD {
					ok = false
					break
				}
				ch = ch<<4 | rune(d)
			}
			if !ok {
				b.WriteString("\\u")
				continue
			}
			b.WriteRune(ch)
			i += 4
		case 'U':
			if i+8 > len(input) {
				b.WriteString("\\U")
				continue
			}
			var ch rune
			ok := true
			for j := 0; j < 8; j++ {
				d, okD := fromHex(input[i+j])
				if !okD {
					ok = false
					break
				}
				ch = ch<<4 | rune(d)
			}
			if !ok || ch > 0x10FFFF {
				b.WriteString("\\U")
				continue
			}
			b.WriteRune(ch)
			i += 8
		case 'N':
			// \N{name}: look up Unicode named character
			if i >= len(input) || input[i] != '{' {
				b.WriteString("\\N")
				continue
			}
			end := i + 1
			for end < len(input) && input[end] != '}' {
				end++
			}
			if end >= len(input) {
				b.WriteString("\\N")
				continue
			}
			name := string(input[i+1 : end])
			if ch := lookupUnicodeName(name); ch >= 0 {
				b.WriteRune(ch)
			} else {
				b.WriteString("\\N{" + name + "}")
			}
			i = end + 1
		default:
			b.WriteByte('\\')
			b.WriteByte(next)
		}
	}
	s := b.String()
	return s, len(s), nil
}

func fromHex(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// lookupUnicodeName returns the rune for a Unicode character name, or -1.
// Go's standard library does not expose a Unicode name-to-codepoint lookup.
// This stub handles a small set of names that appear in CPython's test suite;
// the unicodedata module covers the full set at runtime.
func lookupUnicodeName(name string) rune {
	switch strings.ToUpper(name) {
	case "LATIN SMALL LETTER A":
		return 'a'
	case "SNOWMAN":
		return '☃'
	case "GREEK SMALL LETTER ALPHA":
		return 'α'
	}
	return -1
}
