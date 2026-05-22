// Taiwanese codec ports: big5, cp950. Hand-translated 1:1 from
// CPython Modules/cjkcodecs/_codecs_tw.c so the trailing-byte windows
// (cp950 ext layered ahead of big5) match byte-for-byte.
//
// CPython: Modules/cjkcodecs/_codecs_tw.c

package cjkcodecs

// CPython: _codecs_tw.c:14 ENCODER(big5)
func encodeBig5(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	code, ok := tryMapEnc(&big5_encmap[c>>8], byte(c&0xff))
	if !ok {
		return 1
	}
	out.writeBytes(byte(code>>8), byte(code&0xff))
	return 1
}

// CPython: _codecs_tw.c:45 DECODER(big5)
func decodeBig5(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	row := big5_decmap[c]
	if dec, ok := tryMapDec(&row, in[1]); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}

// CPython: _codecs_tw.c:73 ENCODER(cp950)
func encodeCP950(_ *codecState, input []rune, inpos int, out *encodeBuffer, _ int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	if c > 0xFFFF {
		return 1
	}
	var code uint16
	if v, ok := tryMapEnc(&cp950ext_encmap[c>>8], byte(c&0xff)); ok {
		code = v
	} else if v, ok := tryMapEnc(&big5_encmap[c>>8], byte(c&0xff)); ok {
		code = v
	} else {
		return 1
	}
	out.writeBytes(byte(code>>8), byte(code&0xff))
	return 1
}

// CPython: _codecs_tw.c:104 DECODER(cp950)
func decodeCP950(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	row := cp950ext_decmap[c]
	if dec, ok := tryMapDec(&row, in[1]); ok {
		w.writeRune(rune(dec))
		return 2
	}
	row2 := big5_decmap[c]
	if dec, ok := tryMapDec(&row2, in[1]); ok {
		w.writeRune(rune(dec))
		return 2
	}
	return 1
}
