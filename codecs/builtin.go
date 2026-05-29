// Built-in codecs for utf-8, ascii, and latin-1 (iso-8859-1).
// Each codec is registered at init time so that Lookup("utf-8") etc.
// resolve without an external search function.
//
// CPython: Lib/encodings/utf_8.py, ascii.py, latin_1.py
// CPython: Python/codecs.c:L160 PyCodec_Encode / L195 PyCodec_Decode
package codecs

import (
	"strings"
	"unicode/utf8"
)

func init() {
	Register(builtinSearch)
}

// Encode looks up encoding, then calls its Encode function.
//
// CPython: Python/codecs.c:L160 PyCodec_Encode
func Encode(input, encoding, errors string) (out []byte, n int, err error) {
	ci, err := Lookup(encoding)
	if err != nil {
		return nil, 0, err
	}
	return ci.Encode(input, errors)
}

// Decode looks up encoding, then calls its Decode function.
//
// CPython: Python/codecs.c:L195 PyCodec_Decode
func Decode(input []byte, encoding, errors string) (out string, n int, err error) {
	ci, err := Lookup(encoding)
	if err != nil {
		return "", 0, err
	}
	return ci.Decode(input, errors)
}

// builtinSearch is the built-in codec search function. It recognizes
// the standard name aliases for utf-8, ascii, latin-1, the utf-16 /
// utf-32 BOM-stripping pairs, and the small set of charmap codecs the
// stdlib trips over (iso-8859-15, cp1252, cp1250, cp1251, cp437,
// mac-roman).
//
// CPython: Modules/_codecsmodule.c builtin codec table
// CPython: Lib/encodings/aliases.py aliases
func builtinSearch(name string) (*CodecInfo, error) {
	switch name {
	case "utf_8", "utf8", "u8", "utf", "cp65001":
		return utf8Codec, nil
	case "ascii", "us_ascii", "646", "ansi_x3_4_1968":
		return asciiCodec, nil
	case "latin_1", "latin1", "iso_8859_1", "iso8859_1",
		"iso_8859_1_1987", "l1", "ibm819", "cp819", "csisolatin1",
		"iso_ir_100", "8859":
		return latin1Codec, nil
	case "unicode_escape":
		return unicodeEscapeCodec, nil
	case "raw_unicode_escape":
		return rawUnicodeEscapeCodec, nil
	case "utf_16", "utf16", "u16":
		return utf16Codec, nil
	case "utf_16_le", "utf16le":
		return utf16LECodec, nil
	case "utf_16_be", "utf16be":
		return utf16BECodec, nil
	case "utf_32", "utf32", "u32":
		return utf32Codec, nil
	case "utf_32_le", "utf32le":
		return utf32LECodec, nil
	case "utf_32_be", "utf32be":
		return utf32BECodec, nil
	case "iso8859_15", "iso_8859_15", "iso_8859_15_1998", "latin9", "l9":
		return iso8859_15.Info(), nil
	case "cp1252", "windows_1252", "1252":
		return cp1252.Info(), nil
	case "cp1250", "windows_1250", "1250":
		return cp1250.Info(), nil
	case "cp1251", "windows_1251", "1251":
		return cp1251.Info(), nil
	case "cp437", "ibm437", "437":
		return cp437.Info(), nil
	case "mac_roman", "macroman", "macintosh":
		return macRoman.Info(), nil
	}
	return nil, nil
}

var utf8Codec = &CodecInfo{
	Name:              "utf-8",
	Encode:            encodeUTF8,
	Decode:            decodeUTF8,
	IncrementalDecode: decodeUTF8Incremental,
	IsTextEncoding:    true,
}

var asciiCodec = &CodecInfo{
	Name:           "ascii",
	Encode:         encodeASCII,
	Decode:         decodeASCII,
	IsTextEncoding: true,
}

var latin1Codec = &CodecInfo{
	Name:           "iso8859-1",
	Encode:         encodeLatin1,
	Decode:         decodeLatin1,
	IsTextEncoding: true,
}

// encodeUTF8 encodes a string to UTF-8 bytes. All Go strings are
// valid UTF-8, so the error handler is only invoked for surrogates.
//
// CPython: Objects/unicodeobject.c:L6048 PyUnicode_AsUTF8AndSize
func encodeUTF8(input, errors string) (out []byte, n int, err error) {
	var b strings.Builder
	i := 0
	runes := lenientRunes([]byte(input))
	for i < len(runes) {
		r := runes[i]
		if utf8.ValidRune(r) {
			b.WriteRune(r)
			i++
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, newpos, herr := handler("utf-8", "encode:surrogates not allowed", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		b.WriteString(rep)
		if newpos > i {
			i = newpos
		} else {
			i++
		}
	}
	result := []byte(b.String())
	return result, len(result), nil
}

// decodeUTF8 decodes UTF-8 bytes to a string, invoking the error
// handler for invalid sequences.
//
// decodeUTF8 decodes a UTF-8 encoded byte slice with CPython-compatible
// error recovery. Each invalid byte or invalid sequence is replaced by one
// error handler call, grouping a start byte with its valid continuation
// bytes into a single error per Unicode TR3-8 Table 3-8 Row 2.
//
// CPython: Objects/unicodeobject.c:4756 PyUnicode_DecodeUTF8Stateful
func decodeUTF8(input []byte, errors string) (out string, n int, err error) {
	var b strings.Builder
	i := 0
	for i < len(input) {
		c := input[i]

		// ASCII fast path.
		if c < 0x80 {
			b.WriteByte(c)
			i++
			continue
		}

		// Determine the error region and reason using the CPython algorithm.
		// For valid start bytes (0xC2-0xF4), consume as many valid continuation
		// bytes as possible and treat the whole group as one error if the
		// sequence is incomplete or invalid.
		errStart := i
		errEnd := i + 1
		var reason string

		switch {
		case c < 0xC2:
			// 0x80-0xBF: orphan continuation byte.
			// 0xC0-0xC1: overlong 2-byte sequence (always invalid).
			// Single-byte error.
			reason = "invalid start byte"

		case c < 0xE0:
			// 0xC2-0xDF: 2-byte sequence. Consume one continuation byte if present.
			if i+1 < len(input) && input[i+1] >= 0x80 && input[i+1] < 0xC0 {
				cp := rune(c&0x1F)<<6 | rune(input[i+1]&0x3F)
				b.WriteRune(cp)
				i += 2
				continue
			}
			// Truncated: just the start byte is the error.
			if i+1 >= len(input) {
				reason = "unexpected end of data"
			} else {
				reason = "invalid continuation byte"
			}

		case c < 0xF0:
			// 0xE0-0xEF: 3-byte sequence.
			// Some start bytes have restricted valid ranges for the first
			// continuation byte (to prevent overlong sequences or surrogates):
			// 0xE0: first cb must be 0xA0-0xBF (0x80-0x9F would be overlong).
			// 0xED: first cb must be 0x80-0x9F (0xA0-0xBF would be a surrogate).
			// CPython: Objects/unicodeobject.c:4756 (utf8_decode inner loop)
			var cb1Lo, cb1Hi byte = 0x80, 0xBF
			switch c {
			case 0xE0:
				cb1Lo = 0xA0
			case 0xED:
				cb1Hi = 0x9F
			}
			if i+1 >= len(input) {
				reason = "unexpected end of data"
				break
			}
			if input[i+1] < cb1Lo || input[i+1] > cb1Hi {
				// First cb is out of valid sub-range; only the start byte is
				// in the error region. The first cb is re-processed separately.
				reason = "invalid continuation byte"
				break
			}
			// First cb is valid; try to consume second cb.
			if i+2 < len(input) && input[i+2] >= 0x80 && input[i+2] < 0xC0 {
				// All 3 bytes present and valid.
				cp := rune(c&0x0F)<<12 | rune(input[i+1]&0x3F)<<6 | rune(input[i+2]&0x3F)
				b.WriteRune(cp)
				i += 3
				continue
			}
			// Second cb missing or invalid; error covers start + first cb.
			errEnd = i + 2
			if i+2 >= len(input) {
				reason = "unexpected end of data"
			} else {
				reason = "invalid continuation byte"
			}

		case c < 0xF5:
			// 0xF0-0xF4: 4-byte sequence.
			// Restricted first-cb ranges:
			// 0xF0: first cb must be 0x90-0xBF (0x80-0x8F would be overlong).
			// 0xF4: first cb must be 0x80-0x8F (0x90-0xBF would exceed U+10FFFF).
			// CPython: Objects/unicodeobject.c:4756 (utf8_decode inner loop)
			var cb1Lo4, cb1Hi4 byte = 0x80, 0xBF
			switch c {
			case 0xF0:
				cb1Lo4 = 0x90
			case 0xF4:
				cb1Hi4 = 0x8F
			}
			if i+1 >= len(input) {
				reason = "unexpected end of data"
				break
			}
			if input[i+1] < cb1Lo4 || input[i+1] > cb1Hi4 {
				reason = "invalid continuation byte"
				break
			}
			// First cb is valid; consume additional continuation bytes.
			j := i + 2
			for j < len(input) && j < i+4 && input[j] >= 0x80 && input[j] < 0xC0 {
				j++
			}
			if j == i+4 {
				cp := rune(c&0x07)<<18 | rune(input[i+1]&0x3F)<<12 |
					rune(input[i+2]&0x3F)<<6 | rune(input[i+3]&0x3F)
				if cp >= 0x10000 && cp <= 0x10FFFF {
					b.WriteRune(cp)
					i += 4
					continue
				}
			}
			errEnd = j
			if j >= len(input) {
				reason = "unexpected end of data"
			} else {
				reason = "invalid continuation byte"
			}

		default:
			// 0xF5-0xFF: always single-byte error.
			reason = "invalid start byte"
		}

		handler, herr := LookupError(errors)
		if herr != nil {
			return "", i, herr
		}
		rep, newpos, herr := handler("utf-8", reason, input, errStart, errEnd)
		if herr != nil {
			return "", i, herr
		}
		b.WriteString(rep)
		i = newpos
	}
	s := b.String()
	return s, len(s), nil
}

// decodeUTF8Incremental is like decodeUTF8 but, when final=false,
// holds back trailing bytes that form an incomplete multibyte sequence
// rather than calling the error handler.
//
// CPython: Objects/unicodeobject.c:4756 PyUnicode_DecodeUTF8Stateful
// DecodeUTF8Incremental is the exported entry point for module/_codecs.
//
// CPython: Objects/unicodeobject.c:4756 PyUnicode_DecodeUTF8Stateful
func DecodeUTF8Incremental(input []byte, errors string, final bool) (string, []byte, error) {
	return decodeUTF8Incremental(input, errors, final)
}

func decodeUTF8Incremental(input []byte, errors string, final bool) (string, []byte, error) {
	if final {
		out, _, err := decodeUTF8(input, errors)
		return out, nil, err
	}
	// Find the longest valid UTF-8 prefix, trimming any trailing incomplete sequence.
	trim := utf8IncompleteTrail(input)
	safe := input[:len(input)-trim]
	out, _, err := decodeUTF8(safe, errors)
	if err != nil {
		return "", nil, err
	}
	return out, input[len(input)-trim:], nil
}

// utf8IncompleteTrail returns the number of trailing bytes that form
// an incomplete UTF-8 multibyte sequence. Returns 0 if input ends on
// a complete sequence boundary.
func utf8IncompleteTrail(b []byte) int {
	n := len(b)
	if n == 0 {
		return 0
	}
	// Walk back at most 3 bytes looking for a leading byte followed by
	// too few continuation bytes.
	for i := n - 1; i >= n-4 && i >= 0; i-- {
		c := b[i]
		if c < 0x80 {
			return 0 // ASCII byte — everything before is complete
		}
		if c >= 0xC0 {
			// c is a leading byte. Check how many continuation bytes it needs.
			var need int
			switch {
			case c < 0xE0:
				need = 2
			case c < 0xF0:
				need = 3
			default:
				need = 4
			}
			have := n - i
			if have < need {
				return have // incomplete sequence
			}
			return 0
		}
		// c is a continuation byte (0x80..0xBF); keep looking back.
	}
	return 0
}

// encodeASCII encodes a string to ASCII bytes, failing on any
// character outside U+0000..U+007F.
//
// CPython: Objects/unicodeobject.c:L6420 PyUnicode_EncodeASCII
func encodeASCII(input, errors string) (out []byte, n int, err error) {
	runes := lenientRunes([]byte(input))
	var result []byte
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r < 0x80 {
			result = append(result, byte(r))
			i++
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		// Pass UTF-8 bytes of the input string with rune positions so the
		// error handler can resolve the original Unicode codepoint.
		rep, newpos, herr := handler("ascii", "encode:ordinal not in range(128)", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		result = append(result, []byte(rep)...)
		if newpos > i {
			i = newpos
		} else {
			i++
		}
	}
	return result, len(result), nil
}

// decodeASCII decodes ASCII bytes to a string, failing on any byte
// with the high bit set.
//
// CPython: Objects/unicodeobject.c:L4437 PyUnicode_DecodeASCII
func decodeASCII(input []byte, errors string) (out string, n int, err error) {
	var b strings.Builder
	i := 0
	for i < len(input) {
		c := input[i]
		if c < 0x80 {
			b.WriteByte(c)
			i++
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return "", i, herr
		}
		rep, newpos, herr := handler("ascii", "ordinal not in range(128)", input, i, i+1)
		if herr != nil {
			return "", i, herr
		}
		b.WriteString(rep)
		i = newpos
	}
	s := b.String()
	return s, len(s), nil
}

// encodeLatin1 encodes a string to latin-1 (iso-8859-1) bytes. Each
// rune must fit in U+0000..U+00FF; wider runes are handled by errors.
//
// CPython: Objects/unicodeobject.c:L6547 PyUnicode_AsLatin1String
func encodeLatin1(input, errors string) (out []byte, n int, err error) {
	runes := lenientRunes([]byte(input))
	var result []byte
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r <= 0xFF {
			result = append(result, byte(r))
			i++
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, newpos, herr := handler("iso8859-1", "encode:ordinal not in range(256)", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		result = append(result, []byte(rep)...)
		if newpos > i {
			i = newpos
		} else {
			i++
		}
	}
	return result, len(result), nil
}

// decodeLatin1 decodes latin-1 (iso-8859-1) bytes. Bytes 0x00..0xFF
// map directly to U+0000..U+00FF; this codec is infallible.
//
// CPython: Objects/unicodeobject.c:L4547 PyUnicode_DecodeLatin1
func decodeLatin1(input []byte, _ string) (out string, n int, err error) {
	runes := make([]rune, len(input))
	for i, b := range input {
		runes[i] = rune(b)
	}
	s := string(runes)
	return s, len(s), nil
}
