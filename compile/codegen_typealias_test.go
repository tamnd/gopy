package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestTypeAliasNonGeneric covers `type X = int`. Mirrors CPython 3.14's
// shape: name + None (no type params) + a function-wrapped compute_value
// thunk, packed into a 3-tuple, then handed to INTRINSIC_TYPEALIAS.
//
// CPython: Python/codegen.c:1727 codegen_typealias
func TestTypeAliasNonGeneric(t *testing.T) {
	ta := &ast.TypeAlias{
		Name:  &ast.Name{Id: "X", Ctx: ast.Store},
		Value: nameLoad("int"),
	}
	u := compileMod(t, module(ta))
	want := []string{
		"LOAD_CONST",       // "X"
		"LOAD_CONST",       // None (no type params)
		"LOAD_CONST",       // compute_value code object
		"MAKE_FUNCTION",    // wrap into the thunk
		"BUILD_TUPLE",      // (name, type_params, compute_value)
		"CALL_INTRINSIC_1", // TYPEALIAS
		"STORE_NAME",       // X
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	if op := u.Seq.Instrs[6]; op.Op != CALL_INTRINSIC_1 || op.Oparg != intrinsicTypeAlias {
		t.Errorf("CALL_INTRINSIC_1 oparg = %d, want %d (TYPEALIAS)",
			op.Oparg, intrinsicTypeAlias)
	}
}
