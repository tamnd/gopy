package objects

// GoidHook returns the running goroutine's numeric id cheaply. The vm
// installs the fast getg-based implementation (a single register read plus
// a struct-field load) at init; until then, or on an arch where the fast
// path is unavailable, it stays nil and callers fall back to their own
// runtime.Stack parse. Lock implementations that must detect same-goroutine
// reentry (functools._lru_cache_wrapper) read this on every acquire, so it
// has to be O(1): parsing the goroutine stack is O(stack-depth) and turns a
// deeply recursive cached function into quadratic time.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET reads the
// current thread state from TLS; this is the Go-runtime equivalent.
var GoidHook func() uint64

// CurrentHoldsGILHook reports whether the running goroutine currently
// owns the interpreter GIL. The vm installs it; nil until the eval loop
// is wired (and in unit tests that never start the interpreter), in
// which case the generator protocol treats the driver as not holding the
// lock and the body takes/releases it genuinely.
//
// CPython: Python/ceval_gil.c current_thread_holds_gil
var CurrentHoldsGILHook func() bool

func callerHoldsGIL() bool {
	if CurrentHoldsGILHook == nil {
		return false
	}
	return CurrentHoldsGILHook()
}
