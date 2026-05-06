// Pin the full token-kind table. Numeric values come from
// cpython/Grammar/Tokens via the generator; this test guards the
// generated table from silent edits and pairs each Type with its
// CPython tok_name spelling.
//
// CPython: Lib/token.py tok_name

package partest

import (
	"testing"

	"github.com/tamnd/gopy/token"
)

func TestTokenKindTable(t *testing.T) {
	cases := []struct {
		kind token.Type
		num  int
		name string
	}{
		{token.ENDMARKER, 0, "ENDMARKER"},
		{token.NAME, 1, "NAME"},
		{token.NUMBER, 2, "NUMBER"},
		{token.STRING, 3, "STRING"},
		{token.NEWLINE, 4, "NEWLINE"},
		{token.INDENT, 5, "INDENT"},
		{token.DEDENT, 6, "DEDENT"},
		{token.LPAR, 7, "LPAR"},
		{token.RPAR, 8, "RPAR"},
		{token.LSQB, 9, "LSQB"},
		{token.RSQB, 10, "RSQB"},
		{token.COLON, 11, "COLON"},
		{token.COMMA, 12, "COMMA"},
		{token.SEMI, 13, "SEMI"},
		{token.PLUS, 14, "PLUS"},
		{token.MINUS, 15, "MINUS"},
		{token.STAR, 16, "STAR"},
		{token.SLASH, 17, "SLASH"},
		{token.VBAR, 18, "VBAR"},
		{token.AMPER, 19, "AMPER"},
		{token.LESS, 20, "LESS"},
		{token.GREATER, 21, "GREATER"},
		{token.EQUAL, 22, "EQUAL"},
		{token.DOT, 23, "DOT"},
		{token.PERCENT, 24, "PERCENT"},
		{token.LBRACE, 25, "LBRACE"},
		{token.RBRACE, 26, "RBRACE"},
		{token.EQEQUAL, 27, "EQEQUAL"},
		{token.NOTEQUAL, 28, "NOTEQUAL"},
		{token.LESSEQUAL, 29, "LESSEQUAL"},
		{token.GREATEREQUAL, 30, "GREATEREQUAL"},
		{token.TILDE, 31, "TILDE"},
		{token.CIRCUMFLEX, 32, "CIRCUMFLEX"},
		{token.LEFTSHIFT, 33, "LEFTSHIFT"},
		{token.RIGHTSHIFT, 34, "RIGHTSHIFT"},
		{token.DOUBLESTAR, 35, "DOUBLESTAR"},
		{token.PLUSEQUAL, 36, "PLUSEQUAL"},
		{token.MINEQUAL, 37, "MINEQUAL"},
		{token.STAREQUAL, 38, "STAREQUAL"},
		{token.SLASHEQUAL, 39, "SLASHEQUAL"},
		{token.PERCENTEQUAL, 40, "PERCENTEQUAL"},
		{token.AMPEREQUAL, 41, "AMPEREQUAL"},
		{token.VBAREQUAL, 42, "VBAREQUAL"},
		{token.CIRCUMFLEXEQUAL, 43, "CIRCUMFLEXEQUAL"},
		{token.LEFTSHIFTEQUAL, 44, "LEFTSHIFTEQUAL"},
		{token.RIGHTSHIFTEQUAL, 45, "RIGHTSHIFTEQUAL"},
		{token.DOUBLESTAREQUAL, 46, "DOUBLESTAREQUAL"},
		{token.DOUBLESLASH, 47, "DOUBLESLASH"},
		{token.DOUBLESLASHEQUAL, 48, "DOUBLESLASHEQUAL"},
		{token.AT, 49, "AT"},
		{token.ATEQUAL, 50, "ATEQUAL"},
		{token.RARROW, 51, "RARROW"},
		{token.ELLIPSIS, 52, "ELLIPSIS"},
		{token.COLONEQUAL, 53, "COLONEQUAL"},
		{token.EXCLAMATION, 54, "EXCLAMATION"},
		{token.OP, 55, "OP"},
		{token.TYPE_IGNORE, 56, "TYPE_IGNORE"},
		{token.TYPE_COMMENT, 57, "TYPE_COMMENT"},
		{token.SOFT_KEYWORD, 58, "SOFT_KEYWORD"},
		{token.FSTRING_START, 59, "FSTRING_START"},
		{token.FSTRING_MIDDLE, 60, "FSTRING_MIDDLE"},
		{token.FSTRING_END, 61, "FSTRING_END"},
		{token.TSTRING_START, 62, "TSTRING_START"},
		{token.TSTRING_MIDDLE, 63, "TSTRING_MIDDLE"},
		{token.TSTRING_END, 64, "TSTRING_END"},
		{token.COMMENT, 65, "COMMENT"},
		{token.NL, 66, "NL"},
		{token.ERRORTOKEN, 67, "ERRORTOKEN"},
		{token.ENCODING, 68, "ENCODING"},
	}
	if int(token.NTokens) != 69 {
		t.Fatalf("NTokens = %d, want 69", int(token.NTokens))
	}
	for _, c := range cases {
		if int(c.kind) != c.num {
			t.Errorf("%s = %d, want %d", c.name, int(c.kind), c.num)
		}
		if c.kind.String() != c.name {
			t.Errorf("%s.String() = %q, want %q", c.name, c.kind.String(), c.name)
		}
	}
}
