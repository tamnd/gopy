// Smoke tests for the per-rule emitter. Pin shapes that downstream
// expects: every rule produces a parseRule_<name> function, key
// constants exist, and the artificial _loop / _gather / _group
// helpers are deduplicated by signature so the file stays
// byte-identical across runs.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitShape(t *testing.T) {
	src := []byte(`
file: a=statements ENDMARKER { _PyAST_Module(a, EXTRA) }
statements: a=statement+ { a }
statement: 'pass' NEWLINE
foo: 'a' (b='b' | c='c') 'd'? items=','.NAME+ &'x' !'y' ~ 'z'
b: 'b'
c: 'c'
expr: expr '+' term | term
term: NAME
`)
	g, err := ParseGrammar(src)
	if err != nil {
		t.Fatalf("ParseGrammar: %v", err)
	}
	out := Emit(g)

	wantSubstrs := []string{
		"package pegen",
		"Rule_file = 0",
		"func parseRule_file(p *Parser) any",
		"func parseRule_statements(p *Parser) any",
		"func parseRule_foo(p *Parser) any",
		"GeneratedRuleNames",
		"HardKeywords",
		"SoftKeywords",
		"placeholderMatched",
		"func Dispatch(p *Parser, m StartRule) (any, error)",
		"ErrParserNotImplemented",
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(out, s) {
			t.Errorf("emitted source missing %q", s)
		}
	}

	// Left-recursive rule must emit a stub, not a body that recurses.
	exprStart := strings.Index(out, "// parseRule_expr parses expr.")
	if exprStart < 0 {
		t.Fatalf("parseRule_expr header missing")
	}
	exprBody := out[exprStart : exprStart+300]
	if !strings.Contains(exprBody, "TODO(M4)") {
		t.Errorf("expr (left-recursive) should emit M4 stub, got %q", exprBody)
	}

	// Determinism: emitting twice must produce byte-identical output.
	g2, _ := ParseGrammar(src)
	if Emit(g2) != out {
		t.Errorf("Emit not deterministic across runs")
	}
}

func TestEmitPythonGramRoundtrip(t *testing.T) {
	src := loadPythonGram(t)
	g, err := ParseGrammar(src)
	if err != nil {
		t.Fatalf("ParseGrammar: %v", err)
	}
	out := Emit(g)
	if !strings.Contains(out, "func parseRule_file(") {
		t.Errorf("python.gram emit missing parseRule_file")
	}
	if !strings.Contains(out, "func parseRule_function_def(") {
		t.Errorf("python.gram emit missing parseRule_function_def")
	}
	// Verify left-recursion was detected on at least one rule.
	if !strings.Contains(out, "TODO(M4)") {
		t.Errorf("expected at least one TODO(M4) stub for a left-recursive rule")
	}
}

func loadPythonGram(t *testing.T) []byte {
	t.Helper()
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		cpython = filepath.Join(os.Getenv("HOME"), "github", "python", "cpython")
	}
	path := filepath.Join(cpython, "Grammar", "python.gram")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("python.gram unavailable: %v", err)
	}
	return data
}
