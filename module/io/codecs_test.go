package io

import (
	"bytes"
	"testing"
)

// codecDecodeOnce / codecEncodeOnce wrap the incremental codec layer
// with a single-call API so the table-driven round-trip tests below
// stay short. Mirrors the legacy decodeBytes / encodeString shape
// these tests previously called.
func codecDecodeOnce(data []byte, encoding string) (string, error) {
	d, err := getIncrementalDecoder(encoding, "strict")
	if err != nil {
		return "", err
	}
	return d.Decode(data, true)
}

func codecEncodeOnce(s, encoding string) ([]byte, error) {
	e, err := getIncrementalEncoder(encoding, "strict")
	if err != nil {
		return nil, err
	}
	return e.Encode(s, true)
}

// CPython reference vectors generated with:
//   python3 -c "import sys; sys.stdout.buffer.write('Hello, World! αβγ'.encode('utf-16'))"
//
// Each case round-trips through encode + decode so the table doubles
// as a regression guard against the byte-order / BOM logic.

func TestCodecsRoundtrip(t *testing.T) {
	cases := []struct {
		encoding string
		input    string
	}{
		{"utf-16", "Hello, World!"},
		{"utf-16-le", "αβγ δεζ"},
		{"utf-16-be", "日本語テスト"},
		{"utf-32", "x"},
		{"utf-32-le", "abc"},
		{"utf-32-be", "Ω 𐐷"},
		{"cp1252", "Euro sign: €"},
		{"cp1250", "Polish: Łódź"},
		{"cp1251", "Hello мир"},
		{"cp437", "ABC │─"},
		{"mac-roman", "café résumé"},
	}
	for _, c := range cases {
		t.Run(c.encoding, func(t *testing.T) {
			b, err := codecEncodeOnce(c.input, c.encoding)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := codecDecodeOnce(b, c.encoding)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != c.input {
				t.Errorf("roundtrip = %q, want %q", got, c.input)
			}
		})
	}
}

// TestUTF16BOM verifies that auto utf-16 decoding honors BE/LE BOMs.
func TestUTF16BOM(t *testing.T) {
	le := []byte{0xFF, 0xFE, 'A', 0, 'B', 0}
	be := []byte{0xFE, 0xFF, 0, 'A', 0, 'B'}
	for _, data := range [][]byte{le, be} {
		s, err := codecDecodeOnce(data, "utf-16")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if s != "AB" {
			t.Errorf("decode %v = %q, want AB", data, s)
		}
	}
}

// TestUTF32BOM verifies auto utf-32 decode picks BE/LE based on BOM.
func TestUTF32BOM(t *testing.T) {
	le := []byte{0xFF, 0xFE, 0, 0, 'A', 0, 0, 0}
	be := []byte{0, 0, 0xFE, 0xFF, 0, 0, 0, 'A'}
	for _, data := range [][]byte{le, be} {
		s, err := codecDecodeOnce(data, "utf-32")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if s != "A" {
			t.Errorf("decode %v = %q, want A", data, s)
		}
	}
}

// TestCP1252EuroSign pins the cp1252 byte for U+20AC.
func TestCP1252EuroSign(t *testing.T) {
	b, err := codecEncodeOnce("€", "cp1252")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Equal(b, []byte{0x80}) {
		t.Errorf("euro sign bytes = % x, want 80", b)
	}
}

// TestUnknownCodecReturnsLookup verifies unknown encodings surface a
// LookupError-shaped message rather than NotImplementedError.
func TestUnknownCodecReturnsLookup(t *testing.T) {
	_, err := codecDecodeOnce([]byte("hello"), "nope-fake-encoding")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("LookupError")) {
		t.Errorf("err = %v, want LookupError", err)
	}
}

// TestUTF8Decode keeps the inline utf-8 path working after the wiring
// change.
func TestUTF8Decode(t *testing.T) {
	s, err := codecDecodeOnce([]byte("héllo"), "utf-8")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s != "héllo" {
		t.Errorf("decode = %q", s)
	}
}
