package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func callBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.CALL)))
	buf[0] = byte(compile.CALL)
	return buf
}

func newSimpleFunction(t *testing.T, argcount int) *objects.Function {
	t.Helper()
	c := objects.NewCode()
	c.Argcount = argcount
	c.Flags = int(compile.CoOptimized)
	f := objects.NewFunction("f", c, objects.NewDict())
	f.Version = 0xdeadbeef
	return f
}

func TestCallPyExactArgs(t *testing.T) {
	buf := callBuf()
	f := newSimpleFunction(t, 2)
	Call(f, buf, 0, 2, 2)
	if got := compile.Opcode(buf[0]); got != compile.CALL_PY_EXACT_ARGS {
		t.Fatalf("opcode: got %s want CALL_PY_EXACT_ARGS", got.Name())
	}
	if v := CacheU32(buf, 0, 2); v != f.Version {
		t.Fatalf("func_version: got %d want %d", v, f.Version)
	}
}

func TestCallPyGeneral(t *testing.T) {
	buf := callBuf()
	f := newSimpleFunction(t, 3)
	Call(f, buf, 0, 2, 2)
	if got := compile.Opcode(buf[0]); got != compile.CALL_PY_GENERAL {
		t.Fatalf("opcode: got %s want CALL_PY_GENERAL", got.Name())
	}
}

func TestCallBoundMethodExactArgs(t *testing.T) {
	buf := callBuf()
	f := newSimpleFunction(t, 2)
	bm := objects.NewBoundMethod(f, objects.NewInt(1))
	Call(bm, buf, 0, 1, 1)
	if got := compile.Opcode(buf[0]); got != compile.CALL_BOUND_METHOD_EXACT_ARGS {
		t.Fatalf("opcode: got %s want CALL_BOUND_METHOD_EXACT_ARGS", got.Name())
	}
}

func TestCallStr1(t *testing.T) {
	buf := callBuf()
	Call(objects.StrType(), buf, 0, 1, 1)
	if got := compile.Opcode(buf[0]); got != compile.CALL_STR_1 {
		t.Fatalf("opcode: got %s want CALL_STR_1", got.Name())
	}
}

func TestCallType1(t *testing.T) {
	buf := callBuf()
	Call(objects.TypeType(), buf, 0, 1, 1)
	if got := compile.Opcode(buf[0]); got != compile.CALL_TYPE_1 {
		t.Fatalf("opcode: got %s want CALL_TYPE_1", got.Name())
	}
}

func TestCallTuple1(t *testing.T) {
	buf := callBuf()
	Call(objects.TupleType, buf, 0, 1, 1)
	if got := compile.Opcode(buf[0]); got != compile.CALL_TUPLE_1 {
		t.Fatalf("opcode: got %s want CALL_TUPLE_1", got.Name())
	}
}

func TestCallBuiltinClass(t *testing.T) {
	buf := callBuf()
	Call(objects.ListType, buf, 0, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.CALL_BUILTIN_CLASS {
		t.Fatalf("opcode: got %s want CALL_BUILTIN_CLASS", got.Name())
	}
}

func TestCallUserClassFallsBackToNonPy(t *testing.T) {
	buf := callBuf()
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	Call(cls, buf, 0, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.CALL_NON_PY_GENERAL {
		t.Fatalf("opcode: got %s want CALL_NON_PY_GENERAL", got.Name())
	}
}

func TestCallNonPyGeneralForOther(t *testing.T) {
	buf := callBuf()
	Call(objects.NewInt(1), buf, 0, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.CALL_NON_PY_GENERAL {
		t.Fatalf("opcode: got %s want CALL_NON_PY_GENERAL", got.Name())
	}
}
