package stackref

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/objects"
)

// TestRefSize pins the in-memory width of Ref so an accidental field
// addition (an extra tag, a debug string, a parent pointer) trips a
// test instead of silently doubling the per-stack-slot footprint.
//
// CPython's _PyStackRef is one uintptr in the GIL build (a tagged
// pointer). gopy stores objects.Object directly, which is a Go
// interface header (typeptr + dataptr = two pointer-words). The
// guarantee here is "no wider than one interface header", not the
// 1-word C shape; the wrapper exists so v0.14 can swap in a packed
// representation without touching dispatch arms.
func TestRefSize(t *testing.T) {
	got := unsafe.Sizeof(Ref{})
	want := unsafe.Sizeof(objects.Object(nil))
	if got != want {
		t.Errorf("Ref size = %d, want %d (one interface header)", got, want)
	}
}

func TestNullSentinel(t *testing.T) {
	if !Null.IsNull() {
		t.Error("Null.IsNull() must be true")
	}
	var zero Ref
	if !zero.IsNull() {
		t.Error("zero-value Ref must be null")
	}
}

func TestFromObjectRoundTrip(t *testing.T) {
	o := objects.NewInt(42)
	r := FromObject(o)
	if r.IsNull() {
		t.Error("FromObject of non-nil should not be null")
	}
	if r.AsObject() != o {
		t.Error("AsObject must return the original object")
	}
	if r.AsObjectSteal() != o {
		t.Error("AsObjectSteal must return the original object")
	}
}

func TestDupIndependent(t *testing.T) {
	o := objects.NewInt(7)
	r := FromObject(o)
	d := r.Dup()
	if d.AsObject() != o {
		t.Error("Dup must reference the same object")
	}
	d.Close()
	if r.AsObject() != o {
		t.Error("closing the dup must not affect the original")
	}
}

func TestSentinels(t *testing.T) {
	if None.IsNull() || True.IsNull() || False.IsNull() {
		t.Error("None/True/False sentinels must not be null")
	}
	if None.AsObject() != objects.None() {
		t.Error("None.AsObject() must equal objects.None()")
	}
	if True.AsObject() != objects.True() {
		t.Error("True.AsObject() must equal objects.True()")
	}
	if False.AsObject() != objects.False() {
		t.Error("False.AsObject() must equal objects.False()")
	}
}

func TestBoolNonePredicates(t *testing.T) {
	if !True.IsTrue() {
		t.Error("True.IsTrue must hold")
	}
	if True.IsFalse() || True.IsNone() {
		t.Error("True must not be False or None")
	}
	if !False.IsFalse() {
		t.Error("False.IsFalse must hold")
	}
	if False.IsTrue() || False.IsNone() {
		t.Error("False must not be True or None")
	}
	if !None.IsNone() {
		t.Error("None.IsNone must hold")
	}
	if None.IsTrue() || None.IsFalse() {
		t.Error("None must not be True or False")
	}
	other := FromObject(objects.NewInt(7))
	if other.IsTrue() || other.IsFalse() || other.IsNone() {
		t.Error("arbitrary int must not satisfy any singleton predicate")
	}
}

func TestFromObjectNewAndImmortal(t *testing.T) {
	o := objects.NewInt(1)
	if FromObjectNew(o).AsObject() != o {
		t.Error("FromObjectNew round-trip failed")
	}
	if FromObjectImmortal(o).AsObject() != o {
		t.Error("FromObjectImmortal round-trip failed")
	}
}
