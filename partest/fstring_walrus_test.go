// Walrus assignment and multiline bodies inside f-strings. CPython
// 3.12 lifted the "no walrus inside f-string" restriction by moving
// f-string lexing into the main lexer; the gopy lexer must follow.
//
// CPython: Parser/lexer/lexer.c tok_get_fstring_mode

package partest

import (
	"testing"

	"github.com/tamnd/gopy/tokenize"
)

func TestFStringWalrus(t *testing.T) {
	src := "f'before {(y := 5)} after'\n"
	toks := tokenize_(t, src)
	for _, tk := range toks {
		if tk.Kind == tokenize.ERRORTOKEN {
			t.Fatalf("walrus inside f-string rejected: %v", kinds(toks))
		}
	}
	if !contains(toks, tokenize.FSTRING_START) || !contains(toks, tokenize.FSTRING_END) {
		t.Errorf("walrus f-string missing brackets: %v", kinds(toks))
	}
}

func TestFStringMultiline(t *testing.T) {
	// Triple-quoted f-string spans multiple physical lines and embeds
	// a newline inside the {expr} body.
	src := "f'''\nhello\n{\n    x\n    +\n    y\n}\n'''\n"
	toks := tokenize_(t, src)
	for _, tk := range toks {
		if tk.Kind == tokenize.ERRORTOKEN {
			t.Fatalf("multiline f-string rejected: %v", kinds(toks))
		}
	}
	if !contains(toks, tokenize.FSTRING_START) || !contains(toks, tokenize.FSTRING_END) {
		t.Errorf("multiline f-string missing brackets: %v", kinds(toks))
	}
}
