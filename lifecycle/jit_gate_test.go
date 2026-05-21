package lifecycle

import (
	"testing"

	"github.com/tamnd/gopy/state"
)

// PYTHON_JIT unset must leave interp.JIT alone. CPython: the env-read
// only fires when `env && *env != '\0'`.
func TestApplyJITEnv_Unset(t *testing.T) {
	t.Setenv("PYTHON_JIT", "")
	for _, prior := range []bool{false, true} {
		interp := &state.Interpreter{JIT: prior}
		ApplyJITEnv(interp)
		if interp.JIT != prior {
			t.Errorf("PYTHON_JIT empty: gate flipped from %v to %v", prior, interp.JIT)
		}
	}
}

// PYTHON_JIT=1 opens the gate. CPython: `enabled = *env != '0'`.
func TestApplyJITEnv_Enable(t *testing.T) {
	t.Setenv("PYTHON_JIT", "1")
	interp := &state.Interpreter{JIT: false}
	ApplyJITEnv(interp)
	if !interp.JIT {
		t.Fatalf("PYTHON_JIT=1 did not enable JIT")
	}
}

// PYTHON_JIT=0 closes the gate even when the caller pre-enabled it.
// Mirrors CPython's opt-out semantics: a `PYTHON_JIT=0` env wins over
// the build-time default.
func TestApplyJITEnv_Disable(t *testing.T) {
	t.Setenv("PYTHON_JIT", "0")
	interp := &state.Interpreter{JIT: true}
	ApplyJITEnv(interp)
	if interp.JIT {
		t.Fatalf("PYTHON_JIT=0 did not disable JIT")
	}
}

// CPython's check is `*env != '0'`, so any non-empty leading byte
// other than ASCII '0' enables. Smoke-test a couple of shapes.
func TestApplyJITEnv_NonZeroValuesEnable(t *testing.T) {
	for _, v := range []string{"1", "yes", "true", "2", "off"} {
		t.Setenv("PYTHON_JIT", v)
		interp := &state.Interpreter{JIT: false}
		ApplyJITEnv(interp)
		if !interp.JIT {
			t.Errorf("PYTHON_JIT=%q did not enable JIT", v)
		}
	}
}

// nil interp must not panic. The lifecycle path only calls
// ApplyJITEnv with a real interpreter, but the defensive guard keeps
// the helper usable from any callsite that may not own one yet.
func TestApplyJITEnv_NilInterp(t *testing.T) {
	t.Setenv("PYTHON_JIT", "1")
	ApplyJITEnv(nil)
}
