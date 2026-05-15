package tokenize

import (
	"errors"
	"io"
	"testing"

	"github.com/tamnd/gopy/token"
)

// TestEndmarkerThenEOF: after the lexer emits ENDMARKER, Next reports
// io.EOF on the following call. Pins the iterator-protocol contract.
func TestEndmarkerThenEOF(t *testing.T) {
	it := New("", false)
	tok, err := it.Next()
	if err != nil {
		t.Fatalf("first Next: err=%v", err)
	}
	if tok.Type != token.ENDMARKER {
		t.Fatalf("first token = %s, want ENDMARKER", tok.Type)
	}
	_, err = it.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("second Next error = %v, want io.EOF", err)
	}
}

// TestSimpleAssignment walks a `x = 1` source through the iterator and
// pins the leading kinds against CPython tokenize parity. Full byte-
// for-byte parity with tokenize.tokenize() lives in the partest panel;
// this just verifies the wiring reaches the lexer.
func TestSimpleAssignment(t *testing.T) {
	it := New("x = 1\n", false)
	want := []token.Type{token.NAME, token.EQUAL, token.NUMBER, token.NEWLINE}
	for i, w := range want {
		tok, err := it.Next()
		if err != nil {
			t.Fatalf("Next #%d: %v", i, err)
		}
		if tok.Type != w {
			t.Fatalf("Next #%d type = %s, want %s", i, tok.Type, w)
		}
	}
}

func TestNewReadlineDrainsLines(t *testing.T) {
	lines := []string{"x = 1\n", ""}
	i := 0
	rl := func() (string, error) {
		if i >= len(lines) || lines[i] == "" {
			return "", io.EOF
		}
		s := lines[i]
		i++
		return s, nil
	}
	it := NewReadline(rl, false)
	if it == nil {
		t.Fatal("NewReadline returned nil")
	}
	tok, err := it.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if tok.Type != token.NAME || tok.Value != "x" {
		t.Fatalf("first token = (%s, %q), want (NAME, \"x\")", tok.Type, tok.Value)
	}
}
