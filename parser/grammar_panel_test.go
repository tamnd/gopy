// Grammar panel: parse the full Lib/test/test_grammar.py end to
// end. This is the v0.10.2 gate. The file exercises every shape
// CPython's grammar admits (decorators, classes, comprehensions,
// match, walrus, type aliases, fstrings, async, keyword-only args,
// big-int literals, etc) so a clean parse is a strong signal that
// the lexer + pegen + generated table agree with CPython on the
// surface syntax.
//
// CPython: Lib/test/test_grammar.py

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTestGrammar(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		cpython = filepath.Join(os.Getenv("HOME"), "github", "python", "cpython")
	}
	path := filepath.Join(cpython, "Lib", "test", "test_grammar.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("test_grammar.py unavailable: %v", err)
	}
	if _, err := ParseString(string(src), path, ModeFile); err != nil {
		t.Fatalf("ParseString(test_grammar.py): %v", err)
	}
}
