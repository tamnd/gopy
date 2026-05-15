// Package-level text codecs used by TextIOWrapper. CPython relies on
// the codecs registry (Lib/codecs.py + the C codecs in Modules/_codecs)
// to back open(..., encoding=X). gopy ships a hand-written subset that
// covers the encodings real-world stdlib code actually opens files in:
// utf-8 / ascii / latin-1 (already inline in textiowrapper.go) plus the
// utf-16 / utf-32 families and the small set of 8-bit code pages that
// Lib/ trips over (cp1252, cp1250, cp1251, cp437, mac-roman).
//
// The implementations are stateless: TextIOWrapper currently calls
// decodeBytes / encodeString in one shot per read / write. Multi-byte
// state across read calls (utf-16 surrogate halves split mid-buffer)
// is handled by the buffered layer above us delivering enough bytes;
// the codecs themselves treat the input as a complete unit and error
// on truncation, which matches CPython's strict mode default.
//
// CPython: Modules/_codecs/{utf_16,utf_32,charmap}.c

package io

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

// normalizeCodec maps user-facing encoding aliases to a canonical
// lowercase name. Mirrors encodings.normalize_encoding plus the
// well-known aliases that Lib/encodings/aliases.py registers.
//
// CPython: Lib/encodings/__init__.py normalize_encoding
// CPython: Lib/encodings/aliases.py aliases
func normalizeCodec(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "utf8", "u8", "utf", "utf-8", "cp65001":
		return "utf-8"
	case "ascii", "us-ascii", "646", "ansi-x3.4-1968":
		return "ascii"
	case "latin1", "latin-1", "iso-8859-1", "iso8859-1", "8859", "cp819", "l1":
		return "latin-1"
	case "utf16", "utf-16", "u16":
		return "utf-16"
	case "utf-16-le", "utf16le", "utf-16le":
		return "utf-16-le"
	case "utf-16-be", "utf16be", "utf-16be":
		return "utf-16-be"
	case "utf32", "utf-32", "u32":
		return "utf-32"
	case "utf-32-le", "utf32le", "utf-32le":
		return "utf-32-le"
	case "utf-32-be", "utf32be", "utf-32be":
		return "utf-32-be"
	case "cp1252", "1252", "windows-1252":
		return "cp1252"
	case "cp1250", "1250", "windows-1250":
		return "cp1250"
	case "cp1251", "1251", "windows-1251":
		return "cp1251"
	case "cp437", "437", "ibm437":
		return "cp437"
	case "mac-roman", "macroman", "macintosh":
		return "mac-roman"
	}
	return s
}

// --- UTF-16 ----------------------------------------------------------------

// decodeUTF16 decodes data as UTF-16. variant is "le", "be", or ""
// (auto, which sniffs the BOM and falls back to little-endian to match
// CPython on x86).
//
// CPython: Modules/_codecs/utf_16.c PyUnicode_DecodeUTF16Stateful
func decodeUTF16(data []byte, variant string) (string, error) {
	var bo binary.ByteOrder = binary.LittleEndian
	switch variant {
	case "le":
		bo = binary.LittleEndian
	case "be":
		bo = binary.BigEndian
	case "":
		if len(data) >= 2 {
			switch {
			case data[0] == 0xFF && data[1] == 0xFE:
				bo = binary.LittleEndian
				data = data[2:]
			case data[0] == 0xFE && data[1] == 0xFF:
				bo = binary.BigEndian
				data = data[2:]
			}
		}
	}
	if len(data)%2 != 0 {
		return "", fmt.Errorf("UnicodeDecodeError: utf-16 truncated (odd byte count)")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = bo.Uint16(data[2*i:])
	}
	return string(utf16.Decode(units)), nil
}

// encodeUTF16 encodes s as UTF-16. variant "le" / "be" emit raw code
// units without a BOM; variant "" emits a BOM followed by
// little-endian, matching CPython on x86.
//
// CPython: Modules/_codecs/utf_16.c PyUnicode_EncodeUTF16
func encodeUTF16(s, variant string) ([]byte, error) {
	var bo binary.ByteOrder = binary.LittleEndian
	var out []byte
	switch variant {
	case "le":
	case "be":
		bo = binary.BigEndian
	case "":
		out = []byte{0xFF, 0xFE}
	}
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		bo.PutUint16(buf[2*i:], u)
	}
	return append(out, buf...), nil
}

// --- UTF-32 ----------------------------------------------------------------

// decodeUTF32 decodes data as UTF-32.
//
// CPython: Modules/_codecs/utf_32.c PyUnicode_DecodeUTF32Stateful
func decodeUTF32(data []byte, variant string) (string, error) {
	var bo binary.ByteOrder = binary.LittleEndian
	switch variant {
	case "le":
		bo = binary.LittleEndian
	case "be":
		bo = binary.BigEndian
	case "":
		if len(data) >= 4 {
			switch {
			case data[0] == 0xFF && data[1] == 0xFE && data[2] == 0 && data[3] == 0:
				bo = binary.LittleEndian
				data = data[4:]
			case data[0] == 0 && data[1] == 0 && data[2] == 0xFE && data[3] == 0xFF:
				bo = binary.BigEndian
				data = data[4:]
			}
		}
	}
	if len(data)%4 != 0 {
		return "", fmt.Errorf("UnicodeDecodeError: utf-32 truncated (length %% 4 != 0)", )
	}
	runes := make([]rune, 0, len(data)/4)
	for i := 0; i < len(data); i += 4 {
		cp := bo.Uint32(data[i:])
		if cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF) {
			return "", fmt.Errorf("UnicodeDecodeError: invalid utf-32 codepoint U+%X", cp)
		}
		runes = append(runes, rune(cp))
	}
	return string(runes), nil
}

// encodeUTF32 encodes s as UTF-32.
//
// CPython: Modules/_codecs/utf_32.c PyUnicode_EncodeUTF32
func encodeUTF32(s, variant string) ([]byte, error) {
	var bo binary.ByteOrder = binary.LittleEndian
	var out []byte
	switch variant {
	case "le":
	case "be":
		bo = binary.BigEndian
	case "":
		out = []byte{0xFF, 0xFE, 0x00, 0x00}
	}
	runes := []rune(s)
	buf := make([]byte, 4*len(runes))
	for i, r := range runes {
		bo.PutUint32(buf[4*i:], uint32(r))
	}
	return append(out, buf...), nil
}

// --- 8-bit code pages -------------------------------------------------------

// charmapDecode decodes data using a 256-entry lookup table. -1 in the
// table means the byte is unmapped and should raise UnicodeDecodeError.
//
// CPython: Modules/_codecs/charmap.c PyUnicode_DecodeCharmap
func charmapDecode(data []byte, table *[256]rune, name string) (string, error) {
	runes := make([]rune, len(data))
	for i, b := range data {
		r := table[b]
		if r < 0 {
			return "", fmt.Errorf("UnicodeDecodeError: %s can't decode byte 0x%02x", name, b)
		}
		runes[i] = r
	}
	return string(runes), nil
}

// charmapEncode encodes s using the inverse of the 256-entry table.
// The map is built once at table-construction time.
//
// CPython: Modules/_codecs/charmap.c PyUnicode_EncodeCharmap
func charmapEncode(s string, inverse map[rune]byte, name string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		b, ok := inverse[r]
		if !ok {
			return nil, fmt.Errorf("UnicodeEncodeError: %s can't encode character %q", name, r)
		}
		out = append(out, b)
	}
	return out, nil
}

// codepageTable holds a decode table and its precomputed inverse.
type codepageTable struct {
	decode  [256]rune
	encode  map[rune]byte
	name    string
}

func newCodepage(name string, mapping [128]rune) *codepageTable {
	t := &codepageTable{name: name}
	for i := range 128 {
		t.decode[i] = rune(i)
	}
	for i := range 128 {
		t.decode[i+128] = mapping[i]
	}
	t.encode = make(map[rune]byte, 256)
	for b, r := range t.decode {
		if r >= 0 {
			t.encode[r] = byte(b)
		}
	}
	return t
}

// Code page 1252 (Windows Western). CPython: Lib/encodings/cp1252.py
var cp1252Table = newCodepage("cp1252", [128]rune{
	0x20AC, -1, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, -1, 0x017D, -1,
	-1, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, -1, 0x017E, 0x0178,
	0x00A0, 0x00A1, 0x00A2, 0x00A3, 0x00A4, 0x00A5, 0x00A6, 0x00A7,
	0x00A8, 0x00A9, 0x00AA, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x00AF,
	0x00B0, 0x00B1, 0x00B2, 0x00B3, 0x00B4, 0x00B5, 0x00B6, 0x00B7,
	0x00B8, 0x00B9, 0x00BA, 0x00BB, 0x00BC, 0x00BD, 0x00BE, 0x00BF,
	0x00C0, 0x00C1, 0x00C2, 0x00C3, 0x00C4, 0x00C5, 0x00C6, 0x00C7,
	0x00C8, 0x00C9, 0x00CA, 0x00CB, 0x00CC, 0x00CD, 0x00CE, 0x00CF,
	0x00D0, 0x00D1, 0x00D2, 0x00D3, 0x00D4, 0x00D5, 0x00D6, 0x00D7,
	0x00D8, 0x00D9, 0x00DA, 0x00DB, 0x00DC, 0x00DD, 0x00DE, 0x00DF,
	0x00E0, 0x00E1, 0x00E2, 0x00E3, 0x00E4, 0x00E5, 0x00E6, 0x00E7,
	0x00E8, 0x00E9, 0x00EA, 0x00EB, 0x00EC, 0x00ED, 0x00EE, 0x00EF,
	0x00F0, 0x00F1, 0x00F2, 0x00F3, 0x00F4, 0x00F5, 0x00F6, 0x00F7,
	0x00F8, 0x00F9, 0x00FA, 0x00FB, 0x00FC, 0x00FD, 0x00FE, 0x00FF,
})

// Code page 1250 (Windows Central European). CPython: Lib/encodings/cp1250.py
var cp1250Table = newCodepage("cp1250", [128]rune{
	0x20AC, -1, 0x201A, -1, 0x201E, 0x2026, 0x2020, 0x2021,
	-1, 0x2030, 0x0160, 0x2039, 0x015A, 0x0164, 0x017D, 0x0179,
	-1, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	-1, 0x2122, 0x0161, 0x203A, 0x015B, 0x0165, 0x017E, 0x017A,
	0x00A0, 0x02C7, 0x02D8, 0x0141, 0x00A4, 0x0104, 0x00A6, 0x00A7,
	0x00A8, 0x00A9, 0x015E, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x017B,
	0x00B0, 0x00B1, 0x02DB, 0x0142, 0x00B4, 0x00B5, 0x00B6, 0x00B7,
	0x00B8, 0x0105, 0x015F, 0x00BB, 0x013D, 0x02DD, 0x013E, 0x017C,
	0x0154, 0x00C1, 0x00C2, 0x0102, 0x00C4, 0x0139, 0x0106, 0x00C7,
	0x010C, 0x00C9, 0x0118, 0x00CB, 0x011A, 0x00CD, 0x00CE, 0x010E,
	0x0110, 0x0143, 0x0147, 0x00D3, 0x00D4, 0x0150, 0x00D6, 0x00D7,
	0x0158, 0x016E, 0x00DA, 0x0170, 0x00DC, 0x00DD, 0x0162, 0x00DF,
	0x0155, 0x00E1, 0x00E2, 0x0103, 0x00E4, 0x013A, 0x0107, 0x00E7,
	0x010D, 0x00E9, 0x0119, 0x00EB, 0x011B, 0x00ED, 0x00EE, 0x010F,
	0x0111, 0x0144, 0x0148, 0x00F3, 0x00F4, 0x0151, 0x00F6, 0x00F7,
	0x0159, 0x016F, 0x00FA, 0x0171, 0x00FC, 0x00FD, 0x0163, 0x02D9,
})

// Code page 1251 (Windows Cyrillic). CPython: Lib/encodings/cp1251.py
var cp1251Table = newCodepage("cp1251", [128]rune{
	0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021,
	0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
	0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	-1, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
	0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7,
	0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
	0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7,
	0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
	0x0410, 0x0411, 0x0412, 0x0413, 0x0414, 0x0415, 0x0416, 0x0417,
	0x0418, 0x0419, 0x041A, 0x041B, 0x041C, 0x041D, 0x041E, 0x041F,
	0x0420, 0x0421, 0x0422, 0x0423, 0x0424, 0x0425, 0x0426, 0x0427,
	0x0428, 0x0429, 0x042A, 0x042B, 0x042C, 0x042D, 0x042E, 0x042F,
	0x0430, 0x0431, 0x0432, 0x0433, 0x0434, 0x0435, 0x0436, 0x0437,
	0x0438, 0x0439, 0x043A, 0x043B, 0x043C, 0x043D, 0x043E, 0x043F,
	0x0440, 0x0441, 0x0442, 0x0443, 0x0444, 0x0445, 0x0446, 0x0447,
	0x0448, 0x0449, 0x044A, 0x044B, 0x044C, 0x044D, 0x044E, 0x044F,
})

// Code page 437 (original IBM PC / DOS). CPython: Lib/encodings/cp437.py
var cp437Table = newCodepage("cp437", [128]rune{
	0x00C7, 0x00FC, 0x00E9, 0x00E2, 0x00E4, 0x00E0, 0x00E5, 0x00E7,
	0x00EA, 0x00EB, 0x00E8, 0x00EF, 0x00EE, 0x00EC, 0x00C4, 0x00C5,
	0x00C9, 0x00E6, 0x00C6, 0x00F4, 0x00F6, 0x00F2, 0x00FB, 0x00F9,
	0x00FF, 0x00D6, 0x00DC, 0x00A2, 0x00A3, 0x00A5, 0x20A7, 0x0192,
	0x00E1, 0x00ED, 0x00F3, 0x00FA, 0x00F1, 0x00D1, 0x00AA, 0x00BA,
	0x00BF, 0x2310, 0x00AC, 0x00BD, 0x00BC, 0x00A1, 0x00AB, 0x00BB,
	0x2591, 0x2592, 0x2593, 0x2502, 0x2524, 0x2561, 0x2562, 0x2556,
	0x2555, 0x2563, 0x2551, 0x2557, 0x255D, 0x255C, 0x255B, 0x2510,
	0x2514, 0x2534, 0x252C, 0x251C, 0x2500, 0x253C, 0x255E, 0x255F,
	0x255A, 0x2554, 0x2569, 0x2566, 0x2560, 0x2550, 0x256C, 0x2567,
	0x2568, 0x2564, 0x2565, 0x2559, 0x2558, 0x2552, 0x2553, 0x256B,
	0x256A, 0x2518, 0x250C, 0x2588, 0x2584, 0x258C, 0x2590, 0x2580,
	0x03B1, 0x00DF, 0x0393, 0x03C0, 0x03A3, 0x03C3, 0x00B5, 0x03C4,
	0x03A6, 0x0398, 0x03A9, 0x03B4, 0x221E, 0x03C6, 0x03B5, 0x2229,
	0x2261, 0x00B1, 0x2265, 0x2264, 0x2320, 0x2321, 0x00F7, 0x2248,
	0x00B0, 0x2219, 0x00B7, 0x221A, 0x207F, 0x00B2, 0x25A0, 0x00A0,
})

// Mac Roman. CPython: Lib/encodings/mac_roman.py
var macRomanTable = newCodepage("mac-roman", [128]rune{
	0x00C4, 0x00C5, 0x00C7, 0x00C9, 0x00D1, 0x00D6, 0x00DC, 0x00E1,
	0x00E0, 0x00E2, 0x00E4, 0x00E3, 0x00E5, 0x00E7, 0x00E9, 0x00E8,
	0x00EA, 0x00EB, 0x00ED, 0x00EC, 0x00EE, 0x00EF, 0x00F1, 0x00F3,
	0x00F2, 0x00F4, 0x00F6, 0x00F5, 0x00FA, 0x00F9, 0x00FB, 0x00FC,
	0x2020, 0x00B0, 0x00A2, 0x00A3, 0x00A7, 0x2022, 0x00B6, 0x00DF,
	0x00AE, 0x00A9, 0x2122, 0x00B4, 0x00A8, 0x2260, 0x00C6, 0x00D8,
	0x221E, 0x00B1, 0x2264, 0x2265, 0x00A5, 0x00B5, 0x2202, 0x2211,
	0x220F, 0x03C0, 0x222B, 0x00AA, 0x00BA, 0x03A9, 0x00E6, 0x00F8,
	0x00BF, 0x00A1, 0x00AC, 0x221A, 0x0192, 0x2248, 0x2206, 0x00AB,
	0x00BB, 0x2026, 0x00A0, 0x00C0, 0x00C3, 0x00D5, 0x0152, 0x0153,
	0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00F7, 0x25CA,
	0x00FF, 0x0178, 0x2044, 0x20AC, 0x2039, 0x203A, 0xFB01, 0xFB02,
	0x2021, 0x00B7, 0x201A, 0x201E, 0x2030, 0x00C2, 0x00CA, 0x00C1,
	0x00CB, 0x00C8, 0x00CD, 0x00CE, 0x00CF, 0x00CC, 0x00D3, 0x00D4,
	0xF8FF, 0x00D2, 0x00DA, 0x00DB, 0x00D9, 0x0131, 0x02C6, 0x02DC,
	0x00AF, 0x02D8, 0x02D9, 0x02DA, 0x00B8, 0x02DD, 0x02DB, 0x02C7,
})

// codecDecode is the dispatch entry used by decodeBytes for the
// non-inline codecs. Returns (decoded, true) on hit, (..., false) when
// the encoding is unknown.
func codecDecode(data []byte, encoding string) (string, bool, error) {
	switch normalizeCodec(encoding) {
	case "utf-16":
		s, err := decodeUTF16(data, "")
		return s, true, err
	case "utf-16-le":
		s, err := decodeUTF16(data, "le")
		return s, true, err
	case "utf-16-be":
		s, err := decodeUTF16(data, "be")
		return s, true, err
	case "utf-32":
		s, err := decodeUTF32(data, "")
		return s, true, err
	case "utf-32-le":
		s, err := decodeUTF32(data, "le")
		return s, true, err
	case "utf-32-be":
		s, err := decodeUTF32(data, "be")
		return s, true, err
	case "cp1252":
		s, err := charmapDecode(data, &cp1252Table.decode, "cp1252")
		return s, true, err
	case "cp1250":
		s, err := charmapDecode(data, &cp1250Table.decode, "cp1250")
		return s, true, err
	case "cp1251":
		s, err := charmapDecode(data, &cp1251Table.decode, "cp1251")
		return s, true, err
	case "cp437":
		s, err := charmapDecode(data, &cp437Table.decode, "cp437")
		return s, true, err
	case "mac-roman":
		s, err := charmapDecode(data, &macRomanTable.decode, "mac-roman")
		return s, true, err
	}
	return "", false, nil
}

// codecEncode is the dispatch entry used by encodeString.
func codecEncode(s, encoding string) ([]byte, bool, error) {
	switch normalizeCodec(encoding) {
	case "utf-16":
		b, err := encodeUTF16(s, "")
		return b, true, err
	case "utf-16-le":
		b, err := encodeUTF16(s, "le")
		return b, true, err
	case "utf-16-be":
		b, err := encodeUTF16(s, "be")
		return b, true, err
	case "utf-32":
		b, err := encodeUTF32(s, "")
		return b, true, err
	case "utf-32-le":
		b, err := encodeUTF32(s, "le")
		return b, true, err
	case "utf-32-be":
		b, err := encodeUTF32(s, "be")
		return b, true, err
	case "cp1252":
		b, err := charmapEncode(s, cp1252Table.encode, "cp1252")
		return b, true, err
	case "cp1250":
		b, err := charmapEncode(s, cp1250Table.encode, "cp1250")
		return b, true, err
	case "cp1251":
		b, err := charmapEncode(s, cp1251Table.encode, "cp1251")
		return b, true, err
	case "cp437":
		b, err := charmapEncode(s, cp437Table.encode, "cp437")
		return b, true, err
	case "mac-roman":
		b, err := charmapEncode(s, macRomanTable.encode, "mac-roman")
		return b, true, err
	}
	return nil, false, nil
}
