package pegen

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestMakeComprehensionFamily(t *testing.T) {
	gen := MakeComprehension(&ast.Name{Id: "x"}, &ast.Name{Id: "xs"}, nil, false)
	if gen.IsAsync != 0 {
		t.Errorf("IsAsync = %d, want 0", gen.IsAsync)
	}
	gens := []*ast.Comprehension{gen}
	if MakeListComp(&ast.Name{Id: "x"}, gens).Elt == nil {
		t.Errorf("ListComp Elt nil")
	}
	if MakeSetComp(&ast.Name{Id: "x"}, gens).Elt == nil {
		t.Errorf("SetComp Elt nil")
	}
	dc := MakeDictComp(&ast.Name{Id: "k"}, &ast.Name{Id: "v"}, gens)
	if dc.Key == nil || dc.Value == nil {
		t.Errorf("DictComp missing K/V")
	}
	if MakeGeneratorExp(&ast.Name{Id: "x"}, gens).Generators == nil {
		t.Errorf("GeneratorExp nil generators")
	}
}

func TestMakeComprehensionAsync(t *testing.T) {
	g := MakeComprehension(&ast.Name{Id: "x"}, &ast.Name{Id: "xs"}, nil, true)
	if g.IsAsync != 1 {
		t.Errorf("async flag = %d", g.IsAsync)
	}
}
