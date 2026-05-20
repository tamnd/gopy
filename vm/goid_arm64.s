// Read the current goroutine's g pointer from the dedicated g register.
// Mirrors how the Go runtime accesses its per-goroutine state.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET reads
// _PyRuntime.gilstate.tstate_current via the TLS register in exactly the
// same shape (one register move, no syscalls).

#include "textflag.h"

TEXT ·getg(SB), NOSPLIT, $0-8
	MOVD g, ret+0(FP)
	RET
