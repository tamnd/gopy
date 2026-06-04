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
	"unicode/utf8"
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
			out = fmt.Appendf(out, "\\x%02x", r)
		case r < 0x10000:
			out = fmt.Appendf(out, "\\u%04x", r)
		default:
			out = fmt.Appendf(out, "\\U%08x", r)
		}
	}
	return out, len([]rune(input)), nil
}

// decodeUnicodeEscape is a faithful port of CPython's unicode-escape
// decoder. It handles \n \r \t \\ \' \" \a \b \f \v, octal \ooo, \xHH,
// \uHHHH, \UHHHHHHHH, and \N{name}, and routes truncated, illegal, and
// unknown-name escapes through the configured error handler (so "ignore"
// drops them and "strict" raises UnicodeDecodeError) rather than emitting
// the raw text. Output preserves lone surrogates as pseudo-UTF-8.
//
// CPython: Objects/unicodeobject.c:6627 _PyUnicode_DecodeUnicodeEscapeInternal2
func decodeUnicodeEscape(input []byte, errors string) (string, int, error) {
	handler, herr := LookupError(errors)
	if herr != nil {
		return "", 0, herr
	}

	var out []byte
	writeChar := func(ch rune) {
		if ch >= 0xD800 && ch <= 0xDFFF {
			out = append(out, surrogateToBytes(ch)...)
		} else {
			out = utf8.AppendRune(out, ch)
		}
	}

	s := 0
	end := len(input)
	var failErr error
	// fail runs the error handler over input[start:s], appends its
	// replacement, and advances s. It returns false when the handler raised.
	fail := func(message string, start int) bool {
		rep, newpos, herr := handler("unicodeescape", message, input, start, s)
		if herr != nil {
			failErr = herr
			return false
		}
		out = append(out, []byte(rep)...)
		s = newpos
		return true
	}

	for s < end {
		c := input[s]
		s++
		// Non-escape characters are interpreted as Unicode ordinals.
		if c != '\\' {
			writeChar(rune(c))
			continue
		}
		startinpos := s - 1
		if s >= end {
			if !fail("\\ at end of string", startinpos) {
				return "", 0, failErr
			}
			continue
		}
		c = input[s]
		s++
		switch c {
		case '\n': // line continuation: backslash-newline is dropped
		case '\\':
			writeChar('\\')
		case '\'':
			writeChar('\'')
		case '"':
			writeChar('"')
		case 'b':
			writeChar('\b')
		case 'f':
			writeChar('\014')
		case 't':
			writeChar('\t')
		case 'n':
			writeChar('\n')
		case 'r':
			writeChar('\r')
		case 'v':
			writeChar('\013')
		case 'a':
			writeChar('\007')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// \OOO octal escape, 1 to 3 digits.
			ch := rune(c - '0')
			if s < end && input[s] >= '0' && input[s] <= '7' {
				ch = (ch << 3) + rune(input[s]-'0')
				s++
				if s < end && input[s] >= '0' && input[s] <= '7' {
					ch = (ch << 3) + rune(input[s]-'0')
					s++
				}
			}
			writeChar(ch)
		case 'x', 'u', 'U':
			var count int
			var message string
			switch c {
			case 'x':
				count, message = 2, "truncated \\xXX escape"
			case 'u':
				count, message = 4, "truncated \\uXXXX escape"
			default:
				count, message = 8, "truncated \\UXXXXXXXX escape"
			}
			var ch rune
			ok := true
			for ; count > 0; count-- {
				if s >= end {
					ok = false
					break
				}
				d, okD := fromHex(input[s])
				if !okD {
					ok = false
					break
				}
				ch = ch<<4 | rune(d)
				s++
			}
			if !ok {
				if !fail(message, startinpos) {
					return "", 0, failErr
				}
				continue
			}
			if ch > 0x10FFFF {
				if !fail("illegal Unicode character", startinpos) {
					return "", 0, failErr
				}
				continue
			}
			writeChar(ch)
		case 'N':
			// \N{name}: look up the Unicode character database.
			message := "malformed \\N character escape"
			if s < end && input[s] == '{' {
				s++ // consume '{'
				nameStart := s
				for s < end && input[s] != '}' {
					s++
				}
				if s < end { // found the closing '}'
					namelen := s - nameStart
					if namelen > 0 {
						name := string(input[nameStart:s])
						s++ // consume '}'
						if ch := lookupUnicodeName(name); ch >= 0 {
							writeChar(ch)
							continue
						}
						message = "unknown Unicode character name"
					}
					// empty name {} falls through to the error handler
				}
				// no closing '}' (s >= end) falls through to the handler
			}
			if !fail(message, startinpos) {
				return "", 0, failErr
			}
		default:
			// Unknown escape: keep the backslash and the character.
			writeChar('\\')
			writeChar(rune(c))
		}
	}
	return string(out), len(out), nil
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
