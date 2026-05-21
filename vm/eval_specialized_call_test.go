package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// makeIdentityFunction builds a Python function `def id(x): return x`
// for use as a CALL target in the fast-path tests. Argcount=1,
// Varnames=["x"], body=LOAD_FAST 0 + RETURN_VALUE.
func makeIdentityFunction(t *testing.T, name string) *objects.Function {
	t.Helper()
	body := append(
		instr(compile.RESUME, 0),
		append(
			instr(compile.LOAD_FAST, 0),
			instr(compile.RETURN_VALUE, 0)...)...)
	inner := objects.NewCode()
	inner.Code = body
	inner.Argcount = 1
	inner.Varnames = []string{"x"}
	inner.Stacksize = 4
	inner.Name = name
	inner.Flags = int(compile.CoOptimized)
	fn := objects.NewFunction(name, inner, objects.NewDict())
	fn.Builtins = objects.NewDict()
	fn.Version = 0xc0ffee01
	return fn
}

// makeBinaryAddFunction builds `def add(a, b): return a + b`. Stacksize
// needs to cover (a, b) on the value stack for BINARY_OP.
func makeBinaryAddFunction(t *testing.T, name string) *objects.Function {
	t.Helper()
	body := append(append(append(append(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_FAST, 0)...),
		instr(compile.LOAD_FAST, 1)...),
		instr(compile.BINARY_OP, byte(compile.NB_ADD))...),
		instr(compile.RETURN_VALUE, 0)...)
	inner := objects.NewCode()
	inner.Code = body
	inner.Argcount = 2
	inner.Varnames = []string{"a", "b"}
	inner.Stacksize = 4
	inner.Name = name
	inner.Flags = int(compile.CoOptimized)
	fn := objects.NewFunction(name, inner, objects.NewDict())
	fn.Builtins = objects.NewDict()
	fn.Version = 0xc0ffee02
	return fn
}

// stampCallPyExactArgs rewrites the CALL at instr to CALL_PY_EXACT_ARGS
// and primes the cache (cooldown counter + func version) so the fast
// arm's _CHECK_FUNCTION_VERSION guard holds. Mirrors what
// specialize.Call does on the live tick.
func stampCallPyExactArgs(co *objects.Code, instr int, op compile.Opcode, version uint32) {
	specialize.SetOpcode(co.Code, instr, op)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetCallFuncVersion(co.Code, instr, version)
}

// TestFastCallPyExactArgs drives CALL_PY_EXACT_ARGS end-to-end through
// EvalCode. The outer code loads the function from a const, pushes a
// NULL self slot, loads a single arg, and CALLs with oparg=1. The fast
// arm must skip the generic CALL body, push a frame, copy the arg into
// LocalsPlus[0], and run the inner identity body.
//
// CPython: Python/bytecodes.c:4041 CALL_PY_EXACT_ARGS
func TestFastCallPyExactArgs(t *testing.T) {
	ts := state.NewThread()
	fn := makeIdentityFunction(t, "id")

	outer := &objects.Code{
		Code: append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.CALL, 1)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{fn, int64(42)},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	// Locate the CALL instr by codeunit index: LOAD_CONST (1 unit) +
	// PUSH_NULL (1 unit) + LOAD_CONST (1 unit) = 3 -> CALL at idx 3.
	stampCallPyExactArgs(outer, 3, compile.CALL_PY_EXACT_ARGS, fn.Version)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	// Verify the bytecode was NOT deopted: the CALL stayed as
	// CALL_PY_EXACT_ARGS, proving the fast arm took the dispatch.
	if op := compile.Opcode(outer.Code[2*3]); op != compile.CALL_PY_EXACT_ARGS {
		t.Errorf("opcode at call site: got %s, want CALL_PY_EXACT_ARGS (arm should not deopt)", op.Name())
	}
}

// TestFastCallPyExactArgsTwoArgs covers oparg=2. Exercises the loop in
// _INIT_CALL_PY_EXACT_ARGS that copies args[0..N-1] into LocalsPlus.
func TestFastCallPyExactArgsTwoArgs(t *testing.T) {
	ts := state.NewThread()
	fn := makeBinaryAddFunction(t, "add")

	outer := &objects.Code{
		Code: append(append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.LOAD_CONST, 2)...),
			instr(compile.CALL, 2)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{fn, int64(3), int64(4)},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	// CALL at codeunit index 4 (LOAD_CONST + PUSH_NULL + LOAD_CONST + LOAD_CONST).
	stampCallPyExactArgs(outer, 4, compile.CALL_PY_EXACT_ARGS, fn.Version)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 7 {
		t.Errorf("got %d, want 7", got)
	}
}

// TestFastCallPyExactArgsVersionMissDeopts feeds a stale func_version
// into the cache. The arm must reject the dispatch and the generic
// CALL body must handle the call correctly.
func TestFastCallPyExactArgsVersionMissDeopts(t *testing.T) {
	ts := state.NewThread()
	fn := makeIdentityFunction(t, "id")

	outer := &objects.Code{
		Code: append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.CALL, 1)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{fn, int64(42)},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	// Stale version: stamp one that does not match fn.Version.
	stampCallPyExactArgs(outer, 3, compile.CALL_PY_EXACT_ARGS, fn.Version^0xffff)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	// On version miss the arm returns ok=false and the dispatcher
	// rewrites the opcode back to CALL via maybeDeopt.
	if op := compile.Opcode(outer.Code[2*3]); op != compile.CALL {
		t.Errorf("opcode at call site: got %s, want CALL (arm should deopt on version miss)", op.Name())
	}
}

// TestFastCallPyExactArgsArgcountMismatchDeopts: the bytecode passes 2
// args but the function declares argcount=1. _CHECK_FUNCTION_EXACT_ARGS
// must reject and the generic CALL body must surface the same
// "takes 1 positional argument but 2 were given" TypeError CPython
// raises.
func TestFastCallPyExactArgsArgcountMismatchDeopts(t *testing.T) {
	ts := state.NewThread()
	fn := makeIdentityFunction(t, "id")

	outer := &objects.Code{
		Code: append(append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.LOAD_CONST, 2)...),
			instr(compile.CALL, 2)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{fn, int64(1), int64(2)},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	stampCallPyExactArgs(outer, 4, compile.CALL_PY_EXACT_ARGS, fn.Version)

	_, err := EvalCode(ts, outer, nil, nil)
	if err == nil {
		t.Fatalf("expected TypeError on argcount mismatch")
	}
	// And the bytecode was deopted so the next time this site runs the
	// adaptive parent body picks the right specialization.
	if op := compile.Opcode(outer.Code[2*4]); op != compile.CALL {
		t.Errorf("opcode at call site: got %s, want CALL (arm should deopt on argcount miss)", op.Name())
	}
}

// TestFastCallBoundMethodExactArgs exercises the unwrap step:
// _CHECK_CALL_BOUND_METHOD_EXACT_ARGS asserts the null slot is null,
// _INIT_CALL_BOUND_METHOD_EXACT_ARGS pulls im_self into the self slot
// and im_func into the callable slot, then the _CHECK_FUNCTION_VERSION
// and _INIT_CALL_PY_EXACT_ARGS guards fire on the unwrapped function.
//
// CPython: Python/bytecodes.c:4027 CALL_BOUND_METHOD_EXACT_ARGS
func TestFastCallBoundMethodExactArgs(t *testing.T) {
	ts := state.NewThread()
	fn := makeIdentityFunction(t, "id")
	bm := objects.NewBoundMethod(fn, objects.NewInt(99))

	// Stack layout: [bound_method, NULL, /*no positional args*/]
	// oparg=0 because the bound self counts toward the unwrapped
	// argcount, not toward oparg.
	outer := &objects.Code{
		Code: append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.CALL, 0)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{bm},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	// CALL at codeunit index 2.
	stampCallPyExactArgs(outer, 2, compile.CALL_BOUND_METHOD_EXACT_ARGS, fn.Version)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 99 {
		t.Errorf("got %d, want 99 (the bound self)", got)
	}
	if op := compile.Opcode(outer.Code[2*2]); op != compile.CALL_BOUND_METHOD_EXACT_ARGS {
		t.Errorf("opcode at call site: got %s, want CALL_BOUND_METHOD_EXACT_ARGS (arm should not deopt)", op.Name())
	}
}

// TestFastCallPyExactArgsNonFunctionDeopts: the cache says
// CALL_PY_EXACT_ARGS but the callable is a builtin. The arm must
// reject the type and the generic CALL must service the call through
// the BuiltinFunction's Vectorcall.
func TestFastCallPyExactArgsNonFunctionDeopts(t *testing.T) {
	ts := state.NewThread()
	doubled := objects.NewBuiltinFunction("doubled", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		x, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(x * 2), nil
	})

	outer := &objects.Code{
		Code: append(append(append(append(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0)...),
			instr(compile.LOAD_CONST, 1)...),
			instr(compile.CALL, 1)...),
			instr(compile.RETURN_VALUE, 0)...),
		Consts:    []any{doubled, int64(5)},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	// Pretend the specializer stamped CALL_PY_EXACT_ARGS even though
	// the callable is not a *Function (could happen after a function
	// is replaced at module scope between specialization and dispatch).
	stampCallPyExactArgs(outer, 3, compile.CALL_PY_EXACT_ARGS, 0xdead)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 10 {
		t.Errorf("got %d, want 10", got)
	}
	if op := compile.Opcode(outer.Code[2*3]); op != compile.CALL {
		t.Errorf("opcode at call site: got %s, want CALL (arm should deopt on type miss)", op.Name())
	}
}
