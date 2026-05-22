// UTF-16 codec family: utf-16, utf-16-le, utf-16-be. The bare utf-16
// codec emits a byte-order mark and encodes the body little-endian
// (matching CPython on x86); decode inspects the leading BOM and
// chooses endianness from it, defaulting to native (LE) when absent.
//
// CPython: Modules/_codecs/utf_16.c _PyUnicode_DecodeUTF16Stateful
// CPython: Modules/_codecs/utf_16.c _PyUnicode_EncodeUTF16

package codecs

import (
	"encoding/binary"
	"unicode/utf16"
)

var (
	utf16Codec   = &CodecInfo{Name: "utf-16", Encode: encodeUTF16BOM, Decode: decodeUTF16BOM}
	utf16LECodec = &CodecInfo{Name: "utf-16-le", Encode: encodeUTF16LE, Decode: decodeUTF16LE}
	utf16BECodec = &CodecInfo{Name: "utf-16-be", Encode: encodeUTF16BE, Decode: decodeUTF16BE}
)

// encodeUTF16BOM prefixes the body with U+FEFF as a BOM. Body bytes
// follow the host order, which on every platform gopy targets is
// little-endian. CPython picks the byte order from the build host the
// same way.
//
// CPython: Modules/_codecs/utf_16.c _PyUnicode_EncodeUTF16 (byteorder == 0)
func encodeUTF16BOM(input, errors string) ([]byte, int, error) {
	body, _, err := encodeUTF16Body(input, errors, binary.LittleEndian)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, 0, len(body)+2)
	out = append(out, 0xFF, 0xFE)
	out = append(out, body...)
	return out, len(out), nil
}

// encodeUTF16LE / encodeUTF16BE emit raw code units without a BOM.
//
// CPython: Modules/_codecs/utf_16.c _PyUnicode_EncodeUTF16 (byteorder == -1 / +1)
func encodeUTF16LE(input, errors string) ([]byte, int, error) {
	return encodeUTF16Body(input, errors, binary.LittleEndian)
}

func encodeUTF16BE(input, errors string) ([]byte, int, error) {
	return encodeUTF16Body(input, errors, binary.BigEndian)
}

// encodeUTF16Body converts runes to UTF-16 code units, surrogate-encoding
// non-BMP code points. The error handler is only consulted for runes that
// would land in the surrogate range without surrogatepass, which mirrors
// CPython's "surrogates not allowed" path.
func encodeUTF16Body(input, errors string, bo binary.ByteOrder) ([]byte, int, error) {
	runes := []rune(input)
	for i, r := range runes {
		if r >= 0xD800 && r <= 0xDFFF {
			handler, herr := LookupError(errors)
			if herr != nil {
				return nil, i, herr
			}
			rep, _, herr := handler("utf-16", "surrogates not allowed", []byte(input), i, i+1)
			if herr != nil {
				return nil, i, herr
			}
			runes[i] = []rune(rep)[0]
		}
	}
	units := utf16.Encode(runes)
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		bo.PutUint16(buf[2*i:], u)
	}
	return buf, len(buf), nil
}

// decodeUTF16BOM strips a BOM if present and dispatches to LE or BE.
// With no BOM the bytes are interpreted as native little-endian.
//
// CPython: Modules/_codecs/utf_16.c _PyUnicode_DecodeUTF16Stateful
func decodeUTF16BOM(input []byte, errors string) (string, int, error) {
	if len(input) >= 2 {
		switch {
		case input[0] == 0xFF && input[1] == 0xFE:
			return decodeUTF16Body(input[2:], errors, binary.LittleEndian)
		case input[0] == 0xFE && input[1] == 0xFF:
			return decodeUTF16Body(input[2:], errors, binary.BigEndian)
		}
	}
	return decodeUTF16Body(input, errors, binary.LittleEndian)
}

func decodeUTF16LE(input []byte, errors string) (string, int, error) {
	return decodeUTF16Body(input, errors, binary.LittleEndian)
}

func decodeUTF16BE(input []byte, errors string) (string, int, error) {
	return decodeUTF16Body(input, errors, binary.BigEndian)
}

// decodeUTF16Body reads code units and decodes surrogate pairs. A
// trailing odd byte invokes the error handler with reason "truncated
// data", matching CPython.
func decodeUTF16Body(input []byte, errors string, bo binary.ByteOrder) (string, int, error) {
	units := make([]uint16, 0, len(input)/2)
	i := 0
	for i+1 < len(input) {
		units = append(units, bo.Uint16(input[i:]))
		i += 2
	}
	out := string(utf16.Decode(units))
	if i < len(input) {
		handler, herr := LookupError(errors)
		if herr != nil {
			return "", i, herr
		}
		rep, _, herr := handler("utf-16", "truncated data", input, i, len(input))
		if herr != nil {
			return "", i, herr
		}
		out += rep
	}
	return out, len(out), nil
}
