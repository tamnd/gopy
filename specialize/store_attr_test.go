package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func storeAttrBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.STORE_ATTR)))
	buf[0] = byte(compile.STORE_ATTR)
	return buf
}

func newAttrName(s string) *objects.Unicode {
	return objects.NewStr(s).(*objects.Unicode)
}

func TestStoreAttrSlot(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.Slots = []string{"x"}
	objects.SetTypeDescr(cls, "x", objects.NewMemberDescr("x", 0))
	inst := objects.NewInstance(cls)
	buf := storeAttrBuf()
	StoreAttr(inst, newAttrName("x"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_ATTR_SLOT {
		t.Fatalf("opcode: got %s want STORE_ATTR_SLOT", got.Name())
	}
	if v := CacheU32(buf, 0, 2); v == 0 || v != cls.VersionTag() {
		t.Fatalf("version cache: got %d want %d", v, cls.VersionTag())
	}
	if idx := CacheCell(buf, 0, 4); idx != 0 {
		t.Fatalf("index cache: got %d want 0", idx)
	}
}

func TestStoreAttrInstanceValue(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.HasDict = true
	inst := objects.NewInstance(cls)
	if err := inst.Dict().SetItem(objects.NewStr("y"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	buf := storeAttrBuf()
	StoreAttr(inst, newAttrName("y"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_ATTR_INSTANCE_VALUE {
		t.Fatalf("opcode: got %s want STORE_ATTR_INSTANCE_VALUE", got.Name())
	}
}

func TestStoreAttrWithHint(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	cls.HasDict = true
	inst := objects.NewInstance(cls)
	buf := storeAttrBuf()
	StoreAttr(inst, newAttrName("z"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_ATTR_WITH_HINT {
		t.Fatalf("opcode: got %s want STORE_ATTR_WITH_HINT", got.Name())
	}
}

func TestStoreAttrUnspecializeNonInstance(t *testing.T) {
	buf := storeAttrBuf()
	StoreAttr(objects.NewInt(1), newAttrName("x"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_ATTR {
		t.Fatalf("opcode: got %s want STORE_ATTR", got.Name())
	}
}

func TestStoreAttrUnspecializeNoDict(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	inst := objects.NewInstance(cls)
	buf := storeAttrBuf()
	StoreAttr(inst, newAttrName("z"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_ATTR {
		t.Fatalf("opcode: got %s want STORE_ATTR", got.Name())
	}
}
