package io

import (
	"math/big"
	"testing"
	"unicode/utf16"
)

// TestCookiePackParseRoundTrip exercises the 21-byte layout directly.
func TestCookiePackParseRoundTrip(t *testing.T) {
	cases := []cookie{
		{},
		{StartPos: 0, BytesToFeed: 4, CharsToSkip: 1, DecFlags: 2},
		{StartPos: 1 << 40, DecFlags: -1, BytesToFeed: 1024, CharsToSkip: 17, NeedEOF: true},
	}
	for _, c := range cases {
		v := packCookie(c)
		got, err := parseCookie(v)
		if err != nil {
			t.Fatalf("parse %#v: %v", c, err)
		}
		if got != c {
			t.Fatalf("round trip: got %#v want %#v (packed %s)", got, c, v.String())
		}
	}
}

// TestParseCookieNegative asserts negative ints are rejected, matching
// CPython's _PyLong_AsByteArray with signed=0.
func TestParseCookieNegative(t *testing.T) {
	if _, err := parseCookie(new(big.Int).SetInt64(-1)); err == nil {
		t.Fatal("expected ValueError on negative cookie")
	}
}

// TestTextIOTellSeekRoundTripUTF16 reads through a utf-16 stream, takes
// a mid-stream tell, advances, seeks back, and asserts the second half
// reads the same content. This is the spec 1709 phase-3 gate.
func TestTextIOTellSeekRoundTripUTF16(t *testing.T) {
	body := "abcdef"
	units := utf16.Encode([]rune(body))
	raw := make([]byte, 0, 2+2*len(units))
	raw = append(raw, 0xFF, 0xFE)
	for _, u := range units {
		raw = append(raw, byte(u), byte(u>>8))
	}
	bio := NewBytesIO(raw)
	tw := NewTextIOWrapper(nil, "utf-16", "strict", "<mem>", "r")
	tw.buf = bio
	// Drop the BOM (read returns it as one char in utf-16 dispatch).
	first, err := tw.read(2)
	if err != nil {
		t.Fatal(err)
	}
	if first != "ab" {
		t.Fatalf("first read = %q want \"ab\"", first)
	}
	pos, err := tw.TellCookie()
	if err != nil {
		t.Fatalf("tell: %v", err)
	}
	rest, err := tw.read(-1)
	if err != nil {
		t.Fatal(err)
	}
	if rest != "cdef" {
		t.Fatalf("rest = %q want \"cdef\"", rest)
	}
	if _, err := tw.SeekCookie(pos, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	again, err := tw.read(-1)
	if err != nil {
		t.Fatal(err)
	}
	if again != rest {
		t.Fatalf("after seek = %q want %q", again, rest)
	}
}
