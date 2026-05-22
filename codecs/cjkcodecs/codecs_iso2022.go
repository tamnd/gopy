// ISO-2022 codec ports: iso2022_kr / iso2022_jp / iso2022_jp_1 /
// iso2022_jp_2 / iso2022_jp_2004 / iso2022_jp_3 / iso2022_jp_ext.
// Hand-translated 1:1 from CPython Modules/cjkcodecs/_codecs_iso2022.c
// so the G0..G3 designation stack, the shift-in / shift-out gating,
// and the per-variant designation tables match byte-for-byte.
//
// CPython: Modules/cjkcodecs/_codecs_iso2022.c

package cjkcodecs

const (
	iso2022ESC = 0x1B
	iso2022SO  = 0x0E
	iso2022SI  = 0x0F
	iso2022LF  = 0x0A

	maxEscSeqLen = 16

	charsetISO88591   = 'A'
	charsetASCII      = 'B'
	charsetISO88597   = 'F'
	charsetJISX0201K  = 'I'
	charsetJISX0201R  = 'J'
	charsetDBCSMark   = 0x80
	charsetGB2312     = 'A' | charsetDBCSMark
	charsetJISX0208   = 'B' | charsetDBCSMark
	charsetKSX1001    = 'C' | charsetDBCSMark
	charsetJISX0212   = 'D' | charsetDBCSMark
	charsetJISX02131  = 'O' | charsetDBCSMark
	charsetJISX02132  = 'P' | charsetDBCSMark
	charsetJISX020041 = 'Q' | charsetDBCSMark
	charsetJISX0208O  = '@' | charsetDBCSMark

	mapUnmappable    = 0xFFFF
	mapMultipleAvail = 0xFFFE

	flagShifted       = 0x01
	flagEscThroughout = 0x02

	configNoShift      = 0x01
	configUseG2        = 0x02
	configUseJISX0208E = 0x04
)

// iso2022 state lives in codecState.cBytes:
//
//	cBytes[0..3] = G0..G3 designation marks (high bit indicates DBCS).
//	cBytes[4]    = bit-flags (F_SHIFTED / F_ESCTHROUGHOUT).
//
// CPython: _codecs_iso2022.c:39
func iso2022GetG(state *codecState, n int) uint8     { return state.cBytes[n] }
func iso2022SetG(state *codecState, n int, v uint8)  { state.cBytes[n] = v }
func iso2022GetFlag(state *codecState, f uint8) bool { return state.cBytes[4]&f != 0 }
func iso2022SetFlag(state *codecState, f uint8)      { state.cBytes[4] |= f }
func iso2022ClrFlag(state *codecState, f uint8)      { state.cBytes[4] &^= f }
func iso2022ClrFlags(state *codecState)              { state.cBytes[4] = 0 }

// iso2022Decoder turns a (charset, two-byte) pair into a unicode
// codepoint. For wide / paired tables it can return values in the
// 0x10000..0x30000 plane or, encoded with bits 31..16 set, a pair of
// codepoints that the caller must split.
//
// CPython: _codecs_iso2022.c:126 iso2022_decode_func
type iso2022Decoder func(state *codecState, data []byte) uint32

// iso2022Encoder turns a unicode codepoint (or pair) into a DBCHAR.
// length is the input rune count: 1 on first call, 2 when the codec
// asks for the next rune after the pre-emption return
// mapMultipleAvail.
//
// CPython: _codecs_iso2022.c:128 iso2022_encode_func
type iso2022Encoder func(state *codecState, data []uint32, length *int) uint32

// iso2022Designation maps one charset selector (its escape-sequence
// mark) to its plane (G0..G3), width (1 or 2), and the encode / decode
// pair the runtime calls when the designation is active.
//
// CPython: _codecs_iso2022.c:132 struct iso2022_designation
type iso2022Designation struct {
	mark    uint8
	plane   uint8
	width   uint8
	decoder iso2022Decoder
	encoder iso2022Encoder
}

// iso2022Config is the per-variant tuning: which designations the
// codec is allowed to switch into and the gating flags (no-shift,
// USE_G2, USE_JISX0208_EXT).
//
// CPython: _codecs_iso2022.c:141 struct iso2022_config
type iso2022Config struct {
	flags        int
	designations []iso2022Designation
}

func iso2022CfgIsSet(cfg *iso2022Config, flag int) bool {
	return cfg.flags&flag != 0
}

// CPython: _codecs_iso2022.c:159 ENCODER_INIT(iso2022)
func iso2022EncodeInit(state *codecState) {
	iso2022ClrFlags(state)
	iso2022SetG(state, 0, charsetASCII)
	iso2022SetG(state, 1, charsetASCII)
}

// CPython: _codecs_iso2022.c:167 ENCODER_RESET(iso2022)
func iso2022EncodeReset(state *codecState, out *encodeBuffer) {
	if iso2022GetFlag(state, flagShifted) {
		out.writeByte(iso2022SI)
		iso2022ClrFlag(state, flagShifted)
	}
	if iso2022GetG(state, 0) != charsetASCII {
		out.writeBytes(iso2022ESC, '(', 'B')
		iso2022SetG(state, 0, charsetASCII)
	}
}

// CPython: _codecs_iso2022.c:303 DECODER_INIT(iso2022)
func iso2022DecodeInit(state *codecState) {
	iso2022ClrFlags(state)
	iso2022SetG(state, 0, charsetASCII)
	iso2022SetG(state, 1, charsetASCII)
	iso2022SetG(state, 2, charsetASCII)
}

func isEscEnd(c byte) bool { return (c >= 'A' && c <= 'Z') || c == '@' }
func isISO2022Esc(c byte) bool {
	return c == '(' || c == ')' || c == '$' || c == '.' || c == '&'
}
func escMark(m uint8) byte { return m & 0x7f }

// makeISO2022Encoder returns an encodeFunc closed over the codec's
// config. The encoder behavior is identical across iso2022 variants;
// only the designation table changes.
//
// CPython: _codecs_iso2022.c:182 ENCODER(iso2022)
//
//nolint:gocognit,gocyclo // mirrors CPython ENCODER(iso2022) branch layout 1:1.
func makeISO2022Encoder(cfg *iso2022Config) encodeFunc {
	encoder := func(state *codecState, input []rune, inpos int, out *encodeBuffer, flags int) int {
		// One-shot ENCODER_INIT on first call. Detect via the
		// G0 slot: codecState is zero-valued (G0=0) before the
		// first byte but charsetASCII (0x42) afterwards.
		if iso2022GetG(state, 0) == 0 && iso2022GetG(state, 1) == 0 {
			iso2022EncodeInit(state)
		}
		c := uint32(input[inpos])
		if c < 0x80 {
			if iso2022GetG(state, 0) != charsetASCII {
				out.writeBytes(iso2022ESC, '(', 'B')
				iso2022SetG(state, 0, charsetASCII)
			}
			if iso2022GetFlag(state, flagShifted) {
				out.writeByte(iso2022SI)
				iso2022ClrFlag(state, flagShifted)
			}
			out.writeByte(byte(c))
			return 1
		}
		insize := 1
		var encoded uint32 = mapUnmappable
		var chosen *iso2022Designation
		for i := range cfg.designations {
			dsg := &cfg.designations[i]
			if dsg.mark == 0 {
				break
			}
			buf := [2]uint32{c, 0}
			length := 1
			encoded = dsg.encoder(state, buf[:], &length)
			if encoded == mapMultipleAvail {
				if len(input)-inpos < 2 {
					if flags&MBENC_FLUSH == 0 {
						return MBERR_TOOFEW
					}
					length = -1
				} else {
					buf[1] = uint32(input[inpos+1])
					length = 2
				}
				encoded = dsg.encoder(state, buf[:], &length)
				if encoded != mapUnmappable {
					insize = length
					chosen = dsg
					break
				}
			} else if encoded != mapUnmappable {
				chosen = dsg
				break
			}
		}
		if chosen == nil {
			return 1
		}
		switch chosen.plane {
		case 0:
			if iso2022GetFlag(state, flagShifted) {
				out.writeByte(iso2022SI)
				iso2022ClrFlag(state, flagShifted)
			}
			if iso2022GetG(state, 0) != chosen.mark {
				switch {
				case chosen.width == 1:
					out.writeBytes(iso2022ESC, '(', escMark(chosen.mark))
				case chosen.mark == charsetJISX0208:
					out.writeBytes(iso2022ESC, '$', escMark(chosen.mark))
				default:
					out.writeBytes(iso2022ESC, '$', '(', escMark(chosen.mark))
				}
				iso2022SetG(state, 0, chosen.mark)
			}
		case 1:
			if iso2022GetG(state, 1) != chosen.mark {
				if chosen.width == 1 {
					out.writeBytes(iso2022ESC, ')', escMark(chosen.mark))
				} else {
					out.writeBytes(iso2022ESC, '$', ')', escMark(chosen.mark))
				}
				iso2022SetG(state, 1, chosen.mark)
			}
			if !iso2022GetFlag(state, flagShifted) {
				out.writeByte(iso2022SO)
				iso2022SetFlag(state, flagShifted)
			}
		default:
			return MBERR_INTERNAL
		}
		if chosen.width == 1 {
			out.writeByte(byte(encoded))
		} else {
			out.writeBytes(byte(encoded>>8), byte(encoded&0xff))
		}
		return insize
	}
	return encoder
}

// iso2022ProcessEsc walks the escape sequence starting at in[0]=ESC.
// On success the new G-state is recorded and the caller advances by
// the returned positive length; sentinel return values mirror CPython
// (1 = unterminated, esclen = unrecognized, MBERR_TOOFEW = needs more
// bytes, 0 = consumed, negative = handler error).
//
// CPython: _codecs_iso2022.c:319 iso2022processesc
//
//nolint:gocognit,gocyclo // mirrors CPython iso2022processesc branch layout 1:1.
func iso2022ProcessEsc(cfg *iso2022Config, state *codecState, in []byte) int {
	esclen := 0
	for i := 1; i < maxEscSeqLen; i++ {
		if i >= len(in) {
			return MBERR_TOOFEW
		}
		if isEscEnd(in[i]) {
			esclen = i + 1
			break
		}
		if iso2022CfgIsSet(cfg, configUseJISX0208E) && i+1 < len(in) &&
			in[i] == '&' && in[i+1] == '@' {
			i += 2
		}
	}
	var charset, designation uint8
	switch esclen {
	case 0:
		return 1
	case 3:
		if in[1] == '$' {
			charset = in[2] | charsetDBCSMark
			designation = 0
		} else {
			charset = in[2]
			switch in[1] {
			case '(':
				designation = 0
			case ')':
				designation = 1
			case '.':
				if !iso2022CfgIsSet(cfg, configUseG2) {
					return 3
				}
				designation = 2
			default:
				return 3
			}
		}
	case 4:
		if in[1] != '$' {
			return 4
		}
		charset = in[3] | charsetDBCSMark
		switch in[2] {
		case '(':
			designation = 0
		case ')':
			designation = 1
		default:
			return 4
		}
	case 6:
		if iso2022CfgIsSet(cfg, configUseJISX0208E) &&
			in[3] == iso2022ESC && in[4] == '$' && in[5] == 'B' {
			charset = 'B' | charsetDBCSMark
			designation = 0
		} else {
			return 6
		}
	default:
		return esclen
	}
	if charset != charsetASCII {
		found := false
		for i := range cfg.designations {
			if cfg.designations[i].mark == 0 {
				break
			}
			if cfg.designations[i].mark == charset {
				found = true
				break
			}
		}
		if !found {
			return esclen
		}
	}
	iso2022SetG(state, int(designation), charset)
	return -esclen
}

// iso8859_7 maps the 96-cell upper half of ISO-8859-7 to unicode, used
// by iso-2022-jp-2 when G2 selects ISO-8859-7. Returns (cp, ok).
//
// CPython: _codecs_iso2022.c:403 ISO8859_7_DECODE
func iso88597Decode(c byte) (uint32, bool) {
	switch {
	case c < 0xa0:
		return uint32(c), true
	case c < 0xc0 && (0x288f3bc9&(1<<(c-0xa0))) != 0:
		return uint32(c), true
	case c >= 0xb4 && c <= 0xfe && (c >= 0xd4 || (0xbffffd77&(1<<(c-0xb4))) != 0):
		return 0x02d0 + uint32(c), true
	case c == 0xa1:
		return 0x2018, true
	case c == 0xa2:
		return 0x2019, true
	case c == 0xaf:
		return 0x2015, true
	}
	return 0, false
}

// iso2022ProcessG2 decodes a single-shift-two (ESC N x) sequence using
// G2's currently-bound charset. Mirrors iso2022processg2 at
// _codecs_iso2022.c:419. Returns 0 on success or a positive consume
// count when the byte is unrecognized.
func iso2022ProcessG2(state *codecState, in []byte, w *unicodeWriter) int {
	if len(in) < 3 {
		return MBERR_TOOFEW
	}
	c := in[2]
	switch iso2022GetG(state, 2) {
	case charsetISO88591:
		if c >= 0x80 {
			return 3
		}
		w.writeRune(rune(c) + 0x80)
	case charsetISO88597:
		cp, ok := iso88597Decode(c ^ 0x80)
		if !ok {
			return 3
		}
		w.writeRune(rune(cp))
	case charsetASCII:
		if c&0x80 != 0 {
			return 3
		}
		w.writeRune(rune(c))
	default:
		return MBERR_INTERNAL
	}
	return -3
}

// makeISO2022Decoder returns a decodeFunc bound to the variant's
// config. The decoder is identical across variants; only the
// designation table and flags change.
//
// CPython: _codecs_iso2022.c:451 DECODER(iso2022)
//
//nolint:gocognit,gocyclo // mirrors CPython DECODER(iso2022) branch layout 1:1.
func makeISO2022Decoder(cfg *iso2022Config) decodeFunc {
	return func(state *codecState, in []byte, w *unicodeWriter) int {
		// First-call init: same trick as encoder. After the
		// first byte G0..G2 are charsetASCII.
		if iso2022GetG(state, 0) == 0 && iso2022GetG(state, 1) == 0 && iso2022GetG(state, 2) == 0 {
			iso2022DecodeInit(state)
		}
		c := in[0]
		if iso2022GetFlag(state, flagEscThroughout) {
			w.writeRune(rune(c))
			if isEscEnd(c) {
				iso2022ClrFlag(state, flagEscThroughout)
			}
			return 1
		}
		switch c {
		case iso2022ESC:
			if len(in) < 2 {
				return MBERR_TOOFEW
			}
			switch {
			case isISO2022Esc(in[1]):
				r := iso2022ProcessEsc(cfg, state, in)
				if r < 0 {
					return -r
				}
				return r
			case iso2022CfgIsSet(cfg, configUseG2) && in[1] == 'N':
				if len(in) < 3 {
					return MBERR_TOOFEW
				}
				r := iso2022ProcessG2(state, in, w)
				if r < 0 {
					return -r
				}
				return r
			default:
				w.writeRune(rune(iso2022ESC))
				iso2022SetFlag(state, flagEscThroughout)
				return 1
			}
		case iso2022SI:
			if iso2022CfgIsSet(cfg, configNoShift) {
				w.writeRune(rune(c))
				return 1
			}
			iso2022ClrFlag(state, flagShifted)
			return 1
		case iso2022SO:
			if iso2022CfgIsSet(cfg, configNoShift) {
				w.writeRune(rune(c))
				return 1
			}
			iso2022SetFlag(state, flagShifted)
			return 1
		case iso2022LF:
			iso2022ClrFlag(state, flagShifted)
			w.writeRune(rune(iso2022LF))
			return 1
		}
		if c < 0x20 {
			w.writeRune(rune(c))
			return 1
		}
		if c >= 0x80 {
			return 1
		}
		var charset uint8
		if iso2022GetFlag(state, flagShifted) {
			charset = iso2022GetG(state, 1)
		} else {
			charset = iso2022GetG(state, 0)
		}
		if charset == charsetASCII {
			w.writeRune(rune(c))
			return 1
		}
		var dsg *iso2022Designation
		for i := range cfg.designations {
			if cfg.designations[i].mark == charset {
				dsg = &cfg.designations[i]
				break
			}
		}
		if dsg == nil {
			return 1
		}
		width := int(dsg.width)
		if len(in) < width {
			return MBERR_TOOFEW
		}
		decoded := dsg.decoder(state, in)
		if decoded == mapUnmappable {
			return width
		}
		switch {
		case decoded < 0x30000:
			w.writeRune(rune(decoded))
		default:
			w.writeRune(rune(decoded >> 16))
			w.writeRune(rune(decoded & 0xffff))
		}
		return width
	}
}

// ----- per-charset encoder / decoder helpers -----

// CPython: _codecs_iso2022.c:584 ksx1001_decoder
func ksx1001Decoder(_ *codecState, data []byte) uint32 {
	row := ksx1001_decmap[data[0]]
	if u, ok := tryMapDec(&row, data[1]); ok {
		return uint32(u)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:594 ksx1001_encoder
func ksx1001Encoder(_ *codecState, data []uint32, _ *int) uint32 {
	d := data[0]
	if d < 0x10000 {
		if coded, ok := tryMapEnc(&cp949_encmap[d>>8], byte(d&0xff)); ok {
			if coded&0x8000 == 0 {
				return uint32(coded)
			}
		}
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:621 jisx0208_decoder
func jisx0208Decoder(_ *codecState, data []byte) uint32 {
	if data[0] == 0x21 && data[1] == 0x40 {
		return 0xff3c
	}
	row := jisx0208_decmap[data[0]]
	if u, ok := tryMapDec(&row, data[1]); ok {
		return uint32(u)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:633 jisx0208_encoder
func jisx0208Encoder(_ *codecState, data []uint32, _ *int) uint32 {
	d := data[0]
	if d < 0x10000 {
		if d == 0xff3c {
			return 0x2140
		}
		if coded, ok := tryMapEnc(&jisxcommon_encmap[d>>8], byte(d&0xff)); ok {
			if coded&0x8000 == 0 {
				return uint32(coded)
			}
		}
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:662 jisx0212_decoder
func jisx0212Decoder(_ *codecState, data []byte) uint32 {
	row := jisx0212_decmap[data[0]]
	if u, ok := tryMapDec(&row, data[1]); ok {
		return uint32(u)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:672 jisx0212_encoder
func jisx0212Encoder(_ *codecState, data []uint32, _ *int) uint32 {
	d := data[0]
	if d < 0x10000 {
		if coded, ok := tryMapEnc(&jisxcommon_encmap[d>>8], byte(d&0xff)); ok {
			if coded&0x8000 != 0 {
				return uint32(coded & 0x7fff)
			}
		}
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:929 jisx0201_r_decoder
func jisx0201RDecoder(_ *codecState, data []byte) uint32 {
	c := data[0]
	switch {
	case c < 0x80 && c != 0x5c && c != 0x7e:
		return uint32(c)
	case c == 0x5c:
		return 0x00a5
	case c == 0x7e:
		return 0x203e
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:939 jisx0201_r_encoder
func jisx0201REncoder(_ *codecState, data []uint32, _ *int) uint32 {
	if v, ok := jisx0201REncodeChar(data[0]); ok {
		return uint32(v)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:950 jisx0201_k_decoder
func jisx0201KDecoder(_ *codecState, data []byte) uint32 {
	c := data[0] ^ 0x80
	if c >= 0x21 && c <= 0x5f {
		return 0xff40 + uint32(c)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:960 jisx0201_k_encoder
func jisx0201KEncoder(_ *codecState, data []uint32, _ *int) uint32 {
	if v, ok := jisx0201KEncodeChar(data[0]); ok {
		return uint32(v - 0x80)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:983 gb2312_decoder
func gb2312Decoder(_ *codecState, data []byte) uint32 {
	row := gb2312_decmap[data[0]]
	if u, ok := tryMapDec(&row, data[1]); ok {
		return uint32(u)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:993 gb2312_encoder
func gb2312Encoder(_ *codecState, data []uint32, _ *int) uint32 {
	d := data[0]
	if d < 0x10000 {
		if coded, ok := tryMapEnc(&gbcommon_encmap[d>>8], byte(d&0xff)); ok {
			if coded&0x8000 == 0 {
				return uint32(coded)
			}
		}
	}
	return mapUnmappable
}

// ----- jisx0213 (2000 / 2004) decoders and encoders -----

// jisx0213_2000_1_decoder mirrors _codecs_iso2022.c:707.
//
// CPython: emu_jisx0213_2000.h EMULATE_JISX0213_2000_DECODE_PLANE1
func jisx02132000Plane1Decoder(_ *codecState, data []byte) uint32 {
	c1, c2 := data[0], data[1]
	if emulate2000DecodePlane1(2000, c1, c2) {
		return mapUnmappable
	}
	if c1 == 0x21 && c2 == 0x40 {
		return 0xff3c
	}
	rowJP := jisx0208_decmap[c1]
	if u, ok := tryMapDec(&rowJP, c2); ok {
		return uint32(u)
	}
	rowBMP := jisx0213_1_bmp_decmap[c1]
	if u, ok := tryMapDec(&rowBMP, c2); ok {
		return uint32(u)
	}
	rowEMP := jisx0213_1_emp_decmap[c1]
	if u, ok := tryMapDec(&rowEMP, c2); ok {
		return uint32(u) | 0x20000
	}
	rowPair := jisx0213_pair_decmap[c1]
	if u, ok := tryMapWideDec(&rowPair, c2); ok {
		return u
	}
	return mapUnmappable
}

// jisx0213_2000_2_decoder mirrors _codecs_iso2022.c:727.
func jisx02132000Plane2Decoder(_ *codecState, data []byte) uint32 {
	c1, c2 := data[0], data[1]
	var pre uint32
	if emulate2000DecodePlane2(2000, c1, c2, nil) {
		pre = 0x9B1D
	}
	if pre != 0 {
		return pre
	}
	rowBMP := jisx0213_2_bmp_decmap[c1]
	if u, ok := tryMapDec(&rowBMP, c2); ok {
		return uint32(u)
	}
	rowEMP := jisx0213_2_emp_decmap[c1]
	if u, ok := tryMapDec(&rowEMP, c2); ok {
		return uint32(u) | 0x20000
	}
	return mapUnmappable
}

// jisx0213_2004_1_decoder mirrors _codecs_iso2022.c:742.
func jisx02132004Plane1Decoder(_ *codecState, data []byte) uint32 {
	c1, c2 := data[0], data[1]
	if c1 == 0x21 && c2 == 0x40 {
		return 0xff3c
	}
	rowJP := jisx0208_decmap[c1]
	if u, ok := tryMapDec(&rowJP, c2); ok {
		return uint32(u)
	}
	rowBMP := jisx0213_1_bmp_decmap[c1]
	if u, ok := tryMapDec(&rowBMP, c2); ok {
		return uint32(u)
	}
	rowEMP := jisx0213_1_emp_decmap[c1]
	if u, ok := tryMapDec(&rowEMP, c2); ok {
		return uint32(u) | 0x20000
	}
	rowPair := jisx0213_pair_decmap[c1]
	if u, ok := tryMapWideDec(&rowPair, c2); ok {
		return u
	}
	return mapUnmappable
}

// jisx0213_2004_2_decoder mirrors _codecs_iso2022.c:761.
func jisx02132004Plane2Decoder(_ *codecState, data []byte) uint32 {
	c1, c2 := data[0], data[1]
	rowBMP := jisx0213_2_bmp_decmap[c1]
	if u, ok := tryMapDec(&rowBMP, c2); ok {
		return uint32(u)
	}
	rowEMP := jisx0213_2_emp_decmap[c1]
	if u, ok := tryMapDec(&rowEMP, c2); ok {
		return uint32(u) | 0x20000
	}
	return mapUnmappable
}

// jisx0213_encoder mirrors _codecs_iso2022.c:774 jisx0213_encoder.
// config is 2000 or 0 (== 2004).
//
//nolint:gocognit // mirrors CPython jisx0213_encoder branch layout 1:1.
func jisx0213Encoder(data []uint32, length *int, cfg int) uint32 {
	switch *length {
	case 1:
		d := data[0]
		if d >= 0x10000 {
			if d>>16 == 0x20000>>16 {
				if emulate2000EncodeEMP(cfg, d) {
					return mapUnmappable
				}
				if coded, ok := tryMapEnc(&jisx0213_emp_encmap[(d&0xffff)>>8], byte(d&0xff)); ok {
					return uint32(coded)
				}
			}
			return mapUnmappable
		}
		if code, _, abandon := emulate2000EncodeBMP(cfg, d); abandon {
			return mapUnmappable
		} else if _, abandon2, _ := emulate2000EncodeBMP(cfg, d); abandon2 {
			return uint32(code)
		}
		if coded, ok := tryMapEnc(&jisx0213_bmp_encmap[d>>8], byte(d&0xff)); ok {
			if coded == MULTIC {
				return mapMultipleAvail
			}
			return uint32(coded)
		}
		if coded, ok := tryMapEnc(&jisxcommon_encmap[d>>8], byte(d&0xff)); ok {
			if coded&0x8000 != 0 {
				return mapUnmappable
			}
			return uint32(coded)
		}
		return mapUnmappable
	case 2:
		if data[1] != 0 {
			coded := findPairEnc(uint16(data[0]), uint16(data[1]), jisx0213_pair_encmap[:])
			if coded != DBCINV {
				return uint32(coded)
			}
		}
		fallthrough
	case -1:
		*length = 1
		coded := findPairEnc(uint16(data[0]), 0, jisx0213_pair_encmap[:])
		if coded == DBCINV {
			return mapUnmappable
		}
		return uint32(coded)
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:829 jisx0213_2000_1_encoder
func jisx02132000Plane1Encoder(_ *codecState, data []uint32, length *int) uint32 {
	coded := jisx0213Encoder(data, length, 2000)
	if coded == mapUnmappable || coded == mapMultipleAvail {
		return coded
	}
	if coded&0x8000 != 0 {
		return mapUnmappable
	}
	return coded
}

// CPython: _codecs_iso2022.c:842 jisx0213_2000_1_encoder_paironly
func jisx02132000Plane1EncoderPair(_ *codecState, data []uint32, length *int) uint32 {
	ilength := *length
	coded := jisx0213Encoder(data, length, 2000)
	switch ilength {
	case 1:
		if coded == mapMultipleAvail {
			return mapMultipleAvail
		}
		return mapUnmappable
	case 2:
		if *length != 2 {
			return mapUnmappable
		}
		return coded
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:866 jisx0213_2000_2_encoder
func jisx02132000Plane2Encoder(_ *codecState, data []uint32, length *int) uint32 {
	coded := jisx0213Encoder(data, length, 2000)
	if coded == mapUnmappable || coded == mapMultipleAvail {
		return coded
	}
	if coded&0x8000 != 0 {
		return coded & 0x7fff
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:879 jisx0213_2004_1_encoder
func jisx02132004Plane1Encoder(_ *codecState, data []uint32, length *int) uint32 {
	coded := jisx0213Encoder(data, length, 0)
	if coded == mapUnmappable || coded == mapMultipleAvail {
		return coded
	}
	if coded&0x8000 != 0 {
		return mapUnmappable
	}
	return coded
}

// CPython: _codecs_iso2022.c:892 jisx0213_2004_1_encoder_paironly
func jisx02132004Plane1EncoderPair(_ *codecState, data []uint32, length *int) uint32 {
	ilength := *length
	coded := jisx0213Encoder(data, length, 0)
	switch ilength {
	case 1:
		if coded == mapMultipleAvail {
			return mapMultipleAvail
		}
		return mapUnmappable
	case 2:
		if *length != 2 {
			return mapUnmappable
		}
		return coded
	}
	return mapUnmappable
}

// CPython: _codecs_iso2022.c:916 jisx0213_2004_2_encoder
func jisx02132004Plane2Encoder(_ *codecState, data []uint32, length *int) uint32 {
	coded := jisx0213Encoder(data, length, 0)
	if coded == mapUnmappable || coded == mapMultipleAvail {
		return coded
	}
	if coded&0x8000 != 0 {
		return coded & 0x7fff
	}
	return mapUnmappable
}

// dummyDecoder / dummyEncoder match the no-op slots used by
// ISO-8859-1 / ISO-8859-7 registrations: those charsets only matter
// for G2 (decoded by iso2022ProcessG2) and never appear in G0/G1.
//
// CPython: _codecs_iso2022.c:1009 dummy_decoder / dummy_encoder
func dummyDecoder(_ *codecState, _ []byte) uint32           { return mapUnmappable }
func dummyEncoder(_ *codecState, _ []uint32, _ *int) uint32 { return mapUnmappable }

// ----- per-variant designation tables -----
//
// CPython: _codecs_iso2022.c:1024 REGISTRY_* macros

var (
	regKSX1001G1 = iso2022Designation{
		mark: charsetKSX1001, plane: 1, width: 2,
		decoder: ksx1001Decoder, encoder: ksx1001Encoder,
	}
	regKSX1001G0 = iso2022Designation{
		mark: charsetKSX1001, plane: 0, width: 2,
		decoder: ksx1001Decoder, encoder: ksx1001Encoder,
	}
	regJISX0201R = iso2022Designation{
		mark: charsetJISX0201R, plane: 0, width: 1,
		decoder: jisx0201RDecoder, encoder: jisx0201REncoder,
	}
	regJISX0201K = iso2022Designation{
		mark: charsetJISX0201K, plane: 0, width: 1,
		decoder: jisx0201KDecoder, encoder: jisx0201KEncoder,
	}
	regJISX0208 = iso2022Designation{
		mark: charsetJISX0208, plane: 0, width: 2,
		decoder: jisx0208Decoder, encoder: jisx0208Encoder,
	}
	regJISX0208O = iso2022Designation{
		mark: charsetJISX0208O, plane: 0, width: 2,
		decoder: jisx0208Decoder, encoder: jisx0208Encoder,
	}
	regJISX0212 = iso2022Designation{
		mark: charsetJISX0212, plane: 0, width: 2,
		decoder: jisx0212Decoder, encoder: jisx0212Encoder,
	}
	regJISX02132000Plane1 = iso2022Designation{
		mark: charsetJISX02131, plane: 0, width: 2,
		decoder: jisx02132000Plane1Decoder, encoder: jisx02132000Plane1Encoder,
	}
	regJISX02132000Plane1Pair = iso2022Designation{
		mark: charsetJISX02131, plane: 0, width: 2,
		decoder: jisx02132000Plane1Decoder, encoder: jisx02132000Plane1EncoderPair,
	}
	regJISX02132000Plane2 = iso2022Designation{
		mark: charsetJISX02132, plane: 0, width: 2,
		decoder: jisx02132000Plane2Decoder, encoder: jisx02132000Plane2Encoder,
	}
	regJISX02132004Plane1 = iso2022Designation{
		mark: charsetJISX020041, plane: 0, width: 2,
		decoder: jisx02132004Plane1Decoder, encoder: jisx02132004Plane1Encoder,
	}
	regJISX02132004Plane1Pair = iso2022Designation{
		mark: charsetJISX020041, plane: 0, width: 2,
		decoder: jisx02132004Plane1Decoder, encoder: jisx02132004Plane1EncoderPair,
	}
	regJISX02132004Plane2 = iso2022Designation{
		mark: charsetJISX02132, plane: 0, width: 2,
		decoder: jisx02132004Plane2Decoder, encoder: jisx02132004Plane2Encoder,
	}
	regGB2312 = iso2022Designation{
		mark: charsetGB2312, plane: 0, width: 2,
		decoder: gb2312Decoder, encoder: gb2312Encoder,
	}
	regISO88591 = iso2022Designation{
		mark: charsetISO88591, plane: 2, width: 1,
		decoder: dummyDecoder, encoder: dummyEncoder,
	}
	regISO88597 = iso2022Designation{
		mark: charsetISO88597, plane: 2, width: 1,
		decoder: dummyDecoder, encoder: dummyEncoder,
	}
	regSentinel = iso2022Designation{}
)

// CPython: _codecs_iso2022.c:1088 iso2022_kr_designations
var iso2022KRConfig = iso2022Config{
	flags: 0,
	designations: []iso2022Designation{
		regKSX1001G1, regSentinel,
	},
}

// CPython: _codecs_iso2022.c:1093 iso2022_jp_designations
var iso2022JPConfig = iso2022Config{
	flags: configNoShift | configUseJISX0208E,
	designations: []iso2022Designation{
		regJISX0208, regJISX0201R, regJISX0208O, regSentinel,
	},
}

// CPython: _codecs_iso2022.c:1099 iso2022_jp_1_designations
var iso2022JP1Config = iso2022Config{
	flags: configNoShift | configUseJISX0208E,
	designations: []iso2022Designation{
		regJISX0208, regJISX0212, regJISX0201R, regJISX0208O, regSentinel,
	},
}

// CPython: _codecs_iso2022.c:1105 iso2022_jp_2_designations
var iso2022JP2Config = iso2022Config{
	flags: configNoShift | configUseG2 | configUseJISX0208E,
	designations: []iso2022Designation{
		regJISX0208, regJISX0212, regKSX1001G0, regGB2312,
		regJISX0201R, regJISX0208O, regISO88591, regISO88597, regSentinel,
	},
}

// CPython: _codecs_iso2022.c:1112 iso2022_jp_2004_designations
var iso2022JP2004Config = iso2022Config{
	flags: configNoShift | configUseJISX0208E,
	designations: []iso2022Designation{
		regJISX02132004Plane1Pair, regJISX0208,
		regJISX02132004Plane1, regJISX02132004Plane2, regSentinel,
	},
}

// CPython: _codecs_iso2022.c:1118 iso2022_jp_3_designations
var iso2022JP3Config = iso2022Config{
	flags: configNoShift | configUseJISX0208E,
	designations: []iso2022Designation{
		regJISX02132000Plane1Pair, regJISX0208,
		regJISX02132000Plane1, regJISX02132000Plane2, regSentinel,
	},
}

// CPython: _codecs_iso2022.c:1124 iso2022_jp_ext_designations
var iso2022JPExtConfig = iso2022Config{
	flags: configNoShift | configUseJISX0208E,
	designations: []iso2022Designation{
		regJISX0208, regJISX0212, regJISX0201R, regJISX0201K, regJISX0208O, regSentinel,
	},
}
