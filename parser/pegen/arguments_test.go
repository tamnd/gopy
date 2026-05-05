package pegen

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestEmptyArguments(t *testing.T) {
	p := &Parser{}
	a := EmptyArguments(p)
	if a == nil {
		t.Fatal("EmptyArguments returned nil")
	}
	if len(a.Args) != 0 || len(a.Posonlyargs) != 0 || a.Vararg != nil || a.Kwarg != nil {
		t.Errorf("expected zero-value Arguments, got %+v", a)
	}
}

func TestMakeArgumentsPlainPlusStar(t *testing.T) {
	p := &Parser{}
	a := MakeArguments(
		p,
		nil,
		nil,
		[]*ast.Arg{{Arg: "x"}, {Arg: "y"}},
		[]*NameDefaultPair{{Arg: &ast.Arg{Arg: "z"}, Value: &ast.Constant{Value: 1}}},
		&StarEtc{
			Vararg:     &ast.Arg{Arg: "args"},
			Kwonlyargs: []*NameDefaultPair{{Arg: &ast.Arg{Arg: "k"}, Value: &ast.Constant{Value: 2}}},
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
	if len(a.Kwonlyargs) != 1 || a.Kwonlyargs[0].Arg != "k" {
		t.Errorf("Kwonlyargs = %v", a.Kwonlyargs)
	}
	if len(a.KwDefaults) != 1 {
		t.Errorf("KwDefaults = %v", a.KwDefaults)
	}
}

func TestSeqExtractAndDeleteStarredExprs(t *testing.T) {
	p := &Parser{}
	name := "k"
	items := []*KeywordOrStarred{
		{Element: &ast.Name{Id: "x"}, IsKeyword: false},
		{Element: &ast.Keyword{Arg: &name, Value: &ast.Constant{Value: 1}}, IsKeyword: true},
		{Element: &ast.Starred{Value: &ast.Name{Id: "rest"}}, IsKeyword: false},
	}
	starreds := seqExtractStarredExprs(p, items)
	if len(starreds) != 2 {
		t.Errorf("starreds = %v", starreds)
	}
	kws := seqDeleteStarredExprs(p, items)
	if len(kws) != 1 || kws[0].Arg == nil || *kws[0].Arg != "k" {
		t.Errorf("kws = %v", kws)
	}
}

func TestCollectCallSeqsCarriesArgsAndKeywords(t *testing.T) {
	p := &Parser{}
	name := "k"
	pos := []ast.Expr{&ast.Name{Id: "x"}}
	kwargs := []*KeywordOrStarred{
		{Element: &ast.Keyword{Arg: &name, Value: &ast.Constant{Value: 1}}, IsKeyword: true},
		{Element: &ast.Starred{Value: &ast.Name{Id: "rest"}}, IsKeyword: false},
	}
	out := CollectCallSeqs(p, pos, kwargs).(*ast.Call)
	if len(out.Args) != 2 {
		t.Errorf("Args = %v", out.Args)
	}
	if len(out.Keywords) != 1 {
		t.Errorf("Keywords = %v", out.Keywords)
	}
}

func TestCollectCallSeqsNoKwargs(t *testing.T) {
	p := &Parser{}
	pos := []ast.Expr{&ast.Name{Id: "x"}, &ast.Name{Id: "y"}}
	out := CollectCallSeqs(p, pos, nil).(*ast.Call)
	if len(out.Args) != 2 {
		t.Errorf("Args = %v", out.Args)
	}
	if out.Keywords != nil {
		t.Errorf("Keywords should be nil when b is nil, got %v", out.Keywords)
	}
}
