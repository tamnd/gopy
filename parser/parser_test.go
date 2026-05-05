package parser

import (
	"errors"
	"strings"
	"testing"
)

func TestParseStringSentinel(t *testing.T) {
	_, err := ParseString("x = 1\n", "x.py", ModeFile)
	if !errors.Is(err, ErrParserNotImplemented) {
		t.Errorf("err = %v, want ErrParserNotImplemented", err)
	}
}

func TestParseReader(t *testing.T) {
	_, err := Parse(strings.NewReader("y\n"), "x.py", ModeEval)
	if !errors.Is(err, ErrParserNotImplemented) {
		t.Errorf("err = %v", err)
	}
}
