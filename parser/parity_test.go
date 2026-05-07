// Differential parser parity. The single source of truth is CPython
// 3.14: parse the same source with python3 and with gopy, dump both
// to canonical strings via ast.dump, and require byte-equality. Any
// divergence is a port bug.
//
// CPython: Lib/ast.py:109 ast.dump

package parser

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// parityFixtures is the seed corpus. Every fixture is a Python source
// snippet whose ast.dump output we expect gopy to match byte-for-byte.
// Keep entries small so a divergence reads as a clear regression.
var parityFixtures = []struct {
	name string
	src  string
}{
	{"empty_module", ""},
	{"docstring_only", `"""hello"""` + "\n"},
	{"simple_assign", "x = 1\n"},
	{"two_assigns", "x = 1\ny = 2\n"},
	{"binary_add", "x + y\n"},
	{"chained_compare", "a < b < c\n"},
	{"augassign", "x += 1\n"},
	{"annassign", "x: int = 1\n"},
	{"if_else", "if x:\n    a\nelse:\n    b\n"},
	{"while_loop", "while x:\n    pass\n"},
	{"for_loop", "for x in y:\n    pass\n"},
	{"def_simple", "def f():\n    pass\n"},
	{"def_args", "def f(x, y=1, *a, **kw):\n    return x\n"},
	{"class_simple", "class C:\n    pass\n"},
	{"class_paren", "class C():\n    pass\n"},
	{"class_base", "class C(B):\n    pass\n"},
	{"class_meta", "class C(B, metaclass=M):\n    pass\n"},
	{"decorator", "@dec\ndef f():\n    pass\n"},
	{"import", "import os\n"},
	{"import_as", "import os as o\n"},
	{"from_import", "from os import path\n"},
	{"from_dot_import", "from . import x\n"},
	{"return_value", "def f():\n    return 1\n"},
	{"yield", "def f():\n    yield 1\n"},
	{"raise", "raise ValueError('x')\n"},
	{"raise_from", "raise ValueError('x') from None\n"},
	{"try_except", "try:\n    a\nexcept Exception as e:\n    b\n"},
	{"try_finally", "try:\n    a\nfinally:\n    b\n"},
	{"with", "with open('x') as f:\n    pass\n"},
	{"lambda", "f = lambda x: x\n"},
	{"list_comp", "[x for x in y]\n"},
	{"dict_comp", "{x: y for x in z}\n"},
	{"set_comp", "{x for x in y}\n"},
	{"gen_exp", "(x for x in y)\n"},
	{"slice", "x[1:2:3]\n"},
	{"unpacking", "a, b = 1, 2\n"},
	{"star_unpack", "a, *b = 1, 2, 3\n"},
	{"global", "def f():\n    global x\n"},
	{"nonlocal", "def f():\n    nonlocal x\n"},
	{"assert", "assert x, 'msg'\n"},
	{"fstring", "x = f'{y}'\n"},
	{"fstring_repr", "x = f'{y!r}'\n"},
	{"fstring_fmt", "x = f'{y:>10}'\n"},
	{"match", "match x:\n    case 1:\n        pass\n"},
	{"type_alias", "type X = int\n"},
	{"const_int", "1\n"},
	{"const_float", "1.5\n"},
	{"const_str", "'hi'\n"},
	{"const_bytes", "b'hi'\n"},
	{"const_true", "True\n"},
	{"const_none", "None\n"},
}

func TestParserParity(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	for _, fx := range parityFixtures {
		t.Run(fx.name, func(t *testing.T) {
			want, err := pythonAstDump(fx.src)
			if err != nil {
				t.Fatalf("python3 ast.dump: %v", err)
			}
			mod, err := ParseString(fx.src, fx.name+".py", ModeFile)
			if err != nil {
				t.Fatalf("gopy parse: %v", err)
			}
			got := ast.Dump(mod)
			if got != want {
				t.Errorf("ast.dump mismatch\n want: %s\n got:  %s", want, got)
			}
		})
	}
}

// pythonAstDump runs `python3 -c 'import ast; print(ast.dump(ast.parse(SRC)))'`
// on the raw source and returns the printed string with the trailing
// newline trimmed.
func pythonAstDump(src string) (string, error) {
	cmd := exec.Command("python3", "-c", `
import ast, sys
src = sys.stdin.read()
print(ast.dump(ast.parse(src)))
`)
	cmd.Stdin = strings.NewReader(src)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", errors.New(strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
