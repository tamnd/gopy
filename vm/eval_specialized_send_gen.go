// Fast-path arm for SEND_GEN.
//
// _Py_Specialize_Send picks SEND_GEN when the receiver is a generator
// or coroutine. The CPython macro stack is:
//
//   SEND_GEN = unused/1 + _CHECK_PEP_523 + _SEND_GEN_FRAME + _PUSH_FRAME
//
// _SEND_GEN_FRAME pushes v onto the generator's interpreter frame,
// marks it FRAME_EXECUTING, links exc_info, and _PUSH_FRAME swaps the
// eval-stack to gen_frame via DISPATCH_INLINED so the generator's
// bytecode body runs in the same Tier-1 loop.
//
// gopy can't replicate that path: generators run on a dedicated
// goroutine driven by yieldCh / sendCh channels (see
// vm/eval_gen.go execReturnGenerator). The body's bytecode never
// shares the calling thread's evalState stack; YIELD_VALUE blocks on
// e.genSend, and the goroutine resumes only when the host SEND /
// __next__ thread pushes a value through sendCh. Pushing the gen's
// frame onto the host eval-stack would race with that goroutine.
//
// The legitimate fast arm is therefore dispatch-level: trust the
// specializer's type guard (which already ran identity checks at
// stamp time) and skip the execSend type-switch + GetAttr path. On
// guard miss the arm DEOPTs back to the adaptive parent like every
// other fast arm.
//
// Closing the rest of the CPython win would require retiring the
// goroutine-based generator design in favor of frame-stack pushing,
// which is a separate multi-month port and out of scope here.
//
// CPython: Python/bytecodes.c:1348 _SEND_GEN_FRAME
// CPython: Python/bytecodes.c:1364 SEND_GEN macro

package vm

import (
	"errors"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// fastSendGen runs SEND_GEN. Stack on entry: [..., receiver, v].
// Returns ok=true when the arm took the dispatch; on guard miss
// ok=false and the caller routes through maybeDeopt.
//
// CPython: Python/bytecodes.c:1364 SEND_GEN macro
func (e *evalState) fastSendGen(oparg uint32) (int, bool, error) {
	recv := e.peek(1).AsObject()
	var (
		val  objects.Object
		serr error
	)
	switch r := recv.(type) {
	case *objects.Generator:
		v := e.popObject()
		val, serr = r.Send(v)
	case *objects.Coroutine:
		v := e.popObject()
		val, serr = r.Send(v)
	default:
		// Type guard failed. CPython's _SEND_GEN_FRAME has the same
		// DEOPT_IF on Py_TYPE(gen); we route back through maybeDeopt.
		return 0, false, nil
	}
	if errors.Is(serr, objects.ErrStopIteration) {
		// Leave receiver on stack and push the StopIteration return
		// value (None for generators without an explicit return). The
		// trailing END_SEND will pop both.
		//
		// The jump anchor is cacheAdvance(SEND) + 2*oparg, not the
		// usual jumpBy(oparg+1): e.advance() reads the opcode byte at
		// InstrPtr, and the opcodeCaches table only carries the base
		// SEND row, so on a SEND_GEN byte CacheCount returns 0 and the
		// stride undershoots by one codeunit. forIterJump in
		// eval_specialized_for_iter.go uses the same anchor for the
		// same reason.
		//
		// CPython: Python/bytecodes.c _SEND (StopIteration path)
		e.pushObject(objects.None())
		return e.cacheAdvance(compile.SEND) + 2*int(oparg), true, nil
	}
	if serr != nil {
		return 0, true, serr
	}
	e.pushObject(val)
	return e.cacheAdvance(compile.SEND), true, nil
}
