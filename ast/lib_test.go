// Smoke tests for the ast helpers ported from Lib/ast.py: IterFields,
// IterChildNodes, Walk, Equal, GetDocstring, FixMissingLocations,
// IncrementLineno, LiteralEval, NodeVisitor, NodeTransformer. Each
// test exercises the documented surface against a hand-built AST so
// the package builds without the parser dependency.

package ast

import (
	"testing"
)

func TestIterFieldsSkipsPos(t *testing.T) {
	n := &Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 1, ColOffset: 0}}
	got := IterFields(n)
	if len(got) != 2 {
		t.Fatalf("IterFields(*Name) = %d fields, want 2 (id, ctx)", len(got))
	}
	if got[0].Name != "id" || got[0].Value.(string) != "x" {
		t.Errorf("first field = %+v, want {id, x}", got[0])
	}
	if got[1].Name != "ctx" {
		t.Errorf("second field name = %q, want ctx", got[1].Name)
	}
}

func TestIterChildNodesDescendsLists(t *testing.T) {
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{Value: &Name{Id: "a", Ctx: Load}},
		&ExprStmt{Value: &Name{Id: "b", Ctx: Load}},
	}}
	children := IterChildNodes(mod)
	if len(children) != 2 {
		t.Fatalf("IterChildNodes = %d children, want 2", len(children))
	}
	if _, ok := children[0].(*ExprStmt); !ok {
		t.Errorf("child[0] = %T, want *ExprStmt", children[0])
	}
}

func TestWalkBFS(t *testing.T) {
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{Value: &Name{Id: "a", Ctx: Load}},
	}}
	all := Walk(mod)
	if len(all) != 3 {
		t.Fatalf("Walk = %d nodes, want 3 (Module, ExprStmt, Name)", len(all))
	}
}

func TestEqualIgnoresPositions(t *testing.T) {
	a := &Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 1}}
	b := &Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 99}}
	if !Equal(a, b, false) {
		t.Errorf("Equal(_, _, compareAttributes=false) = false, want true")
	}
	if Equal(a, b, true) {
		t.Errorf("Equal(_, _, compareAttributes=true) = true, want false")
	}
}

func TestGetDocstringPlain(t *testing.T) {
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{Value: &Constant{Value: "hello world"}},
	}}
	got, ok := GetDocstring(mod, true)
	if !ok || got != "hello world" {
		t.Errorf("GetDocstring = (%q, %v), want (\"hello world\", true)", got, ok)
	}
}

func TestGetDocstringMissing(t *testing.T) {
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{Value: &Name{Id: "x", Ctx: Load}},
	}}
	if _, ok := GetDocstring(mod, true); ok {
		t.Errorf("GetDocstring on non-string Expr = ok, want !ok")
	}
}

func TestCleandocStripsCommonIndent(t *testing.T) {
	in := "first\n    second\n    third\n"
	want := "first\nsecond\nthird"
	if got := cleandoc(in); got != want {
		t.Errorf("cleandoc = %q, want %q", got, want)
	}
}

func TestFixMissingLocationsPropagates(t *testing.T) {
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{
			Value: &Name{Id: "x", Ctx: Load},
			Pos:   Pos{Lineno: 5, ColOffset: 2, EndLineno: 5, EndColOffset: 3},
		},
	}}
	FixMissingLocations(mod)
	name := mod.Body[0].(*ExprStmt).Value.(*Name)
	if name.Pos.Lineno != 5 {
		t.Errorf("child Lineno = %d, want 5 (inherited)", name.Pos.Lineno)
	}
}

func TestIncrementLinenoShifts(t *testing.T) {
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{
			Value: &Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 3, EndLineno: 3}},
			Pos:   Pos{Lineno: 3, EndLineno: 3},
		},
	}}
	IncrementLineno(mod, 10)
	stmt := mod.Body[0].(*ExprStmt)
	if stmt.Pos.Lineno != 13 {
		t.Errorf("ExprStmt Lineno = %d, want 13", stmt.Pos.Lineno)
	}
	name := stmt.Value.(*Name)
	if name.Pos.Lineno != 13 {
		t.Errorf("Name Lineno = %d, want 13", name.Pos.Lineno)
	}
}

func TestLiteralEvalConstant(t *testing.T) {
	v, err := LiteralEval(&Constant{Value: 42})
	if err != nil || v.(int) != 42 {
		t.Errorf("LiteralEval(int 42) = (%v, %v), want (42, nil)", v, err)
	}
}

func TestLiteralEvalTuple(t *testing.T) {
	tup := &Tuple{Elts: Seq[Expr]{&Constant{Value: 1}, &Constant{Value: 2}}}
	v, err := LiteralEval(tup)
	if err != nil {
		t.Fatalf("LiteralEval(tuple): %v", err)
	}
	out, ok := v.([]any)
	if !ok || len(out) != 2 || out[0].(int) != 1 || out[1].(int) != 2 {
		t.Errorf("LiteralEval(tuple) = %v, want [1, 2]", v)
	}
}

func TestNodeVisitorDispatch(t *testing.T) {
	got := []string{}
	v := &NodeVisitor{Handlers: map[string]VisitFunc{
		"Name": func(node any) any {
			got = append(got, node.(*Name).Id)
			return nil
		},
	}}
	mod := &Module{Body: Seq[Stmt]{
		&ExprStmt{Value: &Name{Id: "x", Ctx: Load}},
		&ExprStmt{Value: &Name{Id: "y", Ctx: Load}},
	}}
	v.Visit(mod)
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("visited names = %v, want [x y]", got)
	}
}

func TestNodeTransformerReplaces(t *testing.T) {
	tx := &NodeTransformer{Handlers: map[string]VisitFunc{
		"Name": func(node any) any {
			n := node.(*Name)
			return &Name{Id: n.Id + "!", Ctx: n.Ctx}
		},
	}}
	es := &ExprStmt{Value: &Name{Id: "x", Ctx: Load}}
	tx.Visit(es)
	if es.Value.(*Name).Id != "x!" {
		t.Errorf("transformer did not replace Name; got %q, want x!", es.Value.(*Name).Id)
	}
}
