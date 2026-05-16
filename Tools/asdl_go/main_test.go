package main

import (
	"strings"
	"testing"
)

func TestParseSmall(t *testing.T) {
	src := `
-- comment
module Tiny
{
    mod = Module(stmt* body)
    stmt = Pass | Expr(expr value)
           attributes (int lineno, int col_offset, int end_lineno, int end_col_offset)
    expr = Constant(constant value, string? kind)
           attributes (int lineno, int col_offset, int end_lineno, int end_col_offset)
    op = Add | Sub
    arg = (identifier name, expr? annotation)
}
`
	mod, err := parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if mod.Name != "Tiny" {
		t.Fatalf("module name = %q, want Tiny", mod.Name)
	}
	if got := len(mod.Defs); got != 5 {
		t.Fatalf("def count = %d, want 5", got)
	}
}

func TestEmitShape(t *testing.T) {
	src := `
module Tiny
{
    mod = Module(stmt* body)
    stmt = Pass | Expr(expr value)
           attributes (int lineno, int col_offset, int end_lineno, int end_col_offset)
    expr = Constant(constant value)
           attributes (int lineno, int col_offset, int end_lineno, int end_col_offset)
    op = Add | Sub
    arg = (identifier name)
}
`
	mod, err := parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := emit(mod)
	if err != nil {
		t.Fatalf("emit: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"package ast",
		"type Mod interface",
		"type Module struct",
		"func (*Module) isMod()",
		"type Stmt interface",
		"type Pass struct",
		"func (n *Pass) Position() Pos",
		"type Op int",
		"Add Op = iota + 1",
		"type Arg struct",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestParseFieldQuantifiers(t *testing.T) {
	src := `module T { x = (expr a, expr? b, expr* c, expr?* d) }`
	mod, err := parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d := mod.Defs[0]
	if d.Product[0].Quan != 0 || d.Product[1].Quan != '?' ||
		d.Product[2].Quan != '*' || d.Product[3].Quan != 'O' {
		t.Fatalf("quantifiers = %v %v %v %v",
			d.Product[0].Quan, d.Product[1].Quan, d.Product[2].Quan, d.Product[3].Quan)
	}
}

func TestParseRealAsdl(t *testing.T) {
	// Smoke-test the real CPython 3.14 Python.asdl via the input
	// piped in by the user's environment. Skipped if the file is not
	// reachable so the test is hermetic for clean checkouts.
	const path = "../../../../python/cpython/Parser/Python.asdl"
	src, err := readIfExists(path)
	if err != nil {
		t.Skip(err)
	}
	mod, err := parse(string(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := emit(mod); err != nil {
		t.Fatalf("emit: %v", err)
	}
}
