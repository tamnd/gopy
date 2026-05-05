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
	co := codeWithBytecode(instr(compile.YIELD_VALUE, 0))
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
	co := codeWithBytecode(instr(compile.YIELD_VALUE, 0))
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !contains(got, "YIELD_VALUE") {
		t.Errorf("error %q should mention YIELD_VALUE", got)
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
	bc := append(instr(compile.EXTENDED_ARG, 1), instr(compile.YIELD_VALUE, 2)...)
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

func TestEvalListAppendAndExtend(t *testing.T) {
	ts := state.NewThread()
	// BUILD_LIST 0; LOAD_CONST 0; LIST_APPEND 1; RETURN_VALUE.
	co := &objects.Code{
		Code: append(append(append(
			instr(compile.BUILD_LIST, 0),
			instr(compile.LOAD_CONST, 0)...),
			instr(compile.LIST_APPEND, 1)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(42)},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := v.(*objects.List)
	if !ok || l.Len() != 1 {
		t.Fatalf("got %T len? want list len 1", v)
	}
	got, _ := l.Item(0).(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("appended value = %d, want 42", got)
	}
}

func TestEvalCallIntrinsic1ListToTuple(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (list[1,2]); CALL_INTRINSIC_1 6 (UnaryListToTupleID);
	// RETURN_VALUE.
	src := objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	co := &objects.Code{
		Code: append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.CALL_INTRINSIC_1, 6)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{src},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("got %T, want *Tuple len 2", v)
	}
}

func TestEvalBinarySlice(t *testing.T) {
	ts := state.NewThread()
	src := objects.NewList([]objects.Object{
		objects.NewInt(10), objects.NewInt(20),
		objects.NewInt(30), objects.NewInt(40),
	})
	// LOAD_CONST 0 (list); LOAD_CONST 1 (1); LOAD_CONST 2 (3);
	// BINARY_SLICE; RETURN_VALUE.
	co := &objects.Code{
		Code: append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.LOAD_CONST, 2)...),
			instr(compile.BINARY_SLICE, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{src, int64(1), int64(3)},
		Stacksize: 8,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := v.(*objects.List)
	if !ok || l.Len() != 2 {
		t.Fatalf("got %T len? want list len 2", v)
	}
	a, _ := l.Item(0).(*objects.Int).Int64()
	b, _ := l.Item(1).(*objects.Int).Int64()
	if a != 20 || b != 30 {
		t.Errorf("got [%d,%d], want [20,30]", a, b)
	}
}

func TestEvalCallKw(t *testing.T) {
	ts := state.NewThread()
	// Builtin that takes one positional + one keyword.
	greet := objects.NewBuiltinFunction("greet", func(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
		name, _ := objects.Str(args[0])
		excl, _ := objects.Str(kw["excl"])
		return objects.NewStr(name + excl), nil
	})
	kwnames := objects.NewTuple([]objects.Object{objects.NewStr("excl")})
	co := &objects.Code{
		// LOAD_CONST 0 (greet); PUSH_NULL; LOAD_CONST 1 ("hi"); LOAD_CONST 2 ("!");
		// LOAD_CONST 3 (kwnames tuple); CALL_KW 2; RETURN_VALUE.
		Code: append(append(append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.LOAD_CONST, 2)...),
			instr(compile.LOAD_CONST, 3)...),
			instr(compile.CALL_KW, 2)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{greet, "hi", "!", kwnames},
		Stacksize: 8,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := objects.Str(v)
	if s != "hi!" {
		t.Errorf("got %q, want %q", s, "hi!")
	}
}

func TestEvalBinarySubscr(t *testing.T) {
	ts := state.NewThread()
	src := objects.NewList([]objects.Object{
		objects.NewInt(10), objects.NewInt(20), objects.NewInt(30),
	})
	// LOAD_CONST 0 (list); LOAD_CONST 1 (1); BINARY_OP nbSubscr (26); RETURN_VALUE -> 20
	const nbSubscr = 26
	co := &objects.Code{
		Code: append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.BINARY_OP, nbSubscr)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{src, int64(1)},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 20 {
		t.Errorf("got %d, want 20", got)
	}
}

func TestEvalContainsOp(t *testing.T) {
	ts := state.NewThread()
	src := objects.NewList([]objects.Object{
		objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
	})
	// LOAD_CONST 0 (2); LOAD_CONST 1 (list); CONTAINS_OP 0 (`in`); RETURN_VALUE -> True
	co := &objects.Code{
		Code: append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.CONTAINS_OP, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(2), src},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.True() {
		t.Errorf("got %v, want True", v)
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
