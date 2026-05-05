package errors

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestFindInvalidTargetClean(t *testing.T) {
	cases := []ast.Expr{
		&ast.Name{Id: "x"},
		&ast.Attribute{Attr: "y"},
		&ast.Subscript{},
		&ast.Tuple{Elts: []ast.Expr{&ast.Name{Id: "a"}, &ast.Name{Id: "b"}}},
		&ast.List{Elts: []ast.Expr{&ast.Name{Id: "a"}}},
		&ast.Starred{Value: &ast.Name{Id: "a"}},
	}
	for _, e := range cases {
		if got := FindInvalidTarget(e, StarTargets); got != nil {
			t.Errorf("FindInvalidTarget(%T) = %v want nil", e, got)
		}
	}
}

func TestFindInvalidTargetRejects(t *testing.T) {
	bad := &ast.Constant{Value: 1}
	if FindInvalidTarget(bad, StarTargets) != bad {
		t.Errorf("Constant should be rejected as a target")
	}
	starred := &ast.Starred{Value: &ast.Name{Id: "a"}}
	if FindInvalidTarget(starred, DelTargets) != starred {
		t.Errorf("Starred should be rejected in del context")
	}
}

func TestArgumentsParsingMsg(t *testing.T) {
	plain := &ast.Call{Keywords: []*ast.Keyword{{Arg: stringPtr("x"), Value: &ast.Name{Id: "y"}}}}
	if got := ArgumentsParsingMsg(plain); got != "positional argument follows keyword argument" {
		t.Errorf("plain msg = %q", got)
	}
	unpack := &ast.Call{Keywords: []*ast.Keyword{{Value: &ast.Name{Id: "kwargs"}}}}
	if got := ArgumentsParsingMsg(unpack); got != "positional argument follows keyword argument unpacking" {
		t.Errorf("unpack msg = %q", got)
	}
}

func stringPtr(s string) *string { return &s }
