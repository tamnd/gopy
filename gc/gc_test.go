package gc_test

import (
	"testing"

	"github.com/tamnd/gopy/gc"
	"github.com/tamnd/gopy/objects"
)

func TestFinalizerRunsOnce(t *testing.T) {
	o := objects.NewStr("x")
	calls := 0
	gc.RegisterFinalizer(o, func(_ objects.Object) { calls++ })
	gc.Finalize(o)
	gc.Finalize(o)
	if calls != 1 {
		t.Fatalf("finalizer ran %d times, want 1", calls)
	}
}

func TestTrackUntrack(t *testing.T) {
	o := objects.NewStr("x")
	gc.Track(o)
	if !gc.IsTracked(o) {
		t.Fatal("Track must mark the object")
	}
	gc.Untrack(o)
	if gc.IsTracked(o) {
		t.Fatal("Untrack must clear")
	}
}

func TestCollectEmptyReturnsZero(t *testing.T) {
	// With nothing tracked, the collector finds nothing to reclaim.
	if got := gc.Collect(2); got != 0 {
		t.Fatalf("Collect on empty state = %d, want 0", got)
	}
}

func TestCollectDisabledReturnsZero(t *testing.T) {
	gc.Disable()
	defer gc.Enable()
	o := objects.NewList(nil)
	o.Append(o)
	gc.Track(o)
	defer gc.Untrack(o)
	if got := gc.Collect(2); got != 0 {
		t.Fatalf("Collect while disabled = %d, want 0", got)
	}
}
