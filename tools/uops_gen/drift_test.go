package main

import "testing"

func TestExtractMarker(t *testing.T) {
	src := []byte("// Code generated\n// uop header sha256: abc123def\n\npackage optimizer\n")
	if got := ExtractMarker(src); got != "abc123def" {
		t.Errorf("got %q, want %q", got, "abc123def")
	}
	if got := ExtractMarker([]byte("// no marker here\n")); got != "" {
		t.Errorf("expected empty for missing marker, got %q", got)
	}
}
