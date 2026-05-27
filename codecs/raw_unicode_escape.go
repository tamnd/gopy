// raw-unicode-escape codec. Codepoints < 0x100 round-trip as the
// matching byte; wider codepoints are emitted as \uHHHH or \UHHHHHHHH.
// Decoding reverses the transform: \u / \U sequences become the named
// codepoint, every other byte becomes a codepoint with its raw byte
// value, and any other backslash escape stays literal (the backslash
// is preserved verbatim).
//
// Pickle proto 0 leans on this codec to encode str payloads with
// non-ASCII content into a printable form on the wire, so the codec
// needs to be wired before save_str's encode call.
//
// CPython: Objects/unicodeobject.c:7043 _PyUnicode_DecodeRawUnicodeEscapeStateful
// CPython: Objects/unicodeobject.c:7188 PyUnicode_AsRawUnicodeEscapeString

package codecs

import (
	"fmt"
	"strings"
)

var rawUnicodeEscapeCodec = &CodecInfo{
	Name:   "raw-unicode-escape",
	Encode: encodeRawUnicodeEscape,
	Decode: decodeRawUnicodeEscape,
}

// encodeRawUnicodeEscape mirrors PyUnicode_AsRawUnicodeEscapeString.
// 0x00..0xFF emit as a single byte; 0x100..0xFFFF emit as \uHHHH;
// 0x10000..0x10FFFF emit as \UHHHHHHHH. The error handler is never
// invoked because every codepoint < 0x110000 is representable.
//
// CPython: Objects/unicodeobject.c:7188 PyUnicode_AsRawUnicodeEscapeString
func encodeRawUnicodeEscape(input, _ string) ([]byte, int, error) {
	var out []byte
	nChars := 0
	for _, r := range input {
		nChars++
		switch {
		case r < 0x100:
			out = append(out, byte(r))
		case r < 0x10000:
			out = append(out, []byte(fmt.Sprintf("\\u%04x", r))...)
		default:
			out = append(out, []byte(fmt.Sprintf("\\U%08x", r))...)
		}
	}
	return out, nChars, nil
}

// decodeRawUnicodeEscape mirrors _PyUnicode_DecodeRawUnicodeEscapeStateful.
// Only the \u and \U escapes are special; any other byte (including a
// backslash followed by something else) is reproduced literally with
// its raw byte value as the resulting codepoint.
//
// CPython: Objects/unicodeobject.c:7043 _PyUnicode_DecodeRawUnicodeEscapeStateful
func decodeRawUnicodeEscape(input []byte, errors string) (string, int, error) {
	var b strings.Builder
	i := 0
	for i < len(input) {
		c := input[i]
		if c != '\\' || i+1 >= len(input) {
			b.WriteRune(rune(c))
			i++
			continue
		}
		next := input[i+1]
		var count int
		var truncMsg string
		switch next {
		case 'u':
			count = 4
			truncMsg = "truncated \\uXXXX escape"
		case 'U':
			count = 8
			truncMsg = "truncated \\UXXXXXXXX escape"
		default:
			// Non-uU backslash escape: keep the backslash and the
			// next byte verbatim (each as its raw byte codepoint).
			b.WriteRune(rune(c))
			i++
			continue
		}
		// Scan up to `count` hex digits, stopping early on non-hex or end of input.
		// CPython scans character-by-character: non-hex char → "bad hex digit" error
		// ending at that position; input too short → "truncated" ending at EOF.
		var ch rune
		badAt := -1
		available := len(input) - (i + 2)
		scan := count
		if available < scan {
			scan = available
		}
		for j := 0; j < scan; j++ {
			hx := input[i+2+j]
			var d rune
			switch {
			case hx >= '0' && hx <= '9':
				d = rune(hx - '0')
			case hx >= 'a' && hx <= 'f':
				d = rune(hx-'a') + 10
			case hx >= 'A' && hx <= 'F':
				d = rune(hx-'A') + 10
			default:
				badAt = j
			}
			if badAt >= 0 {
				break
			}
			ch = (ch << 4) | d
		}
		if badAt >= 0 {
			// Non-hex character at position i+2+badAt.
			rep, newpos, herr := handleRawUnicodeEscapeError(input, i, i+2+badAt, truncMsg, errors)
			if herr != nil {
				return "", 0, herr
			}
			b.WriteString(rep)
			i = newpos
			continue
		}
		if available < count {
			// Input ran out before we got `count` hex digits.
			rep, newpos, herr := handleRawUnicodeEscapeError(input, i, len(input), truncMsg, errors)
			if herr != nil {
				return "", 0, herr
			}
			b.WriteString(rep)
			i = newpos
			continue
		}
		if ch > 0x10FFFF {
			rep, newpos, herr := handleRawUnicodeEscapeError(input, i, i+2+count, "illegal Unicode character", errors)
			if herr != nil {
				return "", 0, herr
			}
			b.WriteString(rep)
			i = newpos
			continue
		}
		b.WriteRune(ch)
		i += 2 + count
	}
	return b.String(), len(input), nil
}

// handleRawUnicodeEscapeError routes a malformed escape through the
// named error handler. Mirrors the unicode_decode_call_errorhandler
// branch reached from the stateful decoder's "incomplete:" label.
//
// CPython: Objects/unicodeobject.c:7043 (incomplete: label)
func handleRawUnicodeEscapeError(input []byte, start, end int, reason, errors string) (string, int, error) {
	handler, herr := LookupError(errors)
	if herr != nil {
		return "", 0, herr
	}
	rep, newpos, herr := handler("rawunicodeescape", reason, input, start, end)
	if herr != nil {
		return "", 0, herr
	}
	return rep, newpos, nil
}
