package specialize

import "testing"

// TestEnsureWatchersInstalled_FiresHook confirms specialize.Enable
// reaches the registered installer. The hook is what wires the
// optimizer's reserved dict / type watcher slots; without it tier-1
// inline caches can read stale state after a builtins or class
// mutation (spec 1712 P1.6).
//
// Resets the package-level installer so the test does not leak the
// optimizer-supplied hook between runs.
func TestEnsureWatchersInstalled_FiresHook(t *testing.T) {
	saved := watcherInstaller
	t.Cleanup(func() { watcherInstaller = saved })

	calls := 0
	SetWatcherInstaller(func() { calls++ })

	ensureWatchersInstalled()
	if calls != 1 {
		t.Errorf("installer must fire on first ensureWatchersInstalled; calls=%d", calls)
	}
	ensureWatchersInstalled()
	if calls != 2 {
		t.Errorf("installer must fire on every ensureWatchersInstalled; calls=%d", calls)
	}
}

// TestEnsureWatchersInstalled_NoOpWhenUnregistered exercises the
// "specialize tests run without optimizer linked" path. The hook
// must not panic when no installer has been registered.
func TestEnsureWatchersInstalled_NoOpWhenUnregistered(t *testing.T) {
	saved := watcherInstaller
	t.Cleanup(func() { watcherInstaller = saved })

	watcherInstaller = nil
	ensureWatchersInstalled()
}

// TestSetWatcherInstaller_Replaces shows the latest installer wins.
// Useful when test setup wants to substitute a counting installer
// over the optimizer's real one.
func TestSetWatcherInstaller_Replaces(t *testing.T) {
	saved := watcherInstaller
	t.Cleanup(func() { watcherInstaller = saved })

	firstCalls := 0
	secondCalls := 0
	SetWatcherInstaller(func() { firstCalls++ })
	SetWatcherInstaller(func() { secondCalls++ })

	ensureWatchersInstalled()
	if firstCalls != 0 {
		t.Errorf("first installer must be replaced; firstCalls=%d", firstCalls)
	}
	if secondCalls != 1 {
		t.Errorf("second installer must fire; secondCalls=%d", secondCalls)
	}
}
