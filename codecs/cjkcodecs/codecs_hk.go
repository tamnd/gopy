// Hong Kong codec port: big5hkscs. Hand-translated 1:1 from CPython
// Modules/cjkcodecs/_codecs_hk.c so the four HKSCS pair-combining
// rules and the phint disambiguation for U+20000-plane code points
// match byte-for-byte.
//
// CPython: Modules/cjkcodecs/_codecs_hk.c

package cjkcodecs

// big5hkscsPairencTable mirrors CPython's static table of HKSCS pair
// codes for the four (base, combining) sequences listed at
// _codecs_hk.c:30. CPython picks the entry with ((c>>4) | (c2>>3)) & 3.
//
// CPython: _codecs_hk.c:37
var big5hkscsPairencTable = [4]uint16{0x8862, 0x8864, 0x88a3, 0x88a5}

// bh2s mirrors CPython's BH2S macro. It maps a Big5 byte pair to a
// linear offset within the per-band phint tables.
//
// CPython: _codecs_hk.c:108 BH2S
func bh2s(c1, c2 uint8) int {
	return (int(c1)-0x87)*(0xfe-0x40+1) + (int(c2) - 0x40)
}

// CPython: _codecs_hk.c:39 ENCODER(big5hkscs)
func encodeBig5HKSCS(_ *codecState, input []rune, inpos int, out *encodeBuffer, flags int) int {
	c := uint32(input[inpos])
	if c < 0x80 {
		out.writeByte(byte(c))
		return 1
	}
	insize := 1
	var code uint16
	switch {
	case c < 0x10000:
		if v, ok := tryMapEnc(&big5hkscs_bmp_encmap[c>>8], byte(c&0xff)); ok {
			code = v
			if code == MULTIC {
				var c2 uint32
				if len(input)-inpos >= 2 {
					c2 = uint32(input[inpos+1])
				}
				switch {
				case len(input)-inpos >= 2 &&
					(c&0xffdf) == 0x00ca &&
					(c2&0xfff7) == 0x0304:
					code = big5hkscsPairencTable[((c>>4)|(c2>>3))&3]
					insize = 2
				case len(input)-inpos < 2 && (flags&MBENC_FLUSH) == 0:
					return MBERR_TOOFEW
				default:
					if c == 0xca {
						code = 0x8866
					} else {
						code = 0x88a7
					}
				}
			}
		} else if v, ok := tryMapEnc(&big5_encmap[c>>8], byte(c&0xff)); ok {
			code = v
		} else {
			return 1
		}
	case c < 0x20000:
		return insize
	case c < 0x30000:
		if v, ok := tryMapEnc(&big5hkscs_nonbmp_encmap[(c&0xffff)>>8], byte(c&0xff)); ok {
			code = v
		} else {
			return insize
		}
	default:
		return insize
	}
	out.writeBytes(byte(code>>8), byte(code&0xff))
	return insize
}

// CPython: _codecs_hk.c:110 DECODER(big5hkscs)
func decodeBig5HKSCS(_ *codecState, in []byte, w *unicodeWriter) int {
	c := in[0]
	if c < 0x80 {
		w.writeRune(rune(c))
		return 1
	}
	if len(in) < 2 {
		return MBERR_TOOFEW
	}
	c2 := in[1]
	if c < 0xc6 || c > 0xc8 || (c < 0xc7 && c2 < 0xa1) {
		row := big5_decmap[c]
		if dec, ok := tryMapDec(&row, c2); ok {
			w.writeRune(rune(dec))
			return 2
		}
	}
	row := big5hkscs_decmap[c]
	if dec, ok := tryMapDec(&row, c2); ok {
		s := bh2s(c, c2)
		var hintbase []byte
		switch {
		case bh2s(0x87, 0x40) <= s && s <= bh2s(0xa0, 0xfe):
			hintbase = big5hkscs_phint_0[:]
			s -= bh2s(0x87, 0x40)
		case bh2s(0xc6, 0xa1) <= s && s <= bh2s(0xc8, 0xfe):
			hintbase = big5hkscs_phint_12130[:]
			s -= bh2s(0xc6, 0xa1)
		case bh2s(0xf9, 0xd6) <= s && s <= bh2s(0xfe, 0xfe):
			hintbase = big5hkscs_phint_21924[:]
			s -= bh2s(0xf9, 0xd6)
		default:
			return MBERR_INTERNAL
		}
		if hintbase[s>>3]&(1<<(s&7)) != 0 {
			w.writeRune(rune(uint32(dec) | 0x20000))
		} else {
			w.writeRune(rune(dec))
		}
		return 2
	}
	switch (uint16(c) << 8) | uint16(c2) {
	case 0x8862:
		w.writeRune(0x00ca)
		w.writeRune(0x0304)
	case 0x8864:
		w.writeRune(0x00ca)
		w.writeRune(0x030c)
	case 0x88a3:
		w.writeRune(0x00ea)
		w.writeRune(0x0304)
	case 0x88a5:
		w.writeRune(0x00ea)
		w.writeRune(0x030c)
	default:
		return 1
	}
	return 2
}
