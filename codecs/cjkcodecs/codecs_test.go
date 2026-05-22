package cjkcodecs

import "testing"

// TestCP932DecodeNen mirrors test_source_encoding.test_issue2301:
// cp932 decode of b"\x94\x4e" yields the single codepoint U+5E74 (年).
func TestCP932DecodeNen(t *testing.T) {
	got, _, err := CP932.Decode([]byte{0x94, 0x4e}, "strict")
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
	got, _, err := CP949.Decode([]byte{0xaa, 0xa7}, "strict")
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
			enc, _, err := EUCKR.Encode(tc.s, "strict")
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			dec, _, err := EUCKR.Decode(enc, "strict")
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if dec != tc.s {
				t.Fatalf("round-trip mismatch: %q -> % x -> %q", tc.s, enc, dec)
			}
		})
	}
}
