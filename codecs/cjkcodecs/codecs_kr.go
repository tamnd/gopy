// Korean codec ports: euc_kr, cp949, johab. Hand-translated 1:1 from
// CPython Modules/cjkcodecs/_codecs_kr.c so the per-codec decisions
// (jamo make-up sequences, johab bit-packing, CP949 extension
// fall-back) match byte-for-byte.
//
// CPython: Modules/cjkcodecs/_codecs_kr.c

package cjkcodecs

// EUCKR_JAMO_FIRSTBYTE / FILLER mark the KS X 1001:1998 make-up
// sequence used to round-trip CP949-only syllables through EUC-KR.
//
// CPython: _codecs_kr.c:14
const (
	euckrJamoFirstByte = 0xA4
	euckrJamoFiller    = 0xD4
)

// u2cgkChoseong / u2cgkJungseong / u2cgkJongseong map the unicode
// hangul jamo indexes into their KS X 1001 row bytes for the make-up
// sequence emitted by the euc_kr encoder.
//
// CPython: _codecs_kr.c:17
var u2cgkChoseong = [19]byte{
	0xa1, 0xa2, 0xa4, 0xa7, 0xa8, 0xa9, 0xb1, 0xb2,
	0xb3, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb,
	0xbc, 0xbd, 0xbe,
}

var u2cgkJungseong = [21]byte{
	0xbf, 0xc0, 0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6,
	0xc7, 0xc8, 0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xce,
	0xcf, 0xd0, 0xd1, 0xd2, 0xd3,
}

var u2cgkJongseong = [28]byte{
	0xd4, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
	0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0,
	0xb1, 0xb2, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xba,
	0xbb, 0xbc, 0xbd, 0xbe,
}

// cgk2uChoseong / cgk2uJongseong are the reverse tables used by the
// euc_kr decoder when it sees a KS X 1001:1998 make-up sequence.
//
// CPython: _codecs_kr.c:94
const krNONE = 127

var cgk2uChoseong = [...]byte{
	0, 1, krNONE, 2, krNONE, krNONE, 3, 4,
	5, krNONE, krNONE, krNONE, krNONE, krNONE, krNONE, krNONE,
	6, 7, 8, krNONE, 9, 10, 11, 12,
	13, 14, 15, 16, 17, 18,
}

var cgk2uJongseong = [...]byte{
	1, 2, 3, 4, 5, 6, 7, krNONE,
	8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, krNONE, 18, 19, 20, 21, 22,
	krNONE, 23, 24, 25, 26, 27,
}

// CPython: _codecs_kr.c:34 ENCODER(euc_kr)
func encodeEUCKR(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	code, ok := tryMapEnc(&cp949_encmap[c>>8], byte(c&0xff))
	if !ok {
		return 1
	}
	if code&0x8000 == 0 {
		out.writeBytes(byte((code>>8)|0x80), byte((code&0xff)|0x80))
		return 1
	}
	// CP949 extension: emit a KS X 1001 make-up sequence.
	if c < 0xac00 || c > 0xd7a3 {
		return 1
	}
	c -= 0xac00
	out.writeBytes(
		euckrJamoFirstByte, euckrJamoFiller,
		euckrJamoFirstByte, u2cgkChoseong[c/588],
		euckrJamoFirstByte, u2cgkJungseong[(c/28)%21],
		euckrJamoFirstByte, u2cgkJongseong[c%28],
	)
	return 1
}

// CPython: _codecs_kr.c:107 DECODER(euc_kr)
func decodeEUCKR(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	if c == euckrJamoFirstByte && in[1] == euckrJamoFiller {
		if len(in) < 8 {
			return MBERR_TOOFEW
		}
		if in[2] != euckrJamoFirstByte || in[4] != euckrJamoFirstByte || in[6] != euckrJamoFirstByte {
			return 1
		}
		var cho, jung, jong byte
		b := in[3]
		if b >= 0xa1 && b <= 0xbe {
			cho = cgk2uChoseong[b-0xa1]
		} else {
			cho = krNONE
		}
		b = in[5]
		if b >= 0xbf && b <= 0xd3 {
			jung = b - 0xbf
		} else {
			jung = krNONE
		}
		b = in[7]
		switch {
		case b == euckrJamoFiller:
			jong = 0
		case b >= 0xa1 && b <= 0xbe:
			jong = cgk2uJongseong[b-0xa1]
		default:
			jong = krNONE
		}
		if cho == krNONE || jung == krNONE || jong == krNONE {
			return 1
		}
		w.writeRune(rune(0xac00 + uint32(cho)*588 + uint32(jung)*28 + uint32(jong)))
		return 8
	}
	row := ksx1001_decmap[c^0x80]
	if dec, ok := tryMapDec(&row, in[1]^0x80); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}

// CPython: _codecs_kr.c:172 ENCODER(cp949)
func encodeCP949(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	code, ok := tryMapEnc(&cp949_encmap[c>>8], byte(c&0xff))
	if !ok {
		return 1
	}
	out.writeByte(byte((code >> 8) | 0x80))
	if code&0x8000 != 0 {
		out.writeByte(byte(code & 0xff))
	} else {
		out.writeByte(byte((code & 0xff) | 0x80))
	}
	return 1
}

// CPython: _codecs_kr.c:204 DECODER(cp949)
func decodeCP949(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	row := ksx1001_decmap[c^0x80]
	if dec, ok := tryMapDec(&row, in[1]^0x80); ok {
		w.writeRune(rune(dec))
		return 2
	}
	row2 := cp949ext_decmap[c]
	if dec, ok := tryMapDec(&row2, in[1]); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}

// johab tables. CPython: _codecs_kr.c:235..359
var u2johabidxChoseong = [32]byte{
	0, 0, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
	0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14,
}

var u2johabidxJungseong = [32]byte{
	0, 0, 0, 0x03, 0x04, 0x05, 0x06, 0x07,
	0, 0, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0, 0, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
	0, 0, 0x1a, 0x1b, 0x1c, 0x1d,
}

var u2johabidxJongseong = [32]byte{
	0, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
	0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0, 0x13, 0x14, 0x15, 0x16, 0x17,
	0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
}

var u2johabjamo = [...]uint16{
	0x8841, 0x8c41, 0x8444, 0x9041, 0x8446, 0x8447, 0x9441,
	0x9841, 0x9c41, 0x844a, 0x844b, 0x844c, 0x844d, 0x844e, 0x844f,
	0x8450, 0xa041, 0xa441, 0xa841, 0x8454, 0xac41, 0xb041, 0xb441,
	0xb841, 0xbc41, 0xc041, 0xc441, 0xc841, 0xcc41, 0xd041, 0x8461,
	0x8481, 0x84a1, 0x84c1, 0x84e1, 0x8541, 0x8561, 0x8581, 0x85a1,
	0x85c1, 0x85e1, 0x8641, 0x8661, 0x8681, 0x86a1, 0x86c1, 0x86e1,
	0x8741, 0x8761, 0x8781, 0x87a1,
}

const (
	johabFILL = 0xfd
	johabNONE = 0xff
)

var johabidxChoseong = [32]byte{
	johabNONE, johabFILL, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05,
	0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d,
	0x0e, 0x0f, 0x10, 0x11, 0x12, johabNONE, johabNONE, johabNONE,
	johabNONE, johabNONE, johabNONE, johabNONE, johabNONE, johabNONE, johabNONE, johabNONE,
}

var johabidxJungseong = [32]byte{
	johabNONE, johabNONE, johabFILL, 0x00, 0x01, 0x02, 0x03, 0x04,
	johabNONE, johabNONE, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
	johabNONE, johabNONE, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	johabNONE, johabNONE, 0x11, 0x12, 0x13, 0x14, johabNONE, johabNONE,
}

var johabidxJongseong = [32]byte{
	johabNONE, johabFILL, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
	0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e,
	0x0f, 0x10, johabNONE, 0x11, 0x12, 0x13, 0x14, 0x15,
	0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, johabNONE, johabNONE,
}

var johabjamoChoseong = [32]byte{
	johabNONE, johabFILL, 0x31, 0x32, 0x34, 0x37, 0x38, 0x39,
	0x41, 0x42, 0x43, 0x45, 0x46, 0x47, 0x48, 0x49,
	0x4a, 0x4b, 0x4c, 0x4d, 0x4e, johabNONE, johabNONE, johabNONE,
	johabNONE, johabNONE, johabNONE, johabNONE, johabNONE, johabNONE, johabNONE, johabNONE,
}

var johabjamoJungseong = [32]byte{
	johabNONE, johabNONE, johabFILL, 0x4f, 0x50, 0x51, 0x52, 0x53,
	johabNONE, johabNONE, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
	johabNONE, johabNONE, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
	johabNONE, johabNONE, 0x60, 0x61, 0x62, 0x63, johabNONE, johabNONE,
}

var johabjamoJongseong = [32]byte{
	johabNONE, johabFILL, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36,
	0x37, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	0x40, 0x41, johabNONE, 0x42, 0x44, 0x45, 0x46, 0x47,
	0x48, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, johabNONE, johabNONE,
}

// CPython: _codecs_kr.c:262 ENCODER(johab)
func encodeJohab(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	var code uint16
	switch {
	case c >= 0xac00 && c <= 0xd7a3:
		c -= 0xac00
		code = 0x8000 |
			(uint16(u2johabidxChoseong[c/588]) << 10) |
			(uint16(u2johabidxJungseong[(c/28)%21]) << 5) |
			uint16(u2johabidxJongseong[c%28])
	case c >= 0x3131 && c <= 0x3163:
		code = u2johabjamo[c-0x3131]
	default:
		k, ok := tryMapEnc(&cp949_encmap[c>>8], byte(c&0xff))
		if !ok {
			return 1
		}
		if k&0x8000 != 0 {
			return 1
		}
		c1 := byte(k >> 8)
		c2 := byte(k & 0xff)
		c1InRange := (c1 >= 0x21 && c1 <= 0x2c) || (c1 >= 0x4a && c1 <= 0x7d)
		c2InRange := c2 >= 0x21 && c2 <= 0x7e
		if !c1InRange || !c2InRange {
			return 1
		}
		var t1 uint16
		if c1 < 0x4a {
			t1 = uint16(c1) - 0x21 + 0x1b2
		} else {
			t1 = uint16(c1) - 0x21 + 0x197
		}
		t2 := byte(0)
		if t1&1 != 0 {
			t2 = 0x5e
		}
		t2 += c2 - 0x21
		out.writeByte(byte(t1 >> 1))
		if t2 < 0x4e {
			out.writeByte(t2 + 0x31)
		} else {
			out.writeByte(t2 + 0x43)
		}
		return 1
	}
	out.writeByte(byte(code >> 8))
	out.writeByte(byte(code & 0xff))
	return 1
}

// CPython: _codecs_kr.c:361 DECODER(johab)
//
//nolint:gocognit,gocyclo,staticcheck // mirrors CPython DECODER(johab) branch layout 1:1.
func decodeJohab(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	c2 := in[1]
	if c < 0xd8 {
		cCho := (c >> 2) & 0x1f
		cJung := ((c << 3) | (c2 >> 5)) & 0x1f
		cJong := c2 & 0x1f
		iCho := johabidxChoseong[cCho]
		iJung := johabidxJungseong[cJung]
		iJong := johabidxJongseong[cJong]
		if iCho == johabNONE || iJung == johabNONE || iJong == johabNONE {
			return 1
		}
		switch {
		case iCho == johabFILL:
			switch {
			case iJung == johabFILL:
				if iJong == johabFILL {
					w.writeRune(0x3000)
				} else {
					w.writeRune(rune(0x3100 | uint32(johabjamoJongseong[cJong])))
				}
			default:
				if iJong == johabFILL {
					w.writeRune(rune(0x3100 | uint32(johabjamoJungseong[cJung])))
				} else {
					return 1
				}
			}
		default:
			switch {
			case iJung == johabFILL:
				if iJong == johabFILL {
					w.writeRune(rune(0x3100 | uint32(johabjamoChoseong[cCho])))
				} else {
					return 1
				}
			default:
				jong := iJong
				if jong == johabFILL {
					jong = 0
				}
				w.writeRune(rune(0xac00 +
					uint32(iCho)*588 +
					uint32(iJung)*28 +
					uint32(jong)))
			}
		}
		return 2
	}
	// KS X 1001 except hangul jamos and syllables
	if c == 0xdf || c > 0xf9 ||
		c2 < 0x31 || (c2 >= 0x80 && c2 < 0x91) ||
		(c2&0x7f) == 0x7f ||
		(c == 0xda && (c2 >= 0xa1 && c2 <= 0xd3)) {
		return 1
	}
	var t1 byte
	if c < 0xe0 {
		t1 = 2 * (c - 0xd9)
	} else {
		t1 = byte(2*uint16(c) - 0x197)
	}
	var t2 byte
	if c2 < 0x91 {
		t2 = c2 - 0x31
	} else {
		t2 = c2 - 0x43
	}
	if t2 < 0x5e {
		t1 = t1 + 0 + 0x21
	} else {
		t1 = t1 + 1 + 0x21
	}
	if t2 < 0x5e {
		t2 += 0x21
	} else {
		t2 = t2 - 0x5e + 0x21
	}
	row := ksx1001_decmap[t1]
	if dec, ok := tryMapDec(&row, t2); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}
