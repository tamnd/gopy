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
