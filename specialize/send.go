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
