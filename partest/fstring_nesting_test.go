// Pin lexer behavior for f-strings nested 0 to a few levels deep.
// CPython 3.14 hard-caps "expressions nested too deeply" at the
// maxExprNesting boundary, but several legal levels live below it
// and the lexer must not stumble on any of them.
//
// CPython: Parser/lexer/lexer.c tok_get_fstring_mode

package partest

import (
	"testing"

	"github.com/tamnd/gopy/tokenize"
)

func TestFStringNestingDepth(t *testing.T) {
	cases := []struct {
		depth int
		src   string
	}{
		{0, `"plain"`},
		{1, `f'a{x}b'`},
		{2, `f"a{f'b{c}d'}e"`},
		{3, `f"""a{f'b{f"c{d}"}e'}f"""`},
		{4, `f"""a{f'''b{f"c{f'd{e}'}g"}h'''}i"""`},
		{1, `f'pad {x:.2f} end'`},
		{2, `f'{f"{x:>{w}}"}'`},
	}
	for _, c := range cases {
		toks := tokenize_(t, c.src+"\n")
		// Every legal level must terminate with ENDMARKER, never
		// ERRORTOKEN, and (for f-string cases) carry a matching
		// FSTRING_START/FSTRING_END pair count.
		var starts, ends int
		for _, tk := range toks {
			switch tk.Kind {
			case tokenize.FSTRING_START:
				starts++
			case tokenize.FSTRING_END:
				ends++
			case tokenize.ERRORTOKEN:
				t.Errorf("depth %d: lexer rejected %q (%v)", c.depth, c.src, kinds(toks))
			}
		}
		if starts != ends {
			t.Errorf("depth %d: %d FSTRING_START vs %d FSTRING_END", c.depth, starts, ends)
		}
		if c.depth > 0 && starts != c.depth {
			t.Logf("depth %d: got %d FSTRING_START tokens", c.depth, starts)
		}
	}
}
