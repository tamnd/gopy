package codecs

import (
	"bytes"
	"testing"
)

// TestEncodeDecodeUTF8 pins round-trip for the utf-8 codec.
func TestEncodeDecodeUTF8(t *testing.T) {
	cases := []string{
		"",
		"hello",
		"café",
		"中文",
	}
	for _, s := range cases {
		b, n, err := Encode(s, "utf-8", "strict")
		if err != nil {
			t.Fatalf("Encode(%q): %v", s, err)
		}
		if n != len(b) {
			t.Errorf("Encode(%q) n=%d, want %d", s, n, len(b))
		}
		got, _, err := Decode(b, "utf-8", "strict")
		if err != nil {
			t.Fatalf("Decode(%q): %v", s, err)
		}
		if got != s {
			t.Errorf("round-trip %q: got %q", s, got)
		}
	}
}

// TestEncodeDecodeASCII pins the ascii codec for valid input.
func TestEncodeDecodeASCII(t *testing.T) {
	s := "hello world"
	b, _, err := Encode(s, "ascii", "strict")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(b, []byte(s)) {
		t.Errorf("Encode: got %v, want %v", b, []byte(s))
	}
	got, _, err := Decode(b, "ascii", "strict")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != s {
		t.Errorf("Decode: got %q, want %q", got, s)
	}
}

// TestEncodeASCIIStrict pins that non-ASCII input with errors="strict"
// returns an error.
func TestEncodeASCIIStrict(t *testing.T) {
	_, _, err := Encode("café", "ascii", "strict")
	if err == nil {
		t.Fatal("expected error for non-ASCII input with strict handler")
	}
}

// TestEncodeASCIIIgnore pins that non-ASCII characters are skipped
// when errors="ignore".
func TestEncodeASCIIIgnore(t *testing.T) {
	b, _, err := Encode("café", "ascii", "ignore")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(b, []byte("caf")) {
		t.Errorf("got %q, want %q", b, "caf")
	}
}

// TestDecodeASCIIBadByte pins that a byte with the high bit set
// triggers the error handler in decode mode.
func TestDecodeASCIIBadByte(t *testing.T) {
	_, _, err := Decode([]byte{0xFF}, "ascii", "strict")
	if err == nil {
		t.Fatal("expected error for 0xFF byte in ascii decode strict")
	}
}

// TestEncodeDecodeLatin1 pins that latin-1 encodes bytes 0x00–0xFF
// as themselves and decodes them back.
func TestEncodeDecodeLatin1(t *testing.T) {
	// Build a string from all 256 latin-1 code points.
	runes := make([]rune, 256)
	for i := range runes {
		runes[i] = rune(i)
	}
	s := string(runes)
	b, _, err := Encode(s, "latin-1", "strict")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(b) != 256 {
		t.Fatalf("Encode: got %d bytes, want 256", len(b))
	}
	for i, v := range b {
		if int(v) != i {
			t.Errorf("b[%d] = %d, want %d", i, v, i)
		}
	}
	got, _, err := Decode(b, "latin-1", "strict")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != s {
		t.Error("latin-1 round-trip mismatch")
	}
}

// TestEncodeLatin1StrictWide pins that a rune > U+00FF fails strict.
func TestEncodeLatin1StrictWide(t *testing.T) {
	_, _, err := Encode("Ā", "latin-1", "strict")
	if err == nil {
		t.Fatal("expected error for rune > U+00FF in latin-1 strict")
	}
}

// TestEncodeAliases pins that common codec name aliases resolve.
func TestEncodeAliases(t *testing.T) {
	aliases := []string{"utf-8", "utf8", "UTF-8", "utf_8"}
	for _, name := range aliases {
		_, _, err := Encode("hello", name, "strict")
		if err != nil {
			t.Errorf("Encode(%q): %v", name, err)
		}
	}
}

// TestDecodeAliasesLatin1 pins that latin-1 aliases resolve.
func TestDecodeAliasesLatin1(t *testing.T) {
	aliases := []string{"latin-1", "latin1", "iso-8859-1", "iso_8859_1", "l1"}
	for _, name := range aliases {
		_, _, err := Decode([]byte{0x41}, name, "strict")
		if err != nil {
			t.Errorf("Decode(%q): %v", name, err)
		}
	}
}

// TestLookupUnknown pins that an unknown encoding surfaces an error.
func TestLookupUnknown(t *testing.T) {
	_, err := Lookup("no-such-codec-xyz")
	if err == nil {
		t.Fatal("expected error for unknown codec")
	}
}

// TestUTF16RoundTrip pins the BOM-prefixed utf-16 codec and the LE/BE
// variants. The round-trip must produce the original string for every
// variant.
func TestUTF16RoundTrip(t *testing.T) {
	for _, enc := range []string{"utf-16", "utf-16-le", "utf-16-be"} {
		s := "1+1"
		b, _, err := Encode(s, enc, "strict")
		if err != nil {
			t.Fatalf("Encode(%q): %v", enc, err)
		}
		got, _, err := Decode(b, enc, "strict")
		if err != nil {
			t.Fatalf("Decode(%q): %v", enc, err)
		}
		if got != s {
			t.Errorf("%s round-trip: got %q want %q", enc, got, s)
		}
	}
}

// TestUTF32RoundTrip mirrors TestUTF16RoundTrip for utf-32 / -le / -be.
func TestUTF32RoundTrip(t *testing.T) {
	for _, enc := range []string{"utf-32", "utf-32-le", "utf-32-be"} {
		s := "café"
		b, _, err := Encode(s, enc, "strict")
		if err != nil {
			t.Fatalf("Encode(%q): %v", enc, err)
		}
		got, _, err := Decode(b, enc, "strict")
		if err != nil {
			t.Fatalf("Decode(%q): %v", enc, err)
		}
		if got != s {
			t.Errorf("%s round-trip: got %q want %q", enc, got, s)
		}
	}
}

// TestISO885915Euro pins the iso-8859-15 / latin-1 difference: 0xA4 is
// the euro sign in iso-8859-15. (In iso-8859-1 the same byte is the
// generic currency sign, which would not round-trip through latin-9.)
func TestISO885915Euro(t *testing.T) {
	b, _, err := Encode("€", "iso-8859-15", "strict")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(b, []byte{0xA4}) {
		t.Fatalf("Encode: got % x want a4", b)
	}
	got, _, err := Decode([]byte{0xA4}, "iso-8859-15", "strict")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "€" {
		t.Errorf("Decode: got %q want %q", got, "€")
	}
}

// TestCP1252EuroSign pins one of the Windows code-page codecs registered
// via the charmap helper. 0x80 in cp1252 is the euro sign.
func TestCP1252EuroSign(t *testing.T) {
	got, _, err := Decode([]byte{0x80}, "cp1252", "strict")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "€" {
		t.Errorf("Decode cp1252 0x80: got %q want %q", got, "€")
	}
	b, _, err := Encode("€", "cp1252", "strict")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(b, []byte{0x80}) {
		t.Errorf("Encode cp1252 €: got % x want 80", b)
	}
}
