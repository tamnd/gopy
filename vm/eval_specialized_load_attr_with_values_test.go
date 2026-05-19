package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// makeWithValuesType builds a user class with HasDict + inline-values
// flags set and a single descriptor installed under name. Mirrors the
// shape NewUserType produces for `class C: def m(self): ...`.
func makeWithValuesType(name, descrName string, descr objects.Object) *objects.Type {
	cls := objects.NewType(name, []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.HasDict = true
	cls.TpFlags |= objects.TpFlagInlineValues | objects.TpFlagManagedDict
	objects.SetTypeDescr(cls, descrName, descr)
	return cls
}

// stampLoadAttrMethodWithValues rewrites the LOAD_ATTR at instr to
// LOAD_ATTR_METHOD_WITH_VALUES and primes the cache cells the fast arm
// reads: counter cooldown, type_version (cells 2..3), keys_version
// (cells 4..5), and the descriptor pointer in CacheObjects.
func stampLoadAttrMethodWithValues(co *objects.Code, instr int, typeVer, keysVer uint32, descr objects.Object) {
	specialize.SetOpcode(co.Code, instr, compile.LOAD_ATTR_METHOD_WITH_VALUES)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetLoadMethodTypeVersion(co.Code, instr, typeVer)
	specialize.SetLoadMethodKeysVersion(co.Code, instr, keysVer)
	specialize.SetCacheObject(co.CacheObjects, instr, descr)
}

func stampLoadAttrNondescriptorWithValues(co *objects.Code, instr int, typeVer, keysVer uint32, descr objects.Object) {
	specialize.SetOpcode(co.Code, instr, compile.LOAD_ATTR_NONDESCRIPTOR_WITH_VALUES)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetLoadMethodTypeVersion(co.Code, instr, typeVer)
	specialize.SetLoadMethodKeysVersion(co.Code, instr, keysVer)
	specialize.SetCacheObject(co.CacheObjects, instr, descr)
}

// TestFastLoadAttrMethodWithValuesHit drives the arm end-to-end:
// the cached descriptor is pushed and the trailing CALL_FUNCTION
// fold (unbound-method shape) leaves (descr, self) on the stack.
// The opcode must stay specialized (guards all hit).
func TestFastLoadAttrMethodWithValuesHit(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(42), nil
	})
	cls := makeWithValuesType("C", "m", fn)
	inst := objects.NewInstance(cls)
	typeVer := cls.VersionTag()
	keysVer := cls.CachedKeysVersion()

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
	stampLoadAttrMethodWithValues(co, 1, typeVer, keysVer, fn)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastLoadAttrMethodWithValuesVersionMiss plants a stale
// type_version so the guard misses and the opcode rewinds to LOAD_ATTR.
func TestFastLoadAttrMethodWithValuesVersionMiss(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeWithValuesType("D", "m", fn)
	inst := objects.NewInstance(cls)
	keysVer := cls.CachedKeysVersion()

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
	stampLoadAttrMethodWithValues(co, 1, 0xDEADBEEF, keysVer, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("version miss should have deopted, but op stayed LOAD_ATTR_METHOD_WITH_VALUES")
	}
}

// TestFastLoadAttrMethodWithValuesKeysMiss plants a stale keys_version
// so the cached_keys guard misses. The dispatcher rewrites the opcode
// back to LOAD_ATTR and the generic body runs.
func TestFastLoadAttrMethodWithValuesKeysMiss(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeWithValuesType("E", "m", fn)
	inst := objects.NewInstance(cls)
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
	stampLoadAttrMethodWithValues(co, 1, typeVer, 0xCAFEBABE, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("keys miss should have deopted, but op stayed LOAD_ATTR_METHOD_WITH_VALUES")
	}
}

// TestFastLoadAttrMethodWithValuesInlineInvalidated drives a DELETE_ATTR
// path: the test marks inlineValid=false to mirror what instanceSetAttr
// does on delete. The arm must deopt.
func TestFastLoadAttrMethodWithValuesInlineInvalidated(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeWithValuesType("F", "m", fn)
	inst := objects.NewInstance(cls)
	inst.InvalidateInlineValues()
	typeVer := cls.VersionTag()
	keysVer := cls.CachedKeysVersion()

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
	stampLoadAttrMethodWithValues(co, 1, typeVer, keysVer, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("invalid inline values should have deopted, but op stayed LOAD_ATTR_METHOD_WITH_VALUES")
	}
}

// TestFastLoadAttrMethodWithValuesRejectsBareShape verifies the
// METHOD arm refuses oparg bit 0 == 0; CPython encodes the same
// invariant via assert(oparg & 1) in the bytecode body.
func TestFastLoadAttrMethodWithValuesRejectsBareShape(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeWithValuesType("G", "m", fn)
	inst := objects.NewInstance(cls)
	typeVer := cls.VersionTag()
	keysVer := cls.CachedKeysVersion()

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
	stampLoadAttrMethodWithValues(co, 1, typeVer, keysVer, fn)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("bare shape should have deopted, but op stayed LOAD_ATTR_METHOD_WITH_VALUES")
	}
}

// TestFastLoadAttrNondescriptorWithValuesHit drives the NONDESCRIPTOR
// arm: bit 0 clear, owner consumed, cached value pushed.
func TestFastLoadAttrNondescriptorWithValuesHit(t *testing.T) {
	value := objects.NewInt(7)
	cls := makeWithValuesType("H", "x", value)
	inst := objects.NewInstance(cls)
	typeVer := cls.VersionTag()
	keysVer := cls.CachedKeysVersion()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0), // bare shape
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrNondescriptorWithValues(co, 1, typeVer, keysVer, value)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 7 {
		t.Errorf("got %d, want 7", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_NONDESCRIPTOR_WITH_VALUES {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestSpecializeMethodWithValuesEmits asserts the specializer picks
// METHOD_WITH_VALUES for a user class with HasDict + InlineValues
// where the name is not in the type's cached_keys.
func TestSpecializeMethodWithValuesEmits(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeWithValuesType("I", "m", fn)
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
	specialize.LoadAttr(inst, objects.NewStr("m").(*objects.Unicode), co, 1)
	op := compile.Opcode(co.Code[2*1])
	if op != compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("expected LOAD_ATTR_METHOD_WITH_VALUES, got %s", op.Name())
	}
}

// TestSpecializeMethodWithValuesSkipsWhenInCachedKeys verifies the
// specializer refuses WITH_VALUES when an instance attribute name
// collides with the lookup name (i.e. an instance may shadow the
// class-level descriptor).
func TestSpecializeMethodWithValuesSkipsWhenInCachedKeys(t *testing.T) {
	fn := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeWithValuesType("J", "m", fn)
	cls.AddCachedKey("m") // an instance has stored .m before
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
	specialize.LoadAttr(inst, objects.NewStr("m").(*objects.Unicode), co, 1)
	op := compile.Opcode(co.Code[2*1])
	if op == compile.LOAD_ATTR_METHOD_WITH_VALUES {
		t.Errorf("should NOT specialize METHOD_WITH_VALUES when name is in cached_keys")
	}
}
