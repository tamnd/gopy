package objects

// BeginAllowThreads releases the GIL held by the current goroutine before
// it blocks in Go (a thread join, a lock wait, a sleep, an I/O read) and
// returns a function that re-takes the GIL once the block is over. It is
// the gopy spelling of CPython's Py_BEGIN_ALLOW_THREADS /
// Py_END_ALLOW_THREADS: a thread must not hold the GIL while parked, or no
// other Python thread could run.
//
// The vm package installs this. It is nil until the eval loop is wired up
// (and in unit tests that never start the interpreter), so callers must
// guard the nil case; when nil the returned re-take is a no-op.
//
// CPython: Include/internal/pycore_ceval.h Py_BEGIN_ALLOW_THREADS
var BeginAllowThreads func() func()

// AllowThreads runs fn with the GIL released, re-taking it afterwards. It
// is the convenience wrapper around BeginAllowThreads for the common
// "block on this channel/syscall" shape. Safe to call when the hook is
// unset: fn simply runs with whatever lock state the caller had.
func AllowThreads(fn func()) {
	if BeginAllowThreads == nil {
		fn()
		return
	}
	reacquire := BeginAllowThreads()
	defer reacquire()
	fn()
}
