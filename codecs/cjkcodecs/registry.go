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
	return &codecs.CodecInfo{
		Name: name,
		Encode: func(input, errors string) ([]byte, int, error) {
			return runEncode(name, enc, config, input, errors)
		},
		Decode: func(input []byte, errors string) (string, int, error) {
			return runDecode(name, dec, config, input, errors)
		},
	}
}

// Korean codecs.
//
// CPython: Modules/cjkcodecs/_codecs_kr.c BEGIN_CODECS_LIST
var (
	EUCKR = newInfo("euc_kr", encodeEUCKR, decodeEUCKR, 0)
	CP949 = newInfo("cp949", encodeCP949, decodeCP949, 0)
	JOHAB = newInfo("johab", encodeJohab, decodeJohab, 0)
)

// Japanese codecs.
//
// CPython: Modules/cjkcodecs/_codecs_jp.c BEGIN_CODECS_LIST
var (
	SHIFT_JIS      = newInfo("shift_jis", encodeShiftJIS, decodeShiftJIS, 0)
	CP932          = newInfo("cp932", encodeCP932, decodeCP932, 0)
	EUC_JP         = newInfo("euc_jp", encodeEUCJP, decodeEUCJP, 0)
	SHIFT_JIS_2004 = newInfo("shift_jis_2004", encodeShiftJIS2004, decodeShiftJIS2004, 0)
	EUC_JIS_2004   = newInfo("euc_jis_2004", encodeEUCJIS2004, decodeEUCJIS2004, 0)
	EUC_JISX0213   = newInfo("euc_jisx0213", encodeEUCJIS2004, decodeEUCJIS2004, 2000)
	SHIFT_JISX0213 = newInfo("shift_jisx0213", encodeShiftJIS2004, decodeShiftJIS2004, 2000)
)

// Search is the SearchFunc that codecs.Register dispatches into for
// every CJK alias the cjkcodecs subsystem knows about. Wired via
// init() below so importing the package side-loads the codecs.
//
// CPython: Lib/encodings/aliases.py + Lib/encodings/<codec>.py
func Search(name string) (*codecs.CodecInfo, error) {
	switch name {
	case "cp949", "949", "ms949", "uhc":
		return CP949, nil
	case "euc_kr", "euckr", "korean", "ksc5601", "ks_c_5601", "ks_c_5601_1987",
		"ksx1001", "ks_x_1001":
		return EUCKR, nil
	case "johab", "cp1361", "ms1361":
		return JOHAB, nil
	case "cp932", "932", "ms932", "mskanji", "ms_kanji":
		return CP932, nil
	case "shift_jis", "csshiftjis", "shiftjis", "sjis", "s_jis":
		return SHIFT_JIS, nil
	case "euc_jp", "eucjp", "ujis", "u_jis":
		return EUC_JP, nil
	case "shift_jis_2004", "shiftjis2004", "sjis_2004", "sjis2004":
		return SHIFT_JIS_2004, nil
	case "euc_jis_2004", "jisx0213", "eucjis2004":
		return EUC_JIS_2004, nil
	case "euc_jisx0213":
		return EUC_JISX0213, nil
	case "shift_jisx0213":
		return SHIFT_JISX0213, nil
	}
	return nil, nil
}

func init() {
	codecs.Register(Search)
}
