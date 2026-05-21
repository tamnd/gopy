package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// makeLazyDictType builds a user class in the LAZY_DICT shape:
// HasDict + MANAGED_DICT without INLINE_VALUES. NewUserType produces
// the same shape for heap subclasses of built-in C-port types whose
// own carriers do not back inline values (e.g. list, dict, str). In
// CPython this is tp_dictoffset != 0 without
// Py_TPFLAGS_INLINE_VALUES. The test helper goes through NewUserType
// so Setattro / Getattro land on instanceSetAttr / instanceGetAttr,
// then strips INLINE_VALUES so the runtime guard sees the LAZY_DICT
// flag combo and NewInstance leaves the per-instance dict nil.
func makeLazyDictType(name, descrName string, descr objects.Object) *objects.Type {
	cls := objects.NewUserType(name, nil, objects.NewDict())
	cls.TpFlags &^= objects.TpFlagInlineValues
	objects.SetTypeDescr(cls, descrName, descr)
	return cls
}

// stampLoadAttrMethodLazyDict primes the LOAD_ATTR_METHOD_LAZY_DICT
// cache: counter cooldown, type_version (cells 2..3), and the
// cached descriptor pointer.
func stampLoadAttrMethodLazyDict(co *objects.Code, instr int, typeVer uint32, descr objects.Object) {
	specialize.SetOpcode(co.Code, instr, compile.LOAD_ATTR_METHOD_LAZY_DICT)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetLoadMethodTypeVersion(co.Code, instr, typeVer)
	specialize.SetCacheObject(co.CacheObjects, instr, descr)
}

// TestFastLoadAttrMethodLazyDictHit drives the arm end-to-end: the
// cached descriptor is pushed and the unbound-method shape leaves
// (descr, self) on the stack. The opcode must stay specialized.
func TestFastLoadAttrMethodLazyDictHit(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(42), nil
	})
	cls := makeLazyDictType("C", "m", fn)
	inst := objects.NewInstance(cls)
	if inst.Dict() != nil {
		t.Fatalf("LAZY_DICT instance dict should start nil, got %v", inst.Dict())
	}
	typeVer := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 1), // bit 0 set = method shape
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"m"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrMethodLazyDict(co, 1, typeVer, fn)

	ts := state.NewThread()
	if _, err := EvalCode(ts, co, nil, nil); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_METHOD_LAZY_DICT {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastLoadAttrMethodLazyDictDictMaterializedDeopts verifies the
// arm refuses when the per-instance dict has been materialized (a
// previous SetAttr would land a value that could shadow the class
// descriptor on the next lookup).
func TestFastLoadAttrMethodLazyDictDictMaterializedDeopts(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeLazyDictType("D", "m", fn)
	inst := objects.NewInstance(cls)
	// Materialize the managed dict via the bound Setattro slot
	// (instanceSetAttr) - this is what a real first STORE_ATTR does
	// on a LAZY_DICT instance.
	if err := objects.SetAttr(inst, objects.NewStr("seed"), objects.NewInt(1)); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if inst.Dict() == nil {
		t.Fatalf("seed store should have materialized dict")
	}
	typeVer := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"m"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrMethodLazyDict(co, 1, typeVer, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_LAZY_DICT {
		t.Errorf("materialized dict should have deopted, but op stayed LOAD_ATTR_METHOD_LAZY_DICT")
	}
}

// TestFastLoadAttrMethodLazyDictVersionMissDeopts plants a stale
// type_version so the guard misses and the arm rewinds.
func TestFastLoadAttrMethodLazyDictVersionMissDeopts(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeLazyDictType("E", "m", fn)
	inst := objects.NewInstance(cls)

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"m"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrMethodLazyDict(co, 1, 0xDEADBEEF, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_LAZY_DICT {
		t.Errorf("version miss should have deopted, but op stayed LOAD_ATTR_METHOD_LAZY_DICT")
	}
}

// TestFastLoadAttrMethodLazyDictRejectsBareShape verifies the arm
// refuses oparg bit 0 == 0. CPython asserts the same invariant.
func TestFastLoadAttrMethodLazyDictRejectsBareShape(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeLazyDictType("F", "m", fn)
	inst := objects.NewInstance(cls)
	typeVer := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0), // bare shape, bit 0 clear
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"m"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrMethodLazyDict(co, 1, typeVer, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_LAZY_DICT {
		t.Errorf("bare shape should have deopted, but op stayed LOAD_ATTR_METHOD_LAZY_DICT")
	}
}

// TestSpecializeMethodLazyDictEmits asserts the specializer picks
// METHOD_LAZY_DICT for a user class in MANAGED_DICT-without-
// INLINE_VALUES shape whose per-instance dict is still nil.
func TestSpecializeMethodLazyDictEmits(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeLazyDictType("G", "m", fn)
	inst := objects.NewInstance(cls)
	if inst.Dict() != nil {
		t.Fatalf("LAZY_DICT instance dict should start nil")
	}

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"m"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	specialize.LoadAttr(inst, objects.NewStr("m").(*objects.Unicode), co, 1)
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_METHOD_LAZY_DICT {
		t.Errorf("expected LOAD_ATTR_METHOD_LAZY_DICT, got %s", op.Name())
	}
}
