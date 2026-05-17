// SEND family specializer.
//
// _Py_Specialize_Send picks SEND_GEN when the receiver is a generator
// or coroutine; everything else unspecializes. The CPython
// implementation also bails when PEP 523 has installed a custom
// eval-frame hook, but gopy does not expose that hook yet, so the
// guard is a no-op here.
//
// CPython: Python/specialize.c:2964 _Py_Specialize_Send

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// Send specializes the SEND at instr based on the receiver. Currently
// only generators and coroutines have a fast path.
//
// CPython: Python/specialize.c:2964 _Py_Specialize_Send
func Send(receiver objects.Object, code []byte, instr int) {
	if objects.IsGenerator(receiver) || objects.IsCoroutine(receiver) {
		Specialize(code, instr, compile.SEND_GEN)
		return
	}
	Unspecialize(code, instr)
}
