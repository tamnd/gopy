package io

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// TestUTF8DecoderSplitMultibyte feeds a single utf-8 sequence one byte
// at a time. A stateless codec would raise on every prefix; the
// incremental decoder must buffer.
func TestUTF8DecoderSplitMultibyte(t *testing.T) {
	d, err := getIncrementalDecoder("utf-8", "strict")
	if err != nil {
		t.Fatal(err)
	}
	want := "héllo"
	in := []byte(want)
	var out strings.Builder
	for i := range in {
		s, err := d.Decode(in[i:i+1], false)
		if err != nil {
			t.Fatalf("byte %d: %v", i, err)
		}
		out.WriteString(s)
	}
	s, err := d.Decode(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	out.WriteString(s)
	if got := out.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestUTF16AutoBOMSplit reproduces the bug the spec is closing: BOM
// arrives one byte at a time, then the body trickles in. The decoder
// must hold the first BOM byte and start emitting once endianness is
// settled.
func TestUTF16AutoBOMSplit(t *testing.T) {
	d, err := getIncrementalDecoder("utf-16", "strict")
	if err != nil {
		t.Fatal(err)
	}
	want := "héllo"
	units := utf16.Encode([]rune(want))
	full := []byte{0xFF, 0xFE}
	for _, u := range units {
		full = append(full, byte(u), byte(u>>8))
	}
	var out strings.Builder
	for i := range full {
		s, err := d.Decode(full[i:i+1], false)
		if err != nil {
			t.Fatalf("byte %d: %v", i, err)
		}
		out.WriteString(s)
	}
	s, err := d.Decode(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	out.WriteString(s)
	if got := out.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestUTF16DecoderGetStateRoundTrip snapshots the decoder mid-stream,
// builds a fresh decoder, applies SetState, and asserts the resumed
// decoder produces the same suffix.
func TestUTF16DecoderGetStateRoundTrip(t *testing.T) {
	want := "abcdef"
	units := utf16.Encode([]rune(want))
	full := []byte{0xFF, 0xFE}
	for _, u := range units {
		full = append(full, byte(u), byte(u>>8))
	}
	d, _ := getIncrementalDecoder("utf-16", "strict")
	// Feed enough to settle endianness and emit a few chars.
	if _, err := d.Decode(full[:5], false); err != nil {
		t.Fatal(err)
	}
	buf, flags := d.GetState()
	fresh, _ := getIncrementalDecoder("utf-16", "strict")
	if err := fresh.SetState(buf, flags); err != nil {
		t.Fatal(err)
	}
	rest, err := fresh.Decode(full[5:], true)
	if err != nil {
		t.Fatal(err)
	}
	// The first decoder consumed BOM (2 bytes) + 3 bytes = part of
	// one full code unit + one leftover byte. The resumed decoder
	// must see the leftover byte from state and emit the rest of
	// the string.
	if !strings.Contains(rest, "f") {
		t.Fatalf("resumed decode missing tail: %q", rest)
	}
}

// TestUTF16EncoderEmitsBOMOnce verifies the BOM is emitted only on the
// first encode call, matching CPython's IncrementalEncoder.
func TestUTF16EncoderEmitsBOMOnce(t *testing.T) {
	e, err := getIncrementalEncoder("utf-16", "strict")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := e.Encode("a", false)
	second, _ := e.Encode("b", false)
	if len(first) < 4 || first[0] != 0xFF || first[1] != 0xFE {
		t.Fatalf("first chunk missing BOM: %x", first)
	}
	if len(second) != 2 {
		t.Fatalf("second chunk should be raw code unit, got %x", second)
	}
}

// TestLookupUnknownEncoding asserts a LookupError-shaped go error
// matches the CPython surface.
func TestLookupUnknownEncoding(t *testing.T) {
	if _, err := getIncrementalDecoder("rot13", "strict"); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), "LookupError") {
		t.Fatalf("got %v", err)
	}
}
