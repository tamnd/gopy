package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
)

// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Caches.
// The cache widths are load-bearing: a wrong number means dispatch
// reads cache cells from the next opcode.
func TestCacheCounts(t *testing.T) {
	cases := []struct {
		op   compile.Opcode
		want int
	}{
		{compile.TO_BOOL, 3},
		{compile.STORE_SUBSCR, 1},
		{compile.SEND, 1},
		{compile.UNPACK_SEQUENCE, 1},
		{compile.STORE_ATTR, 4},
		{compile.LOAD_GLOBAL, 4},
		{compile.LOAD_SUPER_ATTR, 1},
		{compile.LOAD_ATTR, 9},
		{compile.COMPARE_OP, 1},
		{compile.CONTAINS_OP, 1},
		{compile.JUMP_BACKWARD, 1},
		{compile.POP_JUMP_IF_TRUE, 1},
		{compile.POP_JUMP_IF_FALSE, 1},
		{compile.POP_JUMP_IF_NONE, 1},
		{compile.POP_JUMP_IF_NOT_NONE, 1},
		{compile.FOR_ITER, 1},
		{compile.CALL, 3},
		{compile.CALL_KW, 3},
		{compile.BINARY_OP, 5},
		{compile.NOP, 0},
		{compile.LOAD_CONST, 0},
	}
	for _, c := range cases {
		t.Run(c.op.Name(), func(t *testing.T) {
			if got := CacheCount(c.op); got != c.want {
				t.Fatalf("CacheCount(%s): got %d want %d",
					c.op.Name(), got, c.want)
			}
		})
	}
}

// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Deopt.
// Each specialized variant maps to its adaptive parent.
func TestDeoptMaps(t *testing.T) {
	cases := []struct {
		spec compile.Opcode
		want compile.Opcode
	}{
		// Identity for unspecialized opcodes
		{compile.LOAD_ATTR, compile.LOAD_ATTR},
		{compile.BINARY_OP, compile.BINARY_OP},
		{compile.CALL, compile.CALL},
		{compile.NOP, compile.NOP},
		// Specialized -> parent
		{compile.LOAD_ATTR_INSTANCE_VALUE, compile.LOAD_ATTR},
		{compile.LOAD_ATTR_MODULE, compile.LOAD_ATTR},
		{compile.LOAD_ATTR_SLOT, compile.LOAD_ATTR},
		{compile.LOAD_ATTR_PROPERTY, compile.LOAD_ATTR},
		{compile.LOAD_ATTR_METHOD_NO_DICT, compile.LOAD_ATTR},
		{compile.BINARY_OP_ADD_INT, compile.BINARY_OP},
		{compile.BINARY_OP_ADD_FLOAT, compile.BINARY_OP},
		{compile.BINARY_OP_ADD_UNICODE, compile.BINARY_OP},
		{compile.BINARY_OP_MULTIPLY_INT, compile.BINARY_OP},
		{compile.COMPARE_OP_INT, compile.COMPARE_OP},
		{compile.COMPARE_OP_FLOAT, compile.COMPARE_OP},
		{compile.COMPARE_OP_STR, compile.COMPARE_OP},
		{compile.TO_BOOL_BOOL, compile.TO_BOOL},
		{compile.TO_BOOL_INT, compile.TO_BOOL},
		{compile.TO_BOOL_NONE, compile.TO_BOOL},
		{compile.LOAD_GLOBAL_BUILTIN, compile.LOAD_GLOBAL},
		{compile.LOAD_GLOBAL_MODULE, compile.LOAD_GLOBAL},
		{compile.STORE_SUBSCR_LIST_INT, compile.STORE_SUBSCR},
		{compile.STORE_SUBSCR_DICT, compile.STORE_SUBSCR},
		{compile.STORE_ATTR_SLOT, compile.STORE_ATTR},
		{compile.FOR_ITER_LIST, compile.FOR_ITER},
		{compile.FOR_ITER_TUPLE, compile.FOR_ITER},
		{compile.FOR_ITER_RANGE, compile.FOR_ITER},
		{compile.FOR_ITER_GEN, compile.FOR_ITER},
		{compile.SEND_GEN, compile.SEND},
		{compile.UNPACK_SEQUENCE_LIST, compile.UNPACK_SEQUENCE},
		{compile.UNPACK_SEQUENCE_TUPLE, compile.UNPACK_SEQUENCE},
		{compile.UNPACK_SEQUENCE_TWO_TUPLE, compile.UNPACK_SEQUENCE},
		{compile.CALL_PY_EXACT_ARGS, compile.CALL},
		{compile.CALL_BUILTIN_FAST, compile.CALL},
		{compile.CALL_BOUND_METHOD_GENERAL, compile.CALL},
		{compile.CALL_LIST_APPEND, compile.CALL},
		{compile.CALL_TYPE_1, compile.CALL},
		{compile.CALL_KW_PY, compile.CALL_KW},
		{compile.CALL_KW_BOUND_METHOD, compile.CALL_KW},
		{compile.CONTAINS_OP_DICT, compile.CONTAINS_OP},
		{compile.CONTAINS_OP_SET, compile.CONTAINS_OP},
		{compile.LOAD_SUPER_ATTR_ATTR, compile.LOAD_SUPER_ATTR},
		{compile.LOAD_SUPER_ATTR_METHOD, compile.LOAD_SUPER_ATTR},
		// RESUME_CHECK is a specialized variant of RESUME
		{compile.RESUME_CHECK, compile.RESUME},
	}
	for _, c := range cases {
		t.Run(c.spec.Name(), func(t *testing.T) {
			if got := Deopt(c.spec); got != c.want {
				t.Fatalf("Deopt(%s): got %s want %s",
					c.spec.Name(), got.Name(), c.want.Name())
			}
		})
	}
}
