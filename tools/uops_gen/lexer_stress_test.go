// Stress test: lex the real upstream sources the cases generator
// consumes (Python/bytecodes.c and Python/optimizer_bytecodes.c) and
// confirm Tokenize never errors. The token count is also checked
// against the live Python lexer to catch silent drift; if upstream
// adds tokens, regenerate the count by re-running:
//
//   python3 -c "import sys; sys.path.insert(0, '/Users/apple/cpython-314/Tools/cases_generator'); \
//     import lexer; src=open('/Users/apple/cpython-314/Python/bytecodes.c').read(); \
//     print(sum(1 for _ in lexer.tokenize(src)))"
//
// CPython: Python/bytecodes.c (the actual DSL source the generator
// pipeline parses)

package main

import (
	"os"
	"testing"
)

func TestLex_RealBytecodesC(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/bytecodes.c"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("upstream source not available at %s: %v", path, err)
	}
	tokens, err := Tokenize(string(data), path, 1)
	if err != nil {
		t.Fatalf("Tokenize bytecodes.c: %v", err)
	}
	const wantTokens = 31570 // captured from Python lexer.py
	if len(tokens) != wantTokens {
		t.Errorf("got %d tokens, want %d (Python parity)", len(tokens), wantTokens)
	}
}

func TestLex_RealOptimizerBytecodesC(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/optimizer_bytecodes.c"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("upstream source not available at %s: %v", path, err)
	}
	tokens, err := Tokenize(string(data), path, 1)
	if err != nil {
		t.Fatalf("Tokenize optimizer_bytecodes.c: %v", err)
	}
	const wantTokens = 6438 // captured from Python lexer.py
	if len(tokens) != wantTokens {
		t.Errorf("got %d tokens, want %d (Python parity)", len(tokens), wantTokens)
	}
}
