// Pin the full token-kind table. Numeric values come from
// cpython/Grammar/Tokens via the generator; this test guards the
// generated table from silent edits and pairs each Type with its
// CPython tok_name spelling.
//
// CPython: Lib/token.py tok_name

package partest

import (
	"testing"

	"github.com/tamnd/gopy/tokenize"
)

func TestTokenKindTable(t *testing.T) {
	cases := []struct {
		kind tokenize.Type
		num  int
		name string
	}{
		{tokenize.ENDMARKER, 0, "ENDMARKER"},
		{tokenize.NAME, 1, "NAME"},
		{tokenize.NUMBER, 2, "NUMBER"},
		{tokenize.STRING, 3, "STRING"},
		{tokenize.NEWLINE, 4, "NEWLINE"},
		{tokenize.INDENT, 5, "INDENT"},
		{tokenize.DEDENT, 6, "DEDENT"},
		{tokenize.LPAR, 7, "LPAR"},
		{tokenize.RPAR, 8, "RPAR"},
		{tokenize.LSQB, 9, "LSQB"},
		{tokenize.RSQB, 10, "RSQB"},
		{tokenize.COLON, 11, "COLON"},
		{tokenize.COMMA, 12, "COMMA"},
		{tokenize.SEMI, 13, "SEMI"},
		{tokenize.PLUS, 14, "PLUS"},
		{tokenize.MINUS, 15, "MINUS"},
		{tokenize.STAR, 16, "STAR"},
		{tokenize.SLASH, 17, "SLASH"},
		{tokenize.VBAR, 18, "VBAR"},
		{tokenize.AMPER, 19, "AMPER"},
		{tokenize.LESS, 20, "LESS"},
		{tokenize.GREATER, 21, "GREATER"},
		{tokenize.EQUAL, 22, "EQUAL"},
		{tokenize.DOT, 23, "DOT"},
		{tokenize.PERCENT, 24, "PERCENT"},
		{tokenize.LBRACE, 25, "LBRACE"},
		{tokenize.RBRACE, 26, "RBRACE"},
		{tokenize.EQEQUAL, 27, "EQEQUAL"},
		{tokenize.NOTEQUAL, 28, "NOTEQUAL"},
		{tokenize.LESSEQUAL, 29, "LESSEQUAL"},
		{tokenize.GREATEREQUAL, 30, "GREATEREQUAL"},
		{tokenize.TILDE, 31, "TILDE"},
		{tokenize.CIRCUMFLEX, 32, "CIRCUMFLEX"},
		{tokenize.LEFTSHIFT, 33, "LEFTSHIFT"},
		{tokenize.RIGHTSHIFT, 34, "RIGHTSHIFT"},
		{tokenize.DOUBLESTAR, 35, "DOUBLESTAR"},
		{tokenize.PLUSEQUAL, 36, "PLUSEQUAL"},
		{tokenize.MINEQUAL, 37, "MINEQUAL"},
		{tokenize.STAREQUAL, 38, "STAREQUAL"},
		{tokenize.SLASHEQUAL, 39, "SLASHEQUAL"},
		{tokenize.PERCENTEQUAL, 40, "PERCENTEQUAL"},
		{tokenize.AMPEREQUAL, 41, "AMPEREQUAL"},
		{tokenize.VBAREQUAL, 42, "VBAREQUAL"},
		{tokenize.CIRCUMFLEXEQUAL, 43, "CIRCUMFLEXEQUAL"},
		{tokenize.LEFTSHIFTEQUAL, 44, "LEFTSHIFTEQUAL"},
		{tokenize.RIGHTSHIFTEQUAL, 45, "RIGHTSHIFTEQUAL"},
		{tokenize.DOUBLESTAREQUAL, 46, "DOUBLESTAREQUAL"},
		{tokenize.DOUBLESLASH, 47, "DOUBLESLASH"},
		{tokenize.DOUBLESLASHEQUAL, 48, "DOUBLESLASHEQUAL"},
		{tokenize.AT, 49, "AT"},
		{tokenize.ATEQUAL, 50, "ATEQUAL"},
		{tokenize.RARROW, 51, "RARROW"},
		{tokenize.ELLIPSIS, 52, "ELLIPSIS"},
		{tokenize.COLONEQUAL, 53, "COLONEQUAL"},
		{tokenize.EXCLAMATION, 54, "EXCLAMATION"},
		{tokenize.OP, 55, "OP"},
		{tokenize.TYPE_IGNORE, 56, "TYPE_IGNORE"},
		{tokenize.TYPE_COMMENT, 57, "TYPE_COMMENT"},
		{tokenize.SOFT_KEYWORD, 58, "SOFT_KEYWORD"},
		{tokenize.FSTRING_START, 59, "FSTRING_START"},
		{tokenize.FSTRING_MIDDLE, 60, "FSTRING_MIDDLE"},
		{tokenize.FSTRING_END, 61, "FSTRING_END"},
		{tokenize.TSTRING_START, 62, "TSTRING_START"},
		{tokenize.TSTRING_MIDDLE, 63, "TSTRING_MIDDLE"},
		{tokenize.TSTRING_END, 64, "TSTRING_END"},
		{tokenize.COMMENT, 65, "COMMENT"},
		{tokenize.NL, 66, "NL"},
		{tokenize.ERRORTOKEN, 67, "ERRORTOKEN"},
		{tokenize.ENCODING, 68, "ENCODING"},
	}
	if int(tokenize.NTokens) != 69 {
		t.Fatalf("NTokens = %d, want 69", int(tokenize.NTokens))
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
