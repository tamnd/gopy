// Indent / dedent emission panel. CPython promises one INDENT
// token per opening level and one DEDENT per closing level; blank
// lines and comments inside a block do not move the stack. Tab vs
// space mixing is a hard error.
//
// CPython: Parser/lexer/lexer.c:1616 tok_get indentation block

package partest

import (
	"testing"

	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/tokenize"
)

func countKind(toks []lexer.Tok, k tokenize.Type) int {
	n := 0
	for _, t := range toks {
		if t.Kind == k {
			n++
		}
	}
	return n
}

func TestIndentDedentPairs(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantIndents int
		wantDedents int
	}{
		{"flat", "x = 1\n", 0, 0},
		{"one_level", "if x:\n    y\n", 1, 1},
		{"two_levels", "if x:\n    if y:\n        z\n", 2, 2},
		{"blank_line_in_body", "if x:\n    a\n\n    b\n", 1, 1},
		{"nested_then_dedent_two", "if a:\n    if b:\n        c\nd\n", 2, 2},
	}
	for _, c := range cases {
		toks := tokenize_(t, c.src)
		gotI := countKind(toks, tokenize.INDENT)
		gotD := countKind(toks, tokenize.DEDENT)
		if gotI != c.wantIndents || gotD != c.wantDedents {
			t.Errorf("%s: indents=%d dedents=%d, want %d/%d (kinds=%v)",
				c.name, gotI, gotD, c.wantIndents, c.wantDedents, kinds(toks))
		}
	}
}

func TestDedentBalances(t *testing.T) {
	// Every INDENT must be matched by a DEDENT before ENDMARKER.
	src := "if a:\n    if b:\n        c\n        d\n    e\nf\n"
	toks := tokenize_(t, src)
	if countKind(toks, tokenize.INDENT) != countKind(toks, tokenize.DEDENT) {
		t.Errorf("INDENT/DEDENT not balanced: %v", kinds(toks))
	}
}
