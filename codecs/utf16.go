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
	"unicode/utf8"
)

var (
	utf16Codec = &CodecInfo{
		Name: "utf-16", Encode: encodeUTF16BOM, Decode: decodeUTF16BOM,
		NewIncrementalDecoder: newUTF16BOMIncrementalDecoder,
		NewIncrementalEncoder: newUTF16BOMIncrementalEncoder,
		IsTextEncoding:        true,
	}
	utf16LECodec = &CodecInfo{
		Name: "utf-16-le", Encode: encodeUTF16LE, Decode: decodeUTF16LE,
		IncrementalDecode: utf16LEIncrementalDecode,
		IsTextEncoding:    true,
	}
	utf16BECodec = &CodecInfo{
		Name: "utf-16-be", Encode: encodeUTF16BE, Decode: decodeUTF16BE,
		IncrementalDecode: utf16BEIncrementalDecode,
		IsTextEncoding:    true,
	}
)

// encodeUTF16BOM prefixes the body with U+FEFF as a BOM. Body bytes
// follow the host order, which on every platform gopy targets is
// little-endian. CPython picks the byte order from the build host the
// same way.
//
// CPython: Modules/_codecs/utf_16.c _PyUnicode_EncodeUTF16 (byteorder == 0)
func encodeUTF16BOM(input, errors string) ([]byte, int, error) {
	body, n, err := encodeUTF16Body(input, errors, binary.LittleEndian)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, 0, len(body)+2)
	out = append(out, 0xFF, 0xFE)
	out = append(out, body...)
	return out, n, nil
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
		if r < 0xD800 || r > 0xDFFF {
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, _, herr := handler("utf-16", "surrogates not allowed", []byte(input), i, i+1)
		if herr != nil {
			return nil, i, herr
		}
		repRune, _ := utf8.DecodeRuneInString(rep)
		runes[i] = repRune
	}
	units := utf16.Encode(runes)
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		bo.PutUint16(buf[2*i:], u)
	}
	return buf, len(runes), nil
}

// newUTF16BOMIncrementalEncoder creates a stateful encoder that emits the
// LE BOM on the first encode() call and switches to BOM-less LE for the rest.
//
// CPython: Lib/encodings/utf_16.py StreamWriter.encode (self.encode reassignment trick)
func newUTF16BOMIncrementalEncoder() func(string, string, bool) ([]byte, int, error) {
	first := true
	return func(input, errors string, final bool) ([]byte, int, error) {
		if first {
			first = false
			return encodeUTF16BOM(input, errors)
		}
		return encodeUTF16LE(input, errors)
	}
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

// utf16LEIncrementalDecode buffers incomplete 2-byte code units and
// holds back an isolated high surrogate waiting for its low partner.
//
// CPython: Modules/_codecs/utf_16.c _PyUnicode_DecodeUTF16Stateful (stateful path)
func utf16LEIncrementalDecode(data []byte, errors string, final bool) (string, []byte, error) {
	return utf16IncrementalDecodeBody(data, errors, final, binary.LittleEndian, "utf-16-le")
}

func utf16BEIncrementalDecode(data []byte, errors string, final bool) (string, []byte, error) {
	return utf16IncrementalDecodeBody(data, errors, final, binary.BigEndian, "utf-16-be")
}

func utf16IncrementalDecodeBody(data []byte, errors string, final bool, bo binary.ByteOrder, name string) (string, []byte, error) {
	n := len(data)
	consumable := (n / 2) * 2
	if consumable == 0 {
		if final && n > 0 {
			_, herr := LookupError(errors)
			if herr != nil {
				return "", nil, herr
			}
			return "", nil, &UnicodeDecodeErr{Encoding: name, Object: data, Start: 0, End: n, Reason: "truncated data"}
		}
		return "", data, nil
	}
	// If last complete unit is a high surrogate and more data may follow, hold it.
	if !final {
		lastUnit := bo.Uint16(data[consumable-2:])
		if lastUnit >= 0xD800 && lastUnit < 0xDC00 {
			consumable -= 2
			if consumable == 0 {
				return "", data, nil
			}
		}
	}
	units := make([]uint16, consumable/2)
	for i := range units {
		units[i] = bo.Uint16(data[i*2:])
	}
	out := string(utf16.Decode(units))
	if final && consumable < n {
		// Trailing truncated byte(s)
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

// newUTF16BOMIncrementalDecoder creates a stateful incremental decoder for
// bare utf-16 that detects the BOM once and then uses the fixed-endian path.
//
// CPython: Lib/encodings/utf_16.py IncrementalDecoder
func newUTF16BOMIncrementalDecoder() func([]byte, string, bool) (string, []byte, error) {
	var bo binary.ByteOrder
	var bomConsumed bool
	return func(data []byte, errors string, final bool) (string, []byte, error) {
		if !bomConsumed {
			// Accumulate until we have at least 2 bytes to detect BOM
			if len(data) < 2 {
				if final {
					if len(data) == 0 {
						return "", nil, nil
					}
					return "", nil, &UnicodeDecodeErr{Encoding: "utf-16", Object: data, Start: 0, End: len(data), Reason: "truncated data"}
				}
				return "", data, nil
			}
			if data[0] == 0xFF && data[1] == 0xFE {
				bo = binary.LittleEndian
				data = data[2:]
			} else if data[0] == 0xFE && data[1] == 0xFF {
				bo = binary.BigEndian
				data = data[2:]
			} else {
				bo = binary.LittleEndian
			}
			bomConsumed = true
		}
		return utf16IncrementalDecodeBody(data, errors, final, bo, "utf-16")
	}
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
