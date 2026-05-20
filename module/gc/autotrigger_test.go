// Tests for the threshold-driven auto-trigger added in P13.1. They
// poke the package-level state directly so each test must clean up
// after itself; helper `untrackAll` from collector_test.go does the
// heavy lifting.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// withThresholds installs (g0, g1, g2) for the duration of the test
// and restores the default thresholds + zeroed counts on cleanup.
func withThresholds(t *testing.T, g0, g1, g2 int) {
	t.Helper()
	state.mu.Lock()
	prev := [3]int{
		state.generations[0].threshold,
		state.generations[1].threshold,
		state.generations[2].threshold,
	}
	state.generations[0].threshold = g0
	state.generations[1].threshold = g1
	state.generations[2].threshold = g2
	state.mu.Unlock()
	t.Cleanup(func() {
		untrackAll()
		state.mu.Lock()
		state.generations[0].threshold = prev[0]
		state.generations[1].threshold = prev[1]
		state.generations[2].threshold = prev[2]
		state.longLivedPending = 0
		state.longLivedTotal = 0
		state.collecting = false
		state.mu.Unlock()
	})
}

// TestAutoTriggerFiresAtThreshold checks that Track auto-runs Collect
// when gen-0's count exceeds threshold. After the trigger the gen-0
// count must reset to 0 (everything merged into gen-1).
func TestAutoTriggerFiresAtThreshold(t *testing.T) {
	withThresholds(t, 3, 10, 10)
	objs := []objects.Object{
		objects.NewList(nil),
		objects.NewList(nil),
		objects.NewList(nil),
		objects.NewList(nil),
	}
	for _, o := range objs {
		Track(o)
	}
	g0, g1, _ := GetCount()
	if g0 != 0 {
		t.Fatalf("gen0 count after auto-trigger: got %d, want 0", g0)
	}
	if g1 == 0 {
		t.Fatalf("gen1 count after gen0 promotion: got 0, want > 0")
	}
}

// TestAutoTriggerSkippedWhenDisabled verifies that gc.disable() blocks
// the auto path even after threshold is crossed.
func TestAutoTriggerSkippedWhenDisabled(t *testing.T) {
	withThresholds(t, 1, 10, 10)
	Disable()
	defer Enable()
	objs := []objects.Object{
		objects.NewList(nil),
		objects.NewList(nil),
		objects.NewList(nil),
	}
	for _, o := range objs {
		Track(o)
	}
	g0, _, _ := GetCount()
	if g0 != 3 {
		t.Fatalf("gen0 count with gc disabled: got %d, want 3", g0)
	}
}

// TestAutoTriggerSkippedWhenZeroThreshold verifies that setting gen-0
// threshold to 0 disables the auto path (CPython parity).
func TestAutoTriggerSkippedWhenZeroThreshold(t *testing.T) {
	withThresholds(t, 0, 10, 10)
	objs := []objects.Object{
		objects.NewList(nil),
		objects.NewList(nil),
	}
	for _, o := range objs {
		Track(o)
	}
	g0, _, _ := GetCount()
	if g0 != 2 {
		t.Fatalf("gen0 count with threshold=0: got %d, want 2", g0)
	}
}

// TestAutoTriggerReentrancyGuard ensures Track refuses to schedule
// another auto-collection while one is already in flight. We can't go
// through a real finalizer (gopy runs finalizers with state.mu held,
// so user code that takes the lock would deadlock); instead we set
// state.collecting=true under the lock to simulate the in-flight
// state, then verify Track does not reset gen-0's count.
func TestAutoTriggerReentrancyGuard(t *testing.T) {
	withThresholds(t, 1, 10, 10)
	state.mu.Lock()
	state.collecting = true
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.collecting = false
		state.mu.Unlock()
	}()
	objs := []objects.Object{
		objects.NewList(nil),
		objects.NewList(nil),
		objects.NewList(nil),
	}
	for _, o := range objs {
		Track(o)
	}
	g0, g1, _ := GetCount()
	if g0 != 3 {
		t.Fatalf("gen0 count with collecting=true: got %d, want 3", g0)
	}
	if g1 != 0 {
		t.Fatalf("gen1 count with collecting=true: got %d, want 0", g1)
	}
}

// TestSelectGenerationFullCollectionGate verifies that full
// gen-(NUM_GENERATIONS-1) collection only fires when the long-lived
// ratio exceeds 25%, matching CPython's issue-#4074 heuristic.
func TestSelectGenerationFullCollectionGate(t *testing.T) {
	withThresholds(t, 700, 10, 10)
	state.mu.Lock()
	defer state.mu.Unlock()
	// Cross gen-2 threshold but keep long_lived_pending below 25% of
	// long_lived_total. selectGeneration must skip gen-2 and pick the
	// next eligible older generation (or -1).
	state.generations[2].count = 11
	state.longLivedTotal = 1000
	state.longLivedPending = 100
	if got := selectGeneration(); got != -1 {
		t.Fatalf("selectGeneration with low pending/total: got %d, want -1", got)
	}
	// Push pending above the 25% gate and gen-2 should win.
	state.longLivedPending = 300
	if got := selectGeneration(); got != 2 {
		t.Fatalf("selectGeneration with high pending/total: got %d, want 2", got)
	}
}
