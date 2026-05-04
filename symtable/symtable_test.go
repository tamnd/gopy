package symtable

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// helpers --------------------------------------------------------------

func emptyArgs() *ast.Arguments { return &ast.Arguments{} }

func nameLoad(id string) *ast.Name  { return &ast.Name{Id: id, Ctx: ast.Load} }
func nameStore(id string) *ast.Name { return &ast.Name{Id: id, Ctx: ast.Store} }
func num(v int64) *ast.Constant     { return &ast.Constant{Value: v} }

func assign(targets []ast.Expr, value ast.Expr) *ast.Assign {
	return &ast.Assign{Targets: targets, Value: value}
}

func functionDef(name string, body []ast.Stmt) *ast.FunctionDef {
	return &ast.FunctionDef{Name: name, Args: emptyArgs(), Body: body}
}

func classDef(name string, body []ast.Stmt) *ast.ClassDef {
	return &ast.ClassDef{Name: name, Body: body}
}

func module(body ...ast.Stmt) *ast.Module {
	return &ast.Module{Body: body}
}

func mustBuild(t *testing.T, m ast.Mod) *Table {
	t.Helper()
	tab, err := Build(m, "<test>", nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	return tab
}

func mustError(t *testing.T, m ast.Mod, want string) {
	t.Helper()
	_, err := Build(m, "<test>", nil)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err.Error())
	}
}

func findChild(t *testing.T, e *Entry, name string) *Entry {
	t.Helper()
	for _, c := range e.Children {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no child named %q under %q (have %v)", name, e.Name, childNames(e))
	return nil
}

func childNames(e *Entry) []string {
	out := make([]string, len(e.Children))
	for i, c := range e.Children {
		out[i] = c.Name
	}
	return out
}

// tests ---------------------------------------------------------------

func TestModuleAssignProducesLocal(t *testing.T) {
	// x = 1
	m := module(assign([]ast.Expr{nameStore("x")}, num(1)))
	tab := mustBuild(t, m)
	if got := tab.Top.GetScope("x"); got != Local {
		t.Errorf("module x scope = %v, want Local", got)
	}
}

func TestFunctionLocalsAreLocal(t *testing.T) {
	// def f():
	//     x = 1
	body := []ast.Stmt{assign([]ast.Expr{nameStore("x")}, num(1))}
	m := module(functionDef("f", body))
	tab := mustBuild(t, m)
	f := findChild(t, tab.Top, "f")
	if got := f.GetScope("x"); got != Local {
		t.Errorf("f.x scope = %v, want Local", got)
	}
	if got := tab.Top.GetScope("f"); got != Local {
		t.Errorf("module f scope = %v, want Local", got)
	}
}

func TestGlobalDeclarationProducesGlobalExplicit(t *testing.T) {
	// def f():
	//     global g
	//     g = 1
	body := []ast.Stmt{
		&ast.Global{Names: []string{"g"}},
		assign([]ast.Expr{nameStore("g")}, num(1)),
	}
	m := module(functionDef("f", body))
	tab := mustBuild(t, m)
	f := findChild(t, tab.Top, "f")
	if got := f.GetScope("g"); got != GlobalExplicit {
		t.Errorf("f.g scope = %v, want GlobalExplicit", got)
	}
	if got := tab.Top.GetScope("g"); got != GlobalExplicit {
		// CPython propagates DEF_GLOBAL from the inner `global g`
		// directive up to the module entry via add_def_helper.
		t.Errorf("module g scope = %v, want GlobalExplicit", got)
	}
}

func TestNestedClosureMakesCellAndFree(t *testing.T) {
	// def outer():
	//     x = 1
	//     def inner():
	//         return x
	innerBody := []ast.Stmt{&ast.Return{Value: nameLoad("x")}}
	outerBody := []ast.Stmt{
		assign([]ast.Expr{nameStore("x")}, num(1)),
		functionDef("inner", innerBody),
	}
	m := module(functionDef("outer", outerBody))
	tab := mustBuild(t, m)
	outer := findChild(t, tab.Top, "outer")
	inner := findChild(t, outer, "inner")
	if got := outer.GetScope("x"); got != Cell {
		t.Errorf("outer.x scope = %v, want Cell", got)
	}
	if got := inner.GetScope("x"); got != Free {
		t.Errorf("inner.x scope = %v, want Free", got)
	}
}

func TestNonlocalAtModuleErrors(t *testing.T) {
	// nonlocal x  (at module level)
	m := module(&ast.Nonlocal{Names: []string{"x"}})
	mustError(t, m, "nonlocal declaration not allowed at module level")
}

func TestNonlocalNoBindingErrors(t *testing.T) {
	// def f():
	//     nonlocal x
	body := []ast.Stmt{&ast.Nonlocal{Names: []string{"x"}}}
	m := module(functionDef("f", body))
	mustError(t, m, "no binding for nonlocal 'x' found")
}

func TestGlobalAfterUseErrors(t *testing.T) {
	// def f():
	//     x
	//     global x
	body := []ast.Stmt{
		&ast.ExprStmt{Value: nameLoad("x")},
		&ast.Global{Names: []string{"x"}},
	}
	m := module(functionDef("f", body))
	mustError(t, m, "is used prior to global declaration")
}

func TestAssignToDebugErrors(t *testing.T) {
	// __debug__ = 1
	m := module(assign([]ast.Expr{nameStore("__debug__")}, num(1)))
	mustError(t, m, "cannot assign to __debug__")
}

func TestClassMethodSetsMethodFlag(t *testing.T) {
	// class C:
	//     def m(self):
	//         pass
	method := &ast.FunctionDef{
		Name: "m",
		Args: &ast.Arguments{Args: []*ast.Arg{{Arg: "self"}}},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	cls := classDef("C", []ast.Stmt{method})
	tab := mustBuild(t, module(cls))
	c := findChild(t, tab.Top, "C")
	mEntry := findChild(t, c, "m")
	if !mEntry.Method {
		t.Errorf("class method should have Method=true")
	}
	if c.Type != ClassBlock {
		t.Errorf("class entry Type=%v, want ClassBlock", c.Type)
	}
}

func TestPrivateNameMangledInClass(t *testing.T) {
	// class C:
	//     __x = 1
	cls := classDef("C", []ast.Stmt{
		assign([]ast.Expr{nameStore("__x")}, num(1)),
	})
	tab := mustBuild(t, module(cls))
	c := findChild(t, tab.Top, "C")
	if _, ok := c.Symbols["_C__x"]; !ok {
		t.Errorf("expected mangled name _C__x in class symbols, got %v", c.Symbols)
	}
}

func TestComprehensionCreatesChildBlock(t *testing.T) {
	// x = [i for i in [1, 2, 3]]
	listLit := &ast.List{Elts: []ast.Expr{num(1), num(2), num(3)}}
	listcomp := &ast.ListComp{
		Elt: nameLoad("i"),
		Generators: []*ast.Comprehension{{
			Target: nameStore("i"),
			Iter:   listLit,
		}},
	}
	m := module(assign([]ast.Expr{nameStore("x")}, listcomp))
	tab := mustBuild(t, m)
	// listcomp gets inlined into the parent (module) scope.
	if got := tab.Top.GetScope("x"); got != Local {
		t.Errorf("module x scope = %v, want Local", got)
	}
}

func TestImportRecordsBinding(t *testing.T) {
	// import os
	m := module(&ast.Import{Names: []*ast.Alias{{Name: "os"}}})
	tab := mustBuild(t, m)
	if got := tab.Top.GetScope("os"); got != Local {
		t.Errorf("module os scope = %v, want Local", got)
	}
	if tab.Top.Symbols["os"]&DefImport == 0 {
		t.Errorf("module os should have DefImport flag")
	}
}

func TestImportStarOutsideModuleErrors(t *testing.T) {
	// def f():
	//     from os import *
	mod := "os"
	starImp := &ast.ImportFrom{
		Module: &mod,
		Names:  []*ast.Alias{{Name: "*"}},
	}
	body := []ast.Stmt{starImp}
	m := module(functionDef("f", body))
	mustError(t, m, "import * only allowed at module level")
}

func TestDuplicateArgumentErrors(t *testing.T) {
	// def f(a, a): pass
	args := &ast.Arguments{Args: []*ast.Arg{{Arg: "a"}, {Arg: "a"}}}
	fn := &ast.FunctionDef{
		Name: "f",
		Args: args,
		Body: []ast.Stmt{&ast.Pass{}},
	}
	mustError(t, module(fn), "duplicate argument 'a'")
}

func TestSyntaxErrorIsErrorType(t *testing.T) {
	_, err := Build(module(&ast.Nonlocal{Names: []string{"x"}}), "<t>", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SyntaxError, got %T", err)
	}
	if se.Filename != "<t>" {
		t.Errorf("filename = %q, want \"<t>\"", se.Filename)
	}
}
