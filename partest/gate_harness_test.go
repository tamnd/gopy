// Parse-then-compile gate harness. The generator landed in v0.5.5
// but the action-helper layer that builds typed AST nodes is still
// being filled in: the cases that already hit the typed path
// (currently the empty-source case) round-trip through compile;
// the cases that fall back to ErrParserNotImplemented are noted
// and skipped, so the harness flips one case at a time as the
// action surface grows.
//
// CPython: Lib/test/test_compile.py compile() round-trip cases

package partest

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/parser"
)

func TestParseGateRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"empty", ""},
		{"assign", "x = 1\n"},
		{"def", "def f(a, b=1, *c, d, **e):\n    return a\n"},
		{"if_else", "if True:\n    pass\nelse:\n    pass\n"},
		{"comprehension", "[i for i in range(10) if i % 2]\n"},
		{"async_def", "async def g():\n    await x\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, err := parser.ParseString(tc.src, "<gate>", parser.ModeFile)
			if errors.Is(err, parser.ErrParserNotImplemented) {
				t.Skip("action surface not yet typed for this shape")
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if _, ok := mod.(*ast.Module); !ok {
				t.Fatalf("Parse returned %T, want *ast.Module", mod)
			}
			if _, err := compile.Compile(mod, "<gate>", 0); err != nil {
				t.Fatalf("Compile: %v", err)
			}
		})
	}
}
