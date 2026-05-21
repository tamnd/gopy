package vm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// stampLoadAttrGetattributeOverridden rewrites the LOAD_ATTR at instr
// to LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN and primes the cache cells the
// fast arm reads: counter cooldown so trySpecialized routes here
// instead of ticking the adaptive panel, type_version so the version
// guard passes, and CacheObjects[instr] so the cached function pointer
// resolves. Mirrors what specializeGetattributeOverridden stamps.
func stampLoadAttrGetattributeOverridden(co *objects.Code, instr int, version uint32, ga objects.Object) {
	specialize.SetOpcode(co.Code, instr, compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetLoadMethodTypeVersion(co.Code, instr, version)
	specialize.SetCacheObject(co.CacheObjects, instr, ga)
}

// makeGAType builds a user class whose Getattro slot routes through
// slotTpGetattroHook by installing ga as __getattribute__. The returned
// function is the same pointer the test caches into CacheObjects so the
// fast arm calls back into ga with (owner, name).
func makeGAType(t *testing.T, name string, ga objects.Object) *objects.Type {
	t.Helper()
	cls := objects.NewType(name, []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	objects.SetTypeDescr(cls, "__getattribute__", ga)
	return cls
}

// TestFastLoadAttrGetattributeOverriddenHit drives the arm end-to-end:
// the cached __getattribute__ runs with (instance, "x") and the result
// it returns lands on top of the stack. The opcode must stay specialized
// (no deopt) because the version guard hits.
func TestFastLoadAttrGetattributeOverriddenHit(t *testing.T) {
	ga := objects.NewBuiltinFunction("ga", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		// args = (owner, name); return 77 regardless to make the assertion crisp.
		if len(args) != 2 {
			t.Fatalf("ga called with %d args, want 2", len(args))
		}
		if u, ok := args[1].(*objects.Unicode); !ok || u.Value() != "x" {
			t.Fatalf("ga called with name %v, want \"x\"", args[1])
		}
		return objects.NewInt(77), nil
	})
	cls := makeGAType(t, "C", ga)
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrGetattributeOverridden(co, 1, version, ga)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 77 {
		t.Errorf("got %d, want 77", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastLoadAttrGetattributeOverriddenVersionMiss plants a stale
// type_version in the cache. The fast arm's guard fails (ok=false), the
// dispatcher rewrites the opcode back to LOAD_ATTR, and the generic
// body runs. The post-deopt result depends on tp_getattro wiring
// (objects.NewType bypasses fixupSlotDispatchers so the user GA isn't
// installed here); we assert only the opcode bounce, matching the
// MODULE arm's deopt test.
func TestFastLoadAttrGetattributeOverriddenVersionMiss(t *testing.T) {
	ga := objects.NewBuiltinFunction("ga", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(7), nil
	})
	cls := makeGAType(t, "D", ga)
	inst := objects.NewInstance(cls)

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	// Stamp a bogus version so the guard misses.
	stampLoadAttrGetattributeOverridden(co, 1, 0xDEADBEEF, ga)

	ts := state.NewThread()
	_, _ = EvalCode(ts, co, nil, nil)
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN {
		t.Errorf("guard miss should have deopted, but op stayed LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN")
	}
}

// TestFastLoadAttrGetattributeOverriddenRejectsMethodBit verifies the
// arm refuses oparg bit 0 (the unbound-method shape). CPython asserts
// (oparg & 1) == 0 because slot_tp_getattro doesn't produce a
// (descr, self) pair: the user function decides the shape itself.
func TestFastLoadAttrGetattributeOverriddenRejectsMethodBit(t *testing.T) {
	ga := objects.NewBuiltinFunction("ga", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	cls := makeGAType(t, "E", ga)
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 1|(0<<1)), // load_method bit set, name idx 0
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrGetattributeOverridden(co, 1, version, ga)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	// The generic body runs after the bit-shape refusal; with the
	// method-shape oparg it tries to push (descr, self), which works
	// for a slot-routed GA value of 0, then RETURN_VALUE pops one. Not
	// asserting the return value here; the contract is just that the
	// fast arm bounced and the opcode rewound to LOAD_ATTR.
	_ = err
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN {
		t.Errorf("method-shape oparg should have deopted, but op stayed LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN")
	}
}

// TestFastLoadAttrGetattributeOverriddenErrorPropagates checks that an
// exception raised inside the user __getattribute__ flows back out of
// the fast arm and unwinds the frame. The arm returns (taken=true,
// err) and the dispatcher must surface err rather than swallow it.
func TestFastLoadAttrGetattributeOverriddenErrorPropagates(t *testing.T) {
	ga := objects.NewBuiltinFunction("ga", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("AttributeError: custom boom")
	})
	cls := makeGAType(t, "F", ga)
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrGetattributeOverridden(co, 1, version, ga)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected AttributeError from user GA to propagate")
	}
	if !strings.Contains(err.Error(), "custom boom") {
		t.Errorf("err = %v, want it to mention 'custom boom'", err)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN {
		t.Errorf("error propagation should not deopt: op=%s", op.Name())
	}
}
