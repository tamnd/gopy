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

func TestCollectIsNoop(t *testing.T) {
	if gc.Collect() != 0 {
		t.Fatal("v0.3 Collect must return 0")
	}
}
