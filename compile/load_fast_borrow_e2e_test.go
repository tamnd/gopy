package compile_test

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/parser"
)

// TestLoadFastBorrowEndToEnd locks in that the optimize_load_fast +
// insert_superinstructions passes actually fire on Python source that
// flows through the user-facing compile.Compile entry point. The
// per-pass unit tests in flowgraph_cfg_locals_test.go poke at the cfg
// builder directly; this gate confirms the full pipeline still emits
// the CPython 3.14 borrow / super-instruction opcodes after every
// preceding pass runs.
//
// CPython: Python/flowgraph.c:2776 optimize_load_fast
// CPython: Python/flowgraph.c:2588 insert_superinstructions
func TestLoadFastBorrowEndToEnd(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want compile.Opcode
	}{
		{
			name: "load_then_return_borrows",
			src: `
def f(x):
    return x
`,
			want: compile.LOAD_FAST_BORROW,
		},
		{
			name: "two_loads_consumed_by_binary_op",
			src: `
def f(a, b):
    return a + b
`,
			want: compile.LOAD_FAST_BORROW_LOAD_FAST_BORROW,
		},
		{
			// Tuple unpacking emits two adjacent STORE_FAST.
			// CPython 3.14 fuses them to STORE_FAST_STORE_FAST.
			name: "tuple_unpack_fuses_stores",
			src: `
def f(a, b):
    x, y = a, b
    return x + y
`,
			want: compile.STORE_FAST_STORE_FAST,
		},
		{
			// On a single line, STORE_FAST + LOAD_FAST fuse.
			// Across lines, CPython's make_super_instruction
			// bails out so no fusion happens, and gopy matches.
			name: "store_then_load_same_line_fuses",
			src: `
def f(a):
    x = a; return x
`,
			want: compile.STORE_FAST_LOAD_FAST,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, err := parser.ParseString(tc.src, "test.py", parser.ModeFile)
			if err != nil {
				t.Fatal(err)
			}
			code, err := compile.Compile(mod, "test.py", 0)
			if err != nil {
				t.Fatal(err)
			}
			if !codeUnitContainsOp(code, tc.want) {
				t.Fatalf("expected %s in compiled bytecode (including nested code objects); got opcodes: %v",
					tc.want.Name(), collectOps(code))
			}
		})
	}
}

func codeUnitContainsOp(code *compile.Code, want compile.Opcode) bool {
	for i := 0; i < len(code.Code); i += 2 {
		if compile.Opcode(code.Code[i]) == want {
			return true
		}
	}
	for _, c := range code.Consts {
		if nested, ok := c.(*compile.Code); ok {
			if codeUnitContainsOp(nested, want) {
				return true
			}
		}
	}
	return false
}

func collectOps(code *compile.Code) []string {
	seen := map[string]struct{}{}
	var walk func(c *compile.Code)
	walk = func(c *compile.Code) {
		for i := 0; i < len(c.Code); i += 2 {
			seen[compile.Opcode(c.Code[i]).Name()] = struct{}{}
		}
		for _, k := range c.Consts {
			if nc, ok := k.(*compile.Code); ok {
				walk(nc)
			}
		}
	}
	walk(code)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
