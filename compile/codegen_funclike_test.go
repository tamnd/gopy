package compile

import (
	"slices"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// helpers ------------------------------------------------------------

func arg(name string) *ast.Arg { return &ast.Arg{Arg: name} }

func emptyArgs() *ast.Arguments { return &ast.Arguments{} }

func argsOf(names ...string) *ast.Arguments {
	a := &ast.Arguments{}
	for _, n := range names {
		a.Args = append(a.Args, arg(n))
	}
	return a
}

// findInnerUnit returns the first *Unit constant in u.Consts.
func findInnerUnit(t *testing.T, u *Unit) *Unit {
	t.Helper()
	for _, c := range u.Consts {
		if inner, ok := c.(*Unit); ok {
			return inner
		}
	}
	t.Fatalf("no inner Unit in consts: %v", u.Consts)
	return nil
}

// tests --------------------------------------------------------------

// TestEmptyFunctionDefBuildsInnerUnit covers the smallest function:
// `def f(): pass`. The outer body should evaluate zero defaults, emit
// LOAD_CONST <inner> / MAKE_FUNCTION 0 / STORE_NAME f. The inner unit
// gets RESUME 0, NOP (pass), then the implicit None / RETURN_VALUE.
func TestEmptyFunctionDefBuildsInnerUnit(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: emptyArgs(),
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(fn))

	wantOuter := []string{
		"LOAD_CONST",    // inner code object
		"MAKE_FUNCTION", // build the function
		"STORE_NAME",    // bind f
		"LOAD_CONST",    // implicit None
		"RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, wantOuter) {
		t.Fatalf("outer ops = %v, want %v", got, wantOuter)
	}

	inner := findInnerUnit(t, u)
	if inner.Name != "f" {
		t.Errorf("inner.Name = %q, want %q", inner.Name, "f")
	}
	if inner.Argcount != 0 || inner.PosOnlyArgCount != 0 || inner.KwOnlyArgCount != 0 {
		t.Errorf("inner argcounts = (%d,%d,%d), want all zero",
			inner.Argcount, inner.PosOnlyArgCount, inner.KwOnlyArgCount)
	}
	wantInner := []string{
		"RESUME",
		"NOP",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	wantInnerOps := opNames(inner)
	if !equalStrings(wantInnerOps, wantInner) {
		t.Errorf("inner ops = %v, want %v", wantInnerOps, wantInner)
	}
}

// TestFunctionWithReturnExpr verifies the body sees its own scope:
// `def f(): return 1` puts 1 in inner.Consts and emits LOAD_CONST /
// RETURN_VALUE. There must be no extra LOAD_CONST None tail, since the
// explicit return already emitted one.
func TestFunctionWithReturnExpr(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: emptyArgs(),
		Body: []ast.Stmt{&ast.Return{Value: cnst(int64(1))}},
	}
	u := compileMod(t, module(fn))
	inner := findInnerUnit(t, u)
	want := []string{"RESUME", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(inner); !equalStrings(got, want) {
		t.Errorf("inner ops = %v, want %v", got, want)
	}
}

// TestFunctionParamUsesLoadFast checks that a parameter resolves to
// LOAD_FAST inside the function body, not LOAD_NAME. This exercises
// the FunctionBlock branch of nameOp.
func TestFunctionParamUsesLoadFast(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: argsOf("x"),
		Body: []ast.Stmt{&ast.Return{Value: nameLoad("x")}},
	}
	u := compileMod(t, module(fn))
	inner := findInnerUnit(t, u)
	want := []string{"RESUME", "LOAD_FAST", "RETURN_VALUE"}
	if got := opNames(inner); !equalStrings(got, want) {
		t.Errorf("inner ops = %v, want %v", got, want)
	}
	if inner.Argcount != 1 {
		t.Errorf("Argcount = %d, want 1", inner.Argcount)
	}
	if len(inner.VarNames) == 0 || inner.VarNames[0] != "x" {
		t.Errorf("VarNames = %v, want first entry x", inner.VarNames)
	}
}

// TestFunctionDefaultEmitsBuildTuple verifies positional defaults are
// evaluated in the outer scope, packed via BUILD_TUPLE, and that
// SET_FUNCTION_ATTRIBUTE 0x01 stamps them onto the function after
// MAKE_FUNCTION.
func TestFunctionDefaultEmitsBuildTuple(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: &ast.Arguments{
			Args:     []*ast.Arg{arg("x")},
			Defaults: []ast.Expr{cnst(int64(7))},
		},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(fn))
	want := []string{
		"LOAD_CONST",             // default 7
		"BUILD_TUPLE",            // pack (7,)
		"LOAD_CONST",             // inner code
		"MAKE_FUNCTION",          // wrap code (no oparg)
		"SET_FUNCTION_ATTRIBUTE", // attach defaults via 0x01
		"STORE_NAME",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	sfa := u.Seq.Instrs[4]
	if sfa.Op != SET_FUNCTION_ATTRIBUTE {
		t.Fatalf("expected SET_FUNCTION_ATTRIBUTE at index 4, got %v", sfa.Op)
	}
	if sfa.Oparg != 0x01 {
		t.Errorf("SET_FUNCTION_ATTRIBUTE oparg = %#x, want 0x01", sfa.Oparg)
	}
}

// TestKwonlyDefaultEmitsBuildMap verifies keyword-only defaults are
// emitted as a BUILD_MAP and stamped onto the function via
// SET_FUNCTION_ATTRIBUTE 0x02.
func TestKwonlyDefaultEmitsBuildMap(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: &ast.Arguments{
			Kwonlyargs: []*ast.Arg{arg("y")},
			KwDefaults: []ast.Expr{cnst(int64(9))},
		},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(fn))
	want := []string{
		"LOAD_CONST",             // "y" name
		"LOAD_CONST",             // 9
		"BUILD_MAP",              // {"y": 9}
		"LOAD_CONST",             // inner code
		"MAKE_FUNCTION",          // wrap code
		"SET_FUNCTION_ATTRIBUTE", // attach kwdefaults via 0x02
		"STORE_NAME",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	sfa := u.Seq.Instrs[5]
	if sfa.Op != SET_FUNCTION_ATTRIBUTE {
		t.Fatalf("expected SET_FUNCTION_ATTRIBUTE at index 5, got %v", sfa.Op)
	}
	if sfa.Oparg != 0x02 {
		t.Errorf("SET_FUNCTION_ATTRIBUTE oparg = %#x, want 0x02", sfa.Oparg)
	}
}

// TestVarargsAndKwargsSetFlags makes sure the inner unit's CO_*
// flags pick up vararg / varkeyword presence.
func TestVarargsAndKwargsSetFlags(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: &ast.Arguments{
			Vararg: arg("rest"),
			Kwarg:  arg("kw"),
		},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(fn))
	inner := findInnerUnit(t, u)
	if inner.Flags&CoVarargs == 0 {
		t.Errorf("CoVarargs not set; flags=%#x", inner.Flags)
	}
	if inner.Flags&CoVarkeywords == 0 {
		t.Errorf("CoVarkeywords not set; flags=%#x", inner.Flags)
	}
	// Both rest and kw should be in inner.VarNames so the runtime
	// can find them by fast-local index.
	gotVar := map[string]bool{}
	for _, n := range inner.VarNames {
		gotVar[n] = true
	}
	if !gotVar["rest"] || !gotVar["kw"] {
		t.Errorf("VarNames = %v, want rest+kw present", inner.VarNames)
	}
}

// TestLambdaEmitsInnerCodeAndMakeFunction covers Lambda. A bare
// `lambda: 0` at module scope should evaluate to a function object on
// the stack (no STORE_NAME), but at module scope we wrap it with an
// ExprStmt for testability.
func TestLambdaEmitsInnerCodeAndMakeFunction(t *testing.T) {
	lam := &ast.Lambda{
		Args: emptyArgs(),
		Body: cnst(int64(0)),
	}
	u := compileMod(t, module(&ast.ExprStmt{Value: lam}))
	want := []string{
		"LOAD_CONST",    // inner code
		"MAKE_FUNCTION", // 0 flags
		"POP_TOP",       // ExprStmt discards
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	inner := findInnerUnit(t, u)
	if inner.Name != "<lambda>" {
		t.Errorf("inner.Name = %q, want <lambda>", inner.Name)
	}
	wantInner := []string{"RESUME", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(inner); !equalStrings(got, wantInner) {
		t.Errorf("inner ops = %v, want %v", got, wantInner)
	}
}

// TestNestedFunctionClosureEmitsLoadFast covers the closure path:
// `def outer(x):\n  def inner():\n    return x` should leave the
// outer's `x` as a cell, and emit LOAD_FAST x / BUILD_TUPLE 1 in
// outer before MAKE_FUNCTION, with SET_FUNCTION_ATTRIBUTE 0x08
// stamping the closure tuple onto the new function.
func TestNestedFunctionClosureEmitsLoadFast(t *testing.T) {
	inner := &ast.FunctionDef{
		Name: "inner",
		Args: emptyArgs(),
		Body: []ast.Stmt{&ast.Return{Value: nameLoad("x")}},
	}
	outer := &ast.FunctionDef{
		Name: "outer",
		Args: argsOf("x"),
		Body: []ast.Stmt{inner},
	}
	u := compileMod(t, module(outer))

	outerUnit := findInnerUnit(t, u)
	got := opNames(outerUnit)
	// Look for the LOAD_FAST + BUILD_TUPLE + LOAD_CONST + MAKE_FUNCTION
	// + SET_FUNCTION_ATTRIBUTE sequence inside outer.
	found := false
	for i := 0; i+4 < len(got); i++ {
		if got[i] == "LOAD_FAST" && got[i+1] == "BUILD_TUPLE" &&
			got[i+2] == "LOAD_CONST" && got[i+3] == "MAKE_FUNCTION" &&
			got[i+4] == "SET_FUNCTION_ATTRIBUTE" {
			found = true
			if outerUnit.Seq.Instrs[i+4].Oparg&0x08 == 0 {
				t.Errorf("SET_FUNCTION_ATTRIBUTE oparg = %#x, want closure bit 0x08",
					outerUnit.Seq.Instrs[i+4].Oparg)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected closure attach pattern in outer ops; got %v", got)
	}

	// The innermost unit should LOAD_DEREF x (free var).
	innermost := findInnerUnit(t, outerUnit)
	innerOps := opNames(innermost)
	if !slices.Contains(innerOps, "LOAD_DEREF") {
		t.Errorf("expected LOAD_DEREF in inner ops; got %v", innerOps)
	}
}

// TestDecoratorAppliesCalls verifies decorators are evaluated outer
// of MAKE_FUNCTION and that one CALL is emitted per decorator.
func TestDecoratorAppliesCalls(t *testing.T) {
	fn := &ast.FunctionDef{
		Name:          "f",
		Args:          emptyArgs(),
		Body:          []ast.Stmt{&ast.Pass{}},
		DecoratorList: []ast.Expr{nameLoad("d1"), nameLoad("d2")},
	}
	u := compileMod(t, module(fn))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",     // d1
		"LOAD_NAME",     // d2
		"LOAD_CONST",    // inner code
		"MAKE_FUNCTION", // build function
		"CALL",          // d2(f)
		"CALL",          // d1(d2(f))
		"STORE_NAME",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}
