package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func cmpBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.COMPARE_OP)))
	buf[0] = byte(compile.COMPARE_OP)
	return buf
}

// arg encodes a CompareOp into the high bits of the oparg the way
// CPython's codegen does: `cmp << 5 | other`.
func cmpArg(cmp objects.CompareOp) int32 {
	return int32(cmp) << 5
}

func TestCompareOpInt(t *testing.T) {
	buf := cmpBuf()
	CompareOp(objects.NewInt(1), objects.NewInt(2), buf, 0, cmpArg(objects.CompareLT))
	if got := compile.Opcode(buf[0]); got != compile.COMPARE_OP_INT {
		t.Fatalf("opcode: got %s want COMPARE_OP_INT", got.Name())
	}
}

func TestCompareOpFloat(t *testing.T) {
	buf := cmpBuf()
	CompareOp(objects.NewFloat(1), objects.NewFloat(2), buf, 0, cmpArg(objects.CompareLT))
	if got := compile.Opcode(buf[0]); got != compile.COMPARE_OP_FLOAT {
		t.Fatalf("opcode: got %s", got.Name())
	}
}

func TestCompareOpStrEqOnly(t *testing.T) {
	buf := cmpBuf()
	CompareOp(objects.NewStr("a"), objects.NewStr("b"), buf, 0, cmpArg(objects.CompareEQ))
	if got := compile.Opcode(buf[0]); got != compile.COMPARE_OP_STR {
		t.Fatalf("opcode (==): got %s", got.Name())
	}

	buf = cmpBuf()
	CompareOp(objects.NewStr("a"), objects.NewStr("b"), buf, 0, cmpArg(objects.CompareLT))
	if got := compile.Opcode(buf[0]); got != compile.COMPARE_OP {
		t.Fatalf("opcode (<): got %s want COMPARE_OP (str < falls back)",
			got.Name())
	}
}

func TestCompareOpUnspecializeOnTypeMismatch(t *testing.T) {
	buf := cmpBuf()
	CompareOp(objects.NewInt(1), objects.NewFloat(2), buf, 0, cmpArg(objects.CompareEQ))
	if got := compile.Opcode(buf[0]); got != compile.COMPARE_OP {
		t.Fatalf("opcode: got %s want COMPARE_OP", got.Name())
	}
}
