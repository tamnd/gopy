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
	utf32Codec = &CodecInfo{
		Name: "utf-32", Encode: encodeUTF32BOM, Decode: decodeUTF32BOM,
		NewIncrementalDecoder: newUTF32BOMIncrementalDecoder,
		NewIncrementalEncoder: newUTF32BOMIncrementalEncoder,
		IsTextEncoding:        true,
	}
	utf32LECodec = &CodecInfo{
		Name: "utf-32-le", Encode: encodeUTF32LE, Decode: decodeUTF32LE,
		IncrementalDecode: utf32LEIncrementalDecode,
		IsTextEncoding:    true,
	}
	utf32BECodec = &CodecInfo{
		Name: "utf-32-be", Encode: encodeUTF32BE, Decode: decodeUTF32BE,
		IncrementalDecode: utf32BEIncrementalDecode,
		IsTextEncoding:    true,
	}
)

func encodeUTF32BOM(input, errors string) ([]byte, int, error) {
	body, n, err := encodeUTF32Body(input, errors, binary.LittleEndian)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, 0, len(body)+4)
	out = append(out, 0xFF, 0xFE, 0x00, 0x00)
	out = append(out, body...)
	return out, n, nil
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
	return buf, len(runes), nil
}

// newUTF32BOMIncrementalEncoder creates a stateful encoder that emits the
// LE BOM on the first call and switches to BOM-less LE for subsequent calls.
//
// CPython: Lib/encodings/utf_32.py StreamWriter.encode (self.encode reassignment trick)
func newUTF32BOMIncrementalEncoder() func(string, string, bool) ([]byte, int, error) {
	first := true
	return func(input, errors string, final bool) ([]byte, int, error) {
		if first {
			first = false
			return encodeUTF32BOM(input, errors)
		}
		return encodeUTF32LE(input, errors)
	}
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

// utf32LEIncrementalDecode buffers 1-3 trailing bytes that don't form a
// complete 4-byte code unit yet.
//
// CPython: Modules/_codecs/utf_32.c _PyUnicode_DecodeUTF32Stateful (stateful path)
func utf32LEIncrementalDecode(data []byte, errors string, final bool) (string, []byte, error) {
	return utf32IncrementalDecodeBody(data, errors, final, binary.LittleEndian, "utf-32-le")
}

func utf32BEIncrementalDecode(data []byte, errors string, final bool) (string, []byte, error) {
	return utf32IncrementalDecodeBody(data, errors, final, binary.BigEndian, "utf-32-be")
}

func utf32IncrementalDecodeBody(data []byte, errors string, final bool, bo binary.ByteOrder, name string) (string, []byte, error) {
	n := len(data)
	consumable := (n / 4) * 4
	if consumable == 0 {
		if final && n > 0 {
			return "", nil, &UnicodeDecodeErr{Encoding: name, Object: data, Start: 0, End: n, Reason: "truncated data"}
		}
		return "", data, nil
	}
	out, _, err := decodeUTF32Body(data[:consumable], errors, bo)
	if err != nil {
		return "", nil, err
	}
	if final && consumable < n {
		h, herr := LookupError(errors)
		if herr != nil {
			return "", nil, herr
		}
		rep, _, herr := h(name, "truncated data", data, consumable, n)
		if herr != nil {
			return "", nil, &UnicodeDecodeErr{Encoding: name, Object: data, Start: consumable, End: n, Reason: "truncated data"}
		}
		out += rep
		return out, nil, nil
	}
	return out, data[consumable:], nil
}

// newUTF32BOMIncrementalDecoder creates a stateful incremental decoder for
// bare utf-32 that detects the BOM once then uses the fixed-endian path.
//
// CPython: Lib/encodings/utf_32.py IncrementalDecoder
func newUTF32BOMIncrementalDecoder() func([]byte, string, bool) (string, []byte, error) {
	var bo binary.ByteOrder
	var bomConsumed bool
	return func(data []byte, errors string, final bool) (string, []byte, error) {
		if !bomConsumed {
			if len(data) < 4 {
				if final {
					if len(data) == 0 {
						return "", nil, nil
					}
					return "", nil, &UnicodeDecodeErr{Encoding: "utf-32", Object: data, Start: 0, End: len(data), Reason: "truncated data"}
				}
				return "", data, nil
			}
			switch {
			case data[0] == 0xFF && data[1] == 0xFE && data[2] == 0x00 && data[3] == 0x00:
				bo = binary.LittleEndian
				data = data[4:]
			case data[0] == 0x00 && data[1] == 0x00 && data[2] == 0xFE && data[3] == 0xFF:
				bo = binary.BigEndian
				data = data[4:]
			default:
				bo = binary.LittleEndian
			}
			bomConsumed = true
		}
		return utf32IncrementalDecodeBody(data, errors, final, bo, "utf-32")
	}
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
