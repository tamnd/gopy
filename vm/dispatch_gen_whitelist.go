// Whitelist of opcodes whose generated arm in vm/eval_dispatch_gen.go
// has been verified equivalent to the hand-written arm in eval_*.go.
// Spec 1714 phase 5.2 grows this set as bodies migrate. Each entry's
// invariant is: the generated body matches the previous hand-written
// behavior bit-for-bit (same stack effect, same error path, same
// cache advance), so removing the hand-written arm is safe.
//
// CPython has no analog: every opcode lives in bytecodes.c and the
// generated switch is the only dispatch. This map is gopy-specific
// migration scaffolding that goes away when phase 5 closes and
// trySimple/tryImport/tryGen/tryMatch are folded into dispatchGen.

package vm

import "github.com/tamnd/gopy/compile"

var dispatchGenSupported = map[compile.Opcode]bool{
	compile.NOP:                        true,
	compile.POP_TOP:                    true,
	compile.JUMP_FORWARD:               true,
	compile.JUMP_BACKWARD_NO_INTERRUPT: true,
	compile.END_SEND:                   true,
	compile.PUSH_NULL:                  true,
	compile.LOAD_FAST:                  true,
	compile.LOAD_FAST_BORROW:           true,
	compile.LOAD_FAST_AND_CLEAR:        true,
	compile.STORE_FAST:                 true,
	compile.LOAD_BUILD_CLASS:           true,
	compile.SETUP_ANNOTATIONS:          true,
	compile.LOAD_FROM_DICT_OR_GLOBALS:  true,
	compile.LOAD_SMALL_INT:             true,
	compile.LOAD_LOCALS:                true,
	compile.UNARY_NEGATIVE:             true,
	compile.UNARY_INVERT:               true,
	compile.UNARY_NOT:                  true,
	compile.LIST_APPEND:                true,
	compile.SET_ADD:                    true,
	compile.MAP_ADD:                    true,
	compile.DELETE_SUBSCR:              true,
	compile.GET_LEN:                    true,
	compile.BUILD_STRING:               true,
	compile.FORMAT_SIMPLE:              true,
	compile.COPY:                       true,
	compile.SWAP:                       true,
	compile.SET_UPDATE:                 true,
	compile.DICT_UPDATE:                true,
	compile.LOAD_COMMON_CONSTANT:       true,
	compile.POP_EXCEPT:                 true,
	compile.PUSH_EXC_INFO:              true,
	compile.STORE_GLOBAL:               true,
	compile.DELETE_GLOBAL:              true,
	compile.FORMAT_WITH_SPEC:           true,
	compile.GET_ITER:                   true,
	compile.BUILD_LIST:                 true,
	compile.BUILD_TUPLE:                true,
	compile.BUILD_SLICE:                true,
}
