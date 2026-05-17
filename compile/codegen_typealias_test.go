package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestTypeAliasNonGeneric covers `type X = int`. Expected shape:
//
//	LOAD_CONST "X"
//	LOAD_CONST None
//	LOAD_NAME int
//	CALL_INTRINSIC_1 INTRINSIC_TYPEALIAS
//	STORE_NAME X
//	LOAD_CONST None / RETURN_VALUE      (module trailer)
func TestTypeAliasNonGeneric(t *testing.T) {
	ta := &ast.TypeAlias{
		Name:  &ast.Name{Id: "X", Ctx: ast.Store},
		Value: nameLoad("int"),
	}
	u := compileMod(t, module(ta))
	want := []string{
		"LOAD_CONST",       // "X"
		"LOAD_CONST",       // None
		"LOAD_NAME",        // int
		"CALL_INTRINSIC_1", // TYPEALIAS
		"STORE_NAME",       // X
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	if op := u.Seq.Instrs[4]; op.Op != CALL_INTRINSIC_1 || op.Oparg != intrinsicTypeAlias {
		t.Errorf("CALL_INTRINSIC_1 oparg = %d, want %d (TYPEALIAS)",
			op.Oparg, intrinsicTypeAlias)
	}
}

// TestTypeAliasGenericRejected pins the v0.5 deferral: generic type
// aliases (with TypeParams) error out until the lazy-thunk wrapper
// lands in v0.6.
func TestTypeAliasGenericRejected(t *testing.T) {
	ta := &ast.TypeAlias{
		Name:       &ast.Name{Id: "X", Ctx: ast.Store},
		TypeParams: []ast.TypeParam{&ast.TypeVar{Name: "T"}},
		Value:      nameLoad("int"),
	}
	m := module(ta)
	st := mustBuildSym(t, m)
	c := NewCompiler("<test>", 0, nil, st)
	if _, err := c.Codegen(st.Top, m); err == nil {
		t.Fatal("expected error for generic type alias, got nil")
	}
}
