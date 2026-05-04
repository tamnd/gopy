// Stack-depth analysis for the flowgraph. CPython runs a forward
// dataflow over the CFG, propagating the post-instruction stack
// height; the maximum across all blocks becomes co_stacksize.
//
// Until the full per-opcode metadata table from
// pycore_opcode_metadata.h is generated, we use a hand-written effect
// table covering every opcode the codegen actually emits. Opcodes
// outside the table contribute zero effect, which is the same default
// CPython falls back to for unknown ops in the assertion build.
//
// CPython: Python/flowgraph.c:L809 calculate_stackdepth +
// Python/compile.c stackdepth bookkeeping.

package compile

import "fmt"

// stackEffect returns (push - pop) for op with the given oparg. The
// taken parameter selects the jump-taken vs not-taken effect for
// conditional jumps; in CPython this is the pseudo "jump" stack
// effect column. For non-jumping ops the value is ignored.
//
// CPython: Python/flowgraph.c:L768 get_stack_effect
func stackEffect(op Opcode, oparg int32, taken bool) int {
	if e, ok := stackEffectTable[op]; ok {
		if taken && e.jumpEffect != noJumpEffect {
			return e.jumpEffect
		}
		if e.opargFunc != nil {
			return e.opargFunc(oparg)
		}
		return e.effect
	}
	// Unknown opcode: zero effect. CPython would assert here in debug
	// builds; the caller asserts again at the end of the pass when
	// the running depth would go negative.
	return 0
}

const noJumpEffect = -9999

type stackEffectEntry struct {
	effect     int
	jumpEffect int
	opargFunc  func(int32) int
}

func entry(e int) stackEffectEntry {
	return stackEffectEntry{effect: e, jumpEffect: noJumpEffect}
}

func entryJump(e, jump int) stackEffectEntry {
	return stackEffectEntry{effect: e, jumpEffect: jump}
}

func entryFunc(f func(int32) int) stackEffectEntry {
	return stackEffectEntry{jumpEffect: noJumpEffect, opargFunc: f}
}

// stackEffectTable mirrors the per-opcode rows from
// Python/compile.c PyCompile_OpcodeStackEffectWithJump (the same data
// flowgraph.c reads via _PyOpcode_num_popped/_pushed). Only the rows
// our codegen actually emits are populated.
//
// CPython: Python/compile.c PyCompile_OpcodeStackEffectWithJump
var stackEffectTable = map[Opcode]stackEffectEntry{
	NOP:                          entry(0),
	RESUME:                       entry(0),
	POP_TOP:                      entry(-1),
	POP_ITER:                     entry(-1),
	PUSH_NULL:                    entry(1),
	END_FOR:                      entry(-1),
	END_SEND:                     entry(-1),
	UNARY_NEGATIVE:               entry(0),
	UNARY_NOT:                    entry(0),
	UNARY_INVERT:                 entry(0),
	GET_ITER:                     entry(0),
	GET_LEN:                      entry(1),
	GET_AITER:                    entry(0),
	GET_ANEXT:                    entry(1),
	GET_AWAITABLE:                entry(0),
	GET_YIELD_FROM_ITER:          entry(0),
	BINARY_OP:                    entry(-1),
	BINARY_SLICE:                 entry(-2),
	STORE_SLICE:                  entry(-4),
	STORE_SUBSCR:                 entry(-3),
	DELETE_SUBSCR:                entry(-2),
	BUILD_SLICE:                  entryFunc(func(a int32) int { return 1 - int(a) }),
	BUILD_LIST:                   entryFunc(func(a int32) int { return 1 - int(a) }),
	BUILD_SET:                    entryFunc(func(a int32) int { return 1 - int(a) }),
	BUILD_TUPLE:                  entryFunc(func(a int32) int { return 1 - int(a) }),
	BUILD_MAP:                    entryFunc(func(a int32) int { return 1 - 2*int(a) }),
	BUILD_STRING:                 entryFunc(func(a int32) int { return 1 - int(a) }),
	LIST_APPEND:                  entry(-1),
	SET_ADD:                      entry(-1),
	MAP_ADD:                      entry(-2),
	LIST_EXTEND:                  entry(-1),
	SET_UPDATE:                   entry(-1),
	DICT_UPDATE:                  entry(-1),
	DICT_MERGE:                   entry(-1),
	LOAD_FAST:                    entry(1),
	LOAD_FAST_BORROW:             entry(1),
	LOAD_FAST_CHECK:              entry(1),
	LOAD_FAST_AND_CLEAR:          entry(1),
	LOAD_NAME:                    entry(1),
	LOAD_GLOBAL:                  entryFunc(func(a int32) int { return 1 + int(a&1) }),
	LOAD_DEREF:                   entry(1),
	LOAD_FROM_DICT_OR_DEREF:      entry(0),
	LOAD_CLOSURE:                 entry(1),
	LOAD_CONST:                   entry(1),
	LOAD_SMALL_INT:               entry(1),
	LOAD_COMMON_CONSTANT:         entry(1),
	LOAD_LOCALS:                  entry(1),
	LOAD_BUILD_CLASS:             entry(1),
	LOAD_ATTR:                    entryFunc(func(a int32) int { return int(a & 1) }),
	LOAD_SUPER_ATTR:              entryFunc(func(a int32) int { return -1 + int(a&1) }),
	LOAD_SPECIAL:                 entry(1),
	LOAD_FROM_DICT_OR_GLOBALS:    entry(0),
	STORE_FAST:                   entry(-1),
	STORE_FAST_MAYBE_NULL:        entry(-1),
	STORE_NAME:                   entry(-1),
	STORE_GLOBAL:                 entry(-1),
	STORE_DEREF:                  entry(-1),
	STORE_ATTR:                   entry(-2),
	DELETE_FAST:                  entry(0),
	DELETE_NAME:                  entry(0),
	DELETE_GLOBAL:                entry(0),
	DELETE_DEREF:                 entry(0),
	DELETE_ATTR:                  entry(-1),
	COMPARE_OP:                   entry(-1),
	IS_OP:                        entry(-1),
	CONTAINS_OP:                  entry(-1),
	CHECK_EXC_MATCH:              entry(0),
	CHECK_EG_MATCH:               entry(0),
	IMPORT_NAME:                  entry(-1),
	IMPORT_FROM:                  entry(1),
	JUMP:                         entry(0),
	JUMP_NO_INTERRUPT:            entry(0),
	JUMP_BACKWARD:                entry(0),
	JUMP_BACKWARD_NO_INTERRUPT:   entry(0),
	JUMP_FORWARD:                 entry(0),
	POP_JUMP_IF_FALSE:            entry(-1),
	POP_JUMP_IF_TRUE:             entry(-1),
	POP_JUMP_IF_NONE:             entry(-1),
	POP_JUMP_IF_NOT_NONE:         entry(-1),
	FOR_ITER:                     entryJump(1, 1),
	SEND:                         entryJump(0, -1),
	YIELD_VALUE:                  entry(0),
	RESERVED:                     entry(0),
	RETURN_VALUE:                 entry(-1),
	RETURN_GENERATOR:             entry(1),
	RAISE_VARARGS:                entryFunc(func(a int32) int { return -int(a) }),
	RERAISE:                      entry(-1),
	INTERPRETER_EXIT:             entry(-1),
	END_ASYNC_FOR:                entry(-2),
	CLEANUP_THROW:                entry(-2),
	PUSH_EXC_INFO:                entry(1),
	POP_EXCEPT:                   entry(-1),
	WITH_EXCEPT_START:            entry(1),
	SETUP_WITH:                   entry(0),
	SETUP_FINALLY:                entry(0),
	SETUP_CLEANUP:                entry(0),
	CALL:                         entryFunc(func(a int32) int { return -1 - int(a) }),
	CALL_KW:                      entryFunc(func(a int32) int { return -2 - int(a) }),
	CALL_FUNCTION_EX:             entryFunc(func(a int32) int { return -2 - int(a&1) }),
	CALL_INTRINSIC_1:             entry(0),
	CALL_INTRINSIC_2:             entry(-1),
	MAKE_FUNCTION:                entry(0),
	SET_FUNCTION_ATTRIBUTE:       entry(-1),
	MAKE_CELL:                    entry(0),
	COPY:                         entry(1),
	COPY_FREE_VARS:               entry(0),
	SWAP:                         entry(0),
	UNPACK_SEQUENCE:              entryFunc(func(a int32) int { return int(a) - 1 }),
	UNPACK_EX:                    entryFunc(func(a int32) int { return int(a&0xff) + int(a>>8) }),
	FORMAT_SIMPLE:                entry(0),
	FORMAT_WITH_SPEC:             entry(-1),
	CONVERT_VALUE:                entry(0),
	BUILD_INTERPOLATION:          entryFunc(func(a int32) int { return 1 - int(a&3) }),
	BUILD_TEMPLATE:               entry(-1),
	MATCH_MAPPING:                entry(1),
	MATCH_SEQUENCE:               entry(1),
	MATCH_KEYS:                   entry(1),
	MATCH_CLASS:                  entry(-2),
	TO_BOOL:                      entry(0),
	NOT_TAKEN:                    entry(0),
	SETUP_ANNOTATIONS:            entry(0),
	EXIT_INIT_CHECK:              entry(-1),
}

// calculateStackdepth performs a single forward pass over the flat
// sequence and returns the maximum running stack height plus a
// non-negativity check. CPython's full pass tracks branches through
// the CFG; for the minimum-viable port we run the linear walk and
// rely on the fact that codegen emits well-balanced sequences (every
// branch reconverges with a matching depth before re-use).
//
// CPython: Python/flowgraph.c:L809 calculate_stackdepth
func calculateStackdepth(seq *Sequence) (int, error) {
	depth := 0
	maxDepth := 0
	for i, ins := range seq.Instrs {
		eff := stackEffect(ins.Op, ins.Oparg, false)
		depth += eff
		if depth < 0 {
			return 0, fmt.Errorf("compile: negative stackdepth %d at instr %d (%s)", depth, i, opName(ins.Op))
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth, nil
}

// opName returns the textual mnemonic for op, used by error messages.
// Falls back to a numeric form when the table does not know op.
//
// CPython: Python/compile.c _PyOpcode_OpName
func opName(op Opcode) string {
	if int(op) >= 0 && int(op) < len(opcodeName) {
		if n := opcodeName[op]; n != "" {
			return n
		}
	}
	return fmt.Sprintf("op#%d", op)
}
