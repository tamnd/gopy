package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func loadAttrBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.LOAD_ATTR)))
	buf[0] = byte(compile.LOAD_ATTR)
	return buf
}

func TestLoadAttrModule(t *testing.T) {
	buf := loadAttrBuf()
	m := objects.NewModule("m")
	if err := m.Dict().SetItem(objects.NewStr("x"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	LoadAttr(m, newAttrName("x"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_MODULE {
		t.Fatalf("opcode: got %s want LOAD_ATTR_MODULE", got.Name())
	}
	if v := CacheU32(buf, 0, 2); v == 0 {
		t.Fatalf("keys_version cache empty")
	}
}

func TestLoadAttrModuleWithGetattrFallsBack(t *testing.T) {
	buf := loadAttrBuf()
	m := objects.NewModule("m")
	if err := m.Dict().SetItem(objects.NewStr("__getattr__"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if err := m.Dict().SetItem(objects.NewStr("x"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	LoadAttr(m, newAttrName("x"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR {
		t.Fatalf("opcode: got %s want LOAD_ATTR", got.Name())
	}
}

func TestLoadAttrClass(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	objects.SetTypeDescr(cls, "x", objects.NewInt(7))
	buf := loadAttrBuf()
	LoadAttr(cls, newAttrName("x"), buf, 0)
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
	buf := loadAttrBuf()
	LoadAttr(inst, newAttrName("s"), buf, 0)
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
	buf := loadAttrBuf()
	LoadAttr(inst, newAttrName("v"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_INSTANCE_VALUE {
		t.Fatalf("opcode: got %s want LOAD_ATTR_INSTANCE_VALUE", got.Name())
	}
}

func TestLoadAttrWithHint(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.HasDict = true
	inst := objects.NewInstance(cls)
	if err := inst.Dict().SetItem(objects.NewStr("seed"), objects.NewInt(0)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	buf := loadAttrBuf()
	LoadAttr(inst, newAttrName("missing"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_WITH_HINT {
		t.Fatalf("opcode: got %s want LOAD_ATTR_WITH_HINT", got.Name())
	}
}

func TestLoadAttrUnspecializeOther(t *testing.T) {
	buf := loadAttrBuf()
	LoadAttr(objects.NewInt(1), newAttrName("x"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR {
		t.Fatalf("opcode: got %s want LOAD_ATTR", got.Name())
	}
}
