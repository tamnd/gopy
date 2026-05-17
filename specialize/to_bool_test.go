package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func toBoolBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.TO_BOOL)))
	buf[0] = byte(compile.TO_BOOL)
	return buf
}

func TestToBoolBool(t *testing.T) {
	buf := toBoolBuf()
	ToBool(objects.True(), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL_BOOL {
		t.Fatalf("opcode: got %s want TO_BOOL_BOOL", got.Name())
	}
}

func TestToBoolInt(t *testing.T) {
	buf := toBoolBuf()
	ToBool(objects.NewInt(0), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL_INT {
		t.Fatalf("opcode: got %s want TO_BOOL_INT", got.Name())
	}
}

func TestToBoolList(t *testing.T) {
	buf := toBoolBuf()
	ToBool(objects.NewList(nil), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL_LIST {
		t.Fatalf("opcode: got %s want TO_BOOL_LIST", got.Name())
	}
}

func TestToBoolNone(t *testing.T) {
	buf := toBoolBuf()
	ToBool(objects.None(), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL_NONE {
		t.Fatalf("opcode: got %s want TO_BOOL_NONE", got.Name())
	}
}

func TestToBoolStr(t *testing.T) {
	buf := toBoolBuf()
	ToBool(objects.NewStr("x"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL_STR {
		t.Fatalf("opcode: got %s want TO_BOOL_STR", got.Name())
	}
}

func TestToBoolAlwaysTrueUserType(t *testing.T) {
	buf := toBoolBuf()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	inst := objects.NewInstance(cls)
	ToBool(inst, buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL_ALWAYS_TRUE {
		t.Fatalf("opcode: got %s want TO_BOOL_ALWAYS_TRUE", got.Name())
	}
	if v := ToBoolVersion(buf, 0); v == 0 || v != cls.VersionTag() {
		t.Fatalf("version cache: got %d want %d", v, cls.VersionTag())
	}
}

func TestToBoolUnspecializeOther(t *testing.T) {
	buf := toBoolBuf()
	ToBool(objects.NewTuple(nil), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.TO_BOOL {
		t.Fatalf("opcode: got %s want TO_BOOL", got.Name())
	}
}
