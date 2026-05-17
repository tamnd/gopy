package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// COMPARE_OP oparg encoding: bits 5..7 = comparison kind, bits 0..3 =
// COMPARISON_BIT mask. Mirrors compile/codegen_expr_op.go:cmpXX.
const (
	cmpopLt    = (0 << 5) | 2
	cmpopLtE   = (1 << 5) | (2 | 8)
	cmpopEq    = (2 << 5) | 8
	cmpopNotEq = (3 << 5) | 7
	cmpopGt    = (4 << 5) | 4
	cmpopGtE   = (5 << 5) | (4 | 8)
)

func runCompareOp(t *testing.T, op compile.Opcode, lhs, rhs objects.Object, oparg byte) objects.Object {
	t.Helper()
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.COMPARE_OP, oparg),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{lhs, rhs},
		Stacksize: 4,
	}
	specialize.Enable(co)
	// LOAD_CONST takes 1 codeunit, no cache, so COMPARE_OP sits at idx 2.
	idx := 2
	specialize.SetOpcode(co.Code, idx, op)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	return v
}

func TestFastCompareOpInt(t *testing.T) {
	cases := []struct {
		name  string
		l, r  int64
		oparg byte
		want  objects.Object
	}{
		{"3<7", 3, 7, cmpopLt, objects.True()},
		{"7<3", 7, 3, cmpopLt, objects.False()},
		{"5==5", 5, 5, cmpopEq, objects.True()},
		{"5!=5", 5, 5, cmpopNotEq, objects.False()},
		{"4>=4", 4, 4, cmpopGtE, objects.True()},
		{"3>4", 3, 4, cmpopGt, objects.False()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runCompareOp(t, compile.COMPARE_OP_INT, objects.NewInt(c.l), objects.NewInt(c.r), c.oparg)
			if got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestFastCompareOpFloat(t *testing.T) {
	cases := []struct {
		name  string
		l, r  float64
		oparg byte
		want  objects.Object
	}{
		{"1.5<2.5", 1.5, 2.5, cmpopLt, objects.True()},
		{"2.5<=2.5", 2.5, 2.5, cmpopLtE, objects.True()},
		{"3.0==3.0", 3.0, 3.0, cmpopEq, objects.True()},
		{"3.0!=4.0", 3.0, 4.0, cmpopNotEq, objects.True()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runCompareOp(t, compile.COMPARE_OP_FLOAT, objects.NewFloat(c.l), objects.NewFloat(c.r), c.oparg)
			if got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestFastCompareOpStr(t *testing.T) {
	if got := runCompareOp(t, compile.COMPARE_OP_STR, objects.NewStr("a"), objects.NewStr("a"), cmpopEq); got != objects.True() {
		t.Errorf("a==a: got %v want True", got)
	}
	if got := runCompareOp(t, compile.COMPARE_OP_STR, objects.NewStr("a"), objects.NewStr("b"), cmpopEq); got != objects.False() {
		t.Errorf("a==b: got %v want False", got)
	}
	if got := runCompareOp(t, compile.COMPARE_OP_STR, objects.NewStr("a"), objects.NewStr("b"), cmpopNotEq); got != objects.True() {
		t.Errorf("a!=b: got %v want True", got)
	}
}

// Mismatching operand type forces the fast-path arm to bail and the
// dispatcher deopts back to COMPARE_OP; the generic body still
// produces the right boolean.
func TestFastCompareOpGuardMissDeopts(t *testing.T) {
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.COMPARE_OP, cmpopEq),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.NewStr("x"), objects.NewStr("x")},
		Stacksize: 4,
	}
	specialize.Enable(co)
	idx := 2
	specialize.SetOpcode(co.Code, idx, compile.COMPARE_OP_INT)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if v != objects.True() {
		t.Errorf("got %v want True", v)
	}
	if op := compile.Opcode(co.Code[2*idx]); op == compile.COMPARE_OP_INT {
		t.Errorf("guard miss should have deopted, but op stayed COMPARE_OP_INT")
	}
}
