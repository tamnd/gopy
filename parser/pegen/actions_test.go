package pegen

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestSeqOps(t *testing.T) {
	if got := SingletonSeq(7); len(got) != 1 || got[0] != 7 {
		t.Fatalf("SingletonSeq = %v", got)
	}
	if got := SeqInsertInFront(0, []int{1, 2, 3}); !equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("SeqInsertInFront = %v", got)
	}
	if got := SeqAppendToEnd([]int{1, 2}, 3); !equal(got, []int{1, 2, 3}) {
		t.Errorf("SeqAppendToEnd = %v", got)
	}
	if got := SeqFlatten([][]int{{1}, {2, 3}, nil, {4}}); !equal(got, []int{1, 2, 3, 4}) {
		t.Errorf("SeqFlatten = %v", got)
	}
	if SeqFirstItem([]int{}) != 0 || SeqFirstItem([]int{9, 1}) != 9 {
		t.Errorf("SeqFirstItem mismatch")
	}
	if SeqLastItem([]int{}) != 0 || SeqLastItem([]int{9, 1}) != 1 {
		t.Errorf("SeqLastItem mismatch")
	}
}

func TestJoinNamesWithDot(t *testing.T) {
	a := &ast.Name{Id: "a"}
	b := &ast.Name{Id: "b"}
	if got := JoinNamesWithDot(a, b); got != "a.b" {
		t.Errorf("got %q", got)
	}
	if got := JoinIDsWithDot([]string{"x", "y", "z"}); got != "x.y.z" {
		t.Errorf("got %q", got)
	}
}

func TestSetExprContextRecurses(t *testing.T) {
	p := &Parser{}
	a := &ast.Name{Id: "a"}
	b := &ast.Name{Id: "b"}
	tup := &ast.Tuple{Elts: []ast.Expr{a, &ast.Starred{Value: b}}}
	out := SetExprContext(p, tup, ast.Store).(*ast.Tuple)
	if out.Ctx != ast.Store {
		t.Errorf("tuple ctx = %v, want Store", out.Ctx)
	}
	got0 := out.Elts[0].(*ast.Name)
	if got0.Ctx != ast.Store {
		t.Errorf("name ctx = %v, want Store", got0.Ctx)
	}
	got1 := out.Elts[1].(*ast.Starred)
	if got1.Ctx != ast.Store {
		t.Errorf("starred ctx = %v, want Store", got1.Ctx)
	}
	if got1.Value.(*ast.Name).Ctx != ast.Store {
		t.Errorf("starred.value ctx = %v, want Store", got1.Value.(*ast.Name).Ctx)
	}
	if a.Ctx == ast.Store {
		t.Errorf("original Name should be untouched (CPython returns fresh nodes)")
	}
}

func TestGetExprName(t *testing.T) {
	cases := []struct {
		e    ast.Expr
		want string
	}{
		{&ast.Name{}, "name"},
		{&ast.Attribute{}, "attribute"},
		{&ast.Subscript{}, "subscript"},
		{&ast.Tuple{}, "tuple"},
		{&ast.List{}, "list"},
		{&ast.Lambda{}, "lambda"},
		{&ast.Call{}, "function call"},
		{&ast.GeneratorExp{}, "generator expression"},
		{&ast.ListComp{}, "list comprehension"},
		{&ast.SetComp{}, "set comprehension"},
		{&ast.DictComp{}, "dict comprehension"},
		{&ast.Dict{}, "dict literal"},
		{&ast.Set{}, "set display"},
		{&ast.Compare{}, "comparison"},
		{&ast.IfExp{}, "conditional expression"},
		{&ast.NamedExpr{}, "named expression"},
		{&ast.Constant{Value: nil}, "None"},
		{&ast.Constant{Value: true}, "True"},
		{&ast.Constant{Value: false}, "False"},
		{&ast.Constant{Value: "abc"}, "literal"},
		{&ast.JoinedStr{}, "f-string expression"},
		{&ast.TemplateStr{}, "t-string expression"},
	}
	for _, c := range cases {
		if got := GetExprName(c.e); got != c.want {
			t.Errorf("GetExprName(%T) = %q want %q", c.e, got, c.want)
		}
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
