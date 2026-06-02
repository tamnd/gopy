// vm-side wiring for _thread identity. gopy runs each generator and
// coroutine body on its own goroutine, so the raw goroutine id is not a
// stable Python-thread identity: it changes the moment a coroutine is
// resumed. _thread.get_ident and threading.local must instead track the
// active *state.Thread, which is constant across a yield because the
// generator goroutine inherits its creator's thread (savedTS) through
// the active-thread map. Installing the hooks from the vm side keeps the
// dependency arrow pointing vm -> module/_thread.
//
// CPython: Include/cpython/pystate.h PyThreadState.thread_id

package vm

import (
	thread "github.com/tamnd/gopy/module/_thread"
	"github.com/tamnd/gopy/state"
)

func init() {
	// Resolve the active Python thread's identity for the running
	// goroutine. Returns 0 when no thread is active so _thread falls back
	// to the goroutine id.
	thread.ThreadIdentHook = func() int64 {
		ts := currentThread()
		if ts == nil {
			return 0
		}
		return int64(ts.ID())
	}

	// Allocate a fresh thread state for a goroutine start_new_thread is
	// about to launch. Runs on the parent goroutine so the new identity
	// is known synchronously; the returned enter/leave callbacks publish
	// the thread on the child goroutine's active-thread slot.
	thread.ThreadSpawnHook = func() (int64, func(), func()) {
		var interp *state.Interpreter
		if parent := currentThread(); parent != nil {
			interp = parent.Interp()
		}
		var ts *state.Thread
		if interp != nil {
			ts = interp.AttachThread()
		} else {
			ts = state.NewThread()
		}
		var (
			prev *state.Thread
			g    uint64
		)
		enter := func() { prev, g = setActiveThread(ts) }
		leave := func() { restoreActiveThread(prev, g) }
		return int64(ts.ID()), enter, leave
	}
}
