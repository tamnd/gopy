package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func loadSuperBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.LOAD_SUPER_ATTR)))
	buf[0] = byte(compile.LOAD_SUPER_ATTR)
	return buf
}

func TestLoadSuperAttrAttr(t *testing.T) {
	buf := loadSuperBuf()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	LoadSuperAttr(objects.SuperType, cls, buf, 0, false)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_SUPER_ATTR_ATTR {
		t.Fatalf("opcode: got %s want LOAD_SUPER_ATTR_ATTR", got.Name())
	}
}

func TestLoadSuperAttrMethod(t *testing.T) {
	buf := loadSuperBuf()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	LoadSuperAttr(objects.SuperType, cls, buf, 0, true)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_SUPER_ATTR_METHOD {
		t.Fatalf("opcode: got %s want LOAD_SUPER_ATTR_METHOD", got.Name())
	}
}

func TestLoadSuperAttrShadowed(t *testing.T) {
	buf := loadSuperBuf()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	LoadSuperAttr(objects.NewInt(1), cls, buf, 0, false)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_SUPER_ATTR {
		t.Fatalf("opcode: got %s want LOAD_SUPER_ATTR", got.Name())
	}
}

func TestLoadSuperAttrNonType(t *testing.T) {
	buf := loadSuperBuf()
	LoadSuperAttr(objects.SuperType, objects.NewInt(1), buf, 0, false)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_SUPER_ATTR {
		t.Fatalf("opcode: got %s want LOAD_SUPER_ATTR", got.Name())
	}
}
