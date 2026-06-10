// Executing-frame roots for the cycle collector. CPython keeps every
// value an active frame holds reachable through tstate->current_frame,
// which gc_collect_main never collects. gopy's interpreter frames live
// in a per-thread arena (a plain Go allocation) that the refcount-based
// collector cannot see, so a value referenced only by a live frame
// local would collapse to gc_refs == 0 under subtract_refs and be
// reclaimed out from under the running program. This wires the vm into
// the collector's pin pass so every object held by an executing frame
// is treated as a root.
//
// CPython: Python/gc.c:1430 gc_collect_main (tstate->current_frame roots)

package vm

import (
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func init() {
	objects.GCExecutingRootsHook = pinExecutingFrameRoots
}

// visitFrameHeld reports every object an interpreter frame holds in its
// fast locals, cell and free vars, and live operand stack. Mirrors the
// localsplus walk frame_traverse performs for a materialized frame
// object; here it runs over the raw activation record so the held
// values are rooted even when no PyFrameObject has been allocated.
//
// CPython: Objects/frameobject.c:1163 frame_traverse
func visitFrameHeld(fr objects.InterpreterFrame, pin func(objects.Object)) {
	if fr == nil {
		return
	}
	for i, n := 0, fr.FrameNumLocals(); i < n; i++ {
		pin(fr.FrameFastLocal(i))
	}
	for i, n := 0, fr.FrameNumCells(); i < n; i++ {
		pin(fr.FrameCellLocal(i))
	}
	for i, n := 0, fr.FrameNumFrees(); i < n; i++ {
		pin(fr.FrameFreeLocal(i))
	}
	for i, n := 0, fr.FrameNumStack(); i < n; i++ {
		pin(fr.FrameStackItem(i))
	}
}

// pinExecutingFrameRoots walks every active thread's frame stack and
// every suspended generator body's own frame chain, reporting the
// objects each frame holds to the collector's pin callback. The two
// sources mirror tstate->current_frame for a regular call (the
// per-thread FrameStack) and for a generator body that runs on its own
// goroutine off the shared stack (activeEvalFrames).
//
// CPython: Python/pystate.c:2099 PyThreadState_GetFrame
func pinExecutingFrameRoots(pin func(objects.Object)) {
	activeThreads.Range(func(_, v any) bool {
		ts, ok := v.(*state.Thread)
		if !ok {
			return true
		}
		for f := frameStackFor(ts).Top(); f != nil; f = f.Previous {
			visitFrameHeld(f, pin)
		}
		return true
	})
	activeEvalFrames.Range(func(_, v any) bool {
		for f, _ := v.(*frame.Frame); f != nil; f = f.Previous {
			visitFrameHeld(f, pin)
		}
		return true
	})
}
