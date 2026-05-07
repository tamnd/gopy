package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func containsOpBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.CONTAINS_OP)))
	buf[0] = byte(compile.CONTAINS_OP)
	return buf
}

func TestContainsOpDict(t *testing.T) {
	buf := containsOpBuf()
	ContainsOp(objects.NewDict(), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.CONTAINS_OP_DICT {
		t.Fatalf("opcode: got %s want CONTAINS_OP_DICT", got.Name())
	}
}

func TestContainsOpSet(t *testing.T) {
	buf := containsOpBuf()
	ContainsOp(objects.NewSet(), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.CONTAINS_OP_SET {
		t.Fatalf("opcode: got %s want CONTAINS_OP_SET", got.Name())
	}
}

func TestContainsOpFrozenSet(t *testing.T) {
	fs, err := objects.NewFrozenset(nil)
	if err != nil {
		t.Fatalf("NewFrozenset: %v", err)
	}
	buf := containsOpBuf()
	ContainsOp(fs, buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.CONTAINS_OP_SET {
		t.Fatalf("opcode: got %s want CONTAINS_OP_SET", got.Name())
	}
}

func TestContainsOpUnspecializeOther(t *testing.T) {
	buf := containsOpBuf()
	ContainsOp(objects.NewList(nil), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.CONTAINS_OP {
		t.Fatalf("opcode: got %s want CONTAINS_OP", got.Name())
	}
}
