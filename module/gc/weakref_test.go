// Tests for the weakref half of the cycle collector. Mirrors the
// shape of cpython/Lib/test/test_weakref.py's collector cases: the
// referent must be cleared on collection and the registered callback
// must fire exactly once with the weakref as its sole argument.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// clearWeakrefs drops the weakref registry between tests; the rest of
// the per-package state is wiped by untrackAll.
func clearWeakrefs() {
	state.mu.Lock()
	state.weakrefs = make(map[objects.Object][]*objects.Weakref)
	state.mu.Unlock()
}

func TestRegisterWeakrefSkipsNoneAndNil(t *testing.T) {
	defer clearWeakrefs()
	// Registering nil or a weakref already pointing at None is a
	// no-op. The map must stay empty.
	RegisterWeakref(nil)

	dead := objects.NewWeakref(objects.NewList(nil), nil)
	dead.Clear()
	// The hook registered dead when it was alive; reset before checking
	// that the explicit RegisterWeakref on a dead weakref is a no-op.
	clearWeakrefs()
	RegisterWeakref(dead)

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.weakrefs) != 0 {
		t.Fatalf("weakrefs map = %v, want empty", state.weakrefs)
	}
}

func TestCollectClearsWeakrefForCycle(t *testing.T) {
	defer untrackAll()
	defer clearWeakrefs()
	// Two-node cycle plus a weakref onto one of them. After Collect
	// the weakref must report None: handle_weakrefs runs before
	// reclaim and clears the referent.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	w := objects.NewWeakref(a, nil)
	RegisterWeakref(w)

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	if got := w.Referent(); got != objects.None() {
		t.Fatalf("Referent after Collect = %v, want None", got)
	}
}

func TestWeakrefCallbackFiresOnceWithWeakrefArg(t *testing.T) {
	defer untrackAll()
	defer clearWeakrefs()

	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	var calls int
	var seen objects.Object
	cb := objects.NewBuiltinFunction("cb", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		if len(args) == 1 {
			seen = args[0]
		}
		return objects.None(), nil
	})

	w := objects.NewWeakref(a, cb)
	RegisterWeakref(w)

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	if calls != 1 {
		t.Fatalf("callback fired %d times, want 1", calls)
	}
	if seen != objects.Object(w) {
		t.Fatalf("callback arg = %v, want the weakref %v", seen, w)
	}

	// A second collection must not re-fire the callback: handleWeakrefs
	// has already deleted the registry entry and Clear is idempotent.
	if got := Collect(2); got != 0 {
		t.Fatalf("second Collect = %d, want 0", got)
	}
	if calls != 1 {
		t.Fatalf("callback re-fired: now %d, want 1", calls)
	}
}

func TestWeakrefSurvivesIfReferentSurvives(t *testing.T) {
	defer untrackAll()
	defer clearWeakrefs()
	// Externally rooted target must keep its weakref alive across a
	// collection. Use linkInto's manual Incref to stand in for the
	// container-refcount work that 1679-A will land.
	target := objects.NewList(nil)
	objects.Incref(target)
	defer objects.Decref(target)
	Track(target)

	w := objects.NewWeakref(target, nil)
	RegisterWeakref(w)

	if got := Collect(2); got != 0 {
		t.Fatalf("Collect reclaimed rooted target: %d", got)
	}
	if got := w.Referent(); got != target {
		t.Fatalf("Referent = %v, want %v", got, target)
	}
}

func TestCollectWithNoWeakrefsIsCallbackFree(t *testing.T) {
	defer untrackAll()
	defer clearWeakrefs()
	// Running Collect against a cycle with no registered weakrefs
	// must not touch the weakref registry or panic on the empty map.
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim without weakrefs")
	}
}
