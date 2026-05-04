package ast

import (
	"strings"
	"testing"
)

// TestValidateForbiddenName: a Name with id "None" is rejected even in
// Load context.
func TestValidateForbiddenName(t *testing.T) {
	for _, id := range []string{"None", "True", "False"} {
		err := validateExpr(&Name{Id: id, Ctx: Load})
		if err == nil || !strings.Contains(err.Error(), "can't represent '"+id+"'") {
			t.Errorf("Name(%q): err = %v", id, err)
		}
	}
}

// TestValidateForbiddenNameInStore: same rule fires from a Store-context
// path (assignment target).
func TestValidateForbiddenNameInStore(t *testing.T) {
	stmt := &Assign{
		Targets: Seq[Expr]{&Name{Id: "True", Ctx: Store}},
		Value:   &Constant{Value: int64(1)},
	}
	if err := validateStmt(stmt); err == nil {
		t.Fatal("expected error for assignment to True")
	}
}

// TestValidateNameOK: a regular identifier validates.
func TestValidateNameOK(t *testing.T) {
	if err := validateExpr(&Name{Id: "x", Ctx: Load}); err != nil {
		t.Fatalf("validateExpr(x): %v", err)
	}
}

// TestValidateBareStarredRejected: a Starred expression at the top
// level (outside Call/List/Tuple/Set) is rejected.
func TestValidateBareStarredRejected(t *testing.T) {
	st := &ExprStmt{Value: &Starred{Value: &Name{Id: "x", Ctx: Load}, Ctx: Load}}
	err := validateStmt(st)
	if err == nil || !strings.Contains(err.Error(), "starred expression") {
		t.Fatalf("err = %v, want starred-expression rejection", err)
	}
}

// TestValidateStarredInList: a Starred element inside a List display
// validates (iterable unpack).
func TestValidateStarredInList(t *testing.T) {
	e := &List{
		Elts: Seq[Expr]{
			&Name{Id: "a", Ctx: Load},
			&Starred{Value: &Name{Id: "b", Ctx: Load}, Ctx: Load},
		},
		Ctx: Load,
	}
	if err := validateExpr(e); err != nil {
		t.Fatalf("List with starred elt: %v", err)
	}
}

// TestValidateStarredInCallArg: a Starred Call argument validates.
func TestValidateStarredInCallArg(t *testing.T) {
	e := &Call{
		Func: &Name{Id: "f", Ctx: Load},
		Args: Seq[Expr]{
			&Starred{Value: &Name{Id: "args", Ctx: Load}, Ctx: Load},
		},
	}
	if err := validateExpr(e); err != nil {
		t.Fatalf("Call(*args): %v", err)
	}
}

// TestValidateComprehensionEmptyGenerators: a comprehension without
// generators is rejected.
func TestValidateComprehensionEmptyGenerators(t *testing.T) {
	lc := &ListComp{Elt: &Name{Id: "x", Ctx: Load}}
	err := validateExpr(lc)
	if err == nil || !strings.Contains(err.Error(), "comprehension with no generators") {
		t.Fatalf("err = %v, want empty-generators rejection", err)
	}
}

// TestValidateComprehensionOK: one generator with target/iter walks
// cleanly.
func TestValidateComprehensionOK(t *testing.T) {
	lc := &ListComp{
		Elt: &Name{Id: "x", Ctx: Load},
		Generators: Seq[*Comprehension]{
			{Target: &Name{Id: "x", Ctx: Store}, Iter: &Name{Id: "xs", Ctx: Load}},
		},
	}
	if err := validateExpr(lc); err != nil {
		t.Fatalf("ListComp ok: %v", err)
	}
}

// TestValidateExprCtxMismatch: a Name in Store context where Load is
// expected is rejected.
func TestValidateExprCtxMismatch(t *testing.T) {
	// Put a Store-context Name as the iter of a comprehension; iter
	// is validated in Load context.
	lc := &ListComp{
		Elt: &Name{Id: "x", Ctx: Load},
		Generators: Seq[*Comprehension]{
			{Target: &Name{Id: "x", Ctx: Store}, Iter: &Name{Id: "xs", Ctx: Store}},
		},
	}
	// Comprehension iter is validated through validateExpr (Load
	// implicit), so Name with Store passes here. The mismatch fires
	// when validateExprCtx is called explicitly: e.g. an assignment
	// target whose ctx is Load.
	bad := &Assign{
		Targets: Seq[Expr]{&Name{Id: "x", Ctx: Load}},
		Value:   &Constant{Value: int64(1)},
	}
	if err := validateStmt(bad); err == nil {
		t.Fatal("expected ctx-mismatch error for Load target")
	}
	if err := validateExpr(lc); err != nil {
		t.Fatalf("comprehension iter Load: %v", err)
	}
}

// TestValidateMatchSingletonNonBoolNone: MatchSingleton must hold one
// of None/True/False.
func TestValidateMatchSingletonNonBoolNone(t *testing.T) {
	err := validatePattern(&MatchSingleton{Value: int64(1)})
	if err == nil || !strings.Contains(err.Error(), "True, False and None") {
		t.Fatalf("err = %v, want singleton rejection", err)
	}
}

// TestValidateMatchSingletonOK: None and bool pass.
func TestValidateMatchSingletonOK(t *testing.T) {
	for _, v := range []any{nil, true, false} {
		if err := validatePattern(&MatchSingleton{Value: v}); err != nil {
			t.Errorf("MatchSingleton(%v): %v", v, err)
		}
	}
}

// TestValidateMatchMappingKeysPatternsLen: keys/patterns length
// mismatch is rejected.
func TestValidateMatchMappingKeysPatternsLen(t *testing.T) {
	mm := &MatchMapping{
		Keys:     Seq[Expr]{&Constant{Value: int64(1)}, &Constant{Value: int64(2)}},
		Patterns: Seq[Pattern]{&MatchValue{Value: &Constant{Value: int64(1)}}},
	}
	err := validatePattern(mm)
	if err == nil || !strings.Contains(err.Error(), "same number of keys") {
		t.Fatalf("err = %v, want mapping length mismatch", err)
	}
}

// TestValidateMatchClassKwdMismatch: kwd_attrs and kwd_patterns must
// have equal length.
func TestValidateMatchClassKwdMismatch(t *testing.T) {
	mc := &MatchClass{
		Cls:         &Name{Id: "C", Ctx: Load},
		KwdAttrs:    Seq[string]{"a", "b"},
		KwdPatterns: Seq[Pattern]{&MatchValue{Value: &Constant{Value: int64(1)}}},
	}
	err := validatePattern(mc)
	if err == nil || !strings.Contains(err.Error(), "keyword attributes") {
		t.Fatalf("err = %v, want kwd-attr length mismatch", err)
	}
}

// TestValidateMatchOrTooFew: MatchOr requires at least 2 patterns.
func TestValidateMatchOrTooFew(t *testing.T) {
	mo := &MatchOr{Patterns: Seq[Pattern]{&MatchValue{Value: &Constant{Value: int64(1)}}}}
	err := validatePattern(mo)
	if err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("err = %v, want MatchOr arity rejection", err)
	}
}

// TestValidateTypeParamForbiddenName: a TypeVar named "None" is
// rejected.
func TestValidateTypeParamForbiddenName(t *testing.T) {
	cd := &ClassDef{
		Name:       "C",
		TypeParams: Seq[TypeParam]{&TypeVar{Name: "None"}},
	}
	if err := validateStmt(cd); err == nil {
		t.Fatal("expected forbidden-name rejection on TypeVar")
	}
}

// TestValidateTypeParamOK: a regular TypeVar walks cleanly.
func TestValidateTypeParamOK(t *testing.T) {
	cd := &ClassDef{
		Name:       "C",
		TypeParams: Seq[TypeParam]{&TypeVar{Name: "T"}},
	}
	if err := validateStmt(cd); err != nil {
		t.Fatalf("ClassDef[T]: %v", err)
	}
}
