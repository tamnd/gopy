package stackref

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

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

func TestFromObjectNewAndImmortal(t *testing.T) {
	o := objects.NewInt(1)
	if FromObjectNew(o).AsObject() != o {
		t.Error("FromObjectNew round-trip failed")
	}
	if FromObjectImmortal(o).AsObject() != o {
		t.Error("FromObjectImmortal round-trip failed")
	}
}
