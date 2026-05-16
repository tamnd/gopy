// Whitelist of opcodes whose generated arm in vm/eval_dispatch_gen.go
// has been verified equivalent to the hand-written arm in eval_*.go.
// Spec 1714 phase 5.2 grows this set as bodies migrate. Each entry's
// invariant is: the generated body matches the previous hand-written
// behavior bit-for-bit (same stack effect, same error path, same
// cache advance), so removing the hand-written arm is safe.
//
// CPython has no analogue: every opcode lives in bytecodes.c and the
// generated switch is the only dispatch. This map is gopy-specific
// migration scaffolding that goes away when phase 5 closes and
// trySimple/tryImport/tryGen/tryMatch are folded into dispatchGen.

package vm

import "github.com/tamnd/gopy/compile"

var dispatchGenSupported = map[compile.Opcode]bool{
	compile.NOP:                 true,
	compile.POP_TOP:             true,
	compile.JUMP_FORWARD:        true,
	compile.PUSH_NULL:           true,
	compile.LOAD_FAST:           true,
	compile.LOAD_FAST_BORROW:    true,
	compile.LOAD_FAST_AND_CLEAR: true,
	compile.STORE_FAST:          true,
}
