package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// intVal extracts the int64 from an *objects.Int, asserting it fits.
func intVal(i *objects.Int) int64 {
	v, _ := i.Int64()
	return v
}

func loadConstReturn(consts []any) *objects.Code {
	bc := append(instr(compile.RESUME, 0), instr(compile.LOAD_CONST, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	return &objects.Code{Code: bc, Stacksize: 4, Consts: consts}
}

func TestEvalLoadConstReturnInt(t *testing.T) {
	ts := state.NewThread()
	co := loadConstReturn([]any{int64(42)})
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", v)
	}
	if intVal(got) != 42 {
		t.Errorf("got %v, want 42", intVal(got))
	}
}

func TestEvalLoadConstReturnNone(t *testing.T) {
	ts := state.NewThread()
	co := loadConstReturn([]any{nil})
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if v != objects.None() {
		t.Errorf("got %v, want None", v)
	}
}

func TestEvalNop(t *testing.T) {
	ts := state.NewThread()
	bc := append(instr(compile.NOP, 0), instr(compile.LOAD_CONST, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(7)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 7 {
		t.Errorf("got %v, want 7", v)
	}
}

func TestEvalStoreLoadFast(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0; STORE_FAST 0; LOAD_FAST 0; RETURN_VALUE
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.STORE_FAST, 0)...)
	bc = append(bc, instr(compile.LOAD_FAST, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{
		Code:      bc,
		Stacksize: 4,
		Consts:    []any{int64(99)},
		Varnames:  []string{"x"},
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 99 {
		t.Errorf("got %v, want 99", v)
	}
}

func TestEvalLoadGlobal(t *testing.T) {
	ts := state.NewThread()
	// LOAD_GLOBAL 0 (no push_null), RETURN_VALUE
	bc := append(instr(compile.LOAD_GLOBAL, 0), instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{
		Code:      bc,
		Stacksize: 4,
		Names:     []string{"x"},
	}
	g := objects.NewDict()
	if err := g.SetItem(objects.NewStr("x"), objects.NewInt(123)); err != nil {
		t.Fatal(err)
	}
	v, err := EvalCode(ts, co, g, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 123 {
		t.Errorf("got %v, want 123", v)
	}
}

func TestEvalBinaryOpAdd(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (3); LOAD_CONST 1 (4); BINARY_OP nbAdd; RETURN_VALUE -> 7
	const nbAdd = 0
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.BINARY_OP, nbAdd)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(3), int64(4)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 7 {
		t.Errorf("got %v, want 7", v)
	}
}

// TestEvalBinaryOpIntArith pins the new NB_ sub-ops in one place so a
// regression flips one assertion at a time. Each row drives the
// dispatch through binaryOp with the chosen sub-op constant and
// checks the int (or float, for true division) result.
func TestEvalBinaryOpIntArith(t *testing.T) {
	cases := []struct {
		name     string
		sub      byte
		a, b     int64
		want     int64
		isFloat  bool
		wantFlt  float64
		wantsErr bool
	}{
		{name: "floordiv_pos", sub: 2, a: 7, b: 2, want: 3},
		{name: "floordiv_neg", sub: 2, a: -7, b: 2, want: -4},
		{name: "mod_pos", sub: 6, a: 7, b: 3, want: 1},
		{name: "mod_neg", sub: 6, a: -7, b: 3, want: 2},
		{name: "and", sub: 1, a: 0xF0, b: 0x33, want: 0x30},
		{name: "or", sub: 7, a: 0xF0, b: 0x0F, want: 0xFF},
		{name: "xor", sub: 12, a: 0xF0, b: 0x33, want: 0xC3},
		{name: "lshift", sub: 3, a: 1, b: 4, want: 16},
		{name: "rshift", sub: 9, a: 32, b: 2, want: 8},
		{name: "truediv", sub: 11, a: 7, b: 2, isFloat: true, wantFlt: 3.5},
		{name: "power", sub: 8, a: 2, b: 10, want: 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := state.NewThread()
			bc := append(instr(compile.LOAD_CONST, 0), instr(compile.LOAD_CONST, 1)...)
			bc = append(bc, instr(compile.BINARY_OP, tc.sub)...)
			bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
			co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{tc.a, tc.b}}
			v, err := EvalCode(ts, co, nil, nil)
			if err != nil {
				t.Fatalf("Eval err: %v", err)
			}
			if tc.isFloat {
				f, ok := v.(*objects.Float)
				if !ok {
					t.Fatalf("got %T, want *objects.Float", v)
				}
				if f.Float64() != tc.wantFlt {
					t.Errorf("got %v, want %v", f.Float64(), tc.wantFlt)
				}
				return
			}
			i, ok := v.(*objects.Int)
			if !ok || intVal(i) != tc.want {
				t.Errorf("got %v, want %d", v, tc.want)
			}
		})
	}
}

// TestEvalBinaryOpMixedNumeric pins the abstract-layer fall-through:
// when int's slot returns NotImplemented for an int + float pair,
// the dispatcher must try float's slot instead of treating the
// sentinel as a successful result.
func TestEvalBinaryOpMixedNumeric(t *testing.T) {
	ts := state.NewThread()
	const nbAdd = 0
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.BINARY_OP, nbAdd)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(1), 2.5}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	f, ok := v.(*objects.Float)
	if !ok {
		t.Fatalf("got %T, want *objects.Float", v)
	}
	if f.Float64() != 3.5 {
		t.Errorf("got %v, want 3.5", f.Float64())
	}
}

func TestEvalUnaryNot(t *testing.T) {
	ts := state.NewThread()
	// CPython 3.14 UNARY_NOT asserts PyStackRef_BoolCheck(value); the
	// compiler always emits a TO_BOOL before it. Mirror that here so
	// the input is the False singleton, not a raw int.
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.TO_BOOL, 0)...)
	bc = append(bc, instr(compile.UNARY_NOT, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(0)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if v != objects.True() {
		t.Errorf("got %v, want True", v)
	}
}

func TestEvalUnaryInvert(t *testing.T) {
	ts := state.NewThread()
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.UNARY_INVERT, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(5)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, ok := v.(*objects.Int)
	if !ok || intVal(got) != -6 {
		t.Errorf("got %v, want -6", v)
	}
}

// TestEvalInstrumentedReturnValue confirms an INSTRUMENTED_ opcode
// runs the base behavior when no monitoring tools are attached. Real
// monitoring lands with 1634 at v0.9.
func TestEvalInstrumentedReturnValue(t *testing.T) {
	ts := state.NewThread()
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.INSTRUMENTED_RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(7)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, ok := v.(*objects.Int)
	if !ok || intVal(got) != 7 {
		t.Errorf("got %v, want 7", v)
	}
}

func TestEvalIsOp(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (None); LOAD_CONST 0 (None); IS_OP 0 -> True
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.LOAD_CONST, 0)...)
	bc = append(bc, instr(compile.IS_OP, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{nil}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if v != objects.True() {
		t.Errorf("got %v, want True", v)
	}
}

func TestEvalBuildTuple(t *testing.T) {
	ts := state.NewThread()
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.LOAD_CONST, 2)...)
	bc = append(bc, instr(compile.BUILD_TUPLE, 3)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(1), int64(2), int64(3)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("got %T, want *objects.Tuple", v)
	}
	if tup.Len() != 3 {
		t.Errorf("len = %d, want 3", tup.Len())
	}
}

func TestEvalJumpForward(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (1); JUMP_FORWARD 1 (skip next instr); LOAD_CONST 1 (2); RETURN_VALUE
	// Result should be 1 since we skip the second LOAD_CONST.
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.JUMP_FORWARD, 1)...)
	bc = append(bc, instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(1), int64(2)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 1 {
		t.Errorf("got %v, want 1", v)
	}
}

func TestEvalPopJumpIfFalseTaken(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (False); POP_JUMP_IF_FALSE 1; LOAD_CONST 1 (1); RETURN_VALUE
	// Then at jump target: LOAD_CONST 2 (2); RETURN_VALUE -> 2
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.POP_JUMP_IF_FALSE, 2)...)
	bc = append(bc, instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	bc = append(bc, instr(compile.LOAD_CONST, 2)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{false, int64(1), int64(2)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 2 {
		t.Errorf("got %v, want 2 (jump taken)", v)
	}
}

func TestEvalPopJumpIfFalseNotTaken(t *testing.T) {
	ts := state.NewThread()
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.POP_JUMP_IF_FALSE, 2)...)
	bc = append(bc, instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	bc = append(bc, instr(compile.LOAD_CONST, 2)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{true, int64(1), int64(2)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 1 {
		t.Errorf("got %v, want 1 (jump not taken)", v)
	}
}

func TestEvalPopTopThenReturn(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (10); LOAD_CONST 1 (20); POP_TOP; RETURN_VALUE -> 10
	bc := append(instr(compile.LOAD_CONST, 0), instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.POP_TOP, 0)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{Code: bc, Stacksize: 4, Consts: []any{int64(10), int64(20)}}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	if got, ok := v.(*objects.Int); !ok || intVal(got) != 10 {
		t.Errorf("got %v, want 10", v)
	}
}
