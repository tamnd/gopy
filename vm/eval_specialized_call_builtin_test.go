package vm

import (
	"fmt"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// stampCallVariant rewrites the CALL at codeunit instr to op and primes
// the adaptive counter so the fast arm body runs on the next dispatch.
// The builtin / descriptor arms do not consume func_version cells, so
// this helper omits the SetCallFuncVersion call stampCallPyExactArgs
// needs.
func stampCallVariant(co *objects.Code, instr int, op compile.Opcode) {
	specialize.SetOpcode(co.Code, instr, op)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
}

// callOneArg builds `LOAD_CONST callable; PUSH_NULL; LOAD_CONST arg; CALL 1; RETURN_VALUE`
// and returns the codeunit index of the CALL opcode so callers can
// stamp the desired variant.
func callOneArg(callable, arg objects.Object) (*objects.Code, int) {
	body := append(append(append(append(
		instr(compile.LOAD_CONST, 0),
		instr(compile.PUSH_NULL, 0)...),
		instr(compile.LOAD_CONST, 1)...),
		instr(compile.CALL, 1)...),
		instr(compile.RETURN_VALUE, 0)...)
	return &objects.Code{
		Code:      body,
		Consts:    []any{callable, arg},
		Stacksize: 8,
	}, 3
}

// callTwoArgs builds the CALL frame for `f(arg0, arg1)` and returns
// the codeunit index of the CALL opcode.
func callTwoArgs(callable, arg0, arg1 objects.Object) (*objects.Code, int) {
	body := append(append(append(append(append(
		instr(compile.LOAD_CONST, 0),
		instr(compile.PUSH_NULL, 0)...),
		instr(compile.LOAD_CONST, 1)...),
		instr(compile.LOAD_CONST, 2)...),
		instr(compile.CALL, 2)...),
		instr(compile.RETURN_VALUE, 0)...)
	return &objects.Code{
		Code:      body,
		Consts:    []any{callable, arg0, arg1},
		Stacksize: 8,
	}, 4
}

// TestFastCallBuiltinO drives CALL_BUILTIN_O through a MethO-tagged
// builtin that is NOT in the canonical cache (so the specializer does
// not promote it to CALL_LEN). Verifies the arm dispatches and does
// not deopt.
//
// CPython: Python/bytecodes.c:4233 _CALL_BUILTIN_O
func TestFastCallBuiltinO(t *testing.T) {
	ts := state.NewThread()
	called := false
	bf := objects.NewBuiltinFunctionConv("id", objects.MethO, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		called = true
		if len(args) != 1 {
			return nil, fmt.Errorf("id() expected 1 arg, got %d", len(args))
		}
		return objects.NewInt(int64(7)), nil
	})

	outer, idx := callOneArg(bf, objects.NewInt(42))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_BUILTIN_O)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if !called {
		t.Fatal("MethO builtin closure was never invoked")
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 7 {
		t.Errorf("got %d, want 7", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_BUILTIN_O {
		t.Errorf("opcode at call site: got %s, want CALL_BUILTIN_O", op.Name())
	}
}

// TestFastCallBuiltinOWrongConvDeopts: arm refuses callables whose
// Conv does not match METH_O even when the cache says CALL_BUILTIN_O,
// matching DEOPT_IF on PyCFunction_GET_FLAGS != METH_O.
func TestFastCallBuiltinOWrongConvDeopts(t *testing.T) {
	ts := state.NewThread()
	bf := objects.NewBuiltinFunctionConv("dbl", objects.MethFastcall, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		x, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(x * 2), nil
	})

	outer, idx := callOneArg(bf, objects.NewInt(5))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_BUILTIN_O)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 10 {
		t.Errorf("got %d, want 10 (deopt to generic CALL)", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL {
		t.Errorf("opcode at call site: got %s, want CALL (arm must deopt on Conv mismatch)", op.Name())
	}
}

// TestFastCallBuiltinFast drives CALL_BUILTIN_FAST with a MethFastcall
// builtin and a single positional argument.
//
// CPython: Python/bytecodes.c:4268 _CALL_BUILTIN_FAST
func TestFastCallBuiltinFast(t *testing.T) {
	ts := state.NewThread()
	bf := objects.NewBuiltinFunctionConv("first", objects.MethFastcall, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("first() needs at least 1 arg")
		}
		return args[0], nil
	})

	outer, idx := callOneArg(bf, objects.NewInt(99))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_BUILTIN_FAST)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 99 {
		t.Errorf("got %d, want 99", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_BUILTIN_FAST {
		t.Errorf("opcode at call site: got %s, want CALL_BUILTIN_FAST", op.Name())
	}
}

// TestFastCallBuiltinFastWithKeywords drives the arm with a
// METH_FASTCALL|METH_KEYWORDS builtin. The CALL opcode itself carries
// no kwargs map; the arm still beats generic CALL by skipping the
// vectorcall slot lookup.
//
// CPython: Python/bytecodes.c:4305 _CALL_BUILTIN_FAST_WITH_KEYWORDS
func TestFastCallBuiltinFastWithKeywords(t *testing.T) {
	ts := state.NewThread()
	bf := objects.NewBuiltinFunctionConv("dbl_kw", objects.MethFastcall|objects.MethKeywords, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		x, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(x * 2), nil
	})

	outer, idx := callOneArg(bf, objects.NewInt(11))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_BUILTIN_FAST_WITH_KEYWORDS)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 22 {
		t.Errorf("got %d, want 22", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_BUILTIN_FAST_WITH_KEYWORDS {
		t.Errorf("opcode at call site: got %s, want CALL_BUILTIN_FAST_WITH_KEYWORDS", op.Name())
	}
}

// TestFastCallLen exercises CALL_LEN: `len(list)`. Pre-registers the
// builtin in the callable cache, then stamps CALL_LEN at the call
// site.
//
// CPython: Python/bytecodes.c:4354 _CALL_LEN
func TestFastCallLen(t *testing.T) {
	ts := state.NewThread()
	bf := objects.NewBuiltinFunctionConv("len", objects.MethO, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		n, err := objects.Length(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewInt(int64(n)), nil
	})
	prev := objects.CallableCacheLen()
	objects.RegisterCallableCacheLen(bf)
	defer objects.RegisterCallableCacheLen(prev)

	lst := objects.NewList(nil)
	lst.Append(objects.NewInt(1))
	lst.Append(objects.NewInt(2))
	lst.Append(objects.NewInt(3))

	outer, idx := callOneArg(bf, lst)
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_LEN)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_LEN {
		t.Errorf("opcode at call site: got %s, want CALL_LEN", op.Name())
	}
}

// TestFastCallLenIdentityMissDeopts: the cache says CALL_LEN but the
// callable is a different MethO builtin. The identity guard must fail
// and the dispatch must fall through to generic CALL.
func TestFastCallLenIdentityMissDeopts(t *testing.T) {
	ts := state.NewThread()
	other := objects.NewBuiltinFunctionConv("len_imposter", objects.MethO, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(99), nil
	})

	outer, idx := callOneArg(other, objects.NewInt(7))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_LEN)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 99 {
		t.Errorf("got %d, want 99", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL {
		t.Errorf("opcode at call site: got %s, want CALL (arm must deopt on identity miss)", op.Name())
	}
}

// TestFastCallIsinstance exercises CALL_ISINSTANCE: `isinstance(obj, cls)`.
//
// CPython: Python/bytecodes.c:4374 CALL_ISINSTANCE
func TestFastCallIsinstance(t *testing.T) {
	ts := state.NewThread()
	bf := objects.NewBuiltinFunctionConv("isinstance", objects.MethFastcall, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("isinstance() needs 2 args")
		}
		cls, ok := args[1].(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("isinstance(): 2nd arg must be a type")
		}
		if objects.IsSubtype(args[0].Type(), cls) {
			return objects.True(), nil
		}
		return objects.False(), nil
	})
	prev := objects.CallableCacheIsinstance()
	objects.RegisterCallableCacheIsinstance(bf)
	defer objects.RegisterCallableCacheIsinstance(prev)

	outer, idx := callTwoArgs(bf, objects.NewInt(5), objects.IntType)
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_ISINSTANCE)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if v != objects.True() {
		t.Errorf("got %v, want True", v)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_ISINSTANCE {
		t.Errorf("opcode at call site: got %s, want CALL_ISINSTANCE", op.Name())
	}
}

// TestFastCallListAppend covers the L.append(x) + POP_TOP pattern.
// The arm has to consume both the CALL and the trailing POP_TOP.
//
// Stack on entry to CALL: [list, NULL, value] with the list reached
// through LOAD_ATTR_METHOD_NO_DICT style; in this hand-built test we
// emit LOAD_CONST list / PUSH_NULL / LOAD_CONST arg shape because the
// arm reads (callable, self_or_null, arg). We swap that for the
// descriptor + self layout the CPython emitter produces.
//
// CPython: Python/bytecodes.c:4400 CALL_LIST_APPEND
func TestFastCallListAppend(t *testing.T) {
	ts := state.NewThread()

	lst := objects.NewList(nil)
	d := objects.CallableCacheListAppend()
	if d == nil {
		t.Skip("list.append descriptor not registered in callable cache")
	}

	// Stack layout: [d, list, value]. d acts as the callable (came from
	// LOAD_ATTR's method-shape); list is the self_or_null; value is the
	// single positional argument. Outer code: LOAD_CONST d / LOAD_CONST
	// list / LOAD_CONST value / CALL 1 / POP_TOP / LOAD_CONST None /
	// RETURN_VALUE.
	body := append(append(append(append(append(append(
		instr(compile.LOAD_CONST, 0),
		instr(compile.LOAD_CONST, 1)...),
		instr(compile.LOAD_CONST, 2)...),
		instr(compile.CALL, 1)...),
		instr(compile.POP_TOP, 0)...),
		instr(compile.LOAD_CONST, 3)...),
		instr(compile.RETURN_VALUE, 0)...)
	outer := &objects.Code{
		Code:      body,
		Consts:    []any{d, lst, objects.NewInt(123), objects.None()},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	// CALL is at codeunit index 3 (LOAD_CONST + LOAD_CONST + LOAD_CONST).
	stampCallVariant(outer, 3, compile.CALL_LIST_APPEND)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if !objects.IsNone(v) {
		t.Errorf("got %v, want None", v)
	}
	if lst.Len() != 1 {
		t.Fatalf("list len = %d, want 1", lst.Len())
	}
	item := lst.Item(0)
	got, _ := item.(*objects.Int).Int64()
	if got != 123 {
		t.Errorf("appended item = %d, want 123", got)
	}
	if op := compile.Opcode(outer.Code[2*3]); op != compile.CALL_LIST_APPEND {
		t.Errorf("opcode at call site: got %s, want CALL_LIST_APPEND", op.Name())
	}
}

// TestFastCallType1 exercises CALL_TYPE_1: `type(x)`.
//
// CPython: Python/bytecodes.c:4061 _CALL_TYPE_1
func TestFastCallType1(t *testing.T) {
	ts := state.NewThread()
	outer, idx := callOneArg(objects.TypeType(), objects.NewInt(7))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_TYPE_1)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if v != objects.IntType {
		t.Errorf("got %v, want IntType", v)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_TYPE_1 {
		t.Errorf("opcode at call site: got %s, want CALL_TYPE_1", op.Name())
	}
}

// TestFastCallStr1 exercises CALL_STR_1: `str(x)`.
//
// CPython: Python/bytecodes.c:4086 _CALL_STR_1
func TestFastCallStr1(t *testing.T) {
	ts := state.NewThread()
	outer, idx := callOneArg(objects.StrType(), objects.NewInt(42))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_STR_1)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Unicode)
	if !ok {
		t.Fatalf("got %T, want *objects.Unicode", v)
	}
	if got.Value() != "42" {
		t.Errorf("got %q, want %q", got.Value(), "42")
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_STR_1 {
		t.Errorf("opcode at call site: got %s, want CALL_STR_1", op.Name())
	}
}

// TestFastCallTuple1 exercises CALL_TUPLE_1: `tuple(iterable)`.
//
// CPython: Python/bytecodes.c:4114 _CALL_TUPLE_1
func TestFastCallTuple1(t *testing.T) {
	ts := state.NewThread()
	lst := objects.NewList([]objects.Object{
		objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
	})
	outer, idx := callOneArg(objects.TupleType, lst)
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_TUPLE_1)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("got %T, want *objects.Tuple", v)
	}
	if tup.Len() != 3 {
		t.Errorf("tuple len = %d, want 3", tup.Len())
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_TUPLE_1 {
		t.Errorf("opcode at call site: got %s, want CALL_TUPLE_1", op.Name())
	}
}

// TestFastCallBuiltinClass exercises CALL_BUILTIN_CLASS with a custom
// *Type that exposes a TpNew constructor. The fast arm dispatches
// through the metatype's typeVectorcall, which falls through to TpNew
// for non-user types, matching CPython's _CALL_BUILTIN_CLASS arm.
//
// CPython: Python/bytecodes.c:4203 _CALL_BUILTIN_CLASS
func TestFastCallBuiltinClass(t *testing.T) {
	ts := state.NewThread()
	customType := objects.NewType("custom_class", []*objects.Type{objects.ObjectType()})
	customType.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("custom_class() expected 1 arg, got %d", len(args))
		}
		return args[0], nil
	}

	outer, idx := callOneArg(customType, objects.NewInt(57))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_BUILTIN_CLASS)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 57 {
		t.Errorf("got %d, want 57", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_BUILTIN_CLASS {
		t.Errorf("opcode at call site: got %s, want CALL_BUILTIN_CLASS", op.Name())
	}
}

// TestFastCallMethodDescriptorO exercises CALL_METHOD_DESCRIPTOR_O via
// list.append called WITHOUT a trailing POP_TOP. The list.append
// descriptor is tagged METH_O, so the descriptor arm fires instead of
// CALL_LIST_APPEND.
//
// CPython: Python/bytecodes.c:4424 _CALL_METHOD_DESCRIPTOR_O
func TestFastCallMethodDescriptorO(t *testing.T) {
	ts := state.NewThread()
	d := objects.CallableCacheListAppend()
	if d == nil {
		t.Skip("list.append descriptor not registered")
	}

	lst := objects.NewList(nil)
	outer, idx := callTwoArgs(d, lst, objects.NewInt(42))
	// callTwoArgs builds [d, NULL, list, value] with PUSH_NULL between
	// the descriptor and the args. The descriptor arm reads
	// (callable, self_or_null, args) where self_or_null is the NULL the
	// PUSH_NULL produced; args = [list, value]. That matches CPython's
	// method-call shape, where the descriptor is called like an
	// unbound function.
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_METHOD_DESCRIPTOR_O)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if !objects.IsNone(v) {
		t.Errorf("got %v, want None", v)
	}
	if lst.Len() != 1 {
		t.Fatalf("list len = %d, want 1", lst.Len())
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_METHOD_DESCRIPTOR_O {
		t.Errorf("opcode at call site: got %s, want CALL_METHOD_DESCRIPTOR_O", op.Name())
	}
}

// TestFastCallMethodDescriptorFast exercises
// CALL_METHOD_DESCRIPTOR_FAST with a synthesized MethFastcall
// descriptor on a custom owner type.
//
// CPython: Python/bytecodes.c:4543 _CALL_METHOD_DESCRIPTOR_FAST
func TestFastCallMethodDescriptorFast(t *testing.T) {
	ts := state.NewThread()
	owner := objects.NewType("fastcall_owner", []*objects.Type{objects.ObjectType()})
	inst := objects.NewInstance(owner)
	d := objects.NewMethodDescrConv(owner, "fc", objects.MethFastcall, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("fc() needs self")
		}
		return objects.NewInt(int64(len(args))), nil
	})

	outer, idx := callTwoArgs(d, inst, objects.NewInt(0))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_METHOD_DESCRIPTOR_FAST)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 2 {
		t.Errorf("got %d arg count, want 2", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_METHOD_DESCRIPTOR_FAST {
		t.Errorf("opcode at call site: got %s, want CALL_METHOD_DESCRIPTOR_FAST", op.Name())
	}
}

// TestFastCallMethodDescriptorFastKw exercises
// CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS. The CALL opcode itself
// passes no kwargs map; the descriptor still needs the dual flag tag
// to land on this arm.
//
// CPython: Python/bytecodes.c:4463 _CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS
func TestFastCallMethodDescriptorFastKw(t *testing.T) {
	ts := state.NewThread()
	owner := objects.NewType("fastkw_owner", []*objects.Type{objects.ObjectType()})
	inst := objects.NewInstance(owner)
	d := objects.NewMethodDescrConv(owner, "fckw", objects.MethFastcall|objects.MethKeywords, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(int64(len(args))), nil
	})

	outer, idx := callTwoArgs(d, inst, objects.NewInt(0))
	specialize.Enable(outer)
	stampCallVariant(outer, idx, compile.CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 2 {
		t.Errorf("got %d arg count, want 2", got)
	}
	if op := compile.Opcode(outer.Code[2*idx]); op != compile.CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS {
		t.Errorf("opcode at call site: got %s, want CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS", op.Name())
	}
}

// TestFastCallMethodDescriptorNoArgs exercises
// CALL_METHOD_DESCRIPTOR_NOARGS via a synthesized MethNoArgs
// descriptor.
//
// CPython: Python/bytecodes.c:4505 _CALL_METHOD_DESCRIPTOR_NOARGS
func TestFastCallMethodDescriptorNoArgs(t *testing.T) {
	ts := state.NewThread()
	owner := objects.NewType("noargs_owner", []*objects.Type{objects.ObjectType()})
	inst := objects.NewInstance(owner)
	d := objects.NewMethodDescrConv(owner, "na", objects.MethNoArgs, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(int64(len(args))), nil
	})

	// oparg=0 because METH_NOARGS has only the implicit self.
	body := append(append(append(
		instr(compile.LOAD_CONST, 0),
		instr(compile.LOAD_CONST, 1)...),
		instr(compile.CALL, 0)...),
		instr(compile.RETURN_VALUE, 0)...)
	outer := &objects.Code{
		Code:      body,
		Consts:    []any{d, inst},
		Stacksize: 8,
	}
	specialize.Enable(outer)
	stampCallVariant(outer, 2, compile.CALL_METHOD_DESCRIPTOR_NOARGS)

	v, err := EvalCode(ts, outer, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 1 {
		t.Errorf("got %d arg count, want 1 (just self)", got)
	}
	if op := compile.Opcode(outer.Code[2*2]); op != compile.CALL_METHOD_DESCRIPTOR_NOARGS {
		t.Errorf("opcode at call site: got %s, want CALL_METHOD_DESCRIPTOR_NOARGS", op.Name())
	}
}
