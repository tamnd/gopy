// Pin nullable + first-set analysis on a synthetic grammar covering
// every interesting case (optional, repeat, gather, lookahead, group)
// and on the live python.gram (first sets must contain the obvious
// terminal openers like 'def' for function_def).

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeSynthetic(t *testing.T) {
	src := []byte(`
a: 'x'
b: 'x'?
c: 'x' 'y'?
d: e 'z'
e: 'p' | 'q'
f: g
g: f
h: ('a' | 'b') 'c'
i: ','.NAME+
j: &'x' 'x'
k: !'x' NAME
`)
	g, err := ParseGrammar(src)
	if err != nil {
		t.Fatalf("ParseGrammar: %v", err)
	}
	sets := Analyze(g)

	byName := map[string]*Rule{}
	for _, r := range g.Rules {
		byName[r.Name] = r
	}

	// Build a small visitor to stamp nullability so we can assert.
	rulesMap := map[string]*Rule{}
	for _, r := range g.Rules {
		rulesMap[r.Name] = r
	}
	nv := &nullableVisitor{rules: rulesMap, visited: map[*Rule]bool{}, nullable: map[any]bool{}}
	for _, r := range g.Rules {
		nv.visitRule(r)
	}

	cases := []struct {
		name     string
		nullable bool
		mustHave []string
		mustMiss []string
	}{
		{"a", false, []string{"'x'"}, nil},
		{"b", true, []string{"'x'"}, nil},
		{"c", false, []string{"'x'"}, []string{"'y'"}},
		{"d", false, []string{"'p'", "'q'"}, []string{"'z'"}},
		{"e", false, []string{"'p'", "'q'"}, nil},
		{"h", false, []string{"'a'", "'b'"}, []string{"'c'"}},
		{"i", false, []string{"NAME"}, nil},
		{"j", false, []string{"'x'"}, nil},
		{"k", false, []string{"NAME"}, []string{"'x'"}},
	}
	for _, tc := range cases {
		r := byName[tc.name]
		if r == nil {
			t.Errorf("rule %q missing", tc.name)
			continue
		}
		if got := nv.nullable[r]; got != tc.nullable {
			t.Errorf("rule %q nullable = %v, want %v", tc.name, got, tc.nullable)
		}
		s := sets[tc.name]
		for _, k := range tc.mustHave {
			if !s[k] {
				t.Errorf("rule %q first set missing %q (got %v)", tc.name, k, keys(s))
			}
		}
		for _, k := range tc.mustMiss {
			if s[k] {
				t.Errorf("rule %q first set unexpectedly contains %q (got %v)", tc.name, k, keys(s))
			}
		}
	}
}

func TestAnalyzePythonGram(t *testing.T) {
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
	sets := Analyze(g)

	// function_def starts with 'def' (or '@' decorators / 'async').
	fd := sets["function_def"]
	if !fd["'def'"] && !fd["'@'"] && !fd["'async'"] {
		t.Errorf("function_def first set lacks expected openers, got %v", keys(fd))
	}
	// if_stmt starts with 'if'.
	if !sets["if_stmt"]["'if'"] {
		t.Errorf("if_stmt first set missing 'if': %v", keys(sets["if_stmt"]))
	}
	// class_def_raw starts with 'class'.
	if !sets["class_def_raw"]["'class'"] {
		t.Errorf("class_def_raw first set missing 'class': %v", keys(sets["class_def_raw"]))
	}
	// file is non-nullable in python.gram (must reach ENDMARKER).
	rulesMap := map[string]*Rule{}
	for _, r := range g.Rules {
		rulesMap[r.Name] = r
	}
	nv := &nullableVisitor{rules: rulesMap, visited: map[*Rule]bool{}, nullable: map[any]bool{}}
	for _, r := range g.Rules {
		nv.visitRule(r)
	}
	if nv.nullable[rulesMap["file"]] {
		t.Errorf("file should not be nullable")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
