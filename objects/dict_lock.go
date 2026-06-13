package objects

import (
	"runtime"
	"strconv"
	"sync"
)

// dictMutex serializes access to a single dict's open-addressed table
// across goroutines. CPython 3.14's free-threaded build wraps every
// table read and write in Py_BEGIN_CRITICAL_SECTION(mp), which acquires
// the dict's per-object ob_mutex; the GIL build compiles the macro to a
// no-op because the GIL already serializes. gopy runs Python threads on
// real goroutines with no GIL, so the per-dict lock is always required:
// without it a Keys()/iterator snapshot reading d.entries/d.order races
// a concurrent dictInsert/dictDelete writing the same slices, and the
// container ownership port (Incref-on-store) turns the torn pointer read
// into a fatal dereference (test_weakref.MappingTestCase).
//
// The lock is goroutine-reentrant. A dict operation calls user code
// while holding the table (a key's __hash__/__eq__ during the probe, or
// a displaced value's __del__ during the Decref that follows an insert
// or delete), and that user code can re-enter the SAME dict on the same
// goroutine. A plain mutex would self-deadlock there; CPython's critical
// section instead detects same-thread reentry and suspends the held
// section. The reentrant counter reproduces the no-deadlock outcome for
// the same-goroutine case, while a different goroutine still blocks until
// the owner fully unwinds.
//
// The zero value is an unlocked mutex, so an embedded dictMutex needs no
// constructor; cond.L is wired lazily under mu on first acquire. A Dict
// is only ever handled through a pointer, so the embedded value is never
// copied after first use.
//
// CPython: Python/critical_section.c:21 _PyCriticalSection_BeginSlow
type dictMutex struct {
	mu    sync.Mutex
	cond  sync.Cond
	ready bool
	owner int64
	depth int
}

// lock acquires the table lock for the current goroutine, recursing
// without blocking when this goroutine already owns it.
//
// CPython: Python/critical_section.c:21 _PyCriticalSection_BeginSlow
func (r *dictMutex) lock() {
	gid := dictGoid()
	r.mu.Lock()
	if !r.ready {
		r.cond.L = &r.mu
		r.ready = true
	}
	if r.owner == gid {
		r.depth++
		r.mu.Unlock()
		return
	}
	for r.owner != 0 {
		r.cond.Wait()
	}
	r.owner = gid
	r.depth = 1
	r.mu.Unlock()
}

// unlock drops one level of ownership, releasing to other goroutines
// only when the outermost lock is unwound.
//
// CPython: Python/critical_section.c:173 PyCriticalSection_End
func (r *dictMutex) unlock() {
	r.mu.Lock()
	r.depth--
	if r.depth == 0 {
		r.owner = 0
		r.cond.Signal()
	}
	r.mu.Unlock()
}

// lock acquires d's table lock. Held across the whole operation so the
// table mutation and any user callback it triggers observe a consistent
// dict, mirroring Py_BEGIN_CRITICAL_SECTION(mp).
//
// CPython: Objects/dictobject.c:2707 PyDict_SetItem (Py_BEGIN_CRITICAL_SECTION)
func (d *Dict) lock() { d.mu.lock() }

// unlock releases d's table lock.
//
// CPython: Objects/dictobject.c:2716 PyDict_SetItem (Py_END_CRITICAL_SECTION)
func (d *Dict) unlock() { d.mu.unlock() }

// CriticalSectionOwnerHook returns a stable identity for the running
// Python thread (its *state.Thread id), or 0 when the current goroutine
// is not executing inside the eval loop. The vm wires it; objects/ keeps
// only the hook to avoid importing state.
//
// A generator, coroutine, or async-generator body runs on its own
// goroutine but shares its driver's *state.Thread, exactly as CPython
// runs a generator on the driver's PyThreadState. The critical section
// must therefore key reentrancy on the thread, not the goroutine:
// otherwise a driver that holds a dict's lock and then resumes a
// generator that touches the same dict (e.g. a globals lookup) deadlocks,
// because the body goroutine has a different goid and blocks on a lock its
// own thread already owns.
//
// CPython: Python/critical_section.c keys on PyThreadState; a generator
// shares the driver's tstate (Objects/genobject.c:248 gen_send_ex2).
var CriticalSectionOwnerHook func() int64

// dictGoid returns the identity that owns a dict's critical section: the
// Python thread when the goroutine is inside the eval loop, else the raw
// goroutine id. Thread ids carry a high tag bit so they never collide with
// a bare goroutine id used by a goroutine that runs dict operations outside
// Eval (the Go finalizer goroutine, bootstrap). A key's __hash__/__eq__ or
// a displaced value's __del__ re-enters synchronously on the same thread,
// so the thread id is the exact reentry identity. The goid fallback prefers
// the vm's fast getg-based hook and only parses runtime.Stack when unset.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET
func dictGoid() int64 {
	if CriticalSectionOwnerHook != nil {
		if id := CriticalSectionOwnerHook(); id != 0 {
			// Tag thread-derived ids out of the goroutine number space.
			return id | (int64(1) << 62)
		}
	}
	if GoidHook != nil {
		return int64(GoidHook())
	}
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const prefix = "goroutine "
	if n <= len(prefix) {
		return 0
	}
	s := buf[len(prefix):n]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	id, _ := strconv.ParseInt(string(s[:end]), 10, 64)
	return id
}
