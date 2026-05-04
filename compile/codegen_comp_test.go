package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// listCompXs builds the AST for `[<elt> for <target> in xs]`.
func listCompXs(elt ast.Expr, target *ast.Name) *ast.ListComp {
	return &ast.ListComp{
		Elt: elt,
		Generators: []*ast.Comprehension{{
			Target: target,
			Iter:   nameLoad("xs"),
		}},
	}
}

// TestListCompMakesInnerFunc covers `[i for i in xs]`. The outer scope
// should MAKE_FUNCTION the inner code object, evaluate xs, GET_ITER,
// and CALL with the iter as the implicit .0 argument.
func TestListCompMakesInnerFunc(t *testing.T) {
	lc := listCompXs(nameLoad("i"), nameStore("i"))
	body := &ast.Assign{Targets: []ast.Expr{nameStore("y")}, Value: lc}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "MAKE_FUNCTION", "GET_ITER", "CALL")

	innerUnit := findInnerUnitNamed(t, u, "<listcomp>")
	if innerUnit.Argcount != 1 {
		t.Errorf("inner argcount = %d, want 1", innerUnit.Argcount)
	}
	innerOps := opNames(innerUnit)
	mustContain(t, innerOps, "RESUME", "BUILD_LIST", "FOR_ITER", "LIST_APPEND", "RETURN_VALUE")
}

// TestSetCompUsesSetAdd covers `{i for i in xs}`.
func TestSetCompUsesSetAdd(t *testing.T) {
	sc := &ast.SetComp{
		Elt: nameLoad("i"),
		Generators: []*ast.Comprehension{{
			Target: nameStore("i"),
			Iter:   nameLoad("xs"),
		}},
	}
	body := &ast.Assign{Targets: []ast.Expr{nameStore("y")}, Value: sc}
	u := compileMod(t, module(body))
	innerUnit := findInnerUnitNamed(t, u, "<setcomp>")
	innerOps := opNames(innerUnit)
	mustContain(t, innerOps, "BUILD_SET", "SET_ADD")
}

// TestDictCompUsesMapAdd covers `{k: v for k in xs}`.
func TestDictCompUsesMapAdd(t *testing.T) {
	dc := &ast.DictComp{
		Key:   nameLoad("k"),
		Value: nameLoad("k"),
		Generators: []*ast.Comprehension{{
			Target: nameStore("k"),
			Iter:   nameLoad("xs"),
		}},
	}
	body := &ast.Assign{Targets: []ast.Expr{nameStore("y")}, Value: dc}
	u := compileMod(t, module(body))
	innerUnit := findInnerUnitNamed(t, u, "<dictcomp>")
	innerOps := opNames(innerUnit)
	mustContain(t, innerOps, "BUILD_MAP", "MAP_ADD")
}

// TestGenExprWrapsStopIteration covers `(i for i in xs)`. The genexpr
// inner scope yields each value and must wrap the body in a
// StopIteration handler.
func TestGenExprWrapsStopIteration(t *testing.T) {
	ge := &ast.GeneratorExp{
		Elt: nameLoad("i"),
		Generators: []*ast.Comprehension{{
			Target: nameStore("i"),
			Iter:   nameLoad("xs"),
		}},
	}
	body := &ast.Assign{Targets: []ast.Expr{nameStore("y")}, Value: ge}
	u := compileMod(t, module(body))
	innerUnit := findInnerUnitNamed(t, u, "<genexpr>")
	if innerUnit.Flags&CoGenerator == 0 {
		t.Errorf("genexpr inner unit should have CoGenerator flag, got %x", innerUnit.Flags)
	}
	innerOps := opNames(innerUnit)
	mustContain(t, innerOps, "SETUP_CLEANUP", "YIELD_VALUE", "CALL_INTRINSIC_1", "RERAISE")
}

// findInnerUnitNamed walks the outer Unit's consts looking for an embedded
// inner Unit with the matching Name.
func findInnerUnitNamed(t *testing.T, u *Unit, name string) *Unit {
	t.Helper()
	for _, c := range u.Consts {
		if inner, ok := c.(*Unit); ok && inner.Name == name {
			return inner
		}
	}
	t.Fatalf("inner unit %q not found in consts %v", name, u.Consts)
	return nil
}
