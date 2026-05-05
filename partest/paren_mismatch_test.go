// Pin the lexer's paren-mismatch SyntaxError text against
// CPython 3.14 byte-for-byte, including the line-number tail.
//
// CPython: Parser/tokenizer/helpers.c

package partest

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/tokenize"
)

func drain(s *lexer.State) {
	for {
		tk := s.Get()
		if tk.Kind == tokenize.ENDMARKER || tk.Kind == tokenize.ERRORTOKEN {
			return
		}
	}
}

func TestParenMismatchUnmatchedClose(t *testing.T) {
	s := lexer.FromString("a)\n", lexer.ModeFile)
	drain(s)
	if e := s.Err(); e == nil || e.Message != "unmatched ')'" {
		t.Errorf("err = %v, want \"unmatched ')'\"", e)
	}
}

func TestParenMismatchWrongCloser(t *testing.T) {
	s := lexer.FromString("(a]\n", lexer.ModeFile)
	drain(s)
	e := s.Err()
	if e == nil {
		t.Fatal("no error")
	}
	want := "closing parenthesis ']' does not match opening parenthesis '(' on line 1"
	if e.Message != want {
		t.Errorf("err = %q\nwant %q", e.Message, want)
	}
}

func TestParenMismatchSquareVsBrace(t *testing.T) {
	s := lexer.FromString("[1, 2}\n", lexer.ModeFile)
	drain(s)
	e := s.Err()
	if e == nil || !strings.Contains(e.Message, "does not match opening parenthesis '['") {
		t.Errorf("err = %v", e)
	}
}
