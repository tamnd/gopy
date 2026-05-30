package compile

import (
	"slices"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// NamedExpr (walrus) -------------------------------------------------

// TestNamedExprCopiesAndStores covers `(x := 1)` at module scope.
// The value is evaluated once, COPY 1 leaves a duplicate so the
// expression itself returns the value, and STORE_NAME binds x.
func TestNamedExprCopiesAndStores(t *testing.T) {
	walrus := &ast.NamedExpr{
		Target: nameStore("x"),
		Value:  cnst(int64(1)),
	}
	u := compileMod(t, exprMod(walrus))
	want := []string{
		"LOAD_CONST", // 1
		"COPY",       // dup so the expression returns the value
		"STORE_NAME", // x
		"POP_TOP",    // ExprStmt discard
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[2].Oparg != 1 {
		t.Errorf("COPY oparg = %d, want 1", u.Seq.Instrs[2].Oparg)
	}
}

// Yield / YieldFrom / Await ------------------------------------------

// yieldFunc wraps an expression in a function body so symtable accepts
// it as a generator / coroutine context.
func yieldFunc(t *testing.T, body ast.Expr) *Unit {
	t.Helper()
	fn := &ast.FunctionDef{
		Name: "g",
		Args: emptyArgs(),
		Body: []ast.Stmt{&ast.ExprStmt{Value: body}},
	}
	u := compileMod(t, module(fn))
	return findInnerUnit(t, u)
}

// asyncYieldFunc wraps an expression in an async function body.
func asyncYieldFunc(t *testing.T, body ast.Expr) *Unit {
	t.Helper()
	fn := &ast.AsyncFunctionDef{
		Name: "g",
		Args: emptyArgs(),
		Body: []ast.Stmt{&ast.ExprStmt{Value: body}},
	}
	u := compileMod(t, module(fn))
	return findInnerUnit(t, u)
}

func TestYieldEmitsYieldValue(t *testing.T) {
	// yield 1
	inner := yieldFunc(t, &ast.Yield{Value: cnst(int64(1))})
	got := opNames(inner)
	if !slices.Contains(got, "YIELD_VALUE") {
		t.Errorf("expected YIELD_VALUE in %v", got)
	}
}

func TestBareYieldLoadsNone(t *testing.T) {
	// yield
	inner := yieldFunc(t, &ast.Yield{})
	got := opNames(inner)
	// Sequence should be: SETUP_CLEANUP (PEP 479 wrap), RESUME,
	// LOAD_CONST None, YIELD_VALUE, POP_TOP, ...
	//
	// CPython: Python/codegen.c:1363 codegen_function_body
	// (add_stopiteration_handler emits the wrap)
	if got[2] != "LOAD_CONST" || got[3] != "YIELD_VALUE" {
		t.Errorf("expected LOAD_CONST None then YIELD_VALUE; got %v", got)
	}
}

func TestYieldFromEmitsSendLoop(t *testing.T) {
	// yield from x
	inner := yieldFunc(t, &ast.YieldFrom{Value: nameLoad("x")})
	got := opNames(inner)
	wantOps := []string{
		"GET_YIELD_FROM_ITER",
		"LOAD_CONST", // None to prime SEND
		"SEND",
		"YIELD_VALUE",
		"RESUME",
		"JUMP_BACKWARD",
		"END_SEND",
	}
	for _, op := range wantOps {
		if !slices.Contains(got, op) {
			t.Errorf("expected %s in inner ops; got %v", op, got)
		}
	}
}

func TestAwaitEmitsGetAwaitableLoop(t *testing.T) {
	// await x  (inside async def)
	inner := asyncYieldFunc(t, &ast.Await{Value: nameLoad("x")})
	got := opNames(inner)
	wantOps := []string{
		"GET_AWAITABLE",
		"SEND",
		"YIELD_VALUE",
		"RESUME",
		"END_SEND",
	}
	for _, op := range wantOps {
		if !slices.Contains(got, op) {
			t.Errorf("expected %s in inner ops; got %v", op, got)
		}
	}
}

// JoinedStr / FormattedValue -----------------------------------------

func TestJoinedStrEmitsBuildString(t *testing.T) {
	// f"a{x}b"
	js := &ast.JoinedStr{Values: []ast.Expr{
		cnst("a"),
		&ast.FormattedValue{Value: nameLoad("x")},
		cnst("b"),
	}}
	u := compileMod(t, exprMod(js))
	got := opNames(u)
	want := []string{
		"LOAD_CONST", // "a"
		"LOAD_NAME",  // x
		"FORMAT_SIMPLE",
		"LOAD_CONST",   // "b"
		"BUILD_STRING", // n=3
		"POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[5].Oparg != 3 {
		t.Errorf("BUILD_STRING oparg = %d, want 3", u.Seq.Instrs[5].Oparg)
	}
}

func TestFormattedValueWithConversionEmitsConvertValue(t *testing.T) {
	// f"{x!r}"
	fv := &ast.FormattedValue{Value: nameLoad("x"), Conversion: 'r'}
	js := &ast.JoinedStr{Values: []ast.Expr{fv}}
	u := compileMod(t, exprMod(js))
	got := opNames(u)
	// Single FormattedValue: CPython omits BUILD_STRING (value_count==1 branch).
	// CPython: Python/codegen.c:4104 codegen_joined_str
	want := []string{
		"LOAD_NAME",
		"CONVERT_VALUE",
		"FORMAT_SIMPLE",
		"POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[2].Oparg != 2 {
		t.Errorf("CONVERT_VALUE oparg = %d, want 2 (!r)", u.Seq.Instrs[2].Oparg)
	}
}

func TestFormattedValueWithSpecEmitsFormatWithSpec(t *testing.T) {
	// f"{x:>5}"
	fv := &ast.FormattedValue{
		Value:      nameLoad("x"),
		FormatSpec: &ast.JoinedStr{Values: []ast.Expr{cnst(">5")}},
	}
	js := &ast.JoinedStr{Values: []ast.Expr{fv}}
	u := compileMod(t, exprMod(js))
	got := opNames(u)
	if !slices.Contains(got, "FORMAT_WITH_SPEC") {
		t.Errorf("expected FORMAT_WITH_SPEC in %v", got)
	}
	if slices.Contains(got, "FORMAT_SIMPLE") {
		t.Errorf("did not expect FORMAT_SIMPLE when a spec is present; got %v", got)
	}
}
