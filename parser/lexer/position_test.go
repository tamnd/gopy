package lexer

import (
	"fmt"
	"testing"

	"github.com/tamnd/gopy/token"
)

// posCase captures the canonical (kind, start, end) tuples a raw
// _tokenize.TokenizerIter(extra_tokens=False) call emits for src.
// The expected values were taken from CPython 3.14.5 via:
//
//	it = _tokenize.TokenizerIter(io.BytesIO(src.encode()).readline,
//	                             extra_tokens=False, encoding='utf-8')
//	for tup in it: print(tup)
//
// so the table doubles as a fixture for the wrapper layer.
type posCase struct {
	kind  token.Type
	sLine int
	sCol  int
	eLine int
	eCol  int
}

func (p posCase) String() string {
	return fmt.Sprintf("%s (%d,%d)-(%d,%d)", p.kind, p.sLine, p.sCol, p.eLine, p.eCol)
}

// TestRawTokenPositionsMatchCPython pins NEWLINE/INDENT/DEDENT/ENDMARKER
// positions against CPython's _tokenize.TokenizerIter raw output
// (extra_tokens=False). These are the positions before any wrapper-side
// reshaping (the +1 on NEWLINE end_col and the (lineno+1, 0) trailing
// reshape live in module/_tokenize, not here).
//
// CPython: Python/Python-tokenize.c:205 _get_col_offsets
func TestRawTokenPositionsMatchCPython(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []posCase
	}{
		{
			name: "simple_expr",
			src:  "1 + 1\n",
			want: []posCase{
				{token.NUMBER, 1, 0, 1, 1},
				{token.PLUS, 1, 2, 1, 3},
				{token.NUMBER, 1, 4, 1, 5},
				{token.NEWLINE, 1, 5, 1, 5},
				{token.ENDMARKER, 1, -1, 1, -1},
			},
		},
		{
			name: "def_pass",
			src:  "def f():\n    pass\n",
			want: []posCase{
				{token.NAME, 1, 0, 1, 3},
				{token.NAME, 1, 4, 1, 5},
				{token.LPAR, 1, 5, 1, 6},
				{token.RPAR, 1, 6, 1, 7},
				{token.COLON, 1, 7, 1, 8},
				{token.NEWLINE, 1, 8, 1, 8},
				{token.INDENT, 2, -1, 2, -1},
				{token.NAME, 2, 4, 2, 8},
				{token.NEWLINE, 2, 8, 2, 8},
				{token.DEDENT, 2, -1, 2, -1},
				{token.ENDMARKER, 2, -1, 2, -1},
			},
		},
		{
			name: "blank_line_between",
			src:  "a\n\nb\n",
			want: []posCase{
				{token.NAME, 1, 0, 1, 1},
				{token.NEWLINE, 1, 1, 1, 1},
				{token.NAME, 3, 0, 3, 1},
				{token.NEWLINE, 3, 1, 3, 1},
				{token.ENDMARKER, 3, -1, 3, -1},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks := tokenize_(t, tc.src)
			if len(toks) != len(tc.want) {
				t.Fatalf("token count = %d, want %d (got %v)", len(toks), len(tc.want), kinds(toks))
			}
			for i, w := range tc.want {
				got := posCase{toks[i].Kind, toks[i].Start.Line, toks[i].Start.Col, toks[i].End.Line, toks[i].End.Col}
				if got != w {
					t.Errorf("token %d: got %s, want %s", i, got, w)
				}
			}
		})
	}
}

// TestRawENDMARKERSetsDoneEOF pins that endmarker() flips s.done to
// eEOF on the first call. The wrapper layer (module/_tokenize) checks
// Done() == DoneEOF when deciding whether to reshape DEDENT-at-EOF as
// a trailing token; if the bit only flipped on the final ENDMARKER,
// the intermediate DEDENTs would slip past that check.
//
// CPython: tok->done = E_EOF in the underflow path, observed by
// Python/Python-tokenize.c:277.
func TestRawENDMARKERSetsDoneEOF(t *testing.T) {
	s := FromString("def f():\n    pass\n", ModeFile)
	for range 50 {
		tk := s.Get()
		if tk.Kind == token.DEDENT {
			if s.Done() != DoneEOF {
				t.Errorf("at DEDENT-at-EOF: Done() = %d, want DoneEOF (%d)", s.Done(), DoneEOF)
			}
		}
		if tk.Kind == token.ENDMARKER {
			if s.Done() != DoneEOF {
				t.Errorf("at ENDMARKER: Done() = %d, want DoneEOF (%d)", s.Done(), DoneEOF)
			}
			return
		}
	}
	t.Fatalf("ENDMARKER never reached")
}
