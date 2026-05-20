// Whitelist of opcodes whose generated arm in vm/eval_dispatch_gen.go
// has been verified equivalent to the hand-written arm in eval_*.go.
// Spec 1714 phase 5.2 grows this set as bodies migrate. Each entry's
// invariant is: the generated body matches the previous hand-written
// behavior bit-for-bit (same stack effect, same error path, same
// cache advance), so removing the hand-written arm is safe.
//
// CPython: Python/ceval.c uses a static opcode_targets[256] table for
// computed-goto dispatch (see opcode_targets.h). gopy mirrors the
// constant-indexable lookup shape; the per-dispatch cost drops from
// one map hash to one bounds-checked slice load.

package vm

import "github.com/tamnd/gopy/compile"

// dispatchGenSupported is a 256-entry bitmap indexed by Opcode. Empty
// slots default false (the zero value), so unknown opcodes route to
// the hand-written ladder. Populated at init from the source list
// below so the entry set stays trivially auditable.
var dispatchGenSupported [256]bool

// dispatchGenSupportedList is the source-of-truth set of opcodes that
// have a verified body in dispatchGen. The init function below stamps
// these into dispatchGenSupported.
var dispatchGenSupportedList = []compile.Opcode{
	compile.NOP,
	compile.POP_TOP,
	compile.JUMP_FORWARD,
	compile.JUMP_BACKWARD_NO_INTERRUPT,
	compile.END_SEND,
	compile.PUSH_NULL,
	compile.LOAD_FAST,
	compile.LOAD_FAST_BORROW,
	compile.LOAD_FAST_AND_CLEAR,
	compile.STORE_FAST,
	compile.LOAD_BUILD_CLASS,
	compile.SETUP_ANNOTATIONS,
	compile.LOAD_FROM_DICT_OR_GLOBALS,
	compile.LOAD_SMALL_INT,
	compile.LOAD_LOCALS,
	compile.UNARY_NEGATIVE,
	compile.UNARY_INVERT,
	compile.UNARY_NOT,
	compile.LIST_APPEND,
	compile.SET_ADD,
	compile.MAP_ADD,
	compile.DELETE_SUBSCR,
	compile.GET_LEN,
	compile.BUILD_STRING,
	compile.FORMAT_SIMPLE,
	compile.COPY,
	compile.SWAP,
	compile.SET_UPDATE,
	compile.DICT_UPDATE,
	compile.LOAD_COMMON_CONSTANT,
	compile.POP_EXCEPT,
	compile.PUSH_EXC_INFO,
	compile.STORE_GLOBAL,
	compile.DELETE_GLOBAL,
	compile.FORMAT_WITH_SPEC,
	compile.GET_ITER,
	compile.BUILD_LIST,
	compile.BUILD_TUPLE,
	compile.BUILD_SLICE,
	compile.BUILD_MAP,
	compile.BUILD_TEMPLATE,
	compile.GET_AWAITABLE,
	compile.GET_ANEXT,
	compile.MAKE_CELL,
}

func init() {
	for _, op := range dispatchGenSupportedList {
		dispatchGenSupported[op] = true
	}
}
