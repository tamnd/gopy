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
	for _, r := range input {
		switch {
		case r < 0x100:
			out = append(out, byte(r))
		case r < 0x10000:
			out = append(out, []byte(fmt.Sprintf("\\u%04x", r))...)
		default:
			out = append(out, []byte(fmt.Sprintf("\\U%08x", r))...)
		}
	}
	return out, len(input), nil
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
		switch next {
		case 'u':
			count = 4
		case 'U':
			count = 8
		default:
			// Non-uU backslash escape: keep the backslash and the
			// next byte verbatim (each as its raw byte codepoint).
			b.WriteRune(rune(c))
			i++
			continue
		}
		if i+2+count > len(input) {
			rep, newpos, herr := handleRawUnicodeEscapeError(input, i, len(input), "truncated escape", errors)
			if herr != nil {
				return "", 0, herr
			}
			b.WriteString(rep)
			i = newpos
			continue
		}
		var ch rune
		ok := true
		for j := 0; j < count; j++ {
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
				ok = false
			}
			if !ok {
				break
			}
			ch = (ch << 4) | d
		}
		if !ok || ch > 0x10FFFF {
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
	s := b.String()
	return s, len(s), nil
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
	rep, newpos, herr := handler("rawunicodeescape", input, start, end)
	if herr != nil {
		return "", 0, fmt.Errorf("UnicodeDecodeError: 'rawunicodeescape' codec can't decode bytes in position %d-%d: %s", start, end-1, reason)
	}
	return rep, newpos, nil
}
