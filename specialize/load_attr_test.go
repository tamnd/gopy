package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func loadAttrCode() *objects.Code {
	buf := make([]byte, 2*(1+CacheCount(compile.LOAD_ATTR)))
	buf[0] = byte(compile.LOAD_ATTR)
	co := &objects.Code{Code: buf}
	co.CacheObjects = make([]objects.Object, len(buf)/CodeUnitWidth)
	return co
}

func TestLoadAttrModule(t *testing.T) {
	co := loadAttrCode(); buf := co.Code
	m := objects.NewModule("m")
	if err := m.Dict().SetItem(objects.NewStr("x"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	LoadAttr(m, newAttrName("x"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_MODULE {
		t.Fatalf("opcode: got %s want LOAD_ATTR_MODULE", got.Name())
	}
	if v := CacheU32(co.Code, 0, 2); v == 0 {
		t.Fatalf("keys_version cache empty")
	}
}

func TestLoadAttrModuleWithGetattrFallsBack(t *testing.T) {
	co := loadAttrCode(); buf := co.Code
	m := objects.NewModule("m")
	if err := m.Dict().SetItem(objects.NewStr("__getattr__"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if err := m.Dict().SetItem(objects.NewStr("x"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	LoadAttr(m, newAttrName("x"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR {
		t.Fatalf("opcode: got %s want LOAD_ATTR", got.Name())
	}
}

func TestLoadAttrClass(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	objects.SetTypeDescr(cls, "x", objects.NewInt(7))
	co := loadAttrCode(); buf := co.Code
	LoadAttr(cls, newAttrName("x"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_CLASS {
		t.Fatalf("opcode: got %s want LOAD_ATTR_CLASS", got.Name())
	}
}

func TestLoadAttrSlot(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.Slots = []string{"s"}
	objects.SetTypeDescr(cls, "s", objects.NewMemberDescr("s", 0))
	inst := objects.NewInstance(cls)
	co := loadAttrCode(); buf := co.Code
	LoadAttr(inst, newAttrName("s"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_SLOT {
		t.Fatalf("opcode: got %s want LOAD_ATTR_SLOT", got.Name())
	}
}

func TestLoadAttrInstanceValue(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.HasDict = true
	inst := objects.NewInstance(cls)
	if err := inst.Dict().SetItem(objects.NewStr("v"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	co := loadAttrCode(); buf := co.Code
	LoadAttr(inst, newAttrName("v"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_INSTANCE_VALUE {
		t.Fatalf("opcode: got %s want LOAD_ATTR_INSTANCE_VALUE", got.Name())
	}
}

func TestLoadAttrAbsentKeyFallsBack(t *testing.T) {
	// Absent attribute does not specialize: WITH_HINT requires the
	// managed-dict-offset machinery gopy doesn't model yet, so the
	// opcode falls back to the adaptive LOAD_ATTR.
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.HasDict = true
	inst := objects.NewInstance(cls)
	if err := inst.Dict().SetItem(objects.NewStr("seed"), objects.NewInt(0)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	co := loadAttrCode()
	buf := co.Code
	LoadAttr(inst, newAttrName("missing"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR {
		t.Fatalf("opcode: got %s want LOAD_ATTR", got.Name())
	}
}

func TestLoadAttrProperty(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	getter := objects.NewBuiltinFunction("getn", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	prop := objects.NewProperty(getter, nil, nil, nil)
	objects.SetTypeDescr(cls, "n", prop)
	inst := objects.NewInstance(cls)
	co := loadAttrCode(); buf := co.Code
	LoadAttr(inst, newAttrName("n"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_PROPERTY {
		t.Fatalf("opcode: got %s want LOAD_ATTR_PROPERTY", got.Name())
	}
}

func TestLoadAttrMethodNoDict(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	// HasDict stays false: dict-less type, method-style descriptor → METHOD_NO_DICT.
	meth := objects.NewBuiltinFunction("m", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	objects.SetTypeDescr(cls, "m", meth)
	inst := objects.NewInstance(cls)
	co := loadAttrCode(); buf := co.Code
	LoadAttr(inst, newAttrName("m"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_METHOD_NO_DICT {
		t.Fatalf("opcode: got %s want LOAD_ATTR_METHOD_NO_DICT", got.Name())
	}
}

func TestLoadAttrNondescriptorNoDict(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	// Plain class attribute (int) — not a descriptor.
	objects.SetTypeDescr(cls, "k", objects.NewInt(42))
	inst := objects.NewInstance(cls)
	co := loadAttrCode(); buf := co.Code
	LoadAttr(inst, newAttrName("k"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_NONDESCRIPTOR_NO_DICT {
		t.Fatalf("opcode: got %s want LOAD_ATTR_NONDESCRIPTOR_NO_DICT", got.Name())
	}
}

func TestLoadAttrClassWithMetaclassCheck(t *testing.T) {
	// Build a user-defined metaclass so the lookup target's Type() is
	// mutable, triggering the META check arm.
	meta := objects.NewType("Meta", []*objects.Type{objects.TypeType()})
	meta.IsUser = true
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	// Re-Init binds cls.ob_type to the custom metaclass.
	cls.Header.Init(meta)
	objects.SetTypeDescr(cls, "x", objects.NewInt(1))
	co := loadAttrCode(); buf := co.Code
	LoadAttr(cls, newAttrName("x"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_CLASS_WITH_METACLASS_CHECK {
		t.Fatalf("opcode: got %s want LOAD_ATTR_CLASS_WITH_METACLASS_CHECK", got.Name())
	}
}

func TestLoadAttrUnspecializeOther(t *testing.T) {
	co := loadAttrCode(); buf := co.Code
	LoadAttr(objects.NewInt(1), newAttrName("x"), co, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR {
		t.Fatalf("opcode: got %s want LOAD_ATTR", got.Name())
	}
}
