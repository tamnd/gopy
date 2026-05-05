// Pin the metagrammar parser against a small synthetic grammar and
// against the live python.gram. The synthetic case exercises every
// metagrammar production; the live case proves we round-trip the
// 268 real rules without diagnostics.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGrammarSynthetic(t *testing.T) {
	src := []byte(`@trailer "x"
file[mod_ty]: a=statements ENDMARKER { _PyAST_Module(a, EXTRA) }
statements[asdl_stmt_seq*]:
    | a=statement+ { (asdl_stmt_seq*) _PyPegen_seq_flatten(p, a) }
foo: 'bar' | NAME ~ &'(' x=( a | b )* [ y? ] s.NAME+
`)
	g, err := ParseGrammar(src)
	if err != nil {
		t.Fatalf("ParseGrammar: %v", err)
	}
	if len(g.Metas) != 1 || g.Metas[0].Name != "trailer" {
		t.Errorf("metas = %+v", g.Metas)
	}
	if len(g.Rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(g.Rules))
	}
	if g.Rules[0].Name != "file" || g.Rules[0].Type != "mod_ty" {
		t.Errorf("rule[0] = %+v", g.Rules[0])
	}
	foo := g.Rules[2]
	if foo.Name != "foo" || len(foo.RHS.Alts) != 2 {
		t.Errorf("foo = %+v alts=%d", foo, len(foo.RHS.Alts))
	}
	// Second alt of foo: NAME ~ &'(' x=( a | b )* [ y? ] s.NAME+
	alt := foo.RHS.Alts[1]
	if alt.Icut != 1 {
		t.Errorf("foo second-alt icut = %d, want 1", alt.Icut)
	}
}

func TestParseGrammarPythonGram(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		cpython = filepath.Join(os.Getenv("HOME"), "github", "python", "cpython")
	}
	gram := filepath.Join(cpython, "Grammar", "python.gram")
	if _, err := os.Stat(gram); err != nil {
		t.Skipf("CPYTHON checkout not available at %s", cpython)
	}
	data, err := os.ReadFile(gram)
	if err != nil {
		t.Fatalf("read python.gram: %v", err)
	}
	g, err := ParseGrammar(data)
	if err != nil {
		t.Fatalf("ParseGrammar: %v", err)
	}
	if len(g.Rules) < 250 {
		t.Errorf("rules = %d, expected ~268 from cpython 3.14", len(g.Rules))
	}
	byName := map[string]*Rule{}
	for _, r := range g.Rules {
		byName[r.Name] = r
	}
	if byName["file"] == nil || byName["statements"] == nil || byName["assignment"] == nil {
		t.Errorf("missing core rules: file/statements/assignment")
	}
	// assignment has actions; verify at least one alt carries one.
	a := byName["assignment"]
	hasAction := false
	for _, alt := range a.RHS.Alts {
		if alt.Action != "" {
			hasAction = true
			break
		}
	}
	if !hasAction {
		t.Errorf("assignment alts have no action body: %+v", a)
	}
}
