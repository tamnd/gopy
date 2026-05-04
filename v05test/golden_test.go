package v05test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
)

//go:generate go test -update -run TestGolden ./...

var update = flag.Bool("update", false,
	"rewrite v05test/testdata/golden/*.golden files in place")

type goldenCase struct {
	name string
	mod  *ast.Module
}

// goldenPanel is the curated panel pinned to disassembly text. Each
// entry maps to one .golden file under testdata/golden. The shape of
// each fixture mirrors the table in 1629_gopy_compile_goldens.md.
func goldenPanel() []goldenCase {
	return []goldenCase{
		{
			name: "empty_module",
			mod:  module(),
		},
		{
			name: "simple_assign",
			mod: module(&ast.Assign{
				Targets: []ast.Expr{nameStore("x")},
				Value:   cnst(int64(1)),
			}),
		},
		{
			name: "binary_add",
			mod: module(&ast.Assign{
				Targets: []ast.Expr{nameStore("a")},
				Value: &ast.BinOp{
					Left:  cnst(int64(1)),
					Op:    ast.Add,
					Right: cnst(int64(2)),
				},
			}),
		},
		{
			name: "load_after_store",
			mod: module(
				&ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))},
				&ast.ExprStmt{Value: nameLoad("x")},
			),
		},
		{
			name: "if_pass",
			mod: module(&ast.If{
				Test: nameLoad("x"),
				Body: []ast.Stmt{&ast.Pass{}},
			}),
		},
		{
			name: "while_pass",
			mod: module(&ast.While{
				Test: nameLoad("x"),
				Body: []ast.Stmt{&ast.Pass{}},
			}),
		},
		{
			name: "def_add_one",
			mod: module(&ast.FunctionDef{
				Name: "f",
				Args: &ast.Arguments{Args: []*ast.Arg{{Arg: "x"}}},
				Body: []ast.Stmt{
					&ast.Return{Value: &ast.BinOp{
						Left:  nameLoad("x"),
						Op:    ast.Add,
						Right: cnst(int64(1)),
					}},
				},
			}),
		},
		{
			name: "async_def_pass",
			mod: module(&ast.AsyncFunctionDef{
				Name: "f",
				Args: &ast.Arguments{},
				Body: []ast.Stmt{&ast.Pass{}},
			}),
		},
		{
			name: "class_pass",
			mod: module(&ast.ClassDef{
				Name: "C",
				Body: []ast.Stmt{&ast.Pass{}},
			}),
		},
		{
			name: "type_alias",
			mod: module(&ast.TypeAlias{
				Name:  nameStore("X"),
				Value: nameLoad("int"),
			}),
		},
	}
}

// TestGolden compiles each fixture and compares the disassembly text
// against the checked-in .golden file. With -update, each .golden is
// rewritten in place. CI never passes -update.
func TestGolden(t *testing.T) {
	for _, tc := range goldenPanel() {
		t.Run(tc.name, func(t *testing.T) {
			co, err := compile.Compile(tc.mod, "<gate>", 0)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got := compile.Disassemble(co)
			path := filepath.Join("testdata", "golden", tc.name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run with -update to create)", path, err)
			}
			if got != string(want) {
				t.Fatalf("disassembly diff for %s:\n--- want\n%s\n+++ got\n%s",
					tc.name, want, got)
			}
		})
	}
}
