package ast

import (
	"math/big"
	"strings"
	"testing"
)

func TestValidateNilModule(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("Validate(nil) = nil, want error")
	}
}

func TestValidateEmptyModule(t *testing.T) {
	if err := Validate(&Module{}); err != nil {
		t.Fatalf("empty Module: %v", err)
	}
}

func TestValidatePosNegative(t *testing.T) {
	bad := Pos{Lineno: -2, ColOffset: 0, EndLineno: 1, EndColOffset: 0}
	if err := validatePos(bad); err == nil {
		t.Fatal("expected error for negative lineno")
	}
}

func TestValidatePosNoPosOK(t *testing.T) {
	if err := validatePos(NoPos); err != nil {
		t.Fatalf("NoPos: %v", err)
	}
}

func TestValidatePosEndBeforeStart(t *testing.T) {
	bad := Pos{Lineno: 5, EndLineno: 3}
	if err := validatePos(bad); err == nil {
		t.Fatal("expected error for end_lineno < lineno")
	}
}

func TestValidatePosEndColBeforeStartCol(t *testing.T) {
	bad := Pos{Lineno: 1, ColOffset: 10, EndLineno: 1, EndColOffset: 5}
	if err := validatePos(bad); err == nil {
		t.Fatal("expected error for end_col_offset < col_offset")
	}
}

func TestValidateImportFromNegativeLevel(t *testing.T) {
	neg := -1
	imp := &ImportFrom{Level: &neg}
	err := validateStmt(imp)
	if err == nil || !strings.Contains(err.Error(), "Negative ImportFrom level") {
		t.Fatalf("err = %v, want Negative ImportFrom level", err)
	}
}

func TestValidateImportFromZeroLevel(t *testing.T) {
	zero := 0
	imp := &ImportFrom{Level: &zero, Names: Seq[*Alias]{{Name: "x"}}}
	if err := validateStmt(imp); err != nil {
		t.Fatalf("level=0: %v", err)
	}
}

func TestValidateConstantAcceptedKinds(t *testing.T) {
	values := []any{
		nil, Ellipsis, true, false,
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(1), uint8(2), uint16(3), uint32(4), uint64(5),
		float32(1.5), float64(2.5),
		complex64(1 + 2i), complex128(3 + 4i),
		"s", []byte("b"),
		big.NewInt(1 << 62),
		[]any{int(1), "s", []any{true}},
		FrozenSet{int(1), "x"},
	}
	for _, v := range values {
		if err := validateConstant(v); err != nil {
			t.Errorf("validateConstant(%T) = %v", v, err)
		}
	}
}

func TestValidateConstantRejectsInvalidType(t *testing.T) {
	type weird struct{}
	err := validateConstant(weird{})
	if err == nil || !strings.Contains(err.Error(), "got an invalid type in Constant") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateConstantRejectsNestedInvalid(t *testing.T) {
	type weird struct{}
	if err := validateConstant([]any{1, weird{}}); err == nil {
		t.Fatal("expected nested error")
	}
	if err := validateConstant(FrozenSet{weird{}}); err == nil {
		t.Fatal("expected nested error in frozenset")
	}
}

func TestValidateExpressionMode(t *testing.T) {
	expr := &Expression{Body: &Constant{Value: "ok"}}
	if err := Validate(expr); err != nil {
		t.Fatalf("Expression: %v", err)
	}
}

func TestValidateExpressionRejectsBadConstant(t *testing.T) {
	type weird struct{}
	expr := &Expression{Body: &Constant{Value: weird{}}}
	if err := Validate(expr); err == nil {
		t.Fatal("expected invalid-type error")
	}
}

func TestValidateModuleStmt(t *testing.T) {
	m := &Module{Body: Seq[Stmt]{
		&ExprStmt{Value: &Constant{Value: "doc"}},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("Module: %v", err)
	}
}

func TestValidateNilStmtRejected(t *testing.T) {
	m := &Module{Body: Seq[Stmt]{nil}}
	if err := Validate(m); err == nil {
		t.Fatal("expected nil-statement error")
	}
}

func TestValidateNilExprRejected(t *testing.T) {
	if err := Validate(&Expression{Body: nil}); err == nil {
		t.Fatal("expected nil-expression error")
	}
}

func TestValidateFunctionType(t *testing.T) {
	ft := &FunctionType{
		Argtypes: Seq[Expr]{&Constant{Value: "int"}},
		Returns:  &Constant{Value: "None"},
	}
	if err := Validate(ft); err != nil {
		t.Fatalf("FunctionType: %v", err)
	}
}
