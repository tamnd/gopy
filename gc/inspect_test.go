// Tests for the inspection helpers (get_objects, get_referrers,
// get_referents, freeze/unfreeze).

package gc

import (
	"slices"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func contains(haystack []objects.Object, needle objects.Object) bool {
	return slices.Contains(haystack, needle)
}

func TestGetObjectsAllGenerations(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	Track(a)
	Track(b)

	got := GetObjects(-1)
	if !contains(got, a) || !contains(got, b) {
		t.Fatalf("get_objects missed an entry: %d items", len(got))
	}
}

func TestGetObjectsByGeneration(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	Track(a)

	gen0 := GetObjects(0)
	if !contains(gen0, a) {
		t.Fatalf("expected %v in gen0", a)
	}

	// Promote a to gen1 by collecting gen0.
	Collect(0)

	gen0 = GetObjects(0)
	if contains(gen0, a) {
		t.Fatalf("survivor still in gen0 after Collect(0)")
	}
	gen1 := GetObjects(1)
	if !contains(gen1, a) {
		t.Fatalf("survivor missing from gen1")
	}
}

func TestGetReferentsWalksTraverse(t *testing.T) {
	defer untrackAll()
	inner1 := objects.NewStr("a")
	inner2 := objects.NewStr("b")
	outer := objects.NewList([]objects.Object{inner1, inner2})

	got := GetReferents(outer)
	if len(got) != 2 || !contains(got, inner1) || !contains(got, inner2) {
		t.Fatalf("get_referents = %v, want [inner1 inner2]", got)
	}
}

func TestGetReferrersFindsSourceContainers(t *testing.T) {
	defer untrackAll()
	target := objects.NewList(nil)
	source := objects.NewList([]objects.Object{target})
	other := objects.NewList(nil)
	Track(source)
	Track(other)

	got := GetReferrers(target)
	if !contains(got, source) {
		t.Fatalf("get_referrers missed source: %v", got)
	}
	if contains(got, other) {
		t.Fatalf("get_referrers picked up unrelated tracked object")
	}
}

func TestFreezeAndUnfreezeRoundTrip(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	Track(a)

	if got := GetFreezeCount(); got != 0 {
		t.Fatalf("freeze count before Freeze = %d, want 0", got)
	}
	Freeze()
	if got := GetFreezeCount(); got != 1 {
		t.Fatalf("freeze count after Freeze = %d, want 1", got)
	}

	// Frozen object must not appear in any regular generation.
	for gen := range NumGenerations {
		if contains(GetObjects(gen), a) {
			t.Fatalf("frozen object still in generation %d", gen)
		}
	}

	Unfreeze()
	if got := GetFreezeCount(); got != 0 {
		t.Fatalf("freeze count after Unfreeze = %d, want 0", got)
	}
	if !contains(GetObjects(0), a) {
		t.Fatalf("unfrozen object not back in gen0")
	}
}

func TestFrozenObjectsSkipCollect(t *testing.T) {
	defer untrackAll()
	// Build a self-referential list, freeze it, then Collect.
	// Frozen objects must survive every collection.
	l := objects.NewList(nil)
	l.Append(l)
	Track(l)
	Freeze()

	if Collect(2) != 0 {
		t.Fatalf("Collect reclaimed a frozen object")
	}
	if !IsTracked(l) {
		t.Fatalf("frozen object lost tracking after Collect")
	}
	Unfreeze()
}

func TestGarbageStartsEmpty(t *testing.T) {
	defer untrackAll()
	if got := Garbage(); len(got) != 0 {
		t.Fatalf("Garbage() = %v, want empty", got)
	}
}
