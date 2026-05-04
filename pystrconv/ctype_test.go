package pystrconv_test

import (
	"testing"

	"github.com/tamnd/gopy/pystrconv"
)

// TestCtypeAgainstReference exhaustively walks 0..255 and checks every
// classification predicate plus tolower/toupper against the literal
// table from cpython/Python/pyctype.c. Any divergence is a port bug.
func TestCtypeAgainstReference(t *testing.T) {
	for c := range 256 {
		b := byte(c)
		want := refFlags(b)
		if got := pystrconv.Flags(b); got != want {
			t.Fatalf("Flags(0x%02x): got %#x want %#x", b, got, want)
		}
		if got, want := pystrconv.ToLower(b), refToLower(b); got != want {
			t.Fatalf("ToLower(0x%02x): got 0x%02x want 0x%02x", b, got, want)
		}
		if got, want := pystrconv.ToUpper(b), refToUpper(b); got != want {
			t.Fatalf("ToUpper(0x%02x): got 0x%02x want 0x%02x", b, got, want)
		}
	}
}

func refFlags(c byte) uint32 {
	var f uint32
	if c >= '0' && c <= '9' {
		f |= pystrconv.CtDigit | pystrconv.CtXDigit
	}
	if (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') {
		f |= pystrconv.CtXDigit
	}
	if c >= 'A' && c <= 'Z' {
		f |= pystrconv.CtUpper
	}
	if c >= 'a' && c <= 'z' {
		f |= pystrconv.CtLower
	}
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		f |= pystrconv.CtSpace
	}
	return f
}

func refToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

func refToUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}
	return c
}

func TestPredicateSpotChecks(t *testing.T) {
	if !pystrconv.IsDigit('5') || pystrconv.IsDigit('a') {
		t.Fatal("IsDigit broken")
	}
	if !pystrconv.IsXDigit('A') || !pystrconv.IsXDigit('f') || pystrconv.IsXDigit('g') {
		t.Fatal("IsXDigit broken")
	}
	if !pystrconv.IsSpace(' ') || !pystrconv.IsSpace('\t') || pystrconv.IsSpace('x') {
		t.Fatal("IsSpace broken")
	}
	if !pystrconv.IsAlnum('Z') || !pystrconv.IsAlnum('0') || pystrconv.IsAlnum('!') {
		t.Fatal("IsAlnum broken")
	}
}
