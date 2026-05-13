package compile

import (
	"slices"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestEmptyClassEmitsBuildClassCall covers `class C: pass`. The outer
// emits LOAD_BUILD_CLASS / PUSH_NULL / inner code / MAKE_FUNCTION /
// LOAD_CONST "C" / CALL 2 / STORE_NAME C.
func TestEmptyClassEmitsBuildClassCall(t *testing.T) {
	cd := &ast.ClassDef{
		Name: "C",
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(cd))
	want := []string{
		"LOAD_BUILD_CLASS",
		"PUSH_NULL",
		"LOAD_CONST",    // inner code
		"MAKE_FUNCTION", //
		"LOAD_CONST",    // "C"
		"CALL",          // arg=2
		"STORE_NAME",    // C
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[5].Op != CALL || u.Seq.Instrs[5].Oparg != 2 {
		t.Errorf("CALL oparg = %d, want 2", u.Seq.Instrs[5].Oparg)
	}
}

// TestClassWithBaseEmitsBaseLoad covers `class C(B): pass`. The base
// is a positional arg of __build_class__, so the CALL count is 3.
func TestClassWithBaseEmitsBaseLoad(t *testing.T) {
	cd := &ast.ClassDef{
		Name:  "C",
		Bases: []ast.Expr{nameLoad("B")},
		Body:  []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(cd))
	got := opNames(u)
	want := []string{
		"LOAD_BUILD_CLASS",
		"PUSH_NULL",
		"LOAD_CONST",    // inner code
		"MAKE_FUNCTION", //
		"LOAD_CONST",    // "C"
		"LOAD_NAME",     // B
		"CALL",          // arg=3
		"STORE_NAME",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[6].Oparg != 3 {
		t.Errorf("CALL oparg = %d, want 3", u.Seq.Instrs[6].Oparg)
	}
}

// TestClassWithKeywordEmitsCallKw covers `class C(metaclass=M): pass`.
// Keywords route through CALL_KW with a tuple of names.
func TestClassWithKeywordEmitsCallKw(t *testing.T) {
	mc := "metaclass"
	cd := &ast.ClassDef{
		Name: "C",
		Keywords: []*ast.Keyword{
			{Arg: &mc, Value: nameLoad("M")},
		},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(cd))
	got := opNames(u)
	if !slices.Contains(got, "CALL_KW") {
		t.Errorf("expected CALL_KW for class with keyword args; got %v", got)
	}
}

// TestClassDecoratorAppliesCalls verifies decorators wrap the class
// after the build_class call.
func TestClassDecoratorAppliesCalls(t *testing.T) {
	cd := &ast.ClassDef{
		Name:          "C",
		Body:          []ast.Stmt{&ast.Pass{}},
		DecoratorList: []ast.Expr{nameLoad("d1"), nameLoad("d2")},
	}
	u := compileMod(t, module(cd))
	got := opNames(u)
	want := []string{
		"LOAD_NAME", // d1
		"LOAD_NAME", // d2
		"LOAD_BUILD_CLASS",
		"PUSH_NULL",
		"LOAD_CONST",    // inner code
		"MAKE_FUNCTION", //
		"LOAD_CONST",    // "C"
		"CALL",          // build the class
		"CALL",          // d2(C)
		"CALL",          // d1(d2(C))
		"STORE_NAME",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

// TestClassBodyStoresDocstring covers the spec 1704 phase-6 gate: a
// leading bare-string in the body emits LOAD_CONST <docstring> /
// STORE_NAME __doc__ right after the qualname store, so user classes
// surface a `__doc__` attribute the way CPython does.
//
// CPython: Python/codegen.c codegen_class_body (docstring branch)
func TestClassBodyStoresDocstring(t *testing.T) {
	cd := &ast.ClassDef{
		Name: "C",
		Body: []ast.Stmt{
			&ast.ExprStmt{Value: cnst("hello doc")},
		},
	}
	u := compileMod(t, module(cd))
	inner := findInnerUnit(t, u)
	got := opNames(inner)
	want := []string{
		"RESUME",
		"LOAD_NAME",  // __name__
		"STORE_NAME", // __module__
		"LOAD_CONST", // "C" qualname
		"STORE_NAME", // __qualname__
		"LOAD_CONST", // "hello doc"
		"STORE_NAME", // __doc__
		"LOAD_CONST", // implicit None
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Fatalf("inner ops = %v, want %v", got, want)
	}
	if !slices.Contains(inner.Names, "__doc__") {
		t.Errorf("inner.Names %v missing __doc__", inner.Names)
	}
	// The docstring lands as a const (deduped with the qualname const
	// in the per-unit cache only if they happen to match).
	hasDocConst := false
	for _, c := range inner.Consts {
		if s, ok := c.(string); ok && s == "hello doc" {
			hasDocConst = true
		}
	}
	if !hasDocConst {
		t.Errorf("inner.Consts %v missing docstring", inner.Consts)
	}
}

// TestNestedClassQualname covers the spec 1704 phase-6 gate: an inner
// class declared inside another class emits its qualified name
// ("Outer.Inner") as the __qualname__ const, not the bare "Inner".
//
// CPython: Python/compile.c:644 compiler_set_qualname
func TestNestedClassQualname(t *testing.T) {
	cd := &ast.ClassDef{
		Name: "Outer",
		Body: []ast.Stmt{&ast.ClassDef{
			Name: "Inner",
			Body: []ast.Stmt{&ast.Pass{}},
		}},
	}
	u := compileMod(t, module(cd))
	outer := findInnerUnit(t, u)
	if outer.Qualname != "Outer" {
		t.Errorf("outer.Qualname = %q, want Outer", outer.Qualname)
	}
	inner := findInnerUnit(t, outer)
	if inner.Qualname != "Outer.Inner" {
		t.Errorf("inner.Qualname = %q, want Outer.Inner", inner.Qualname)
	}
	if !slices.Contains(inner.Consts, "Outer.Inner") {
		t.Errorf("inner.Consts %v missing dotted qualname", inner.Consts)
	}
}

// TestClassBodyStoresModuleAndQualname verifies the inner class code
// object opens with LOAD_NAME __name__ / STORE_NAME __module__ /
// LOAD_CONST <name> / STORE_NAME __qualname__.
func TestClassBodyStoresModuleAndQualname(t *testing.T) {
	cd := &ast.ClassDef{
		Name: "C",
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(cd))
	inner := findInnerUnit(t, u)
	got := opNames(inner)
	want := []string{
		"RESUME",
		"LOAD_NAME",  // __name__
		"STORE_NAME", // __module__
		"LOAD_CONST", // "C"
		"STORE_NAME", // __qualname__
		"NOP",        // pass
		"LOAD_CONST", // implicit None
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Fatalf("inner ops = %v, want %v", got, want)
	}
	// Verify the names pool has __name__, __module__, __qualname__.
	want_names := map[string]bool{"__name__": true, "__module__": true, "__qualname__": true}
	for _, n := range inner.Names {
		delete(want_names, n)
	}
	if len(want_names) != 0 {
		t.Errorf("missing names %v in inner.Names = %v", want_names, inner.Names)
	}
}
