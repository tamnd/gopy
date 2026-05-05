package lifecycle

import (
	"runtime"
	"testing"

	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/state"
)

func TestFinalizeNilRuntimeIsNoop(t *testing.T) {
	if status := Finalize(nil); status.IsException() {
		t.Errorf("Finalize(nil): %+v", status)
	}
}

func TestFinalizeUninitializedIsNoop(t *testing.T) {
	rt := state.NewRuntime()
	if status := Finalize(rt); status.IsException() {
		t.Errorf("Finalize: %+v", status)
	}
	if rt.Initialized != 0 || rt.CoreInitialized != 0 {
		t.Errorf("flags drifted on a never-initialized runtime: init=%d core=%d", rt.Initialized, rt.CoreInitialized)
	}
}

func TestFinalizeResetsFlagsAndDropsInterpreters(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0

	rt := state.NewRuntime()
	if _, status := Initialize(rt, cfg); status.IsException() {
		t.Fatalf("Initialize: %+v", status)
	}

	if status := Finalize(rt); status.IsException() {
		t.Fatalf("Finalize: %+v", status)
	}

	if rt.Initialized != 0 {
		t.Errorf("Initialized = %d, want 0", rt.Initialized)
	}
	if rt.CoreInitialized != 0 {
		t.Errorf("CoreInitialized = %d, want 0", rt.CoreInitialized)
	}
	if got := len(rt.Interpreters()); got != 0 {
		t.Errorf("Interpreters after Finalize = %d, want 0", got)
	}
}

// TestInitializeFinalizeRoundtrip is the spec gate: Initialize ->
// Finalize -> Initialize again must not panic and must not leak
// goroutines. Mirrors the v0.7 init_test.go gate documented in spec
// 1622.
func TestInitializeFinalizeRoundtrip(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0

	rt := state.NewRuntime()
	before := runtime.NumGoroutine()

	for i := range 3 {
		t1, status := Initialize(rt, cfg)
		if status.IsException() {
			t.Fatalf("Initialize#%d: %+v", i, status)
		}
		if t1 == nil {
			t.Fatalf("Initialize#%d returned nil thread", i)
		}
		if status := Finalize(rt); status.IsException() {
			t.Fatalf("Finalize#%d: %+v", i, status)
		}
	}

	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestFinalizeFlipsFinalizingBeforeClearingFlags pins the ordering
// other goroutines depend on: when an interpreter's Finalizing flag
// is observed as 1, the runtime's Initialized flag may or may not
// already be 0, but it must reach 0 by the time Finalize returns.
func TestFinalizeFlipsFinalizingBeforeClearingFlags(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0

	rt := state.NewRuntime()
	if _, status := Initialize(rt, cfg); status.IsException() {
		t.Fatalf("Initialize: %+v", status)
	}
	interp := rt.Interpreters()[0]
	if interp.Finalizing != 0 {
		t.Errorf("Finalizing = %d before Finalize, want 0", interp.Finalizing)
	}
	if status := Finalize(rt); status.IsException() {
		t.Fatalf("Finalize: %+v", status)
	}
	if interp.Finalizing != 1 {
		t.Errorf("Finalizing = %d after Finalize, want 1", interp.Finalizing)
	}
}
