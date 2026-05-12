package _json

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestScanstringSimple verifies that scanstring parses a simple JSON string.
// scanstring('"hello"', 1) should return ('hello', 7).
// The outer quotes are part of the input; end=1 points past the opening '"'.
func TestScanstringSimple(t *testing.T) {
	// In CPython: scanstring('"hello"', 1) returns ('hello', 7).
	// The string '"hello"' has runes ['"','h','e','l','l','o','"'].
	// We pass pos=1 which points to 'h'; the scanner reads until '"'.
	args := []objects.Object{
		objects.NewStr(`"hello"`),
		objects.NewInt(1),
	}
	result, err := moduleScanstring(args, nil)
	if err != nil {
		t.Fatalf("scanstring error: %v", err)
	}
	tup, ok := result.(*objects.Tuple)
	if !ok {
		t.Fatalf("scanstring returned non-tuple: %T", result)
	}
	if tup.Len() != 2 {
		t.Fatalf("tuple length = %d, want 2", tup.Len())
	}
	s, err := objects.Str(tup.Item(0))
	if err != nil {
		t.Fatalf("str error: %v", err)
	}
	if s != "hello" {
		t.Fatalf("scanstring string = %q, want %q", s, "hello")
	}
	endInt, ok2 := tup.Item(1).(*objects.Int)
	if !ok2 {
		t.Fatalf("end is not int: %T", tup.Item(1))
	}
	endVal, _ := endInt.Int64()
	if endVal != 7 {
		t.Fatalf("scanstring end = %d, want 7", endVal)
	}
}

// TestScanstringEscapes verifies backslash escape handling.
func TestScanstringEscapes(t *testing.T) {
	// '\"hello\\nworld\"' starting at 1.
	args := []objects.Object{
		objects.NewStr(`"hello\nworld"`),
		objects.NewInt(1),
	}
	result, err := moduleScanstring(args, nil)
	if err != nil {
		t.Fatalf("scanstring error: %v", err)
	}
	tup := result.(*objects.Tuple)
	s, _ := objects.Str(tup.Item(0))
	if s != "hello\nworld" {
		t.Fatalf("scanstring escape result = %q, want %q", s, "hello\nworld")
	}
}

// TestEncodeBasestring verifies that encode_basestring adds quotes and
// escapes special characters.
func TestEncodeBasestring(t *testing.T) {
	args := []objects.Object{objects.NewStr("hello\nworld")}
	result, err := moduleEncodeBasestring(args, nil)
	if err != nil {
		t.Fatalf("encode_basestring error: %v", err)
	}
	got, _ := objects.Str(result)
	want := `"hello\nworld"`
	if got != want {
		t.Fatalf("encode_basestring = %q, want %q", got, want)
	}
}

// TestEncodeBasestringASCII verifies that non-ASCII is escaped as \uXXXX.
func TestEncodeBasestringASCII(t *testing.T) {
	// U+00E9 is 'é'. encode_basestring_ascii must produce "café"
	// (six characters: backslash, u, 0, 0, e, 9 — not the literal rune).
	args := []objects.Object{objects.NewStr("café")}
	result, err := moduleEncodeBasestringASCII(args, nil)
	if err != nil {
		t.Fatalf("encode_basestring_ascii error: %v", err)
	}
	got, _ := objects.Str(result)
	// want is the string: "café" including surrounding quotes.
	want := "\"caf\\u00e9\""
	if got != want {
		t.Fatalf("encode_basestring_ascii = %q, want %q", got, want)
	}
}

// TestEncodeBasestringBackslash verifies backslash escaping.
func TestEncodeBasestringBackslash(t *testing.T) {
	args := []objects.Object{objects.NewStr(`a\b`)}
	result, err := moduleEncodeBasestring(args, nil)
	if err != nil {
		t.Fatalf("encode_basestring error: %v", err)
	}
	got, _ := objects.Str(result)
	want := `"a\\b"`
	if got != want {
		t.Fatalf("encode_basestring backslash = %q, want %q", got, want)
	}
}
