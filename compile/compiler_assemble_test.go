package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestOptimizeAndAssembleCodeUnitSmoke drives the C.3 four-call closer
// against a tiny module-shaped Unit (LOAD_CONST None + RETURN_VALUE).
// The driver should produce a Code with the same Stacksize the cfg
// stackdepth pass reports and a non-empty bytecode slice. Wired up
// before D.1 flips assembleUnit, so the function has a live caller.
func TestOptimizeAndAssembleCodeUnitSmoke(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})

	unit := &Unit{
		Name:        "<m>",
		Qualname:    "<m>",
		FirstLineno: 1,
		Seq:         seq,
		Consts:      []any{nil},
		FastHidden:  map[string]bool{},
	}

	co, err := optimizeAndAssembleCodeUnit(unit, 0, "<test>")
	if err != nil {
		t.Fatalf("optimizeAndAssembleCodeUnit: %v", err)
	}
	if len(co.Code) == 0 {
		t.Fatal("empty bytecode")
	}
	if co.Stacksize < 1 {
		t.Errorf("Stacksize = %d, want >= 1", co.Stacksize)
	}
	if co.Filename != "<test>" {
		t.Errorf("Filename = %q, want <test>", co.Filename)
	}
	if co.Name != "<m>" {
		t.Errorf("Name = %q, want <m>", co.Name)
	}
}
