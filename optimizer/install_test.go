package optimizer

import (
	"testing"

	"github.com/tamnd/gopy/state"
)

// TestInstallWatchers_NoMainInterpSkips covers the "Enable() fired
// before any interpreter exists" path. The latch must stay open so a
// later Enable() once an interp is minted can still drive the
// install.
func TestInstallWatchers_NoMainInterpSkips(t *testing.T) {
	ResetWatcherInstalled()
	state.DropMainInterpreter()
	t.Cleanup(state.DropMainInterpreter)

	installWatchers()
	if watcherInstalled.Load() {
		t.Errorf("install latch must not flip when no main interp exists")
	}
}

// TestInstallWatchers_InstallsOnce verifies the optimizer-side latch
// fires exactly once per process even if specialize.Enable hands the
// hook multiple invocations. The watcher table is shared, so a
// double-install would clobber any user-installed slot.
func TestInstallWatchers_InstallsOnce(t *testing.T) {
	ResetWatcherInstalled()
	state.DropMainInterpreter()
	rt := state.NewRuntime()
	interp := rt.NewInterpreter()
	t.Cleanup(state.DropMainInterpreter)

	installWatchers()
	if !watcherInstalled.Load() {
		t.Fatalf("install latch must flip when an interp is available")
	}
	if state.MainInterpreter() != interp {
		t.Errorf("MainInterpreter must return the first-minted interp")
	}

	// A second call must be a no-op (the CAS short-circuits on the
	// already-set load).
	installWatchers()
	installWatchers()
}
