// Top-level switch that flips a code object onto the adaptive
// specializer. Call sites are the three places a *objects.Code is
// minted: pythonrun.liftCode (fresh compile of top-level source),
// vm.liftNestedCode (a Code constant lifted out of a parent's Consts),
// and marshal.unmarshalCode (load from .pyc / frozen module).
//
// CPython: every freshly-minted PyCodeObject runs _PyCode_Quicken in
// _PyCode_New. We split it out so the wire-up is a single named call.

package specialize

import "github.com/tamnd/gopy/objects"

// Enable stamps initial cache counters into code's bytecode and marks
// it Quickened. The dispatch loop in vm/adaptive.go gates every
// specialize attempt on the Quickened flag, so without this call the
// ~3500 LOC of specialize/ never fires (which is the v0.12.4 perf
// smoking gun — see spec 1712 P1).
//
// Idempotent: a second call is a no-op so it's safe to call from
// every Code-creation site without coordinating which fires first.
func Enable(code *objects.Code) {
	if code == nil || code.Quickened || len(code.Code) < 4 {
		return
	}
	// Spec 1712 P1.6: arm the optimizer's dict / type watcher slots
	// before any specialized opcode can fire so inline caches read
	// only state the runtime has subscribed to. CPython splits the
	// install across pylifecycle.c:1380 (slot 0 builtins) and
	// optimizer_analysis.c:175 (slot 1 globals + type slot 0); gopy
	// collapses both onto first Enable via a sync.Once-gated hook.
	//
	// CPython: Python/pylifecycle.c:1378-1383 / optimizer_analysis.c:175-180
	ensureWatchersInstalled()
	Quicken(code.Code, true)
	// Allocate the parallel pointer-cache slab. One slot per codeunit
	// of bytecode lets emit / read by codeunit index match CPython's
	// in-cache pointer storage (read_obj / write_obj). Slots stay nil
	// until a specializer stamps a descriptor; reads are gated by the
	// matching version cell so a stale entry is never observed.
	//
	// CPython: Include/internal/pycore_code.h:175 write_obj / read_obj
	code.CacheObjects = make([]objects.Object, len(code.Code)/CodeUnitWidth)
	code.Quickened = true
}
