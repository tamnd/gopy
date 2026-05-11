// Tests for gc.Collect that exercise the full pipeline through the
// public surface. These run against the package-level state, so each
// test cleans up after itself with an Untrack.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// untrackAll drops every entry in the tracked map. Tests that fail
// partway through this file would otherwise leave residue in the
// shared state and cascade into later tests.
func untrackAll() {
	state.mu.Lock()
	for o := range state.tracked {
		if g, ok := state.tracked[o]; ok {
			listRemove(g)
		}
		delete(state.tracked, o)
	}
	for i := range state.generations {
		state.generations[i].count = 0
	}
	state.finalizers = make(map[objects.Object]Finalizer)
	state.mu.Unlock()
}

func TestCollectGenerationClampedDownward(t *testing.T) {
	defer untrackAll()
	// Negative gen must be treated as 0 and not panic.
	if got := Collect(-5); got != 0 {
		t.Fatalf("Collect(-5) = %d, want 0", got)
	}
}

func TestCollectGenerationClampedUpward(t *testing.T) {
	defer untrackAll()
	if got := Collect(1024); got != 0 {
		t.Fatalf("Collect(1024) = %d, want 0", got)
	}
}

func TestCollectPromotesSurvivors(t *testing.T) {
	defer untrackAll()
	// A tracked, externally-rooted list should survive collection
	// and end up in the next generation.
	a := objects.NewList(nil)
	Track(a)
	defer Untrack(a)

	if got, _, _ := GetCount(); got == 0 {
		t.Fatalf("expected gen0 count > 0 after Track, got 0")
	}
	if collected := Collect(0); collected != 0 {
		t.Fatalf("Collect(0) reclaimed %d, want 0 (object is rooted)", collected)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	g, ok := state.tracked[a]
	if !ok {
		t.Fatalf("survivor untracked")
	}
	// Should now sit on gen1's list.
	gen1 := state.generations[1].head
	found := false
	for h := gen1.next; h != gen1; h = h.next {
		if h == g {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("survivor not promoted to gen1")
	}
}

func TestCollectClearsCollectingAndUnreachableFlags(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	Track(a)
	Track(b)
	defer Untrack(a)
	defer Untrack(b)

	Collect(2)

	state.mu.Lock()
	defer state.mu.Unlock()
	for _, g := range state.tracked {
		if g.flags&gcCollecting != 0 {
			t.Fatalf("survivor still has COLLECTING bit")
		}
		if g.flags&gcUnreachable != 0 {
			t.Fatalf("survivor still has UNREACHABLE bit")
		}
	}
}
