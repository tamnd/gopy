package cjkcodecs

import "testing"

// These spot checks verify that the generator preserves the same
// codepoint mappings CPython publishes. They cover the two byte
// sequences exercised by test_source_encoding.test_issue2301 and
// test_exec_valid_coding.
//
// CPython: Modules/cjkcodecs/_codecs_jp.c cp932 DECODER (line 84)
// CPython: Modules/cjkcodecs/_codecs_kr.c cp949 DECODER (line 116)

func TestKSX1001DecmapSpotCheck(t *testing.T) {
	// cp949 (\xaa\xa7) is decoded by the KSX1001 path with byte 1 mapped
	// to row (c1 & 0x7f) = 0x2a, byte 2 mapped to col (c2 & 0x7f) = 0x27.
	// CPython: _codecs_kr.c:121-126.
	row := ksx1001_decmap[0x2a]
	if row.Map == nil {
		t.Fatal("ksx1001 row 0x2a empty")
	}
	got, ok := tryMapDec(&row, 0x27)
	if !ok {
		t.Fatal("ksx1001 (0x2a, 0x27) miss")
	}
	if got != 0x3047 {
		t.Fatalf("ksx1001 (0x2a, 0x27) = U+%04x, want U+3047", got)
	}
}

func TestJISX0208DecmapSpotCheck(t *testing.T) {
	// cp932 (\x94\x4e): the cp932 algorithm normalizes (0x94, 0x4e)
	// into a JIS X 0208 (row=0x47, col=0x2f) lookup. CPython: _codecs_jp.c:119-124.
	row := jisx0208_decmap[0x47]
	if row.Map == nil {
		t.Fatal("jisx0208 row 0x47 empty")
	}
	got, ok := tryMapDec(&row, 0x2f)
	if !ok {
		t.Fatal("jisx0208 (0x47, 0x2f) miss")
	}
	if got != 0x5e74 {
		t.Fatalf("jisx0208 (0x47, 0x2f) = U+%04x, want U+5e74", got)
	}
}
