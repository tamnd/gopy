package pegen

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestEmptyArguments(t *testing.T) {
	a := EmptyArguments()
	if a == nil {
		t.Fatal("EmptyArguments returned nil")
	}
	if len(a.Args) != 0 || len(a.Posonlyargs) != 0 || a.Vararg != nil || a.Kwarg != nil {
		t.Errorf("expected zero-value Arguments, got %+v", a)
	}
}

func TestMakeArgumentsPlainPlusStar(t *testing.T) {
	a := MakeArguments(
		nil, nil,
		[]*ast.Arg{{Arg: "x"}, {Arg: "y"}},
		[]*NameDefaultPair{{Arg: &ast.Arg{Arg: "z"}, Value: &ast.Constant{Value: 1}}},
		&StarEtc{
			Vararg:     &ast.Arg{Arg: "args"},
			Kwonlyargs: []*ast.Arg{{Arg: "k"}},
			KwDefaults: []ast.Expr{&ast.Constant{Value: 2}},
			Kwarg:      &ast.Arg{Arg: "kw"},
		},
	)
	if len(a.Args) != 3 {
		t.Errorf("Args = %d, want 3", len(a.Args))
	}
	if a.Vararg == nil || a.Vararg.Arg != "args" {
		t.Errorf("Vararg = %+v", a.Vararg)
	}
	if a.Kwarg == nil || a.Kwarg.Arg != "kw" {
		t.Errorf("Kwarg = %+v", a.Kwarg)
	}
	if len(a.Defaults) != 1 {
		t.Errorf("Defaults = %v", a.Defaults)
	}
}

func TestSplitKeywordOrStarred(t *testing.T) {
	name := "k"
	items := []KeywordOrStarred{
		{Element: &ast.Name{Id: "x"}, IsKeyword: false},
		{Element: &ast.Keyword{Arg: &name, Value: &ast.Constant{Value: 1}}, IsKeyword: true},
		{Element: &ast.Starred{Value: &ast.Name{Id: "rest"}}, IsKeyword: false},
	}
	args, kws := SplitKeywordOrStarred(items)
	if len(args) != 2 {
		t.Errorf("args = %v", args)
	}
	if len(kws) != 1 || kws[0].Arg == nil || *kws[0].Arg != "k" {
		t.Errorf("kws = %v", kws)
	}
}
