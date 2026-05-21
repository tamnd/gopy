// PYTHON_JIT env-var hook that opens the tier-2 gate at main-interp
// init time. Ports the (_Py_TIER2 & is_main_interp) block in CPython's
// init_interp_main:
//
//	#ifdef _Py_TIER2
//	if (is_main_interp) {
//	    int enabled = 1;
//	#if _Py_TIER2 & 2
//	    enabled = 0;
//	#endif
//	    char *env = Py_GETENV("PYTHON_JIT");
//	    if (env && *env != '\0') {
//	        enabled = *env != '0';
//	    }
//	    if (enabled) { interp->jit = true; }
//	}
//	#endif
//
// gopy's tier-2 executor scaffolding is in tree but the uop bodies
// (P2.2 + P2.3) are not yet ported; the gate stays default-false to
// match CPython's release-build default (the `#if _Py_TIER2 & 2`
// branch zeros `enabled` when CPython is built without the JIT
// machine-code backend). Users opt in with `PYTHON_JIT=1`, opt out
// with `PYTHON_JIT=0`. Anything else (unset / empty) leaves the gate
// untouched so callers that have already flipped JIT in their
// `*state.Interpreter` keep their setting.
//
// CPython: Python/pylifecycle.c:1325 init_interp_main (PYTHON_JIT block)
// CPython: Python/pystate.c:674 interp->jit = false default

package lifecycle

import (
	"os"

	"github.com/tamnd/gopy/state"
)

// ApplyJITEnv reads $PYTHON_JIT and flips interp.JIT to match. With
// the env unset (or empty) the gate is left alone, mirroring how the
// CPython block falls back to the build-time default. A non-empty
// value enables JIT iff the value's first byte is not '0' (CPython:
// `enabled = *env != '0'`); the simple shape captures both
// `PYTHON_JIT=1` and `PYTHON_JIT=anything` opt-ins while leaving the
// `PYTHON_JIT=0` opt-out path intact.
//
// CPython: Python/pylifecycle.c:1331 Py_GETENV("PYTHON_JIT")
func ApplyJITEnv(interp *state.Interpreter) {
	if interp == nil {
		return
	}
	env, ok := os.LookupEnv("PYTHON_JIT")
	if !ok || env == "" {
		return
	}
	interp.JIT = env[0] != '0'
}
