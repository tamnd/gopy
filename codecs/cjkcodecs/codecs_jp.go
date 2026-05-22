// Japanese codec ports: cp932, shift_jis, euc_jp, shift_jis_2004,
// euc_jis_2004, plus the shift_jisx0213 / euc_jisx0213 wrappers that
// pin the runtime to the 2000 edition of JIS X 0213. Hand-translated
// from CPython Modules/cjkcodecs/_codecs_jp.c so the per-codec
// decisions (Yen / overline / FW REVERSE SOLIDUS fall-throughs, the
// 0xa1c0 mapping, the JIS X 0213 pair-encode binary search) match
// byte-for-byte.
//
// CPython: Modules/cjkcodecs/_codecs_jp.c
// CPython: Modules/cjkcodecs/alg_jisx0201.h
// CPython: Modules/cjkcodecs/emu_jisx0213_2000.h

package cjkcodecs

const empBase uint32 = 0x20000

// jisx0201REncodeChar follows JISX0201_R_ENCODE_CHAR semantics: it
// returns the encoded byte and true when c falls in the Roman set,
// otherwise leaves code unset and returns false. CPython collapses
// the 0x5c (Yen) and 0x7e (overline) cases into the same macro;
// shift_jis / euc_jp have an extra non-STRICT branch that lets bare
// 0x5c / 0x7e pass through unchanged.
//
// CPython: alg_jisx0201.h JISX0201_R_ENCODE
func jisx0201REncodeChar(c uint32) (uint16, bool) {
	switch {
	case c < 0x80 && c != 0x5c && c != 0x7e:
		return uint16(c), true
	case c == 0x00a5:
		return 0x5c, true
	case c == 0x203e:
		return 0x7e, true
	}
	return 0, false
}

func jisx0201KEncodeChar(c uint32) (uint16, bool) {
	if c >= 0xff61 && c <= 0xff9f {
		return uint16(c - 0xfec0), true
	}
	return 0, false
}

// jisx0201RDecode emits the Roman half. CPython's R_DECODE has the
// extra Yen / overline pairing that the strict variant skips.
//
// CPython: alg_jisx0201.h JISX0201_R_DECODE
func jisx0201RDecode(c byte, w *unicodeWriter) bool {
	switch {
	case c < 0x5c:
		w.writeRune(rune(c))
		return true
	case c == 0x5c:
		w.writeRune(0x00a5)
		return true
	case c < 0x7e:
		w.writeRune(rune(c))
		return true
	case c == 0x7e:
		w.writeRune(0x203e)
		return true
	case c == 0x7f:
		w.writeRune(0x7f)
		return true
	}
	return false
}

func jisx0201KDecode(c byte, w *unicodeWriter) bool {
	if c >= 0xa1 && c <= 0xdf {
		w.writeRune(rune(0xfec0 + uint32(c)))
		return true
	}
	return false
}

// emulate2000EncodeBMP returns (code, abandonWith) where the second
// component is set when the 2000 emulator pre-empts the lookup with
// either a hard-coded byte sequence or a hard rejection.
//
// CPython: emu_jisx0213_2000.h EMULATE_JISX0213_2000_ENCODE_BMP
func emulate2000EncodeBMP(config int, c uint32) (uint16, bool, bool) {
	if config != 2000 {
		return 0, false, false
	}
	switch c {
	case 0x9B1C, 0x4FF1, 0x525D, 0x541E, 0x5653, 0x59F8,
		0x5C5B, 0x5E77, 0x7626, 0x7E6B:
		return 0, false, true // pre-empt: encode fails
	case 0x9B1D:
		return 0x8000 | 0x7d3b, true, false
	}
	return 0, false, false
}

func emulate2000EncodeEMP(config int, c uint32) bool {
	return config == 2000 && c == 0x20B9F
}

// emulate2000DecodePlane1 returns true when the (c1, c2) pair should
// be rejected by the 2000 emulator regardless of the JIS X 0213 table
// contents. CPython: emu_jisx0213_2000.h EMULATE_JISX0213_2000_DECODE_PLANE1
func emulate2000DecodePlane1(config int, c1, c2 byte) bool {
	if config != 2000 {
		return false
	}
	switch {
	case c1 == 0x2E && c2 == 0x21,
		c1 == 0x2F && c2 == 0x7E,
		c1 == 0x4F && c2 == 0x54,
		c1 == 0x4F && c2 == 0x7E,
		c1 == 0x74 && c2 == 0x27,
		c1 == 0x7E && c2 == 0x7A,
		c1 == 0x7E && c2 == 0x7B,
		c1 == 0x7E && c2 == 0x7C,
		c1 == 0x7E && c2 == 0x7D,
		c1 == 0x7E && c2 == 0x7E:
		return true
	}
	return false
}

func emulate2000DecodePlane2(config int, c1, c2 byte, w *unicodeWriter) bool {
	if config == 2000 && c1 == 0x7D && c2 == 0x3B {
		w.writeRune(0x9B1D)
		return true
	}
	return false
}

// CPython: _codecs_jp.c:20 ENCODER(cp932)
func encodeCP932(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	switch {
	case c <= 0x80:
		out.writeByte(byte(c))
		return 1
	case c >= 0xff61 && c <= 0xff9f:
		out.writeByte(byte(c - 0xfec0))
		return 1
	case c >= 0xf8f0 && c <= 0xf8f3:
		// Windows compatibility
		if c == 0xf8f0 {
			out.writeByte(0xa0)
		} else {
			out.writeByte(byte(c - 0xf8f1 + 0xfd))
		}
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	if code, ok := tryMapEnc(&cp932ext_encmap[c>>8], byte(c&0xff)); ok {
		out.writeByte(byte(code >> 8))
		out.writeByte(byte(code & 0xff))
		return 1
	}
	if code, ok := tryMapEnc(&jisxcommon_encmap[c>>8], byte(c&0xff)); ok {
		if code&0x8000 != 0 { // JIS X 0212
			return 1
		}
		c1 := byte(code >> 8)
		c2 := byte(code & 0xff)
		var add byte
		if (c1-0x21)&1 != 0 {
			add = 0x5e
		}
		c2 = add + (c2 - 0x21)
		c1 = (c1 - 0x21) >> 1
		if c1 < 0x1f {
			out.writeByte(c1 + 0x81)
		} else {
			out.writeByte(c1 + 0xc1)
		}
		if c2 < 0x3f {
			out.writeByte(c2 + 0x40)
		} else {
			out.writeByte(c2 + 0x41)
		}
		return 1
	}
	if c >= 0xe000 && c < 0xe758 {
		// User-defined area
		c1 := byte((c - 0xe000) / 188)
		c2 := byte((c - 0xe000) % 188)
		out.writeByte(c1 + 0xf0)
		if c2 < 0x3f {
			out.writeByte(c2 + 0x40)
		} else {
			out.writeByte(c2 + 0x41)
		}
		return 1
	}
	return 1
}

// CPython: _codecs_jp.c:84 DECODER(cp932)
func decodeCP932(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	switch {
	case c <= 0x80:
		w.writeRune(rune(c))
		return 1
	case c >= 0xa0 && c <= 0xdf:
		if c == 0xa0 {
			w.writeRune(0xf8f0) // half-width katakana
		} else {
			w.writeRune(rune(0xfec0 + uint32(c)))
		}
		return 1
	case c >= 0xfd:
		w.writeRune(rune(0xf8f1 - 0xfd + uint32(c)))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	c2 := in[1]
	if row := cp932ext_decmap[c]; row.Map != nil {
		if dec, ok := tryMapDec(&row, c2); ok {
			w.writeRune(rune(dec))
			return 2
		}
	}
	if (c >= 0x81 && c <= 0x9f) || (c >= 0xe0 && c <= 0xea) {
		if c2 < 0x40 || (c2 > 0x7e && c2 < 0x80) || c2 > 0xfc {
			return 1
		}
		var c1 byte
		if c < 0xe0 {
			c1 = c - 0x81
		} else {
			c1 = c - 0xc1
		}
		if c2 < 0x80 {
			c2 -= 0x40
		} else {
			c2 -= 0x41
		}
		var addRow byte
		if c2 >= 0x5e {
			addRow = 1
		}
		c1 = 2*c1 + addRow + 0x21
		if c2 < 0x5e {
			c2 = c2 + 0x21
		} else {
			c2 = c2 - 0x5e + 0x21
		}
		row := jisx0208_decmap[c1]
		if dec, ok := tryMapDec(&row, c2); ok {
			w.writeRune(rune(dec))
			return 2
		}
		return 1
	}
	if c >= 0xf0 && c <= 0xf9 {
		if (c2 >= 0x40 && c2 <= 0x7e) || (c2 >= 0x80 && c2 <= 0xfc) {
			var sub uint32
			if c2 < 0x80 {
				sub = uint32(c2) - 0x40
			} else {
				sub = uint32(c2) - 0x41
			}
			w.writeRune(rune(0xe000 + 188*(uint32(c)-0xf0) + sub))
			return 2
		}
		return 1
	}
	return 1
}

// CPython: _codecs_jp.c:332 ENCODER(euc_jp)
func encodeEUCJP(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	if code, ok := tryMapEnc(&jisxcommon_encmap[c>>8], byte(c&0xff)); ok {
		if code&0x8000 != 0 {
			out.writeByte(0x8f)
			out.writeByte(byte(code >> 8))
			out.writeByte(byte(code&0xff) | 0x80)
		} else {
			out.writeByte(byte(code>>8) | 0x80)
			out.writeByte(byte(code&0xff) | 0x80)
		}
		return 1
	}
	if c >= 0xff61 && c <= 0xff9f {
		out.writeByte(0x8e)
		out.writeByte(byte(c - 0xfec0))
		return 1
	}
	switch c {
	case 0xff3c:
		out.writeByte(0xa1 | 0x80)
		out.writeByte(0x40 | 0x80)
		return 1
	case 0xa5:
		out.writeByte(0x5c)
		return 1
	case 0x203e:
		out.writeByte(0x7e)
		return 1
	}
	return 1
}

// CPython: _codecs_jp.c:385 DECODER(euc_jp)
func decodeEUCJP(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if c == 0x8e {
		if len(in) < 2 {
			return MBERR_TOOFEW
		}
		c2 := in[1]
		if c2 >= 0xa1 && c2 <= 0xdf {
			w.writeRune(rune(0xfec0 + uint32(c2)))
			return 2
		}
		return 1
	}
	if c == 0x8f {
		if len(in) < 3 {
			return MBERR_TOOFEW
		}
		row := jisx0212_decmap[in[1]^0x80]
		if dec, ok := tryMapDec(&row, in[2]^0x80); ok {
			w.writeRune(rune(dec))
			return 3
		}
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	c2 := in[1]
	if c == 0xa1 && c2 == 0xc0 {
		w.writeRune(0xff3c)
		return 2
	}
	row := jisx0208_decmap[c^0x80]
	if dec, ok := tryMapDec(&row, c2^0x80); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}

// CPython: _codecs_jp.c:452 ENCODER(shift_jis)
func encodeShiftJIS(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	var code uint16
	hasCode := false
	switch {
	case c < 0x80:
		code = uint16(c)
		hasCode = true
	case c == 0x00a5:
		code = 0x5c
		hasCode = true
	case c == 0x203e:
		code = 0x7e
		hasCode = true
	default:
		if k, ok := jisx0201KEncodeChar(c); ok {
			code = k
			hasCode = true
		}
	}
	if hasCode && (code < 0x80 || (code >= 0xa1 && code <= 0xdf)) {
		out.writeByte(byte(code))
		return 1
	}
	if !hasCode {
		k, ok := tryMapEnc(&jisxcommon_encmap[c>>8], byte(c&0xff))
		if ok {
			if k&0x8000 != 0 { // JIS X 0212
				return 1
			}
			code = k
			hasCode = true
		} else if c == 0xff3c {
			code = 0x2140
			hasCode = true
		} else {
			return 1
		}
	}
	c1 := byte(code >> 8)
	c2 := byte(code & 0xff)
	var add byte
	if (c1-0x21)&1 != 0 {
		add = 0x5e
	}
	c2 = add + (c2 - 0x21)
	c1 = (c1 - 0x21) >> 1
	if c1 < 0x1f {
		out.writeByte(c1 + 0x81)
	} else {
		out.writeByte(c1 + 0xc1)
	}
	if c2 < 0x3f {
		out.writeByte(c2 + 0x40)
	} else {
		out.writeByte(c2 + 0x41)
	}
	return 1
}

// CPython: _codecs_jp.c:511 DECODER(shift_jis)
func decodeShiftJIS(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if jisx0201KDecode(c, w) {
		return 1
	}
	if (c >= 0x81 && c <= 0x9f) || (c >= 0xe0 && c <= 0xea) {
		if len(in) < 2 {
			return MBERR_TOOFEW
		}
		c2 := in[1]
		if c2 < 0x40 || (c2 > 0x7e && c2 < 0x80) || c2 > 0xfc {
			return 1
		}
		var c1 byte
		if c < 0xe0 {
			c1 = c - 0x81
		} else {
			c1 = c - 0xc1
		}
		if c2 < 0x80 {
			c2 -= 0x40
		} else {
			c2 -= 0x41
		}
		var addRow byte
		if c2 >= 0x5e {
			addRow = 1
		}
		c1 = 2*c1 + addRow + 0x21
		if c2 < 0x5e {
			c2 += 0x21
		} else {
			c2 = c2 - 0x5e + 0x21
		}
		if c1 == 0x21 && c2 == 0x40 {
			w.writeRune(0xff3c)
			return 2
		}
		row := jisx0208_decmap[c1]
		if dec, ok := tryMapDec(&row, c2); ok {
			w.writeRune(rune(dec))
			return 2
		}
		return 1
	}
	return 1
}

// shiftJISDecodeRoman handles the Roman half (0x00..0x7f) including
// the strict-build pairing for 0x5c / 0x7e. shift_jis stays in
// non-strict mode, so bare 0x5c / 0x7e pass through unchanged.
func shiftJISDecodeRoman(c byte, w *unicodeWriter) bool {
	if c < 0x80 {
		w.writeRune(rune(c))
		return true
	}
	return false
}

// CPython: _codecs_jp.c:151 ENCODER(euc_jis_2004)
func encodeEUCJIS2004(st *codecState, input []rune, inpos int, out *encodeBuffer, flags int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	insize := 1
	var code uint16
	gotCode := false
	switch {
	case c <= 0xFFFF:
		if k, ok, abandon := emulate2000EncodeBMP(st.config, c); abandon {
			return 1
		} else if ok {
			code = k
			gotCode = true
		} else if k, ok := tryMapEnc(&jisx0213_bmp_encmap[c>>8], byte(c&0xff)); ok {
			if k == MULTIC {
				if inpos+1 >= len(input) {
					if flags&MBENC_FLUSH != 0 {
						r := findPairEnc(uint16(c), 0, jisx0213_pair_encmap[:])
						if r == DBCINV {
							return 1
						}
						code = r
						gotCode = true
					} else {
						return MBERR_TOOFEW
					}
				} else {
					c2 := uint32(input[inpos+1])
					r := findPairEnc(uint16(c), uint16(c2), jisx0213_pair_encmap[:])
					if r == DBCINV {
						r = findPairEnc(uint16(c), 0, jisx0213_pair_encmap[:])
						if r == DBCINV {
							return 1
						}
						code = r
						gotCode = true
					} else {
						code = r
						gotCode = true
						if c2 != 0 {
							insize = 2
						}
					}
				}
			} else {
				code = k
				gotCode = true
			}
		} else if k, ok := tryMapEnc(&jisxcommon_encmap[c>>8], byte(c&0xff)); ok {
			code = k
			gotCode = true
		} else if c >= 0xff61 && c <= 0xff9f {
			out.writeByte(0x8e)
			out.writeByte(byte(c - 0xfec0))
			return 1
		} else if c == 0xff3c {
			code = 0x2140
			gotCode = true
		} else if c == 0xff5e {
			code = 0x2232
			gotCode = true
		} else {
			return 1
		}
	case c>>16 == empBase>>16:
		if emulate2000EncodeEMP(st.config, c) {
			return insize
		}
		if k, ok := tryMapEnc(&jisx0213_emp_encmap[(c&0xffff)>>8], byte(c&0xff)); ok {
			code = k
			gotCode = true
		} else {
			return insize
		}
	default:
		return insize
	}
	if !gotCode {
		return insize
	}
	if code&0x8000 != 0 {
		out.writeByte(0x8f)
		out.writeByte(byte(code >> 8))
		out.writeByte(byte(code&0xff) | 0x80)
	} else {
		out.writeByte(byte(code>>8) | 0x80)
		out.writeByte(byte(code&0xff) | 0x80)
	}
	return insize
}

// CPython: _codecs_jp.c:244 DECODER(euc_jis_2004)
func decodeEUCJIS2004(st *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if c == 0x8e {
		if len(in) < 2 {
			return MBERR_TOOFEW
		}
		c2 := in[1]
		if c2 >= 0xa1 && c2 <= 0xdf {
			w.writeRune(rune(0xfec0 + uint32(c2)))
			return 2
		}
		return 1
	}
	if c == 0x8f {
		if len(in) < 3 {
			return MBERR_TOOFEW
		}
		c2 := in[1] ^ 0x80
		c3 := in[2] ^ 0x80
		if emulate2000DecodePlane2(st.config, c2, c3, w) {
			return 3
		}
		row := jisx0213_2_bmp_decmap[c2]
		if dec, ok := tryMapDec(&row, c3); ok {
			w.writeRune(rune(dec))
			return 3
		}
		rowEmp := jisx0213_2_emp_decmap[c2]
		if dec, ok := tryMapDec(&rowEmp, c3); ok {
			w.writeRune(rune(empBase | uint32(dec)))
			return 3
		}
		row212 := jisx0212_decmap[c2]
		if dec, ok := tryMapDec(&row212, c3); ok {
			w.writeRune(rune(dec))
			return 3
		}
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	c ^= 0x80
	c2 := in[1] ^ 0x80
	if emulate2000DecodePlane1(st.config, c, c2) {
		return 1
	}
	if c == 0x21 && c2 == 0x40 {
		w.writeRune(0xff3c)
		return 2
	}
	if c == 0x22 && c2 == 0x32 {
		w.writeRune(0xff5e)
		return 2
	}
	row := jisx0208_decmap[c]
	if dec, ok := tryMapDec(&row, c2); ok {
		w.writeRune(rune(dec))
		return 2
	}
	rowBMP := jisx0213_1_bmp_decmap[c]
	if dec, ok := tryMapDec(&rowBMP, c2); ok {
		w.writeRune(rune(dec))
		return 2
	}
	rowEmp := jisx0213_1_emp_decmap[c]
	if dec, ok := tryMapDec(&rowEmp, c2); ok {
		w.writeRune(rune(empBase | uint32(dec)))
		return 2
	}
	rowPair := jisx0213_pair_decmap[c]
	if dec, ok := tryMapWideDec(&rowPair, c2); ok {
		w.writeRune(rune(dec >> 16))
		w.writeRune(rune(dec & 0xffff))
		return 2
	}
	return 1
}

// CPython: _codecs_jp.c:567 ENCODER(shift_jis_2004)
func encodeShiftJIS2004(st *codecState, input []rune, inpos int, out *encodeBuffer, flags int) int {
	c := uint32(input[inpos])
	var code uint16
	codeIsSet := false

	if r, ok := jisx0201REncodeChar(c); ok {
		code = r
		codeIsSet = true
	} else if k, ok := jisx0201KEncodeChar(c); ok {
		code = k
		codeIsSet = true
	}

	if codeIsSet && (code < 0x80 || (code >= 0xa1 && code <= 0xdf)) {
		out.writeByte(byte(code))
		return 1
	}

	insize := 1
	if !codeIsSet {
		switch {
		case c <= 0xffff:
			if k, ok, abandon := emulate2000EncodeBMP(st.config, c); abandon {
				return 1
			} else if ok {
				code = k
				codeIsSet = true
			} else if k, ok := tryMapEnc(&jisx0213_bmp_encmap[c>>8], byte(c&0xff)); ok {
				if k == MULTIC {
					if inpos+1 >= len(input) {
						if flags&MBENC_FLUSH != 0 {
							r := findPairEnc(uint16(c), 0, jisx0213_pair_encmap[:])
							if r == DBCINV {
								return 1
							}
							code = r
							codeIsSet = true
						} else {
							return MBERR_TOOFEW
						}
					} else {
						c2 := uint32(input[inpos+1])
						r := findPairEnc(uint16(c), uint16(c2), jisx0213_pair_encmap[:])
						if r == DBCINV {
							r = findPairEnc(uint16(c), 0, jisx0213_pair_encmap[:])
							if r == DBCINV {
								return 1
							}
							code = r
							codeIsSet = true
						} else {
							code = r
							codeIsSet = true
							if c2 != 0 {
								insize = 2
							}
						}
					}
				} else {
					code = k
					codeIsSet = true
				}
			} else if k, ok := tryMapEnc(&jisxcommon_encmap[c>>8], byte(c&0xff)); ok {
				if k&0x8000 != 0 {
					return 1
				}
				code = k
				codeIsSet = true
			} else {
				return 1
			}
		case c>>16 == empBase>>16:
			if emulate2000EncodeEMP(st.config, c) {
				return insize
			}
			if k, ok := tryMapEnc(&jisx0213_emp_encmap[(c&0xffff)>>8], byte(c&0xff)); ok {
				code = k
				codeIsSet = true
			} else {
				return insize
			}
		default:
			return insize
		}
	}
	if !codeIsSet {
		return insize
	}
	c1 := int(code >> 8)
	c2 := int(code&0xff) - 0x21
	if c1&0x80 != 0 {
		// Plane 2
		switch {
		case c1 >= 0xee:
			c1 -= 0x87
		case c1 >= 0xac || c1 == 0xa8:
			c1 -= 0x49
		default:
			c1 -= 0x43
		}
	} else {
		c1 -= 0x21
	}
	if c1&1 != 0 {
		c2 += 0x5e
	}
	c1 >>= 1
	if c1 < 0x1f {
		out.writeByte(byte(c1 + 0x81))
	} else {
		out.writeByte(byte(c1 + 0xc1))
	}
	if c2 < 0x3f {
		out.writeByte(byte(c2 + 0x40))
	} else {
		out.writeByte(byte(c2 + 0x41))
	}
	return insize
}

// CPython: _codecs_jp.c:672 DECODER(shift_jis_2004)
func decodeShiftJIS2004(st *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		// JISX0201_DECODE: Roman set, lets 0x5c -> 0x00a5 / 0x7e -> 0x203e.
		switch {
		case c < 0x5c:
			w.writeRune(rune(c))
		case c == 0x5c:
			w.writeRune(0x00a5)
		case c < 0x7e:
			w.writeRune(rune(c))
		case c == 0x7e:
			w.writeRune(0x203e)
		case c == 0x7f:
			w.writeRune(0x7f)
		}
		return 1
	}
	if jisx0201KDecode(c, w) {
		return 1
	}
	if (c >= 0x81 && c <= 0x9f) || (c >= 0xe0 && c <= 0xfc) {
		if len(in) < 2 {
			return MBERR_TOOFEW
		}
		c2 := in[1]
		if c2 < 0x40 || (c2 > 0x7e && c2 < 0x80) || c2 > 0xfc {
			return 1
		}
		var c1 byte
		if c < 0xe0 {
			c1 = c - 0x81
		} else {
			c1 = c - 0xc1
		}
		if c2 < 0x80 {
			c2 -= 0x40
		} else {
			c2 -= 0x41
		}
		var addRow byte
		if c2 >= 0x5e {
			addRow = 1
		}
		c1 = 2*c1 + addRow
		if c2 < 0x5e {
			c2 += 0x21
		} else {
			c2 = c2 - 0x5e + 0x21
		}
		if c1 < 0x5e {
			// Plane 1
			c1 += 0x21
			if emulate2000DecodePlane1(st.config, c1, c2) {
				return 1
			}
			row := jisx0208_decmap[c1]
			if dec, ok := tryMapDec(&row, c2); ok {
				w.writeRune(rune(dec))
				return 2
			}
			rowBMP := jisx0213_1_bmp_decmap[c1]
			if dec, ok := tryMapDec(&rowBMP, c2); ok {
				w.writeRune(rune(dec))
				return 2
			}
			rowEmp := jisx0213_1_emp_decmap[c1]
			if dec, ok := tryMapDec(&rowEmp, c2); ok {
				w.writeRune(rune(empBase | uint32(dec)))
				return 2
			}
			rowPair := jisx0213_pair_decmap[c1]
			if dec, ok := tryMapWideDec(&rowPair, c2); ok {
				w.writeRune(rune(dec >> 16))
				w.writeRune(rune(dec & 0xffff))
				return 2
			}
			return 1
		}
		// Plane 2
		switch {
		case c1 >= 0x67:
			c1 += 0x07
		case c1 >= 0x63 || c1 == 0x5f:
			c1 -= 0x37
		default:
			c1 -= 0x3d
		}
		if emulate2000DecodePlane2(st.config, c1, c2, w) {
			return 2
		}
		row := jisx0213_2_bmp_decmap[c1]
		if dec, ok := tryMapDec(&row, c2); ok {
			w.writeRune(rune(dec))
			return 2
		}
		rowEmp := jisx0213_2_emp_decmap[c1]
		if dec, ok := tryMapDec(&rowEmp, c2); ok {
			w.writeRune(rune(empBase | uint32(dec)))
			return 2
		}
		return 1
	}
	return 1
}
