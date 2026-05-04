package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// helper -------------------------------------------------------------

func exprMod(value ast.Expr) *ast.Module {
	return module(&ast.ExprStmt{Value: value})
}

// BoolOp -------------------------------------------------------------

func TestBoolOpAndShortCircuits(t *testing.T) {
	// x and y
	e := &ast.BoolOp{
		Op:     ast.And,
		Values: []ast.Expr{nameLoad("x"), nameLoad("y")},
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_NAME",         // x
		"COPY",              // dup for jump check
		"POP_JUMP_IF_FALSE", // -> end (x falsy: leave x)
		"POP_TOP",           // discard the dup
		"LOAD_NAME",         // y
		"POP_TOP",           // ExprStmt discard
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestBoolOpOrUsesPopJumpIfTrue(t *testing.T) {
	// x or y
	e := &ast.BoolOp{
		Op:     ast.Or,
		Values: []ast.Expr{nameLoad("x"), nameLoad("y")},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	if got[2] != "POP_JUMP_IF_TRUE" {
		t.Errorf("expected POP_JUMP_IF_TRUE for `or`, got %s", got[2])
	}
}

// BinOp / UnaryOp ----------------------------------------------------

func TestBinOpEmitsBinaryOp(t *testing.T) {
	// 1 + 2
	e := &ast.BinOp{Left: cnst(int64(1)), Op: ast.Add, Right: cnst(int64(2))}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_CONST", "LOAD_CONST", "BINARY_OP", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if got := u.Seq.Instrs[2].Oparg; got != nbAdd {
		t.Errorf("BINARY_OP oparg = %d, want NB_ADD (%d)", got, nbAdd)
	}
}

func TestUnaryNotEmitsToBoolThenUnaryNot(t *testing.T) {
	e := &ast.UnaryOp{Op: ast.Not, Operand: nameLoad("x")}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_NAME", "TO_BOOL", "UNARY_NOT", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestUnaryNegEmitsUnaryNegative(t *testing.T) {
	e := &ast.UnaryOp{Op: ast.USub, Operand: cnst(int64(1))}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	if got[1] != "UNARY_NEGATIVE" {
		t.Errorf("expected UNARY_NEGATIVE, got %s", got[1])
	}
}

func TestUnaryPlusIsIdentity(t *testing.T) {
	e := &ast.UnaryOp{Op: ast.UAdd, Operand: cnst(int64(1))}
	u := compileMod(t, exprMod(e))
	want := []string{"LOAD_CONST", "POP_TOP", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

// Compare ------------------------------------------------------------

func TestCompareSingleEmitsCompareOp(t *testing.T) {
	// a < b
	e := &ast.Compare{
		Left:        nameLoad("a"),
		Ops:         []ast.Cmpop{ast.Lt},
		Comparators: []ast.Expr{nameLoad("b")},
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_NAME", "LOAD_NAME", "COMPARE_OP", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestCompareIsUsesIsOp(t *testing.T) {
	e := &ast.Compare{
		Left:        nameLoad("a"),
		Ops:         []ast.Cmpop{ast.Is},
		Comparators: []ast.Expr{nameLoad("b")},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	if got[2] != "IS_OP" {
		t.Errorf("expected IS_OP, got %s", got[2])
	}
}

func TestCompareInUsesContainsOp(t *testing.T) {
	e := &ast.Compare{
		Left:        nameLoad("a"),
		Ops:         []ast.Cmpop{ast.NotIn},
		Comparators: []ast.Expr{nameLoad("b")},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	if got[2] != "CONTAINS_OP" {
		t.Errorf("expected CONTAINS_OP, got %s", got[2])
	}
	if u.Seq.Instrs[2].Oparg != 1 {
		t.Errorf("expected `not in` to set oparg=1, got %d", u.Seq.Instrs[2].Oparg)
	}
}

func TestCompareChainedEmitsSwapAndCleanup(t *testing.T) {
	// a < b < c
	e := &ast.Compare{
		Left: nameLoad("a"),
		Ops:  []ast.Cmpop{ast.Lt, ast.Lt},
		Comparators: []ast.Expr{
			nameLoad("b"),
			nameLoad("c"),
		},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	// Look for SWAP/COPY after the first operand of the chain.
	hasSwap, hasCopy := false, false
	for _, op := range got {
		if op == "SWAP" {
			hasSwap = true
		}
		if op == "COPY" {
			hasCopy = true
		}
	}
	if !hasSwap || !hasCopy {
		t.Errorf("expected SWAP and COPY in chained compare; got %v", got)
	}
}

// IfExp --------------------------------------------------------------

func TestIfExpEmitsConditional(t *testing.T) {
	// 1 if c else 2
	e := &ast.IfExp{
		Test:   nameLoad("c"),
		Body:   cnst(int64(1)),
		Orelse: cnst(int64(2)),
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_NAME",
		"POP_JUMP_IF_FALSE",
		"LOAD_CONST", // 1
		"JUMP",
		"LOAD_CONST", // 2
		"POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

// Containers ---------------------------------------------------------

func TestListEmitsBuildList(t *testing.T) {
	e := &ast.List{
		Elts: []ast.Expr{cnst(int64(1)), cnst(int64(2))},
		Ctx:  ast.Load,
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_CONST", "LOAD_CONST", "BUILD_LIST", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestTupleEmitsBuildTuple(t *testing.T) {
	e := &ast.Tuple{
		Elts: []ast.Expr{cnst(int64(1)), cnst(int64(2))},
		Ctx:  ast.Load,
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_CONST", "LOAD_CONST", "BUILD_TUPLE", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestSetEmitsBuildSet(t *testing.T) {
	e := &ast.Set{Elts: []ast.Expr{cnst(int64(1))}}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	if got[1] != "BUILD_SET" {
		t.Errorf("ops = %v, want BUILD_SET at index 1", got)
	}
}

func TestDictEmitsBuildMap(t *testing.T) {
	// {1: 2}
	e := &ast.Dict{
		Keys:   []ast.Expr{cnst(int64(1))},
		Values: []ast.Expr{cnst(int64(2))},
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_CONST", "LOAD_CONST", "BUILD_MAP", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestDictWithSplatUsesDictUpdate(t *testing.T) {
	// {**x}
	e := &ast.Dict{
		Keys:   []ast.Expr{nil},
		Values: []ast.Expr{nameLoad("x")},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	if got[0] != "BUILD_MAP" || got[1] != "LOAD_NAME" || got[2] != "DICT_UPDATE" {
		t.Errorf("ops = %v, want BUILD_MAP / LOAD_NAME / DICT_UPDATE prefix", got)
	}
}

func TestListWithStarUsesListExtend(t *testing.T) {
	// [1, *xs]
	e := &ast.List{
		Elts: []ast.Expr{
			cnst(int64(1)),
			&ast.Starred{Value: nameLoad("xs"), Ctx: ast.Load},
		},
		Ctx: ast.Load,
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	hasExtend := false
	for _, op := range got {
		if op == "LIST_EXTEND" {
			hasExtend = true
		}
	}
	if !hasExtend {
		t.Errorf("expected LIST_EXTEND in ops; got %v", got)
	}
}

// Attribute / Subscript / Slice --------------------------------------

func TestAttributeLoadEmitsLoadAttr(t *testing.T) {
	// x.y
	e := &ast.Attribute{Value: nameLoad("x"), Attr: "y", Ctx: ast.Load}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_NAME", "LOAD_ATTR", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestSubscriptLoadEmitsBinaryOpSubscr(t *testing.T) {
	// x[0]
	e := &ast.Subscript{
		Value: nameLoad("x"),
		Slice: cnst(int64(0)),
		Ctx:   ast.Load,
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	want := []string{
		"LOAD_NAME", "LOAD_CONST", "BINARY_OP", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[2].Oparg != nbSubscr {
		t.Errorf("expected NB_SUBSCR oparg, got %d", u.Seq.Instrs[2].Oparg)
	}
}

func TestSliceEmitsBuildSlice(t *testing.T) {
	// x[1:2]
	e := &ast.Subscript{
		Value: nameLoad("x"),
		Slice: &ast.Slice{Lower: cnst(int64(1)), Upper: cnst(int64(2))},
		Ctx:   ast.Load,
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	wantPrefix := []string{
		"LOAD_NAME", "LOAD_CONST", "LOAD_CONST", "BUILD_SLICE", "BINARY_OP",
	}
	if len(got) < len(wantPrefix) {
		t.Fatalf("ops too short: %v", got)
	}
	for i, op := range wantPrefix {
		if got[i] != op {
			t.Errorf("ops[%d] = %s, want %s (full=%v)", i, got[i], op, got)
		}
	}
}

// Call ---------------------------------------------------------------

func TestCallPlainEmitsCall(t *testing.T) {
	// f(1, 2)
	e := &ast.Call{
		Func: nameLoad("f"),
		Args: []ast.Expr{cnst(int64(1)), cnst(int64(2))},
	}
	u := compileMod(t, exprMod(e))
	want := []string{
		"LOAD_NAME", "LOAD_CONST", "LOAD_CONST", "CALL", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[3].Oparg != 2 {
		t.Errorf("CALL oparg = %d, want 2", u.Seq.Instrs[3].Oparg)
	}
}

func TestCallKwEmitsCallKw(t *testing.T) {
	// f(x=1)
	kwName := "x"
	e := &ast.Call{
		Func:     nameLoad("f"),
		Keywords: []*ast.Keyword{{Arg: &kwName, Value: cnst(int64(1))}},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",  // f
		"LOAD_CONST", // 1
		"LOAD_CONST", // ("x",) tuple
		"CALL_KW",
		"POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestCallStarUsesCallFunctionEx(t *testing.T) {
	// f(*xs)
	e := &ast.Call{
		Func: nameLoad("f"),
		Args: []ast.Expr{&ast.Starred{Value: nameLoad("xs"), Ctx: ast.Load}},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	hasEx := false
	for _, op := range got {
		if op == "CALL_FUNCTION_EX" {
			hasEx = true
		}
	}
	if !hasEx {
		t.Errorf("expected CALL_FUNCTION_EX; got %v", got)
	}
}

func TestCallStarStarUsesCallFunctionExWithFlag(t *testing.T) {
	// f(**kw)
	e := &ast.Call{
		Func:     nameLoad("f"),
		Keywords: []*ast.Keyword{{Arg: nil, Value: nameLoad("kw")}},
	}
	u := compileMod(t, exprMod(e))
	got := opNames(u)
	exIdx := -1
	for i, op := range got {
		if op == "CALL_FUNCTION_EX" {
			exIdx = i
		}
	}
	if exIdx < 0 {
		t.Fatalf("expected CALL_FUNCTION_EX in ops; got %v", got)
	}
	if u.Seq.Instrs[exIdx].Oparg&1 == 0 {
		t.Errorf("CALL_FUNCTION_EX oparg low bit must be set when **kw present, got %d",
			u.Seq.Instrs[exIdx].Oparg)
	}
}
