package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// stampLoadAttrCache rewrites the LOAD_ATTR at instr to op and primes
// the inline cache cells (counter cooldown + version + index) so the
// fast-path arm's guards hit. Mirrors what _Py_Specialize_LoadAttr
// does for each variant.
func stampLoadAttrCache(co *objects.Code, instr int, op compile.Opcode, version uint32, idx uint16) {
	specialize.SetOpcode(co.Code, instr, op)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
	specialize.SetCacheU32(co.Code, instr, 2, version)
	specialize.SetCacheCell(co.Code, instr, 4, idx)
}

// TestFastLoadAttrModule drives LOAD_ATTR_MODULE end-to-end: the
// cache cells point at module.x = 11 in a dict whose keys_version is
// pre-recorded, the fast path skips the GetAttr slot lookup and
// returns the cached entry directly.
func TestFastLoadAttrModule(t *testing.T) {
	ts := state.NewThread()
	mod := objects.NewModule("m")
	if err := mod.Dict().SetItem(objects.NewStr("x"), objects.NewInt(11)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	d := mod.Dict()
	idx, ok := d.LookupString(objects.NewStr("x").(*objects.Unicode))
	if !ok {
		t.Fatalf("LookupString: missing")
	}
	version := d.GetKeysVersion()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{mod},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrCache(co, 1, compile.LOAD_ATTR_MODULE, version, uint16(idx))

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 11 {
		t.Errorf("got %d, want 11", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.LOAD_ATTR_MODULE {
		t.Errorf("bytecode rewritten: op=%s, want LOAD_ATTR_MODULE (fast path should not deopt)", op.Name())
	}
}

// TestFastLoadAttrModuleVersionMissDeopts feeds a stale keys_version
// in the cache. The arm must reject the dispatch, fall through to
// maybeDeopt, and EvalCode still returns the right value via the
// generic LOAD_ATTR body.
func TestFastLoadAttrModuleVersionMissDeopts(t *testing.T) {
	ts := state.NewThread()
	mod := objects.NewModule("m")
	if err := mod.Dict().SetItem(objects.NewStr("x"), objects.NewInt(11)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	idx, _ := mod.Dict().LookupString(objects.NewStr("x").(*objects.Unicode))

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{mod},
		Names:     []string{"x"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	// Plant a version that won't match.
	stampLoadAttrCache(co, 1, compile.LOAD_ATTR_MODULE, 0xDEADBEEF, uint16(idx))

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 11 {
		t.Errorf("got %d, want 11", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op == compile.LOAD_ATTR_MODULE {
		t.Errorf("guard miss should have deopted, but op stayed LOAD_ATTR_MODULE")
	}
}

// TestFastLoadAttrSlot exercises the LOAD_ATTR_SLOT arm: build a
// __slots__-only instance, pre-stamp type_version + slot index, run
// LOAD_ATTR and verify the slot value comes back without invoking
// the member descriptor's __get__.
func TestFastLoadAttrSlot(t *testing.T) {
	ts := state.NewThread()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.Slots = []string{"s"}
	objects.SetTypeDescr(cls, "s", objects.NewMemberDescr("s", 0))
	inst := objects.NewInstance(cls)
	// Stamp slot 0 directly through the member descriptor's __set__.
	descr, _ := objects.LookupDescriptor(cls, "s")
	if err := descr.Type().DescrSet(descr, inst, objects.NewInt(99)); err != nil {
		t.Fatalf("descrSet: %v", err)
	}
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"s"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrCache(co, 1, compile.LOAD_ATTR_SLOT, version, 0)

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 99 {
		t.Errorf("got %d, want 99", got)
	}
}

// TestFastLoadAttrClass exercises the LOAD_ATTR_CLASS arm: a user
// class with an int class attribute. The fast path validates the
// type version and reads the cached descriptor pointer from the
// parallel CacheObjects slab.
func TestFastLoadAttrClass(t *testing.T) {
	ts := state.NewThread()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	value := objects.NewInt(123)
	objects.SetTypeDescr(cls, "k", value)
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls},
		Names:     []string{"k"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampLoadAttrCache(co, 1, compile.LOAD_ATTR_CLASS, version, 0)
	specialize.SetCacheObject(co.CacheObjects, 1, value)

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 123 {
		t.Errorf("got %d, want 123", got)
	}
}

// TestFastLoadAttrClassWithMetaclassCheck pins the META arm: cls has
// a user metaclass, so the cache stamps both type_version and
// meta_version; both must validate before the descr is read.
func TestFastLoadAttrClassWithMetaclassCheck(t *testing.T) {
	ts := state.NewThread()
	meta := objects.NewType("Meta", []*objects.Type{objects.TypeType()})
	meta.IsUser = true
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.Init(meta)
	value := objects.NewInt(77)
	objects.SetTypeDescr(cls, "k", value)
	version := cls.VersionTag()
	metaVersion := meta.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{cls},
		Names:     []string{"k"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	specialize.SetOpcode(co.Code, 1, compile.LOAD_ATTR_CLASS_WITH_METACLASS_CHECK)
	specialize.StoreCounter(co.Code, 1, specialize.AdaptiveCounterCooldown())
	specialize.SetCacheU32(co.Code, 1, 2, version)
	specialize.SetCacheU32(co.Code, 1, 4, metaVersion)
	specialize.SetCacheObject(co.CacheObjects, 1, value)

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 77 {
		t.Errorf("got %d, want 77", got)
	}
}

// TestFastLoadAttrMethodNoDict exercises the METHOD_NO_DICT arm: the
// owner is a __slots__-only instance whose type binds a function;
// oparg's bit 0 is set to ask for the unbound-method shape.
func TestFastLoadAttrMethodNoDict(t *testing.T) {
	ts := state.NewThread()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	called := false
	meth := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		called = true
		return objects.NewInt(42), nil
	})
	objects.SetTypeDescr(cls, "m", meth)
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()

	// LOAD_CONST 0; LOAD_ATTR (oparg=1, method shape); CALL 0; RETURN_VALUE.
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 1),
			instr(compile.CALL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"m"},
		Stacksize: 8,
	}
	specialize.Enable(co)
	specialize.SetOpcode(co.Code, 1, compile.LOAD_ATTR_METHOD_NO_DICT)
	specialize.StoreCounter(co.Code, 1, specialize.AdaptiveCounterCooldown())
	specialize.SetCacheU32(co.Code, 1, 2, version)
	specialize.SetCacheObject(co.CacheObjects, 1, meth)

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if !called {
		t.Errorf("method was not invoked")
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

// TestFastLoadAttrNondescriptorNoDict exercises the
// NONDESCRIPTOR_NO_DICT arm: a plain (non-descriptor) class attribute
// on a dict-less type, oparg requesting the plain attribute shape
// (bit 0 cleared).
func TestFastLoadAttrNondescriptorNoDict(t *testing.T) {
	ts := state.NewThread()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	value := objects.NewInt(55)
	objects.SetTypeDescr(cls, "k", value)
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"k"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	specialize.SetOpcode(co.Code, 1, compile.LOAD_ATTR_NONDESCRIPTOR_NO_DICT)
	specialize.StoreCounter(co.Code, 1, specialize.AdaptiveCounterCooldown())
	specialize.SetCacheU32(co.Code, 1, 2, version)
	specialize.SetCacheObject(co.CacheObjects, 1, value)

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 55 {
		t.Errorf("got %d, want 55", got)
	}
}

// TestFastLoadAttrProperty exercises the PROPERTY arm: the cached
// fget pointer is invoked with the owner; the fast path replaces
// the owner on the stack with the property's return value.
func TestFastLoadAttrProperty(t *testing.T) {
	ts := state.NewThread()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	fget := objects.NewBuiltinFunction("getn", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(31), nil
	})
	prop := objects.NewProperty(fget, nil, nil, nil)
	objects.SetTypeDescr(cls, "n", prop)
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()

	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_ATTR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{inst},
		Names:     []string{"n"},
		Stacksize: 4,
	}
	specialize.Enable(co)
	specialize.SetOpcode(co.Code, 1, compile.LOAD_ATTR_PROPERTY)
	specialize.StoreCounter(co.Code, 1, specialize.AdaptiveCounterCooldown())
	specialize.SetCacheU32(co.Code, 1, 2, version)
	specialize.SetCacheObject(co.CacheObjects, 1, fget)

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 31 {
		t.Errorf("got %d, want 31", got)
	}
}
