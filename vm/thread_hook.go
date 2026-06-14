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
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func init() {
	// Share the fast getg-based goid with leaf packages that need cheap
	// goroutine identity (functools._lru_cache_wrapper's reentrant lock).
	// Routing through objects keeps the dependency arrow pointing the
	// right way: objects is a leaf both vm and module/_functools import.
	objects.GoidHook = goid

	// Key each dict's critical section on the active Python thread, not the
	// raw goroutine. A generator/coroutine/async-generator body runs on its
	// own goroutine but shares its driver's *state.Thread, so a driver that
	// holds a dict lock and resumes a body touching the same dict must be
	// recognised as the same reentrant owner or the body goroutine
	// deadlocks. Returns 0 when no thread is active so the lock falls back
	// to the goroutine id.
	//
	// CPython: Python/critical_section.c keys on PyThreadState; a generator
	// shares the driver's tstate (Objects/genobject.c:248 gen_send_ex2).
	objects.CriticalSectionOwnerHook = func() int64 {
		ts := currentThread()
		if ts == nil {
			return 0
		}
		return int64(ts.ID())
	}

	// Py_BEGIN_ALLOW_THREADS: release the GIL while the current goroutine
	// blocks in Go (thread join, lock wait, sleep, blocking I/O) so other
	// Python threads can run, then re-take it. A no-op when this goroutine
	// does not own the GIL (e.g. a generator body running under its
	// driver's hold, where the driver, not this goroutine, owns the lock).
	//
	// CPython: Include/internal/pycore_ceval.h Py_BEGIN_ALLOW_THREADS
	objects.BeginAllowThreads = func() func() {
		ts := currentThread()
		if ts == nil {
			return func() {}
		}
		v := vmFor(ts)
		if v.gil == nil {
			return func() {}
		}
		g := goid()
		if !v.gil.HeldByGoid(g) {
			return func() {}
		}
		v.gil.ReleaseGoid(ts, g)
		return func() {
			v.gil.AcquireReentrant(ts, g)
			v.gilTimer.reset()
		}
	}

	// Report whether the running goroutine currently owns the GIL. The
	// generator protocol reads this on every Send/Throw/Close to pick its
	// resume mode: when the driver holds the lock the body borrows it via
	// a baton handoff (driver and body share a thread and never run
	// concurrently); when the driver holds nothing (a bare gen.Send() from
	// Go outside any Eval frame) the body acquires and releases the lock
	// for real. Returns false when no thread or GIL is active (unit tests).
	//
	// CPython: Python/ceval_gil.c current_thread_holds_gil
	objects.CurrentHoldsGILHook = func() bool {
		ts := currentThread()
		if ts == nil {
			return false
		}
		v := vmFor(ts)
		if v.gil == nil {
			return false
		}
		return v.gil.HeldByGoid(goid())
	}

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
