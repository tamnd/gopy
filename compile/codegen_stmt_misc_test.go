package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// Delete -------------------------------------------------------------

func TestDeleteNameEmitsDeleteName(t *testing.T) {
	d := &ast.Delete{Targets: []ast.Expr{
		&ast.Name{Id: "x", Ctx: ast.Del},
	}}
	u := compileMod(t, module(d))
	want := []string{"DELETE_NAME", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestDeleteAttributeEmitsDeleteAttr(t *testing.T) {
	// del x.y
	d := &ast.Delete{Targets: []ast.Expr{
		&ast.Attribute{Value: nameLoad("x"), Attr: "y", Ctx: ast.Del},
	}}
	u := compileMod(t, module(d))
	got := opNames(u)
	want := []string{"LOAD_NAME", "DELETE_ATTR", "LOAD_CONST", "RETURN_VALUE"}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

// AugAssign ----------------------------------------------------------

func TestAugAssignNameEmitsLoadInPlaceStore(t *testing.T) {
	// x += 1
	a := &ast.AugAssign{
		Target: nameStore("x"),
		Op:     ast.Add,
		Value:  cnst(int64(1)),
	}
	u := compileMod(t, module(a))
	want := []string{
		"LOAD_NAME", "LOAD_CONST", "BINARY_OP", "STORE_NAME",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[3].Oparg != 13 {
		t.Errorf("BINARY_OP oparg = %d, want NB_INPLACE_ADD (13)", u.Seq.Instrs[3].Oparg)
	}
}

// AnnAssign ----------------------------------------------------------

func TestAnnAssignWithValueAssigns(t *testing.T) {
	// x: int = 1 — value lands in the local namespace; PEP 649 records
	// the annotation into the deferred list and the module body emits
	// MAKE_FUNCTION + STORE_NAME __annotate__ after visitStmts.
	a := &ast.AnnAssign{
		Target:     nameStore("x"),
		Annotation: nameLoad("int"),
		Value:      cnst(int64(1)),
	}
	u := compileMod(t, module(a))
	want := []string{
		"BUILD_SET", "STORE_NAME",
		"LOAD_CONST", "STORE_NAME",
		"LOAD_CONST", "MAKE_FUNCTION", "STORE_NAME",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if len(u.DeferredAnnotations) != 1 || u.DeferredAnnotations[0].Name != "x" {
		t.Errorf("DeferredAnnotations = %v, want one entry for x", u.DeferredAnnotations)
	}
}

func TestAnnAssignNoValueRecordsAnnotationOnly(t *testing.T) {
	// x: int — no store of x. The annotation lands in
	// DeferredAnnotations and the module body's __annotate__ install
	// follows.
	a := &ast.AnnAssign{
		Target:     nameStore("x"),
		Annotation: nameLoad("int"),
	}
	u := compileMod(t, module(a))
	want := []string{
		"BUILD_SET", "STORE_NAME",
		"LOAD_CONST", "MAKE_FUNCTION", "STORE_NAME",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if len(u.DeferredAnnotations) != 1 {
		t.Errorf("DeferredAnnotations = %v, want one entry", u.DeferredAnnotations)
	}
}

// Raise --------------------------------------------------------------

func TestRaiseBareEmitsRaiseVarargs0(t *testing.T) {
	u := compileMod(t, module(&ast.Raise{}))
	got := opNames(u)
	if got[0] != "RAISE_VARARGS" {
		t.Fatalf("ops = %v, want RAISE_VARARGS first", got)
	}
	if u.Seq.Instrs[1].Oparg != 0 {
		t.Errorf("RAISE_VARARGS oparg = %d, want 0", u.Seq.Instrs[1].Oparg)
	}
}

func TestRaiseExcEmitsRaiseVarargs1(t *testing.T) {
	u := compileMod(t, module(&ast.Raise{Exc: nameLoad("E")}))
	got := opNames(u)
	want := []string{"LOAD_NAME", "RAISE_VARARGS", "LOAD_CONST", "RETURN_VALUE"}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[2].Oparg != 1 {
		t.Errorf("RAISE_VARARGS oparg = %d, want 1", u.Seq.Instrs[2].Oparg)
	}
}

func TestRaiseFromEmitsRaiseVarargs2(t *testing.T) {
	u := compileMod(t, module(&ast.Raise{Exc: nameLoad("E"), Cause: nameLoad("C")}))
	got := opNames(u)
	want := []string{"LOAD_NAME", "LOAD_NAME", "RAISE_VARARGS", "LOAD_CONST", "RETURN_VALUE"}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if u.Seq.Instrs[3].Oparg != 2 {
		t.Errorf("RAISE_VARARGS oparg = %d, want 2", u.Seq.Instrs[3].Oparg)
	}
}

// Assert -------------------------------------------------------------

func TestAssertEmitsLoadCommonConstantAndRaise(t *testing.T) {
	a := &ast.Assert{Test: nameLoad("c")}
	u := compileMod(t, module(a))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",
		"TO_BOOL",
		"POP_JUMP_IF_TRUE",
		"LOAD_COMMON_CONSTANT",
		"RAISE_VARARGS",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestAssertWithMessageCallsConstructor(t *testing.T) {
	// assert c, "boom"
	a := &ast.Assert{Test: nameLoad("c"), Msg: cnst("boom")}
	u := compileMod(t, module(a))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",
		"TO_BOOL",
		"POP_JUMP_IF_TRUE",
		"LOAD_COMMON_CONSTANT",
		"LOAD_CONST", // "boom"
		"CALL",
		"RAISE_VARARGS",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

// Import / ImportFrom ------------------------------------------------

func TestSimpleImportEmitsImportName(t *testing.T) {
	// import os
	u := compileMod(t, module(&ast.Import{
		Names: []*ast.Alias{{Name: "os"}},
	}))
	got := opNames(u)
	want := []string{
		"LOAD_CONST", // level=0
		"LOAD_CONST", // fromlist=None
		"IMPORT_NAME",
		"STORE_NAME",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestImportAsBindsAlias(t *testing.T) {
	// import a.b as c
	asname := "c"
	u := compileMod(t, module(&ast.Import{
		Names: []*ast.Alias{{Name: "a.b", Asname: &asname}},
	}))
	got := opNames(u)
	hasImportFrom := false
	hasStore := false
	for _, op := range got {
		if op == "IMPORT_FROM" {
			hasImportFrom = true
		}
		if op == "STORE_NAME" {
			hasStore = true
		}
	}
	if !hasImportFrom {
		t.Errorf("expected IMPORT_FROM in ops; got %v", got)
	}
	if !hasStore {
		t.Errorf("expected STORE_NAME for alias bind; got %v", got)
	}
}

func TestFromImportEmitsImportFrom(t *testing.T) {
	// from a import b
	mod := "a"
	u := compileMod(t, module(&ast.ImportFrom{
		Module: &mod,
		Names:  []*ast.Alias{{Name: "b"}},
	}))
	got := opNames(u)
	want := []string{
		"LOAD_CONST", // level
		"LOAD_CONST", // fromlist=("b",)
		"IMPORT_NAME",
		"IMPORT_FROM",
		"STORE_NAME",
		"POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestFromImportStarUsesIntrinsic(t *testing.T) {
	// from m import *
	mod := "m"
	u := compileMod(t, module(&ast.ImportFrom{
		Module: &mod,
		Names:  []*ast.Alias{{Name: "*"}},
	}))
	got := opNames(u)
	hasIntrinsic := false
	for _, op := range got {
		if op == "CALL_INTRINSIC_1" {
			hasIntrinsic = true
		}
	}
	if !hasIntrinsic {
		t.Errorf("expected CALL_INTRINSIC_1 (IMPORT_STAR); got %v", got)
	}
}
