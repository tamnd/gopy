// Process-init wiring. The optimizer owns the dict / type watcher
// callbacks the tier-2 trace folder relies on; the specialize package
// owns the single ChokePoint (Enable) every Code-creation site routes
// through. specialize cannot import optimizer without creating a
// cycle (optimizer.WatcherInit needs state.Interpreter, which only
// optimizer-side code reaches by way of ExecutorsInvalidateAll), so
// instead the optimizer registers an installer with specialize at
// package-init time. Specialize fires the installer from Enable; the
// installer here owns its own idempotency latch.
//
// CPython lands its equivalent inline at pylifecycle.c:1378-1383
// (slot 0 builtins watcher) and optimizer_analysis.c:175-180 (slot 1
// globals + type slot 0). Both sites assume the main interpreter has
// already been minted; gopy mirrors that assumption by reading
// state.MainInterpreter() and skipping when it has not been set up
// (in which case Enable was fired by a code-only test path that has
// no runtime).

package optimizer

import (
	"sync/atomic"

	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// watcherInstalled latches the slot installation. The first Enable()
// call after a main interpreter exists flips the flag and runs
// WatcherInit; subsequent Enables short-circuit on the load.
var watcherInstalled atomic.Bool

func init() {
	specialize.SetWatcherInstaller(installWatchers)
}

// installWatchers wires the optimizer's reserved watcher slots and
// stamps the latch. Skips when no interpreter has been minted yet so
// pure compile-only test paths (which exercise Enable without
// constructing a runtime) do not crash. Once the latch flips, the
// install never repeats.
//
// CPython: Python/pylifecycle.c:1378-1383 +
// Python/optimizer_analysis.c:175-180
func installWatchers() {
	if watcherInstalled.Load() {
		return
	}
	interp := state.MainInterpreter()
	if interp == nil {
		return
	}
	if !watcherInstalled.CompareAndSwap(false, true) {
		return
	}
	WatcherInit(interp)
}

// ResetWatcherInstalled clears the install latch. Test-only helper
// for parity gates that need to drive a fresh install on a second
// pass; production code never calls this.
func ResetWatcherInstalled() { watcherInstalled.Store(false) }
