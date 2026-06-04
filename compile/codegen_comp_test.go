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

// TestListCompMakesInnerFunc covers `[i for i in xs]`. Under PEP 709
// the comprehension is inlined into the enclosing scope: no
// MAKE_FUNCTION / CALL and no nested <listcomp> code object. The outer
// scope evaluates xs, GET_ITERs it, isolates the loop var with
// LOAD_FAST_AND_CLEAR / STORE_FAST_MAYBE_NULL, then BUILD_LIST and
// LIST_APPEND directly.
func TestListCompMakesInnerFunc(t *testing.T) {
	lc := listCompXs(nameLoad("i"), nameStore("i"))
	body := &ast.Assign{Targets: []ast.Expr{nameStore("y")}, Value: lc}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "GET_ITER", "LOAD_FAST_AND_CLEAR", "BUILD_LIST",
		"FOR_ITER", "LIST_APPEND", "STORE_FAST_MAYBE_NULL")
	mustNotContain(t, got, "MAKE_FUNCTION", "CALL")
	if findInnerUnitMaybe(u, "<listcomp>") != nil {
		t.Errorf("inlined listcomp should not emit a nested <listcomp> unit")
	}
}

// TestSetCompUsesSetAdd covers `{i for i in xs}`, inlined per PEP 709.
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
	got := opNames(u)
	mustContain(t, got, "BUILD_SET", "SET_ADD")
	mustNotContain(t, got, "MAKE_FUNCTION", "CALL")
}

// TestDictCompUsesMapAdd covers `{k: v for k in xs}`, inlined per PEP 709.
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
	got := opNames(u)
	mustContain(t, got, "BUILD_MAP", "MAP_ADD")
	mustNotContain(t, got, "MAKE_FUNCTION", "CALL")
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

// findInnerUnitMaybe returns the embedded inner Unit with the matching
// Name, or nil if none is present. Used to assert that inlined
// comprehensions emit no nested code object.
func findInnerUnitMaybe(u *Unit, name string) *Unit {
	for _, c := range u.Consts {
		if inner, ok := c.(*Unit); ok && inner.Name == name {
			return inner
		}
	}
	return nil
}

// mustNotContain fails if any of the named opcodes appears in ops.
func mustNotContain(t *testing.T, ops []string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		for _, op := range ops {
			if op == w {
				t.Errorf("op %q should not appear in %v", w, ops)
				break
			}
		}
	}
}
