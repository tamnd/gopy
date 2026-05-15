// Stateful incremental decoders and encoders backing TextIOWrapper.
//
// CPython holds a codec-specific IncrementalDecoder / IncrementalEncoder
// on every TextIOWrapper (`Modules/_io/textio.c:912`
// `_textiowrapper_set_decoder`, `:968` `_textiowrapper_set_encoder`),
// so multi-byte sequences split across `buffer.read1` chunks decode
// correctly and tell/seek can snapshot the decoder state. Until
// 2026-05-15 gopy decoded each chunk independently via the
// `decodeBytes` switch in `textiowrapper.go`, which corrupted utf-16
// reads whose BOM landed on a chunk boundary.

package io

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// IncrementalDecoder is the stateful decoder protocol every codec
// exposes to TextIOWrapper. The contract matches CPython's
// `codecs.IncrementalDecoder` (`Lib/codecs.py:251`).
type IncrementalDecoder interface {
	// Decode consumes input and returns whatever characters it can
	// fully decode. Bytes that begin an incomplete sequence stay
	// buffered for the next call. final=true forces a flush; any
	// still-pending bytes raise UnicodeDecodeError.
	Decode(input []byte, final bool) (string, error)
	// GetState returns (unconsumed-bytes, codec-specific-flags).
	// CPython: `codecs.IncrementalDecoder.getstate` (`Lib/codecs.py:285`).
	GetState() ([]byte, int64)
	// SetState restores a snapshot. Used by TextIOWrapper.Seek.
	SetState(buffer []byte, flags int64) error
	// Reset drops the buffer and flag state.
	Reset()
}

// IncrementalEncoder mirrors `codecs.IncrementalEncoder`
// (`Lib/codecs.py:174`). Stateful encoders track whether a leading
// signature (BOM) has been emitted yet.
type IncrementalEncoder interface {
	Encode(input string, final bool) ([]byte, error)
	GetState() int64
	SetState(state int64) error
	Reset()
}

// getIncrementalDecoder returns a fresh decoder for encoding. errors
// is the error-handling strategy ("strict", "replace", "ignore"); only
// "strict" is implemented today, matching the current one-shot
// `decodeBytes` behavior. Unknown encodings return a LookupError-shaped
// Go error so the caller can surface it to Python.
//
// CPython: Modules/_io/textio.c:912 _textiowrapper_set_decoder
// (calls _PyCodecInfo_GetIncrementalDecoder).
func getIncrementalDecoder(encoding, _ string) (IncrementalDecoder, error) {
	switch normalizeCodec(encoding) {
	case "utf-8":
		return &utf8Decoder{}, nil
	case "ascii":
		return &asciiDecoder{}, nil
	case "latin-1":
		return &latin1Decoder{}, nil
	case "utf-16":
		return &utf16Decoder{variant: ""}, nil
	case "utf-16-le":
		return &utf16Decoder{variant: "le"}, nil
	case "utf-16-be":
		return &utf16Decoder{variant: "be"}, nil
	case "utf-32":
		return &utf32Decoder{variant: ""}, nil
	case "utf-32-le":
		return &utf32Decoder{variant: "le"}, nil
	case "utf-32-be":
		return &utf32Decoder{variant: "be"}, nil
	case "cp1252":
		return &charmapDecoder{table: &cp1252Table.decode, name: "cp1252"}, nil
	case "cp1250":
		return &charmapDecoder{table: &cp1250Table.decode, name: "cp1250"}, nil
	case "cp1251":
		return &charmapDecoder{table: &cp1251Table.decode, name: "cp1251"}, nil
	case "cp437":
		return &charmapDecoder{table: &cp437Table.decode, name: "cp437"}, nil
	case "mac-roman":
		return &charmapDecoder{table: &macRomanTable.decode, name: "mac-roman"}, nil
	}
	return nil, fmt.Errorf("LookupError: unknown encoding: %s", encoding)
}

// getIncrementalEncoder mirrors getIncrementalDecoder for the write
// side.
//
// CPython: Modules/_io/textio.c:968 _textiowrapper_set_encoder.
func getIncrementalEncoder(encoding, _ string) (IncrementalEncoder, error) {
	switch normalizeCodec(encoding) {
	case "utf-8":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return []byte(s), nil }}, nil
	case "ascii":
		return &statelessEncoder{enc: encodeASCII}, nil
	case "latin-1":
		return &statelessEncoder{enc: encodeLatin1}, nil
	case "utf-16":
		return &bomEncoder{body: func(s string) []byte { return encodeUTF16(s, "le") }, bom: []byte{0xFF, 0xFE}}, nil
	case "utf-16-le":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return encodeUTF16(s, "le"), nil }}, nil
	case "utf-16-be":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return encodeUTF16(s, "be"), nil }}, nil
	case "utf-32":
		return &bomEncoder{body: func(s string) []byte { return encodeUTF32(s, "le") }, bom: []byte{0xFF, 0xFE, 0x00, 0x00}}, nil
	case "utf-32-le":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return encodeUTF32(s, "le"), nil }}, nil
	case "utf-32-be":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return encodeUTF32(s, "be"), nil }}, nil
	case "cp1252":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return charmapEncode(s, cp1252Table.encode, "cp1252") }}, nil
	case "cp1250":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return charmapEncode(s, cp1250Table.encode, "cp1250") }}, nil
	case "cp1251":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return charmapEncode(s, cp1251Table.encode, "cp1251") }}, nil
	case "cp437":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return charmapEncode(s, cp437Table.encode, "cp437") }}, nil
	case "mac-roman":
		return &statelessEncoder{enc: func(s string) ([]byte, error) { return charmapEncode(s, macRomanTable.encode, "mac-roman") }}, nil
	}
	return nil, fmt.Errorf("LookupError: unknown encoding: %s", encoding)
}

// --- utf-8 -----------------------------------------------------------------

// utf8Decoder buffers up to three trailing bytes that begin an
// incomplete multi-byte sequence. CPython's utf-8 incremental decoder
// keeps the same window because a code point spans at most four bytes.
type utf8Decoder struct {
	buf []byte
}

func (d *utf8Decoder) Decode(input []byte, final bool) (string, error) {
	var src []byte
	if len(d.buf) == 0 {
		src = input
	} else {
		src = append(append([]byte{}, d.buf...), input...)
		d.buf = d.buf[:0]
	}
	// Walk back from the end to find the longest tail that is either
	// a complete utf-8 sequence or an incomplete (but valid so-far)
	// prefix. RuneStart marks the first byte of a sequence.
	keep := 0
	if !final && len(src) > 0 {
		for i := len(src) - 1; i >= 0 && i >= len(src)-4; i-- {
			if utf8.RuneStart(src[i]) {
				if !utf8.FullRune(src[i:]) {
					keep = len(src) - i
				}
				break
			}
		}
	}
	complete := src[:len(src)-keep]
	if !utf8.Valid(complete) {
		return "", fmt.Errorf("UnicodeDecodeError: invalid utf-8 sequence")
	}
	if keep > 0 {
		d.buf = append(d.buf[:0], src[len(src)-keep:]...)
	}
	return string(complete), nil
}

func (d *utf8Decoder) GetState() ([]byte, int64) { return append([]byte{}, d.buf...), 0 }
func (d *utf8Decoder) SetState(buffer []byte, _ int64) error {
	d.buf = append(d.buf[:0], buffer...)
	return nil
}
func (d *utf8Decoder) Reset() { d.buf = d.buf[:0] }

// --- ascii / latin-1 -------------------------------------------------------

type asciiDecoder struct{}

func (asciiDecoder) Decode(input []byte, _ bool) (string, error) {
	for _, b := range input {
		if b > 127 {
			return "", fmt.Errorf("UnicodeDecodeError: ordinal not in range(128)")
		}
	}
	return string(input), nil
}
func (asciiDecoder) GetState() ([]byte, int64)   { return nil, 0 }
func (asciiDecoder) SetState([]byte, int64) error { return nil }
func (asciiDecoder) Reset()                       {}

func encodeASCII(s string) ([]byte, error) {
	for _, r := range s {
		if r > 127 {
			return nil, fmt.Errorf("UnicodeEncodeError: character %q is not in ASCII range", r)
		}
	}
	return []byte(s), nil
}

type latin1Decoder struct{}

func (latin1Decoder) Decode(input []byte, _ bool) (string, error) {
	runes := make([]rune, len(input))
	for i, b := range input {
		runes[i] = rune(b)
	}
	return string(runes), nil
}
func (latin1Decoder) GetState() ([]byte, int64)   { return nil, 0 }
func (latin1Decoder) SetState([]byte, int64) error { return nil }
func (latin1Decoder) Reset()                       {}

func encodeLatin1(s string) ([]byte, error) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 255 {
			return nil, fmt.Errorf("UnicodeEncodeError: character %q is not in Latin-1 range", r)
		}
		b = append(b, byte(r))
	}
	return b, nil
}

// --- utf-16 ----------------------------------------------------------------

// utf16Decoder tracks endianness (decided once via BOM on the auto
// variant) and buffers trailing bytes that don't fit in a 16-bit
// code unit boundary.
type utf16Decoder struct {
	variant string // "", "le", or "be"
	buf     []byte
	// flags encodes endianness for tell/seek snapshots.
	// 0 = undecided (auto-variant before BOM sniff)
	// 1 = little-endian
	// 2 = big-endian
	flags int64
}

func (d *utf16Decoder) byteOrder() (binary.ByteOrder, bool) {
	switch d.variant {
	case "le":
		return binary.LittleEndian, true
	case "be":
		return binary.BigEndian, true
	}
	switch d.flags {
	case 1:
		return binary.LittleEndian, true
	case 2:
		return binary.BigEndian, true
	}
	return nil, false
}

func (d *utf16Decoder) Decode(input []byte, final bool) (string, error) {
	src := append(d.buf[:0:0], d.buf...)
	src = append(src, input...)
	d.buf = d.buf[:0]
	// On the auto variant, sniff the BOM the first time we see
	// >=2 bytes.
	bo, decided := d.byteOrder()
	if !decided {
		if len(src) < 2 {
			d.buf = append(d.buf, src...)
			return "", nil
		}
		switch {
		case src[0] == 0xFF && src[1] == 0xFE:
			d.flags = 1
			bo = binary.LittleEndian
			src = src[2:]
		case src[0] == 0xFE && src[1] == 0xFF:
			d.flags = 2
			bo = binary.BigEndian
			src = src[2:]
		default:
			// No BOM: default to little-endian (matches CPython on
			// x86; the BOM-less case isn't well-specified).
			d.flags = 1
			bo = binary.LittleEndian
		}
	}
	keep := len(src) % 2
	if final && keep != 0 {
		return "", fmt.Errorf("UnicodeDecodeError: utf-16 truncated (odd byte count)")
	}
	body := src[:len(src)-keep]
	if keep > 0 {
		d.buf = append(d.buf, src[len(src)-keep:]...)
	}
	units := make([]uint16, len(body)/2)
	for i := range units {
		units[i] = bo.Uint16(body[2*i:])
	}
	return string(utf16.Decode(units)), nil
}

func (d *utf16Decoder) GetState() ([]byte, int64) {
	return append([]byte{}, d.buf...), d.flags
}
func (d *utf16Decoder) SetState(buffer []byte, flags int64) error {
	d.buf = append(d.buf[:0], buffer...)
	d.flags = flags
	return nil
}
func (d *utf16Decoder) Reset() {
	d.buf = d.buf[:0]
	d.flags = 0
}

// --- utf-32 ----------------------------------------------------------------

type utf32Decoder struct {
	variant string
	buf     []byte
	flags   int64
}

func (d *utf32Decoder) byteOrder() (binary.ByteOrder, bool) {
	switch d.variant {
	case "le":
		return binary.LittleEndian, true
	case "be":
		return binary.BigEndian, true
	}
	switch d.flags {
	case 1:
		return binary.LittleEndian, true
	case 2:
		return binary.BigEndian, true
	}
	return nil, false
}

func (d *utf32Decoder) Decode(input []byte, final bool) (string, error) {
	src := append(d.buf[:0:0], d.buf...)
	src = append(src, input...)
	d.buf = d.buf[:0]
	bo, decided := d.byteOrder()
	if !decided {
		if len(src) < 4 {
			d.buf = append(d.buf, src...)
			return "", nil
		}
		switch {
		case src[0] == 0xFF && src[1] == 0xFE && src[2] == 0 && src[3] == 0:
			d.flags = 1
			bo = binary.LittleEndian
			src = src[4:]
		case src[0] == 0 && src[1] == 0 && src[2] == 0xFE && src[3] == 0xFF:
			d.flags = 2
			bo = binary.BigEndian
			src = src[4:]
		default:
			d.flags = 1
			bo = binary.LittleEndian
		}
	}
	keep := len(src) % 4
	if final && keep != 0 {
		return "", fmt.Errorf("UnicodeDecodeError: utf-32 truncated (length %% 4 != 0)")
	}
	body := src[:len(src)-keep]
	if keep > 0 {
		d.buf = append(d.buf, src[len(src)-keep:]...)
	}
	runes := make([]rune, 0, len(body)/4)
	for i := 0; i < len(body); i += 4 {
		cp := bo.Uint32(body[i:])
		if cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF) {
			return "", fmt.Errorf("UnicodeDecodeError: invalid utf-32 codepoint U+%X", cp)
		}
		runes = append(runes, rune(cp))
	}
	return string(runes), nil
}

func (d *utf32Decoder) GetState() ([]byte, int64) {
	return append([]byte{}, d.buf...), d.flags
}
func (d *utf32Decoder) SetState(buffer []byte, flags int64) error {
	d.buf = append(d.buf[:0], buffer...)
	d.flags = flags
	return nil
}
func (d *utf32Decoder) Reset() {
	d.buf = d.buf[:0]
	d.flags = 0
}

// --- charmap (single-byte) -------------------------------------------------

type charmapDecoder struct {
	table *[256]rune
	name  string
}

func (d *charmapDecoder) Decode(input []byte, _ bool) (string, error) {
	return charmapDecode(input, d.table, d.name)
}
func (d *charmapDecoder) GetState() ([]byte, int64)   { return nil, 0 }
func (d *charmapDecoder) SetState([]byte, int64) error { return nil }
func (d *charmapDecoder) Reset()                       {}

// --- stateless encoder -----------------------------------------------------

type statelessEncoder struct {
	enc func(string) ([]byte, error)
}

func (e *statelessEncoder) Encode(input string, _ bool) ([]byte, error) { return e.enc(input) }
func (e *statelessEncoder) GetState() int64                              { return 0 }
func (e *statelessEncoder) SetState(int64) error                         { return nil }
func (e *statelessEncoder) Reset()                                       {}

// --- BOM-emitting encoder --------------------------------------------------

// bomEncoder emits the BOM exactly once on the first non-empty call,
// then delegates to body for every subsequent encode. State 0 = BOM
// not yet emitted, 1 = emitted.
type bomEncoder struct {
	body  func(string) []byte
	bom   []byte
	state int64
}

func (e *bomEncoder) Encode(input string, _ bool) ([]byte, error) {
	body := e.body(input)
	if e.state == 0 {
		e.state = 1
		out := make([]byte, 0, len(e.bom)+len(body))
		out = append(out, e.bom...)
		out = append(out, body...)
		return out, nil
	}
	return body, nil
}
func (e *bomEncoder) GetState() int64        { return e.state }
func (e *bomEncoder) SetState(s int64) error { e.state = s; return nil }
func (e *bomEncoder) Reset()                 { e.state = 0 }
