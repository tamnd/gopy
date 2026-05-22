package cjkcodecs

import (
	"bytes"
	"testing"
)

// TestCP932DecodeNen mirrors test_source_encoding.test_issue2301:
// cp932 decode of b"\x94\x4e" yields the single codepoint U+5E74 (年).
func TestCP932DecodeNen(t *testing.T) {
	got, _, err := cp932.Decode([]byte{0x94, 0x4e}, "strict")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got != "年" {
		t.Fatalf("got %q (% x), want U+5E74", got, []byte(got))
	}
}

// TestCP949DecodeHiraganaE mirrors
// test_source_encoding.test_exec_valid_coding: cp949 decode of
// b"\xaa\xa7" yields the single codepoint U+3047 (ぇ).
func TestCP949DecodeHiraganaE(t *testing.T) {
	got, _, err := cp949.Decode([]byte{0xaa, 0xa7}, "strict")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got != "ぇ" {
		t.Fatalf("got %q (% x), want U+3047", got, []byte(got))
	}
}

// TestEUCKRRoundTrip covers an ASCII-only string and a hangul
// syllable (U+AC00) to exercise the make-up sequence in the encoder.
func TestEUCKRRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		s    string
	}{
		{"ascii", "hello"},
		{"hangul", "가"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, _, err := eucKR.Encode(tc.s, "strict")
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			dec, _, err := eucKR.Decode(enc, "strict")
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if dec != tc.s {
				t.Fatalf("round-trip mismatch: %q -> % x -> %q", tc.s, enc, dec)
			}
		})
	}
}

// TestBig5Encode pins the big5 / cp950 encoding of U+4E2D (中).
// CPython: '中'.encode('big5') == b'\\xa4\\xa4'.
func TestBig5Encode(t *testing.T) {
	want := []byte{0xa4, 0xa4}
	cases := []struct {
		name   string
		encode func(string, string) ([]byte, int, error)
		decode func([]byte, string) (string, int, error)
	}{
		{"big5", big5.Encode, big5.Decode},
		{"cp950", cp950.Encode, cp950.Decode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := c.encode("中", "strict")
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("encode 中 = % x, want % x", got, want)
			}
			dec, _, err := c.decode(got, "strict")
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if dec != "中" {
				t.Fatalf("decode round-trip = %q, want %q", dec, "中")
			}
		})
	}
}

// TestBig5HKSCSPairDecode pins the four HKSCS pair-decoded sequences
// from _codecs_hk.c:166.
func TestBig5HKSCSPairDecode(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{0x88, 0x62}, "Ê̄"},
		{[]byte{0x88, 0x64}, "Ê̌"},
		{[]byte{0x88, 0xa3}, "ê̄"},
		{[]byte{0x88, 0xa5}, "ê̌"},
	}
	for _, c := range cases {
		got, _, err := big5hkscs.Decode(c.in, "strict")
		if err != nil {
			t.Fatalf("decode % x: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("decode % x = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBig5HKSCSPairEncode pins the four HKSCS pair-combining encode
// outputs and the bare U+00CA / U+00EA fall-backs.
func TestBig5HKSCSPairEncode(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"Ê̄", []byte{0x88, 0x62}},
		{"Ê̌", []byte{0x88, 0x64}},
		{"ê̄", []byte{0x88, 0xa3}},
		{"ê̌", []byte{0x88, 0xa5}},
		{"Ê", []byte{0x88, 0x66}},
		{"ê", []byte{0x88, 0xa7}},
	}
	for _, c := range cases {
		got, _, err := big5hkscs.Encode(c.in, "strict")
		if err != nil {
			t.Fatalf("encode %q: %v", c.in, err)
		}
		if !bytes.Equal(got, c.want) {
			t.Fatalf("encode %q = % x, want % x", c.in, got, c.want)
		}
	}
}

// TestGBKDivergence pins the three byte sequences where GBK and
// GB2312 disagree (em-dash, horizontal bar, middle dot).
// CPython: '—'.encode('gbk') == b'\\xa1\\xaa'.
func TestGBKDivergence(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"—", []byte{0xa1, 0xaa}},
		{"―", []byte{0xa8, 0x44}},
		{"·", []byte{0xa1, 0xa4}},
	}
	for _, c := range cases {
		got, _, err := gbkCodec.Encode(c.in, "strict")
		if err != nil {
			t.Fatalf("encode %q: %v", c.in, err)
		}
		if !bytes.Equal(got, c.want) {
			t.Fatalf("encode %q = % x, want % x", c.in, got, c.want)
		}
		dec, _, err := gbkCodec.Decode(got, "strict")
		if err != nil {
			t.Fatalf("decode % x: %v", got, err)
		}
		if dec != c.in {
			t.Fatalf("round-trip %q = %q", c.in, dec)
		}
	}
}

// TestGB18030FourByte pins the four-byte sequence for U+1F600 (grinning
// face) that exercises the c >= 0x10000 path.
// CPython: '\U0001F600'.encode('gb18030') == b'\\x94\\x39\\xfc\\x36'.
func TestGB18030FourByte(t *testing.T) {
	got, _, err := gb18030Codec.Encode("\U0001F600", "strict")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{0x94, 0x39, 0xfc, 0x36}
	if !bytes.Equal(got, want) {
		t.Fatalf("encode = % x, want % x", got, want)
	}
	dec, _, err := gb18030Codec.Decode(got, "strict")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != "\U0001F600" {
		t.Fatalf("decode = %q, want %q", dec, "\U0001F600")
	}
}

// TestHZRoundTrip pins the encoded form of "一" in hz:
//
//	7e7b 523b 7e7d == ~{R; ~}
//
// CPython: '一'.encode('hz') == b'~{R;~}'.
func TestHZRoundTrip(t *testing.T) {
	got, _, err := hzCodec.Encode("一", "strict")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{0x7e, 0x7b, 0x52, 0x3b, 0x7e, 0x7d}
	if !bytes.Equal(got, want) {
		t.Fatalf("encode = % x, want % x", got, want)
	}
	dec, _, err := hzCodec.Decode(got, "strict")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != "一" {
		t.Fatalf("decode = %q, want %q", dec, "一")
	}
}

// TestISO2022JPEncode pins the encoded byte form of "こん" in
// iso2022_jp: ESC $ B こん ESC ( B.
// CPython: 'こん'.encode('iso2022_jp') == b'\\x1b$B$3$s\\x1b(B'.
func TestISO2022JPEncode(t *testing.T) {
	got, _, err := iso2022JP.Encode("こん", "strict")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{0x1b, 0x24, 0x42, 0x24, 0x33, 0x24, 0x73, 0x1b, 0x28, 0x42}
	if !bytes.Equal(got, want) {
		t.Fatalf("encode = % x, want % x", got, want)
	}
	dec, _, err := iso2022JP.Decode(got, "strict")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != "こん" {
		t.Fatalf("decode = %q, want %q", dec, "こん")
	}
}

// TestISO2022KREncode pins the encoded byte form of "한글" in
// iso2022_kr: ESC $ ) C SO ... SI.
// CPython: '한글'.encode('iso2022_kr') == b'\\x1b$)C\\x0eGQ1[\\x0f'.
func TestISO2022KREncode(t *testing.T) {
	got, _, err := iso2022KR.Encode("한글", "strict")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := []byte{0x1b, 0x24, 0x29, 0x43, 0x0e, 0x47, 0x51, 0x31, 0x5b, 0x0f}
	if !bytes.Equal(got, want) {
		t.Fatalf("encode = % x, want % x", got, want)
	}
	dec, _, err := iso2022KR.Decode(got, "strict")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != "한글" {
		t.Fatalf("decode = %q, want %q", dec, "한글")
	}
}
