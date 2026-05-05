package vm

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// codeWithBytecode builds a minimal Code object whose Code blob is bc.
func codeWithBytecode(bc []byte) *objects.Code {
	return &objects.Code{Code: bc, Stacksize: 4}
}

// instr packs an opcode + oparg byte pair.
func instr(op compile.Opcode, arg byte) []byte { return []byte{byte(op), arg} }

func TestEvalNotImplementedSurface(t *testing.T) {
	ts := state.NewThread()
	// BINARY_OP is not in the hand-written panel, so dispatch should
	// fall through to ErrNotImplemented.
	co := codeWithBytecode(instr(compile.LOAD_DEREF, 0))
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected ErrNotImplemented for ungenerated dispatch")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("err = %v, want wraps ErrNotImplemented", err)
	}
}

func TestEvalErrorMentionsOpcodeName(t *testing.T) {
	ts := state.NewThread()
	co := codeWithBytecode(instr(compile.LOAD_DEREF, 0))
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !contains(got, "LOAD_DEREF") {
		t.Errorf("error %q should mention BINARY_OP", got)
	}
}

func TestEvalEmptyCodeFails(t *testing.T) {
	ts := state.NewThread()
	co := codeWithBytecode(nil)
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestThreadVMLazyInit(t *testing.T) {
	ts := state.NewThread()
	b1 := BreakerFor(ts)
	b2 := BreakerFor(ts)
	if b1 != b2 {
		t.Error("BreakerFor must return the same breaker for the same thread")
	}
	p := PendingFor(ts)
	if p == nil {
		t.Error("PendingFor must not be nil")
	}
}

func TestEvalExtendedArgFetch(t *testing.T) {
	ts := state.NewThread()
	// EXTENDED_ARG 0x01, then BINARY_OP 0x02 -> oparg should be 0x0102.
	// BINARY_OP is unimplemented so we expect ErrNotImplemented to bubble.
	bc := append(instr(compile.EXTENDED_ARG, 1), instr(compile.LOAD_DEREF, 2)...)
	co := codeWithBytecode(bc)

	_, err := EvalCode(ts, co, nil, nil)
	if err == nil || !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented after EXTENDED_ARG fetch, got %v", err)
	}
	// We can't observe oparg directly without a real arm, but at least the
	// fetch must not have looped past the end.
}

func TestEvalCall(t *testing.T) {
	ts := state.NewThread()
	doubled := objects.NewBuiltinFunction("doubled", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		x, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(x * 2), nil
	})
	co := &objects.Code{
		// LOAD_CONST 0 (callable), PUSH_NULL, LOAD_CONST 1 (5), CALL 1, RETURN_VALUE
		Code: append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.CALL, 1)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{doubled, int64(5)},
		Stacksize: 8,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 10 {
		t.Errorf("got %d, want 10", got)
	}
}

func TestEvalUnpackSequence(t *testing.T) {
	ts := state.NewThread()
	// Build tuple (1, 2), UNPACK_SEQUENCE 2, BUILD_TUPLE 2, RETURN_VALUE.
	co := &objects.Code{
		Code: append(append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.UNPACK_SEQUENCE, 2)...),
			instr(compile.BUILD_TUPLE, 2)...),
			instr(compile.RETURN_VALUE, 0)...),
			[]byte{}...),
			[]byte{}...),
		Consts:    []any{objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("got %T, want *objects.Tuple", v)
	}
	if tup.Len() != 2 {
		t.Errorf("got len %d, want 2", tup.Len())
	}
}

func TestEvalMakeFunctionAndCall(t *testing.T) {
	ts := state.NewThread()
	// Inner code: takes one arg "x", returns x.
	// LOAD_FAST 0, RETURN_VALUE.
	inner := &objects.Code{
		Code: append(
			instr(compile.LOAD_FAST, 0),
			instr(compile.RETURN_VALUE, 0)...),
		Varnames:  []string{"x"},
		Stacksize: 4,
		Name:      "id",
	}
	inner.Init(objects.CodeType)
	// Outer: LOAD_CONST 0 (inner code), MAKE_FUNCTION,
	// PUSH_NULL, LOAD_CONST 1 (7), CALL 1, RETURN_VALUE.
	outer := &objects.Code{
		Code: append(append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.MAKE_FUNCTION, 0)...),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.CALL, 1)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{inner, int64(7)},
		Stacksize: 8,
	}
	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 7 {
		t.Errorf("got %d, want 7", got)
	}
}

func contains(s, sub string) bool {
	return sub == "" || (len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
