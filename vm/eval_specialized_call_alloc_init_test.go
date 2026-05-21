package vm

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// makeSimpleInit builds `def __init__(self): pass` as an objects.Function.
// Body is RESUME, LOAD_CONST 0 (None), RETURN_VALUE.
//
// CPython: Python/specialize.c:1955 get_init_for_simple_managed_python_class
// (SIMPLE_FUNCTION classification: CO_OPTIMIZED, no varargs/kwargs/kwonly)
func makeSimpleInit(t *testing.T) *objects.Function {
	t.Helper()
	body := concat(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_CONST, 0),
		instr(compile.RETURN_VALUE, 0),
	)
	code := objects.NewCode()
	code.Code = body
	code.Argcount = 1
	code.Varnames = []string{"self"}
	code.Stacksize = 4
	code.Name = "__init__"
	code.Flags = int(compile.CoOptimized)
	code.Consts = []any{objects.None()}
	fn := objects.NewFunction("__init__", code, objects.NewDict())
	fn.Builtins = objects.NewDict()
	fn.Version = 0xc0ffee10
	return fn
}

// makeInitWithArg builds `def __init__(self, x): pass`. Used for the
// one-positional-arg fast path.
func makeInitWithArg(t *testing.T) *objects.Function {
	t.Helper()
	body := concat(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_CONST, 0),
		instr(compile.RETURN_VALUE, 0),
	)
	code := objects.NewCode()
	code.Code = body
	code.Argcount = 2
	code.Varnames = []string{"self", "x"}
	code.Stacksize = 4
	code.Name = "__init__"
	code.Flags = int(compile.CoOptimized)
	code.Consts = []any{objects.None()}
	fn := objects.NewFunction("__init__", code, objects.NewDict())
	fn.Builtins = objects.NewDict()
	fn.Version = 0xc0ffee11
	return fn
}

// makeBadInit builds `def __init__(self): return 1`. Used to exercise
// the EXIT_INIT_CHECK TypeError arm.
func makeBadInit(t *testing.T) *objects.Function {
	t.Helper()
	body := concat(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_CONST, 0),
		instr(compile.RETURN_VALUE, 0),
	)
	code := objects.NewCode()
	code.Code = body
	code.Argcount = 1
	code.Varnames = []string{"self"}
	code.Stacksize = 4
	code.Name = "__init__"
	code.Flags = int(compile.CoOptimized)
	code.Consts = []any{objects.NewInt(1)}
	fn := objects.NewFunction("__init__", code, objects.NewDict())
	fn.Builtins = objects.NewDict()
	fn.Version = 0xc0ffee12
	return fn
}

// userClassWithInit assembles a user class whose namespace carries the
// given __init__ function. Mirrors what __build_class__ does at runtime.
func userClassWithInit(name string, init *objects.Function) *objects.Type {
	ns := objects.NewDict()
	_ = ns.SetItem(objects.NewStr("__init__"), init)
	return objects.NewUserType(name, nil, ns)
}

// stampAllocAndEnterInit rewrites the CALL at instr to
// CALL_ALLOC_AND_ENTER_INIT, primes the adaptive counter to cooldown,
// caches init on the type, and stamps the type version into cells 2-3
// so the fast arm's version guard holds. Mirrors what
// specializeClassCall does once it has picked the arm.
func stampAllocAndEnterInit(t *testing.T, co *objects.Code, instr int, cls *objects.Type) {
	t.Helper()
	descr, _ := objects.LookupDescriptor(cls, "__init__")
	fn, ok := descr.(*objects.Function)
	if !ok {
		t.Fatalf("class %q lacks a Function __init__", cls.Name)
	}
	version, ok := cls.CacheInitForSpecialization(fn)
	if !ok {
		t.Fatalf("CacheInitForSpecialization gave up (counter wrapped?)")
	}
	specialize.SetOpcode(co.Code, instr, compile.CALL_ALLOC_AND_ENTER_INIT)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetCallFuncVersion(co.Code, instr, version)
}

// TestFastCallAllocAndEnterInitHit drives a 0-arg __init__ end-to-end.
// The fast arm allocates the instance, runs the init body (which
// implicitly returns None), drops the call frame, and pushes the
// instance.
//
// CPython: Python/bytecodes.c:4186 CALL_ALLOC_AND_ENTER_INIT
func TestFastCallAllocAndEnterInitHit(t *testing.T) {
	cls := userClassWithInit("C", makeSimpleInit(t))

	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.CALL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls},
		Stacksize: 4,
	}
	specialize.Enable(outer)
	// LOAD_CONST + PUSH_NULL = 2 codeunits, CALL at idx 2.
	stampAllocAndEnterInit(t, outer, 2, cls)

	ts := state.NewThread()
	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	inst, ok := v.(*objects.Instance)
	if !ok {
		t.Fatalf("result not an *Instance: %T", v)
	}
	if inst.Type() != cls {
		t.Errorf("instance type: got %s, want %s", inst.Type().Name, cls.Name)
	}
	if op := compile.Opcode(outer.Code[2*2]); op != compile.CALL_ALLOC_AND_ENTER_INIT {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastCallAllocAndEnterInitOneArg covers a 1-positional-arg
// __init__. Argument copy and frame setup must run for the (self, x)
// pair.
func TestFastCallAllocAndEnterInitOneArg(t *testing.T) {
	cls := userClassWithInit("D", makeInitWithArg(t))

	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.CALL, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls, objects.NewInt(7)},
		Stacksize: 4,
	}
	specialize.Enable(outer)
	// LOAD_CONST + PUSH_NULL + LOAD_CONST = 3, CALL at idx 3.
	stampAllocAndEnterInit(t, outer, 3, cls)

	ts := state.NewThread()
	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if _, ok := v.(*objects.Instance); !ok {
		t.Fatalf("result not an *Instance: %T", v)
	}
	if op := compile.Opcode(outer.Code[2*3]); op != compile.CALL_ALLOC_AND_ENTER_INIT {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastCallAllocAndEnterInitReturnsNonNoneRaises confirms the arm
// fires EXIT_INIT_CHECK's TypeError when __init__ returns a non-None
// value.
//
// CPython: Python/bytecodes.c:4193 EXIT_INIT_CHECK
func TestFastCallAllocAndEnterInitReturnsNonNoneRaises(t *testing.T) {
	cls := userClassWithInit("E", makeBadInit(t))

	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.CALL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls},
		Stacksize: 4,
	}
	specialize.Enable(outer)
	stampAllocAndEnterInit(t, outer, 2, cls)

	ts := state.NewThread()
	_, err := EvalCode(ts, outer, nil, nil)
	if err == nil {
		t.Fatal("EvalCode: want TypeError, got nil")
	}
	if !strings.Contains(err.Error(), "should return None") {
		t.Fatalf("EvalCode err=%v, want '__init__() should return None'", err)
	}
}

// TestFastCallAllocAndEnterInitNonTypeDeopts plants a non-Type
// callable (a plain Function) under a CALL_ALLOC_AND_ENTER_INIT stamp.
// The fast arm's IsUser guard must fail and route through maybeDeopt.
func TestFastCallAllocAndEnterInitNonTypeDeopts(t *testing.T) {
	// Use an identity function as the callable; CALL_ALLOC_AND_ENTER_INIT
	// expects a *Type so this must deopt and the generic CALL body has
	// to take over.
	fn := makeIdentityFunction(t, "id")

	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.CALL, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{fn, objects.NewInt(42)},
		Stacksize: 4,
	}
	specialize.Enable(outer)
	// Stamp without populating any spec cache: the type guard fails first.
	specialize.SetOpcode(outer.Code, 3, compile.CALL_ALLOC_AND_ENTER_INIT)
	specialize.StoreCounter(outer.Code, 3, specialize.AdaptiveCounterCooldown())

	ts := state.NewThread()
	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Int).Int64()
	if !ok {
		t.Fatalf("result not int64-representable: %v", v)
	}
	if got != 42 {
		t.Errorf("slow-path id(42): got %d, want 42", got)
	}
	if op := compile.Opcode(outer.Code[2*3]); op == compile.CALL_ALLOC_AND_ENTER_INIT {
		t.Errorf("guard miss should have deopted, but op stayed CALL_ALLOC_AND_ENTER_INIT")
	}
}

// TestFastCallAllocAndEnterInitVersionMissDeopts confirms a type
// mutation between stamp and call invalidates the cache. The fast arm
// rejects on version mismatch, generic CALL takes over, and the
// resulting instance still has the expected type.
func TestFastCallAllocAndEnterInitVersionMissDeopts(t *testing.T) {
	cls := userClassWithInit("F", makeSimpleInit(t))

	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.CALL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls},
		Stacksize: 4,
	}
	specialize.Enable(outer)
	stampAllocAndEnterInit(t, outer, 2, cls)
	// Invalidate after stamping. CPython's PyType_Modified path zeroes
	// versionTag + clears _spec_cache.init, so the fast arm guard fails
	// on the next call.
	cls.InvalidateVersionTag()

	ts := state.NewThread()
	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if _, ok := v.(*objects.Instance); !ok {
		t.Fatalf("result not an *Instance after deopt: %T", v)
	}
	if op := compile.Opcode(outer.Code[2*2]); op == compile.CALL_ALLOC_AND_ENTER_INIT {
		t.Errorf("guard miss should have deopted, but op stayed CALL_ALLOC_AND_ENTER_INIT")
	}
}

// TestFastCallAllocAndEnterInitArgcountMismatchDeopts: the cache was
// stamped for `def __init__(self, x)` but the call site supplies zero
// args. The Argcount check must fail and deopt so the slow path
// surfaces the TypeError the generic body raises.
func TestFastCallAllocAndEnterInitArgcountMismatchDeopts(t *testing.T) {
	cls := userClassWithInit("G", makeInitWithArg(t))

	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.CALL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls},
		Stacksize: 4,
	}
	specialize.Enable(outer)
	stampAllocAndEnterInit(t, outer, 2, cls)

	ts := state.NewThread()
	_, err := EvalCode(ts, outer, nil, nil)
	if err == nil {
		t.Fatal("EvalCode: want TypeError from missing __init__ arg, got nil")
	}
	if op := compile.Opcode(outer.Code[2*2]); op == compile.CALL_ALLOC_AND_ENTER_INIT {
		t.Errorf("argcount mismatch should have deopted, but op stayed CALL_ALLOC_AND_ENTER_INIT")
	}
}
