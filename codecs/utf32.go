// UTF-32 codec family: utf-32, utf-32-le, utf-32-be. Mirrors the
// utf-16 codec layout: the bare utf-32 codec emits a 4-byte BOM and
// encodes the body little-endian; decode strips the BOM and dispatches
// to LE or BE.
//
// CPython: Modules/_codecs/utf_32.c _PyUnicode_DecodeUTF32Stateful
// CPython: Modules/_codecs/utf_32.c _PyUnicode_EncodeUTF32

package codecs

import (
	"encoding/binary"
	"unicode/utf8"
)

var (
	utf32Codec   = &CodecInfo{Name: "utf-32", Encode: encodeUTF32BOM, Decode: decodeUTF32BOM}
	utf32LECodec = &CodecInfo{Name: "utf-32-le", Encode: encodeUTF32LE, Decode: decodeUTF32LE}
	utf32BECodec = &CodecInfo{Name: "utf-32-be", Encode: encodeUTF32BE, Decode: decodeUTF32BE}
)

func encodeUTF32BOM(input, errors string) ([]byte, int, error) {
	body, _, err := encodeUTF32Body(input, errors, binary.LittleEndian)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, 0, len(body)+4)
	out = append(out, 0xFF, 0xFE, 0x00, 0x00)
	out = append(out, body...)
	return out, len(out), nil
}

func encodeUTF32LE(input, errors string) ([]byte, int, error) {
	return encodeUTF32Body(input, errors, binary.LittleEndian)
}

func encodeUTF32BE(input, errors string) ([]byte, int, error) {
	return encodeUTF32Body(input, errors, binary.BigEndian)
}

// encodeUTF32Body writes one 32-bit code unit per rune. Surrogates trip
// the error handler (CPython rejects them unless surrogatepass).
func encodeUTF32Body(input, errors string, bo binary.ByteOrder) ([]byte, int, error) {
	runes := []rune(input)
	for i, r := range runes {
		if r < 0xD800 || r > 0xDFFF {
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, _, herr := handler("utf-32", "surrogates not allowed", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		repRune, _ := utf8.DecodeRuneInString(rep)
		runes[i] = repRune
	}
	buf := make([]byte, 4*len(runes))
	for i, r := range runes {
		bo.PutUint32(buf[4*i:], uint32(r))
	}
	return buf, len(buf), nil
}

func decodeUTF32BOM(input []byte, errors string) (string, int, error) {
	if len(input) >= 4 {
		switch {
		case input[0] == 0xFF && input[1] == 0xFE && input[2] == 0x00 && input[3] == 0x00:
			return decodeUTF32Body(input[4:], errors, binary.LittleEndian)
		case input[0] == 0x00 && input[1] == 0x00 && input[2] == 0xFE && input[3] == 0xFF:
			return decodeUTF32Body(input[4:], errors, binary.BigEndian)
		}
	}
	return decodeUTF32Body(input, errors, binary.LittleEndian)
}

func decodeUTF32LE(input []byte, errors string) (string, int, error) {
	return decodeUTF32Body(input, errors, binary.LittleEndian)
}

func decodeUTF32BE(input []byte, errors string) (string, int, error) {
	return decodeUTF32Body(input, errors, binary.BigEndian)
}

func decodeUTF32Body(input []byte, errors string, bo binary.ByteOrder) (string, int, error) {
	runes := make([]rune, 0, len(input)/4)
	i := 0
	for i+3 < len(input) {
		c := bo.Uint32(input[i:])
		if c > 0x10FFFF || (c >= 0xD800 && c <= 0xDFFF) {
			handler, herr := LookupError(errors)
			if herr != nil {
				return "", i, herr
			}
			rep, newpos, herr := handler("utf-32", "code point out of range", input, i, i+4)
			if herr != nil {
				return "", i, herr
			}
			runes = append(runes, []rune(rep)...)
			i = newpos
			continue
		}
		runes = append(runes, rune(c))
		i += 4
	}
	out := string(runes)
	if i < len(input) {
		handler, herr := LookupError(errors)
		if herr != nil {
			return "", i, herr
		}
		rep, _, herr := handler("utf-32", "truncated data", input, i, len(input))
		if herr != nil {
			return "", i, herr
		}
		out += rep
	}
	return out, len(out), nil
}
