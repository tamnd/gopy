// Read the current goroutine's g pointer from the TLS slot the Go
// runtime uses for its per-goroutine state.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET reads
// _PyRuntime.gilstate.tstate_current via the TLS register in exactly the
// same shape (one register move, no syscalls).

#include "textflag.h"
#include "go_tls.h"

TEXT ·getg(SB), NOSPLIT, $0-8
	get_tls(CX)
	MOVQ g(CX), AX
	MOVQ AX, ret+0(FP)
	RET
