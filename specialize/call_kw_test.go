package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func callKwBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.CALL_KW)))
	buf[0] = byte(compile.CALL_KW)
	return buf
}

func TestCallKwPy(t *testing.T) {
	buf := callKwBuf()
	f := newSimpleFunction(t, 2)
	CallKw(f, buf, 0, 2)
	if got := compile.Opcode(buf[0]); got != compile.CALL_KW_PY {
		t.Fatalf("opcode: got %s want CALL_KW_PY", got.Name())
	}
	if v := CallFuncVersion(buf, 0); v != f.Version {
		t.Fatalf("func_version: got %d want %d", v, f.Version)
	}
}

func TestCallKwBoundMethod(t *testing.T) {
	buf := callKwBuf()
	f := newSimpleFunction(t, 2)
	bm := objects.NewBoundMethod(f, objects.NewInt(1))
	CallKw(bm, buf, 0, 1)
	if got := compile.Opcode(buf[0]); got != compile.CALL_KW_BOUND_METHOD {
		t.Fatalf("opcode: got %s want CALL_KW_BOUND_METHOD", got.Name())
	}
}

func TestCallKwBoundMethodNonFuncUnspecializes(t *testing.T) {
	buf := callKwBuf()
	bm := objects.NewBoundMethod(objects.NewInt(1), objects.NewInt(2))
	CallKw(bm, buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.CALL_KW {
		t.Fatalf("opcode: got %s want CALL_KW", got.Name())
	}
}

func TestCallKwNonPyForType(t *testing.T) {
	buf := callKwBuf()
	CallKw(objects.ListType, buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.CALL_KW_NON_PY {
		t.Fatalf("opcode: got %s want CALL_KW_NON_PY", got.Name())
	}
}

func TestCallKwNonPyForOther(t *testing.T) {
	buf := callKwBuf()
	CallKw(objects.NewInt(1), buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.CALL_KW_NON_PY {
		t.Fatalf("opcode: got %s want CALL_KW_NON_PY", got.Name())
	}
}
