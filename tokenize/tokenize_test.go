package tokenize

import (
	"errors"
	"io"
	"testing"
)

// TestSkeletonReturnsEOF: the v0.5 skeleton always reports io.EOF.
// This test pins the contract so consumers can program against the
// stable surface today.
func TestSkeletonReturnsEOF(t *testing.T) {
	it := New("x = 1", false)
	_, err := it.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("Next() error = %v, want io.EOF", err)
	}
}

// TestTypeNumericValues pins a hand-picked subset of token numeric
// values against CPython 3.14 Grammar/Tokens. The generator emits the
// full set; this test catches drift if the file is hand-edited.
func TestTypeNumericValues(t *testing.T) {
	cases := []struct {
		got  Type
		want int
		name string
	}{
		{ENDMARKER, 0, "ENDMARKER"},
		{NAME, 1, "NAME"},
		{NUMBER, 2, "NUMBER"},
		{STRING, 3, "STRING"},
		{NEWLINE, 4, "NEWLINE"},
		{INDENT, 5, "INDENT"},
		{DEDENT, 6, "DEDENT"},
		{OP, 55, "OP"},
		{COMMENT, 65, "COMMENT"},
		{NL, 66, "NL"},
		{ENCODING, 68, "ENCODING"},
	}
	for _, c := range cases {
		if int(c.got) != c.want {
			t.Errorf("%s = %d, want %d", c.name, int(c.got), c.want)
		}
	}
}

// TestTypeString pins the CPython tok_name parity for Type.String.
func TestTypeString(t *testing.T) {
	if got := NAME.String(); got != "NAME" {
		t.Errorf("NAME.String() = %q, want NAME", got)
	}
	if got := ENCODING.String(); got != "ENCODING" {
		t.Errorf("ENCODING.String() = %q, want ENCODING", got)
	}
	if got := Type(-1).String(); got != "TYPE(-1)" {
		t.Errorf("Type(-1).String() = %q, want TYPE(-1)", got)
	}
}

func TestNewReadlineSkeleton(t *testing.T) {
	it := NewReadline(func() (string, error) { return "", io.EOF }, true)
	if it == nil {
		t.Fatal("NewReadline returned nil")
	}
	_, err := it.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("Next() error = %v, want io.EOF", err)
	}
}
