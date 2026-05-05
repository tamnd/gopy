// Eval-breaker bit constants. Numerically match
// Include/internal/pycore_ceval.h so that any debug print of the
// breaker word lines up with CPython's definitions.
//
// CPython: Include/internal/pycore_ceval.h:323 _PY_*_BIT

package gil

const (
	// BreakerGILDropRequest is set when another thread wants the GIL.
	BreakerGILDropRequest uint32 = 1 << 0

	// BreakerSignalsPending is set when a Unix signal arrived.
	BreakerSignalsPending uint32 = 1 << 1

	// BreakerCallsPending is set when Pending.Add queued a callback.
	BreakerCallsPending uint32 = 1 << 2

	// BreakerAsyncException is set by PyThreadState_SetAsyncExc.
	BreakerAsyncException uint32 = 1 << 3

	// BreakerGCScheduled is set when the cycle collector wants to run.
	BreakerGCScheduled uint32 = 1 << 4

	// BreakerPleaseStop asks the eval loop to bail (interpreter shutdown).
	BreakerPleaseStop uint32 = 1 << 5

	// BreakerExplicitMerge is the free-threaded build's per-thread
	// QSBR merge bit. No-op in v0.6.
	BreakerExplicitMerge uint32 = 1 << 6

	// BreakerJITInvalidateCold is the JIT's cold-code invalidation
	// bit. No-op in v0.6 (no JIT).
	BreakerJITInvalidateCold uint32 = 1 << 7

	// BreakerEventsMask is the mask of all currently defined event
	// bits. CPython reserves the low 8 bits.
	BreakerEventsMask uint32 = (1 << 8) - 1
)
