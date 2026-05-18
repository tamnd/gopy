package compile_test

import (
	"testing"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/parser"
)

func TestYieldFromBytecode(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"simple_yield_from", `
def outer():
    yield from inner()

def inner():
    yield 1
    yield 2
`},
		{"for_yield", `
def gen_with_for(it):
    for x in it:
        yield x
`},
		{"try_except_yield", `
def gen_with_try(it):
    try:
        for x in it:
            yield x
    except Exception as e:
        pass
`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, err := parser.ParseString(tc.src, "test.py", parser.ModeFile)
			if err != nil {
				t.Fatal(err)
			}
			code, err := compile.Compile(mod.(ast.Mod), "test.py", 0)
			if err != nil {
				t.Fatal(err)
			}
			printCode(t, code)
		})
	}
}

func printCode(t *testing.T, code *compile.Code) {
	t.Logf("Code: %s", code.Name)
	for i := 0; i < len(code.Code); i += 2 {
		op := compile.Opcode(code.Code[i])
		arg := code.Code[i+1]
		if op == compile.CACHE {
			continue
		}
		t.Logf("  %4d: %-30s %d", i, op.Name(), arg)
	}
	for _, nested := range code.Consts {
		if c, ok := nested.(*compile.Code); ok {
			t.Log("---")
			printCode(t, c)
		}
	}
}
