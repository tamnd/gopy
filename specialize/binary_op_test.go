package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func binOpBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.BINARY_OP)))
	buf[0] = byte(compile.BINARY_OP)
	return buf
}

func TestBinaryOpAddInt(t *testing.T) {
	buf := binOpBuf()
	BinaryOp(objects.NewInt(1), objects.NewInt(2), buf, 0, compile.NB_ADD,
		compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_ADD_INT {
		t.Fatalf("opcode: got %s want BINARY_OP_ADD_INT", got.Name())
	}
}

func TestBinaryOpAddFloat(t *testing.T) {
	buf := binOpBuf()
	BinaryOp(objects.NewFloat(1.0), objects.NewFloat(2.0), buf, 0,
		compile.NB_ADD, compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_ADD_FLOAT {
		t.Fatalf("opcode: got %s want BINARY_OP_ADD_FLOAT", got.Name())
	}
}

func TestBinaryOpAddStr(t *testing.T) {
	buf := binOpBuf()
	BinaryOp(objects.NewStr("a"), objects.NewStr("b"), buf, 0,
		compile.NB_ADD, compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_ADD_UNICODE {
		t.Fatalf("opcode: got %s want BINARY_OP_ADD_UNICODE", got.Name())
	}
}

func TestBinaryOpInplaceAddStrSameLocal(t *testing.T) {
	buf := binOpBuf()
	s := objects.NewStr("a")
	locals := []objects.Object{s}
	BinaryOp(s, objects.NewStr("b"), buf, 0, compile.NB_ADD,
		compile.STORE_FAST, 0, locals)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_INPLACE_ADD_UNICODE {
		t.Fatalf("opcode: got %s want BINARY_OP_INPLACE_ADD_UNICODE", got.Name())
	}
}

func TestBinaryOpMultiplyInt(t *testing.T) {
	buf := binOpBuf()
	BinaryOp(objects.NewInt(2), objects.NewInt(3), buf, 0,
		compile.NB_MULTIPLY, compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_MULTIPLY_INT {
		t.Fatalf("opcode: got %s", got.Name())
	}
}

func TestBinaryOpSubtractFloat(t *testing.T) {
	buf := binOpBuf()
	BinaryOp(objects.NewFloat(1), objects.NewFloat(2), buf, 0,
		compile.NB_SUBTRACT, compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_SUBTRACT_FLOAT {
		t.Fatalf("opcode: got %s", got.Name())
	}
}

func TestBinaryOpSubscrListInt(t *testing.T) {
	buf := binOpBuf()
	l := objects.NewList(nil)
	BinaryOp(l, objects.NewInt(0), buf, 0, compile.NB_SUBSCR,
		compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_SUBSCR_LIST_INT {
		t.Fatalf("opcode: got %s", got.Name())
	}
}

func TestBinaryOpSubscrDict(t *testing.T) {
	buf := binOpBuf()
	BinaryOp(objects.NewDict(), objects.NewStr("k"), buf, 0,
		compile.NB_SUBSCR, compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP_SUBSCR_DICT {
		t.Fatalf("opcode: got %s", got.Name())
	}
}

func TestBinaryOpUnspecializeOnTypeMismatch(t *testing.T) {
	buf := binOpBuf()
	StoreCounter(buf, 0, AdaptiveCounterWarmup())
	BinaryOp(objects.NewInt(1), objects.NewFloat(2), buf, 0,
		compile.NB_ADD, compile.NOP, 0, nil)
	if got := compile.Opcode(buf[0]); got != compile.BINARY_OP {
		t.Fatalf("opcode: got %s want BINARY_OP", got.Name())
	}
}
