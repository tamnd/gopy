package future

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func mkImport(names []string, level int, module string) *ast.ImportFrom {
	aliases := ast.NewSeq[*ast.Alias](len(names))
	for i, n := range names {
		aliases.Set(i, &ast.Alias{Name: n, Pos: ast.Pos{Lineno: 1}})
	}
	return &ast.ImportFrom{
		Module: module,
		Names:  aliases,
		Level:  level,
		Pos:    ast.Pos{Lineno: 1},
	}
}

func mkModule(stmts ...ast.Stmt) *ast.Module {
	body := ast.NewSeq[ast.Stmt](len(stmts))
	for i, s := range stmts {
		body.Set(i, s)
	}
	return &ast.Module{Body: body}
}

func TestFromAST_Annotations(t *testing.T) {
	m := mkModule(mkImport([]string{"annotations"}, 0, "__future__"))
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != Annotations {
		t.Fatalf("Bits = %#x, want %#x", ff.Bits, Annotations)
	}
}

func TestFromAST_BarryAsBdfl(t *testing.T) {
	m := mkModule(mkImport([]string{"barry_as_FLUFL"}, 0, "__future__"))
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != BarryAsBdfl {
		t.Fatalf("Bits = %#x, want %#x", ff.Bits, BarryAsBdfl)
	}
}

func TestFromAST_NoOpFeatures(t *testing.T) {
	// "division" etc. are recognized but do not flip a bit (they're
	// always on in Python 3).
	m := mkModule(mkImport([]string{"division", "print_function", "with_statement"}, 0, "__future__"))
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != 0 {
		t.Fatalf("Bits = %#x, want 0", ff.Bits)
	}
}

func TestFromAST_BracesRejected(t *testing.T) {
	m := mkModule(mkImport([]string{"braces"}, 0, "__future__"))
	_, err := FromAST(m, "<t>")
	if err == nil {
		t.Fatal("expected SyntaxError")
	}
	var se *SyntaxError
	if !errors.As(err, &se) || se.Msg != "not a chance" {
		t.Fatalf("got %v, want SyntaxError 'not a chance'", err)
	}
}

func TestFromAST_UnknownFeature(t *testing.T) {
	m := mkModule(mkImport([]string{"warp_drive"}, 0, "__future__"))
	_, err := FromAST(m, "<t>")
	if err == nil {
		t.Fatal("expected SyntaxError")
	}
	if !strings.Contains(err.Error(), "warp_drive is not defined") {
		t.Fatalf("got %q", err.Error())
	}
}

func TestFromAST_DocStringSkipped(t *testing.T) {
	doc := &ast.ExprStmt{Value: &ast.Constant{Value: "module doc", Pos: ast.Pos{Lineno: 1}}, Pos: ast.Pos{Lineno: 1}}
	m := mkModule(doc, mkImport([]string{"annotations"}, 0, "__future__"))
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != Annotations {
		t.Fatalf("Bits = %#x, want %#x", ff.Bits, Annotations)
	}
}

func TestFromAST_StopsAtFirstNonFuture(t *testing.T) {
	// from os import path  -- not __future__, parsing stops there.
	other := mkImport([]string{"path"}, 0, "os")
	later := mkImport([]string{"annotations"}, 0, "__future__")
	m := mkModule(other, later)
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != 0 {
		t.Fatalf("Bits = %#x, want 0 (later __future__ ignored)", ff.Bits)
	}
}

func TestFromAST_RelativeFutureIgnored(t *testing.T) {
	// `from . import __future__` has level=1; not the future statement.
	m := mkModule(mkImport([]string{"annotations"}, 1, "__future__"))
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != 0 {
		t.Fatalf("Bits = %#x, want 0", ff.Bits)
	}
}

func TestFromAST_LocationTracksLast(t *testing.T) {
	first := mkImport([]string{"annotations"}, 0, "__future__")
	first.Pos = ast.Pos{Lineno: 5}
	second := mkImport([]string{"barry_as_FLUFL"}, 0, "__future__")
	second.Pos = ast.Pos{Lineno: 10}
	m := mkModule(first, second)
	ff, err := FromAST(m, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Location.Lineno != 10 {
		t.Fatalf("Location.Lineno = %d, want 10", ff.Location.Lineno)
	}
	if ff.Bits != Annotations|BarryAsBdfl {
		t.Fatalf("Bits = %#x", ff.Bits)
	}
}

func TestFromAST_Expression(t *testing.T) {
	// Expression mode: no body to walk.
	ff, err := FromAST(&ast.Expression{Body: &ast.Constant{Value: int64(1)}}, "<t>")
	if err != nil {
		t.Fatal(err)
	}
	if ff.Bits != 0 || ff.Location != ast.NoPos {
		t.Fatalf("got %+v", ff)
	}
}
