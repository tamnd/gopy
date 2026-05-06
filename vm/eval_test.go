package vm

import (
	"errors"
	"sync"
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
	// LOAD_SUPER_ATTR is not in any hand-written panel, so dispatch should
	// fall through to ErrNotImplemented.
	co := codeWithBytecode(instr(compile.LOAD_SUPER_ATTR, 0))
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
	co := codeWithBytecode(instr(compile.LOAD_SUPER_ATTR, 0))
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !contains(got, "LOAD_SUPER_ATTR") {
		t.Errorf("error %q should mention LOAD_SUPER_ATTR", got)
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
	// EXTENDED_ARG 0x01, then LOAD_SUPER_ATTR 0x02 -> oparg should be 0x0102.
	// LOAD_SUPER_ATTR is unimplemented so we expect ErrNotImplemented to bubble.
	bc := append(instr(compile.EXTENDED_ARG, 1), instr(compile.LOAD_SUPER_ATTR, 2)...)
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

func TestEvalLoadSmallInt(t *testing.T) {
	ts := state.NewThread()
	// LOAD_SMALL_INT 42; RETURN_VALUE -> 42
	co := codeWithBytecode(append(
		instr(compile.LOAD_SMALL_INT, 42),
		instr(compile.RETURN_VALUE, 0)...))
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestEvalToBool(t *testing.T) {
	ts := state.NewThread()
	// LOAD_CONST 0 (1); TO_BOOL; RETURN_VALUE -> True
	co := &objects.Code{
		Code: append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.TO_BOOL, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(1)},
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

// TestEvalLoadAttrMissingSurfacesError exercises the LOAD_ATTR
// dispatch path: the int type has no Getattro slot, so PyObject_GetAttr
// returns AttributeError and EvalCode bubbles it up.
func TestEvalLoadAttrMissingSurfacesError(t *testing.T) {
	ts := state.NewThread()
	co := &objects.Code{
		Code: append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(7)},
		Names:     []string{"missing"},
		Stacksize: 4,
	}
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected AttributeError")
	}
	if !contains(err.Error(), "AttributeError") {
		t.Errorf("err = %v, want AttributeError", err)
	}
}

// TestEvalStoreDeleteAttrMissingSurfacesError exercises STORE_ATTR
// (and DELETE_ATTR via the same Setattro slot) on a type that has
// neither a getattro nor a setattro slot. Both should produce the
// "no attributes" TypeError.
func TestEvalStoreDeleteAttrMissingSurfacesError(t *testing.T) {
	ts := state.NewThread()
	storeCo := &objects.Code{
		Code: append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.STORE_ATTR, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(1), int64(7)},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	if _, err := EvalCode(ts, storeCo, nil, nil); err == nil ||
		!contains(err.Error(), "TypeError") {
		t.Errorf("STORE_ATTR err = %v, want TypeError", err)
	}

	delCo := &objects.Code{
		Code: append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.DELETE_ATTR, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(7)},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	if _, err := EvalCode(ts, delCo, nil, nil); err == nil ||
		!contains(err.Error(), "TypeError") {
		t.Errorf("DELETE_ATTR err = %v, want TypeError", err)
	}
}

// TestEvalCodeGoroutineSafety pins that distinct EvalCode calls from
// different goroutines, sharing a single *objects.Code, do not race
// on shared interpreter state. Run with `go test -race` to catch
// regressions where someone caches per-call state on the Code object
// or a shared map without locking.
//
// CPython: Doc/c-api/init.rst (PyEval_EvalCode is reentrant per thread)
func TestEvalCodeGoroutineSafety(t *testing.T) {
	const goroutines = 32
	// LOAD_CONST 0 (5); LOAD_CONST 1 (7); BINARY_OP nbAdd (0);
	// RETURN_VALUE -> 12. Same Code object shared across all goroutines.
	co := &objects.Code{
		Code: append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.BINARY_OP, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{int64(5), int64(7)},
		Stacksize: 4,
	}

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts := state.NewThread()
			v, err := EvalCode(ts, co, nil, nil)
			if err != nil {
				errs <- err
				return
			}
			got, _ := v.(*objects.Int).Int64()
			if got != 12 {
				errs <- errors.New("unexpected result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("goroutine: %v", err)
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

// concat joins instruction byte slices for readability.
func concat(parts ...[]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestEvalBuildSet(t *testing.T) {
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.BUILD_SET, 2),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{int64(1), int64(2)},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(*objects.Set)
	if !ok {
		t.Fatalf("got %T, want *objects.Set", v)
	}
	if s.Len() != 2 {
		t.Errorf("set len = %d, want 2", s.Len())
	}
}

func TestEvalSetAdd(t *testing.T) {
	ts := state.NewThread()
	// BUILD_SET 0 (empty), LOAD_CONST 0 (42), SET_ADD 1, RETURN_VALUE.
	co := &objects.Code{
		Code: concat(
			instr(compile.BUILD_SET, 0),
			instr(compile.LOAD_CONST, 0),
			instr(compile.SET_ADD, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{int64(42)},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(*objects.Set)
	if !ok || s.Len() != 1 {
		t.Fatalf("got %T len? want set len 1", v)
	}
	has, _ := s.Contains(objects.NewInt(42))
	if !has {
		t.Errorf("set missing 42")
	}
}

func TestEvalSetUpdate(t *testing.T) {
	ts := state.NewThread()
	// BUILD_SET 0, LOAD_CONST 0 (list[1,2,3]), SET_UPDATE 1, RETURN_VALUE.
	src := objects.NewList([]objects.Object{
		objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
	})
	co := &objects.Code{
		Code: concat(
			instr(compile.BUILD_SET, 0),
			instr(compile.LOAD_CONST, 0),
			instr(compile.SET_UPDATE, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{src},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(*objects.Set)
	if !ok || s.Len() != 3 {
		t.Fatalf("got %T len? want set len 3", v)
	}
}

func TestEvalMatchMapping(t *testing.T) {
	ts := state.NewThread()
	d := objects.NewDict()
	if err := d.SetItem(objects.NewStr("k"), objects.NewInt(1)); err != nil {
		t.Fatal(err)
	}
	// LOAD_CONST 0 (dict); MATCH_MAPPING; RETURN_VALUE -> True (pops the bool).
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.MATCH_MAPPING, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{d},
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

func TestEvalMatchSequence(t *testing.T) {
	ts := state.NewThread()
	l := objects.NewList([]objects.Object{objects.NewInt(1)})
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.MATCH_SEQUENCE, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{l},
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

func TestEvalMatchSequenceFalse(t *testing.T) {
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.MATCH_SEQUENCE, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{int64(7)},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.False() {
		t.Errorf("got %v, want False", v)
	}
}

func TestEvalMatchKeys(t *testing.T) {
	ts := state.NewThread()
	d := objects.NewDict()
	if err := d.SetItem(objects.NewStr("a"), objects.NewInt(10)); err != nil {
		t.Fatal(err)
	}
	if err := d.SetItem(objects.NewStr("b"), objects.NewInt(20)); err != nil {
		t.Fatal(err)
	}
	keys := objects.NewTuple([]objects.Object{
		objects.NewStr("a"), objects.NewStr("b"),
	})
	// LOAD_CONST 0 (dict), LOAD_CONST 1 (keys), MATCH_KEYS, RETURN_VALUE -> tuple(10,20).
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.MATCH_KEYS, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{d, keys},
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
	a, _ := tup.Item(0).(*objects.Int).Int64()
	b, _ := tup.Item(1).(*objects.Int).Int64()
	if a != 10 || b != 20 {
		t.Errorf("got (%d,%d), want (10,20)", a, b)
	}
}

func TestEvalMatchKeysMissing(t *testing.T) {
	ts := state.NewThread()
	d := objects.NewDict()
	if err := d.SetItem(objects.NewStr("a"), objects.NewInt(10)); err != nil {
		t.Fatal(err)
	}
	keys := objects.NewTuple([]objects.Object{
		objects.NewStr("a"), objects.NewStr("missing"),
	})
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.MATCH_KEYS, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{d, keys},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.None() {
		t.Errorf("got %v, want None on missing key", v)
	}
}

func TestEvalGenerator(t *testing.T) {
	ts := state.NewThread()
	// A code object representing a generator that yields 42 once. Layout:
	//   RESUME 0           - initial entry; advances to RETURN_GENERATOR
	//   RETURN_GENERATOR 0 - detaches frame; first EvalCode call returns the gen
	//   LOAD_CONST 0 (42)  - body starts here on first Send()
	//   YIELD_VALUE 0      - yields 42, blocks for next Send()
	//   RESUME 1           - re-entry after yield
	//   RETURN_VALUE 0     - returns the sent value (None); goroutine signals StopIteration
	co := &objects.Code{
		Code: concat(
			instr(compile.RESUME, 0),
			instr(compile.RETURN_GENERATOR, 0),
			instr(compile.LOAD_CONST, 0),
			instr(compile.YIELD_VALUE, 0),
			instr(compile.RESUME, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{int64(42)},
		Stacksize: 4,
		Name:      "gen",
	}
	obj, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	gen, ok := obj.(*objects.Generator)
	if !ok {
		t.Fatalf("got %T, want *objects.Generator", obj)
	}
	v, err := gen.Send(objects.None())
	if err != nil {
		t.Fatalf("first Send: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("first yield = %d, want 42", got)
	}
	if _, err := gen.Send(objects.None()); !errors.Is(err, objects.ErrStopIteration) {
		t.Errorf("second Send err = %v, want ErrStopIteration", err)
	}
}

func TestEvalGetYieldFromIterPassesGenerator(t *testing.T) {
	ts := state.NewThread()
	gen := objects.NewGenerator("dummy")
	// LOAD_CONST 0 (generator); GET_YIELD_FROM_ITER; RETURN_VALUE.
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_YIELD_FROM_ITER, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{gen},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != gen {
		t.Errorf("GET_YIELD_FROM_ITER changed a generator: got %v, want same gen", v)
	}
}

func TestEvalGetYieldFromIterCallsIter(t *testing.T) {
	ts := state.NewThread()
	src := objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_YIELD_FROM_ITER, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{src},
		Stacksize: 4,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v == src {
		t.Errorf("GET_YIELD_FROM_ITER returned the list unchanged; expected an iterator")
	}
	if v == nil {
		t.Fatal("got nil iterator")
	}
}
