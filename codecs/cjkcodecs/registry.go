// Public CodecInfo registry for the cjkcodecs package. Each entry
// wraps a runDecode / runEncode closure pinned to a specific codec
// implementation so callers in codecs/builtin.go can dispatch by
// canonical name.
//
// CPython: Modules/cjkcodecs/_codecs_kr.c BEGIN_CODECS_LIST(3)
// CPython: Modules/cjkcodecs/_codecs_jp.c BEGIN_CODECS_LIST(7)

package cjkcodecs

import "github.com/tamnd/gopy/codecs"

func newInfo(name string, enc encodeFunc, dec decodeFunc, config int) *codecs.CodecInfo {
	makeEncoderFull := func() (
		func(string, string, bool) ([]byte, int, error),
		func() ([]rune, [8]byte),
		func([]rune, [8]byte) error,
	) {
		st := &codecState{config: config}
		var pending []rune
		encodeFn := func(input string, errors string, final bool) ([]byte, int, error) {
			savedPending := pending
			inputRunes := wtf8ToRunes(input)
			runes := make([]rune, len(pending)+len(inputRunes))
			copy(runes, pending)
			copy(runes[len(pending):], inputRunes)
			pending = nil
			out, remaining, n, err := runEncodeStatefulWithState(name, enc, nil, st, runes, errors, final)
			if err != nil {
				pending = savedPending
				return nil, n, err
			}
			if len(remaining) > 0 && !final {
				pending = append(pending[:0], remaining...)
			}
			return out, n, err
		}
		getStateFn := func() ([]rune, [8]byte) {
			return pending, st.cBytes
		}
		setStateFn := func(newPending []rune, newCBytes [8]byte) error {
			pending = newPending
			st.cBytes = newCBytes
			return nil
		}
		return encodeFn, getStateFn, setStateFn
	}
	return &codecs.CodecInfo{
		Name: name,
		Encode: func(input, errors string) ([]byte, int, error) {
			return runEncode(name, enc, config, input, errors)
		},
		Decode: func(input []byte, errors string) (string, int, error) {
			return runDecode(name, dec, config, input, errors)
		},
		IncrementalDecode: func(input []byte, errors string, final bool) (string, []byte, error) {
			return runDecodeIncremental(name, dec, config, input, errors, final)
		},
		NewIncrementalDecoder: func() func([]byte, string, bool) (string, []byte, error) {
			st := &codecState{config: config}
			return func(input []byte, errors string, final bool) (string, []byte, error) {
				return runDecodeIncrementalWithState(name, dec, st, input, errors, final)
			}
		},
		NewIncrementalEncoder: func() func(string, string, bool) ([]byte, int, error) {
			encodeFn, _, _ := makeEncoderFull()
			return encodeFn
		},
		NewIncrementalEncoderFull: makeEncoderFull,
	}
}

// newStatefulInfo wraps a codec whose encoder needs an ENCODER_RESET
// step after the input is fully consumed. CPython runs the reset
// callback as the last action of multibytecodec_encode (line 595);
// gopy threads the callback through runEncodeStateful.
//
// encoderInitFn, when non-nil, is called on a fresh codecState immediately
// after construction, mirroring CPython's mbincrencoder_init which calls
// encinit before returning the new encoder object.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:595
// CPython: Modules/cjkcodecs/multibytecodec.c:749 mbincrencoder_new
func newStatefulInfo(name string, enc encodeFunc, dec decodeFunc, reset encodeResetFunc, encoderInitFn func(*codecState)) *codecs.CodecInfo {
	makeEncoderFull := func() (
		func(string, string, bool) ([]byte, int, error),
		func() ([]rune, [8]byte),
		func([]rune, [8]byte) error,
	) {
		st := &codecState{}
		if encoderInitFn != nil {
			encoderInitFn(st)
		}
		var pending []rune
		encodeFn := func(input string, errors string, final bool) ([]byte, int, error) {
			savedPending := pending
			inputRunes := wtf8ToRunes(input)
			runes := make([]rune, len(pending)+len(inputRunes))
			copy(runes, pending)
			copy(runes[len(pending):], inputRunes)
			pending = nil
			out, remaining, n, err := runEncodeStatefulWithState(name, enc, reset, st, runes, errors, final)
			if err != nil {
				pending = savedPending
				return nil, n, err
			}
			if len(remaining) > 0 && !final {
				pending = append(pending[:0], remaining...)
			}
			return out, n, err
		}
		getStateFn := func() ([]rune, [8]byte) {
			return pending, st.cBytes
		}
		setStateFn := func(newPending []rune, newCBytes [8]byte) error {
			pending = newPending
			st.cBytes = newCBytes
			return nil
		}
		return encodeFn, getStateFn, setStateFn
	}
	return &codecs.CodecInfo{
		Name: name,
		Encode: func(input, errors string) ([]byte, int, error) {
			return runEncodeStateful(name, enc, reset, 0, input, errors)
		},
		Decode: func(input []byte, errors string) (string, int, error) {
			return runDecode(name, dec, 0, input, errors)
		},
		IncrementalDecode: func(input []byte, errors string, final bool) (string, []byte, error) {
			return runDecodeIncremental(name, dec, 0, input, errors, final)
		},
		NewIncrementalDecoder: func() func([]byte, string, bool) (string, []byte, error) {
			st := &codecState{}
			return func(input []byte, errors string, final bool) (string, []byte, error) {
				return runDecodeIncrementalWithState(name, dec, st, input, errors, final)
			}
		},
		NewIncrementalEncoder: func() func(string, string, bool) ([]byte, int, error) {
			encodeFn, _, _ := makeEncoderFull()
			return encodeFn
		},
		NewIncrementalEncoderFull: makeEncoderFull,
	}
}

// Korean codecs.
//
// CPython: Modules/cjkcodecs/_codecs_kr.c BEGIN_CODECS_LIST
var (
	eucKR = newInfo("euc_kr", encodeEUCKR, decodeEUCKR, 0)
	cp949 = newInfo("cp949", encodeCP949, decodeCP949, 0)
	johab = newInfo("johab", encodeJohab, decodeJohab, 0)
)

// Japanese codecs.
//
// CPython: Modules/cjkcodecs/_codecs_jp.c BEGIN_CODECS_LIST
var (
	shiftJIS      = newInfo("shift_jis", encodeShiftJIS, decodeShiftJIS, 0)
	cp932         = newInfo("cp932", encodeCP932, decodeCP932, 0)
	eucJP         = newInfo("euc_jp", encodeEUCJP, decodeEUCJP, 0)
	shiftJIS2004  = newInfo("shift_jis_2004", encodeShiftJIS2004, decodeShiftJIS2004, 0)
	eucJIS2004    = newInfo("euc_jis_2004", encodeEUCJIS2004, decodeEUCJIS2004, 0)
	eucJISX0213   = newInfo("euc_jisx0213", encodeEUCJIS2004, decodeEUCJIS2004, 2000)
	shiftJISX0213 = newInfo("shift_jisx0213", encodeShiftJIS2004, decodeShiftJIS2004, 2000)
)

// Taiwanese codecs.
//
// CPython: Modules/cjkcodecs/_codecs_tw.c BEGIN_CODECS_LIST
var (
	big5  = newInfo("big5", encodeBig5, decodeBig5, 0)
	cp950 = newInfo("cp950", encodeCP950, decodeCP950, 0)
)

// Hong Kong codecs.
//
// CPython: Modules/cjkcodecs/_codecs_hk.c BEGIN_CODECS_LIST
var big5hkscs = newInfo("big5hkscs", encodeBig5HKSCS, decodeBig5HKSCS, 0)

// Mainland Chinese codecs.
//
// CPython: Modules/cjkcodecs/_codecs_cn.c BEGIN_CODECS_LIST
var (
	gb2312Codec  = newInfo("gb2312", encodeGB2312, decodeGB2312, 0)
	gbkCodec     = newInfo("gbk", encodeGBK, decodeGBK, 0)
	gb18030Codec = newInfo("gb18030", encodeGB18030, decodeGB18030, 0)
	hzCodec      = newStatefulInfo("hz", encodeHZ, decodeHZ, hzEncodeReset, nil)
)

// ISO-2022 codecs.
//
// CPython: Modules/cjkcodecs/_codecs_iso2022.c BEGIN_CODECS_LIST
var (
	iso2022KR     = newStatefulInfo("iso2022_kr", makeISO2022Encoder(&iso2022KRConfig), makeISO2022Decoder(&iso2022KRConfig), iso2022EncodeReset, iso2022EncodeInit)
	iso2022JP     = newStatefulInfo("iso2022_jp", makeISO2022Encoder(&iso2022JPConfig), makeISO2022Decoder(&iso2022JPConfig), iso2022EncodeReset, iso2022EncodeInit)
	iso2022JP1    = newStatefulInfo("iso2022_jp_1", makeISO2022Encoder(&iso2022JP1Config), makeISO2022Decoder(&iso2022JP1Config), iso2022EncodeReset, iso2022EncodeInit)
	iso2022JP2    = newStatefulInfo("iso2022_jp_2", makeISO2022Encoder(&iso2022JP2Config), makeISO2022Decoder(&iso2022JP2Config), iso2022EncodeReset, iso2022EncodeInit)
	iso2022JP2004 = newStatefulInfo("iso2022_jp_2004", makeISO2022Encoder(&iso2022JP2004Config), makeISO2022Decoder(&iso2022JP2004Config), iso2022EncodeReset, iso2022EncodeInit)
	iso2022JP3    = newStatefulInfo("iso2022_jp_3", makeISO2022Encoder(&iso2022JP3Config), makeISO2022Decoder(&iso2022JP3Config), iso2022EncodeReset, iso2022EncodeInit)
	iso2022JPExt  = newStatefulInfo("iso2022_jp_ext", makeISO2022Encoder(&iso2022JPExtConfig), makeISO2022Decoder(&iso2022JPExtConfig), iso2022EncodeReset, iso2022EncodeInit)
)

// Search is the SearchFunc that codecs.Register dispatches into for
// every CJK alias the cjkcodecs subsystem knows about. Wired via
// init() below so importing the package side-loads the codecs.
//
// CPython: Lib/encodings/aliases.py + Lib/encodings/<codec>.py
//
//nolint:gocyclo // dispatches every CJK codec alias; mirrors Lib/encodings/aliases.py.
func Search(name string) (*codecs.CodecInfo, error) {
	switch name {
	case "cp949", "949", "ms949", "uhc":
		return cp949, nil
	case "euc_kr", "euckr", "korean", "ksc5601", "ks_c_5601", "ks_c_5601_1987",
		"ksx1001", "ks_x_1001":
		return eucKR, nil
	case "johab", "cp1361", "ms1361":
		return johab, nil
	case "cp932", "932", "ms932", "mskanji", "ms_kanji":
		return cp932, nil
	case "shift_jis", "csshiftjis", "shiftjis", "sjis", "s_jis":
		return shiftJIS, nil
	case "euc_jp", "eucjp", "ujis", "u_jis":
		return eucJP, nil
	case "shift_jis_2004", "shiftjis2004", "sjis_2004", "sjis2004":
		return shiftJIS2004, nil
	case "euc_jis_2004", "jisx0213", "eucjis2004":
		return eucJIS2004, nil
	case "euc_jisx0213":
		return eucJISX0213, nil
	case "shift_jisx0213":
		return shiftJISX0213, nil
	case "big5", "big5_tw", "csbig5":
		return big5, nil
	case "cp950", "950", "ms950":
		return cp950, nil
	case "big5hkscs", "big5_hkscs", "hkscs":
		return big5hkscs, nil
	case "gb2312", "chinese", "csiso58gb231280", "euc_cn", "euccn", "eucgb2312_cn",
		"gb2312_1980", "gb2312_80", "iso_ir_58":
		return gb2312Codec, nil
	case "gbk", "936", "cp936", "ms936":
		return gbkCodec, nil
	case "gb18030", "gb18030_2000":
		return gb18030Codec, nil
	case "hz", "hzgb", "hz_gb", "hz_gb_2312":
		return hzCodec, nil
	case "iso2022_kr", "iso_2022_kr", "csiso2022kr":
		return iso2022KR, nil
	case "iso2022_jp", "iso_2022_jp", "csiso2022jp":
		return iso2022JP, nil
	case "iso2022_jp_1", "iso_2022_jp_1":
		return iso2022JP1, nil
	case "iso2022_jp_2", "iso_2022_jp_2":
		return iso2022JP2, nil
	case "iso2022_jp_2004", "iso_2022_jp_2004":
		return iso2022JP2004, nil
	case "iso2022_jp_3", "iso_2022_jp_3":
		return iso2022JP3, nil
	case "iso2022_jp_ext", "iso_2022_jp_ext":
		return iso2022JPExt, nil
	}
	return nil, nil
}

func init() {
	codecs.Register(Search)
}
