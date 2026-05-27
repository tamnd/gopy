// Mainland Chinese codec ports: gb2312, gbk, gb18030, hz.
// Hand-translated 1:1 from CPython Modules/cjkcodecs/_codecs_cn.c so
// the GBK / GB2312 divergences (A1A4, A1AA, A844) and the GB18030
// four-byte range table walk match byte-for-byte.
//
// CPython: Modules/cjkcodecs/_codecs_cn.c

package cjkcodecs

// gbkDecodeStep mirrors the GBK_DECODE macro at _codecs_cn.c:26 and
// returns true when one of the three rewrite cases (or the gb2312 /
// gbkext fall-through) consumes (dc1, dc2). Decoded runes are written
// to w. The caller advances by two bytes when this returns true.
//
// CPython: _codecs_cn.c:26 GBK_DECODE
func gbkDecodeStep(dc1, dc2 uint8, w *unicodeWriter) bool {
	switch {
	case dc1 == 0xa1 && dc2 == 0xaa:
		w.writeRune(0x2014)
		return true
	case dc1 == 0xa8 && dc2 == 0x44:
		w.writeRune(0x2015)
		return true
	case dc1 == 0xa1 && dc2 == 0xa4:
		w.writeRune(0x00b7)
		return true
	}
	row := gb2312_decmap[dc1^0x80]
	if dec, ok := tryMapDec(&row, dc2^0x80); ok {
		w.writeRune(rune(dec))
		return true
	}
	row2 := gbkext_decmap[dc1]
	if dec, ok := tryMapDec(&row2, dc2); ok {
		w.writeRune(rune(dec))
		return true
	}
	return false
}

// gbkEncodeStep mirrors the GBK_ENCODE macro at _codecs_cn.c:43.
// Returns (code, true) when the codepoint maps under the GBK layering;
// otherwise the caller chains into the next encoder branch.
//
// CPython: _codecs_cn.c:43 GBK_ENCODE
func gbkEncodeStep(c uint32) (uint16, bool) {
	switch c {
	case 0x2014:
		return 0xa1aa, true
	case 0x2015:
		return 0xa844, true
	case 0x00b7:
		return 0xa1a4, true
	}
	if c == 0x30fb {
		return 0, false
	}
	return tryMapEnc(&gbcommon_encmap[c>>8], byte(c&0xff))
}

// CPython: _codecs_cn.c:64 ENCODER(gb2312)
func encodeGB2312(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	code, ok := tryMapEnc(&gbcommon_encmap[c>>8], byte(c&0xff))
	if !ok {
		return 1
	}
	if code&0x8000 != 0 {
		return 1
	}
	out.writeBytes(byte((code>>8)|0x80), byte((code&0xff)|0x80))
	return 1
}

// CPython: _codecs_cn.c:96 DECODER(gb2312)
func decodeGB2312(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	row := gb2312_decmap[c^0x80]
	if dec, ok := tryMapDec(&row, in[1]^0x80); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}

// CPython: _codecs_cn.c:125 ENCODER(gbk)
func encodeGBK(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	code, ok := gbkEncodeStep(c)
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

// CPython: _codecs_cn.c:157 DECODER(gbk)
func decodeGBK(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	if !gbkDecodeStep(c, in[1], w) {
		return 1
	}
	return 2
}

// CPython: _codecs_cn.c:186 ENCODER(gb18030)
func encodeGB18030(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c >= 0xD800 && c <= 0xDFFF {
		// Surrogates are not valid Unicode codepoints; CPython rejects them.
		// Return 1 with no bytes written so the outer loop treats it as unmapped.
		return 1
	}
	if c >= 0x10000 {
		tc := c - 0x10000
		b4 := byte(tc%10) + 0x30
		tc /= 10
		b3 := byte(tc%126) + 0x81
		tc /= 126
		b2 := byte(tc%10) + 0x30
		tc /= 10
		b1 := byte(tc) + 0x90
		out.writeBytes(b1, b2, b3, b4)
		return 1
	}
	if code, ok := gbkEncodeStep(c); ok {
		out.writeByte(byte((code >> 8) | 0x80))
		if code&0x8000 != 0 {
			out.writeByte(byte(code & 0xff))
		} else {
			out.writeByte(byte((code & 0xff) | 0x80))
		}
		return 1
	}
	if code, ok := tryMapEnc(&gb18030ext_encmap[c>>8], byte(c&0xff)); ok {
		out.writeByte(byte((code >> 8) | 0x80))
		if code&0x8000 != 0 {
			out.writeByte(byte(code & 0xff))
		} else {
			out.writeByte(byte((code & 0xff) | 0x80))
		}
		return 1
	}
	for i := 0; i < len(gb18030_to_unibmp_ranges); i++ {
		r := gb18030_to_unibmp_ranges[i]
		if r.First == 0 {
			return 1
		}
		if r.First <= c && c <= r.Last {
			tc := c - r.First + uint32(r.Base)
			b4 := byte(tc%10) + 0x30
			tc /= 10
			b3 := byte(tc%126) + 0x81
			tc /= 126
			b2 := byte(tc%10) + 0x30
			tc /= 10
			b1 := byte(tc) + 0x81
			out.writeBytes(b1, b2, b3, b4)
			return 1
		}
	}
	return 1
}

// CPython: _codecs_cn.c:265 DECODER(gb18030)
func decodeGB18030(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	c2 := in[1]
	if c2 >= 0x30 && c2 <= 0x39 {
		if len(in) < 4 {
			return MBERR_TOOFEW
		}
		c3 := in[2]
		c4 := in[3]
		if c < 0x81 || c > 0xFE || c3 < 0x81 || c3 > 0xFE || c4 < 0x30 || c4 > 0x39 {
			return 1
		}
		bc := uint32(c) - 0x81
		bc2 := uint32(c2) - 0x30
		bc3 := uint32(c3) - 0x81
		bc4 := uint32(c4) - 0x30
		switch {
		case bc < 4:
			lseq := (bc*10+bc2)*1260 + bc3*10 + bc4
			if lseq < 39420 {
				idx := 0
				for idx+1 < len(gb18030_to_unibmp_ranges) && lseq >= uint32(gb18030_to_unibmp_ranges[idx+1].Base) {
					idx++
				}
				utr := gb18030_to_unibmp_ranges[idx]
				w.writeRune(rune(utr.First - uint32(utr.Base) + lseq))
				return 4
			}
		case bc >= 15:
			lseq := 0x10000 + (bc-15)*10*1260 + bc2*1260 + bc3*10 + bc4
			if lseq <= 0x10FFFF {
				w.writeRune(rune(lseq))
				return 4
			}
		}
		return 1
	}
	if !gbkDecodeStep(c, c2, w) {
		row := gb18030ext_decmap[c]
		if dec, ok := tryMapDec(&row, c2); ok {
			w.writeRune(rune(dec))
			return 2
		}
		return 1
	}
	return 2
}

// cnStateOffset is the slot in codecState.cBytes that the HZ codec
// uses to record "are we currently in GB mode?".
//
// CPython: _codecs_cn.c:58 CN_STATE_OFFSET
const cnStateOffset = 0

// CPython: _codecs_cn.c:352 ENCODER(hz)
func encodeHZ(state *codecState, input []rune, inpos int, out *encodeBuffer, flags int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		if state.cBytes[cnStateOffset] != 0 {
			out.writeBytes('~', '}')
			state.cBytes[cnStateOffset] = 0
		}
		out.writeByte(byte(c))
		if c == '~' {
			out.writeByte('~')
		}
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	code, ok := tryMapEnc(&gbcommon_encmap[c>>8], byte(c&0xff))
	if !ok {
		return 1
	}
	if code&0x8000 != 0 {
		return 1
	}
	if state.cBytes[cnStateOffset] == 0 {
		out.writeBytes('~', '{', byte(code>>8), byte(code&0xff))
		state.cBytes[cnStateOffset] = 1
	} else {
		out.writeBytes(byte(code>>8), byte(code&0xff))
	}
	_ = flags
	return 1
}

// hzEncodeReset emits a closing `~}` sequence when leaving GB mode at
// the end of a stream. Mirrors ENCODER_RESET(hz) at _codecs_cn.c:342.
func hzEncodeReset(state *codecState, out *encodeBuffer) {
	if state.cBytes[cnStateOffset] != 0 {
		out.writeBytes('~', '}')
		state.cBytes[cnStateOffset] = 0
	}
}

// CPython: _codecs_cn.c:410 DECODER(hz)
func decodeHZ(state *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c == '~' {
		if len(in) < 2 {
			return MBERR_TOOFEW
		}
		c2 := in[1]
		switch {
		case c2 == '~' && state.cBytes[cnStateOffset] == 0:
			w.writeRune('~')
		case c2 == '{' && state.cBytes[cnStateOffset] == 0:
			state.cBytes[cnStateOffset] = 1
			w.stateAdv = 2
		case c2 == '\n' && state.cBytes[cnStateOffset] == 0:
			// line continuation, drop
			w.stateAdv = 2
		case c2 == '}' && state.cBytes[cnStateOffset] == 1:
			state.cBytes[cnStateOffset] = 0
			w.stateAdv = 2
		default:
			return 1
		}
		return 2
	}
	if c&0x80 != 0 {
		return 1
	}
	if state.cBytes[cnStateOffset] == 0 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	row := gb2312_decmap[c]
	if dec, ok := tryMapDec(&row, in[1]); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}
