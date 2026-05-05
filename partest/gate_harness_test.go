// Parse-then-compile gate harness. Until parser_gen lands the
// generated rule table, parser.ParseString returns
// ErrParserNotImplemented; this test skips in that mode so it
// stays green on CI but flips the moment the parser ships.
//
// CPython: Lib/test/test_compile.py compile() round-trip cases

package partest

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/parser"
)

func TestParseGateRoundtrip(t *testing.T) {
	cases := []string{
		"x = 1\n",
		"def f(a, b=1, *c, d, **e):\n    return a\n",
		"if True:\n    pass\nelse:\n    pass\n",
		"[i for i in range(10) if i % 2]\n",
		"async def g():\n    await x\n",
	}
	for _, src := range cases {
		_, err := parser.ParseString(src, "<gate>", parser.ModeFile)
		if errors.Is(err, parser.ErrParserNotImplemented) {
			t.Skipf("parser table offline; gate harness will activate once parser_gen lands")
			return
		}
		if err != nil {
			t.Errorf("Parse(%q) err = %v", src, err)
		}
	}
}
