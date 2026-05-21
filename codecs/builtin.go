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
// the standard name aliases for utf-8, ascii, and latin-1.
//
// CPython: Modules/_codecsmodule.c builtin codec table
func builtinSearch(name string) (*CodecInfo, error) {
	switch name {
	case "utf_8", "utf8", "u8", "utf":
		return utf8Codec, nil
	case "ascii", "us_ascii", "646", "ansi_x3_4_1968":
		return asciiCodec, nil
	case "latin_1", "latin1", "iso_8859_1", "iso8859_1",
		"iso_8859_1_1987", "l1", "ibm819", "cp819", "csisolatin1",
		"iso_ir_100", "8859":
		return latin1Codec, nil
	case "raw_unicode_escape":
		return rawUnicodeEscapeCodec, nil
	}
	return nil, nil
}

var utf8Codec = &CodecInfo{
	Name:   "utf-8",
	Encode: encodeUTF8,
	Decode: decodeUTF8,
}

var asciiCodec = &CodecInfo{
	Name:   "ascii",
	Encode: encodeASCII,
	Decode: decodeASCII,
}

var latin1Codec = &CodecInfo{
	Name:   "iso-8859-1",
	Encode: encodeLatin1,
	Decode: decodeLatin1,
}

// encodeUTF8 encodes a string to UTF-8 bytes. All Go strings are
// valid UTF-8, so the error handler is only invoked for surrogates.
//
// CPython: Objects/unicodeobject.c:L6048 PyUnicode_AsUTF8AndSize
func encodeUTF8(input, errors string) (out []byte, n int, err error) {
	var b strings.Builder
	i := 0
	runes := []rune(input)
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
		rep, newpos, herr := handler("utf-8", "surrogates not allowed", []byte(string(r)), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		b.WriteString(rep)
		i = newpos
	}
	result := []byte(b.String())
	return result, len(result), nil
}

// decodeUTF8 decodes UTF-8 bytes to a string, invoking the error
// handler for invalid sequences.
//
// CPython: Objects/unicodeobject.c:L4756 PyUnicode_DecodeUTF8Stateful
func decodeUTF8(input []byte, errors string) (out string, n int, err error) {
	var b strings.Builder
	i := 0
	for i < len(input) {
		r, size := utf8.DecodeRune(input[i:])
		if r == utf8.RuneError && size == 1 {
			handler, herr := LookupError(errors)
			if herr != nil {
				return "", i, herr
			}
			rep, newpos, herr := handler("utf-8", "invalid start byte", input, i, i+1)
			if herr != nil {
				return "", i, herr
			}
			b.WriteString(rep)
			i = newpos
			continue
		}
		b.WriteRune(r)
		i += size
	}
	s := b.String()
	return s, len(s), nil
}

// encodeASCII encodes a string to ASCII bytes, failing on any
// character outside U+0000..U+007F.
//
// CPython: Objects/unicodeobject.c:L6420 PyUnicode_EncodeASCII
func encodeASCII(input, errors string) (out []byte, n int, err error) {
	var result []byte
	for i, r := range input {
		if r < 0x80 {
			result = append(result, byte(r))
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, _, herr := handler("ascii", "ordinal not in range(128)", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		result = append(result, []byte(rep)...)
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
	var result []byte
	for i, r := range input {
		if r <= 0xFF {
			result = append(result, byte(r))
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, _, herr := handler("iso-8859-1", "ordinal not in range(256)", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		result = append(result, []byte(rep)...)
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
