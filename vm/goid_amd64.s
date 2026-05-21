// Read the current goroutine's g pointer from the TLS slot the Go
// runtime uses for its per-goroutine state.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET reads
// _PyRuntime.gilstate.tstate_current via the TLS register in exactly the
// same shape (one register move, no syscalls).
//
// The Go runtime's runtime/go_tls.h ships in src/runtime but is not
// exposed under pkg/include as of Go 1.26, so we inline the two macros
// it defines for amd64 here instead of #including the header.

#include "textflag.h"

TEXT ·getg(SB), NOSPLIT, $0-8
	MOVQ TLS, CX
	MOVQ 0(CX)(TLS*1), AX
	MOVQ AX, ret+0(FP)
	RET
