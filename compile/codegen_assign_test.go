package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// helpers ------------------------------------------------------------

func tupleStore(elts ...ast.Expr) *ast.Tuple {
	return &ast.Tuple{Elts: elts, Ctx: ast.Store}
}

func listStore(elts ...ast.Expr) *ast.List {
	return &ast.List{Elts: elts, Ctx: ast.Store}
}

func starStore(inner ast.Expr) *ast.Starred {
	return &ast.Starred{Value: inner, Ctx: ast.Store}
}

func attrStore(value ast.Expr, attr string) *ast.Attribute {
	return &ast.Attribute{Value: value, Attr: attr, Ctx: ast.Store}
}

func subscriptStore(value, slice ast.Expr) *ast.Subscript {
	return &ast.Subscript{Value: value, Slice: slice, Ctx: ast.Store}
}

// Attribute / Subscript stores ---------------------------------------

func TestAssignAttributeEmitsStoreAttr(t *testing.T) {
	// x.y = 1
	a := &ast.Assign{
		Targets: []ast.Expr{attrStore(nameLoad("x"), "y")},
		Value:   cnst(int64(1)),
	}
	u := compileMod(t, module(a))
	want := []string{
		"LOAD_CONST", // 1
		"LOAD_NAME",  // x
		"STORE_ATTR", // .y
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestAssignSubscriptEmitsStoreSubscr(t *testing.T) {
	// x[i] = 1
	a := &ast.Assign{
		Targets: []ast.Expr{subscriptStore(nameLoad("x"), nameLoad("i"))},
		Value:   cnst(int64(1)),
	}
	u := compileMod(t, module(a))
	want := []string{
		"LOAD_CONST",   // 1
		"LOAD_NAME",    // x
		"LOAD_NAME",    // i
		"STORE_SUBSCR", //
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

// Tuple / List unpack ------------------------------------------------

func TestAssignTupleEmitsUnpackSequence(t *testing.T) {
	// a, b = (1, 2)
	rhs := &ast.Tuple{
		Elts: []ast.Expr{cnst(int64(1)), cnst(int64(2))},
		Ctx:  ast.Load,
	}
	a := &ast.Assign{
		Targets: []ast.Expr{tupleStore(nameStore("a"), nameStore("b"))},
		Value:   rhs,
	}
	u := compileMod(t, module(a))
	want := []string{
		"LOAD_CONST",      // 1
		"LOAD_CONST",      // 2
		"BUILD_TUPLE",     // (1, 2)
		"UNPACK_SEQUENCE", // n=2
		"STORE_NAME",      // a
		"STORE_NAME",      // b
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[3].Oparg != 2 {
		t.Errorf("UNPACK_SEQUENCE oparg = %d, want 2", u.Seq.Instrs[3].Oparg)
	}
}

func TestAssignListEmitsUnpackSequence(t *testing.T) {
	// [a, b] = expr
	a := &ast.Assign{
		Targets: []ast.Expr{listStore(nameStore("a"), nameStore("b"))},
		Value:   nameLoad("seq"),
	}
	u := compileMod(t, module(a))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",       // seq
		"UNPACK_SEQUENCE", // n=2
		"STORE_NAME",      // a
		"STORE_NAME",      // b
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestAssignWithStarUsesUnpackEx(t *testing.T) {
	// `a, *b, c = expr` should emit UNPACK_EX with before=1 after=1.
	a := &ast.Assign{
		Targets: []ast.Expr{tupleStore(
			nameStore("a"),
			starStore(nameStore("b")),
			nameStore("c"),
		)},
		Value: nameLoad("seq"),
	}
	u := compileMod(t, module(a))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",  // seq
		"UNPACK_EX",  //
		"STORE_NAME", // a
		"STORE_NAME", // b (star)
		"STORE_NAME", // c
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	// CPython packs (after << 8) | before. For a, *b, c: before=1 after=1.
	want_oparg := int32((1 << 8) | 1)
	if u.Seq.Instrs[1].Oparg != want_oparg {
		t.Errorf("UNPACK_EX oparg = %#x, want %#x", u.Seq.Instrs[1].Oparg, want_oparg)
	}
}

func TestAssignNestedTupleEmitsNestedUnpack(t *testing.T) {
	// (a, (b, c)) = expr
	inner := tupleStore(nameStore("b"), nameStore("c"))
	a := &ast.Assign{
		Targets: []ast.Expr{tupleStore(nameStore("a"), inner)},
		Value:   nameLoad("seq"),
	}
	u := compileMod(t, module(a))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",       // seq
		"UNPACK_SEQUENCE", // outer n=2
		"STORE_NAME",      // a
		"UNPACK_SEQUENCE", // inner n=2
		"STORE_NAME",      // b
		"STORE_NAME",      // c
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}
