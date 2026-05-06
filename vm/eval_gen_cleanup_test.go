package vm

import (
	"testing"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func TestStopIterationValueExtractsArgs0(t *testing.T) {
	v := objects.NewInt(42)
	exc := pyerrors.New(pyerrors.PyExc_StopIteration, objects.NewTuple([]objects.Object{v}))
	got := stopIterationValue(exc)
	iv, ok := got.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", got)
	}
	if n, _ := iv.Int64(); n != 42 {
		t.Fatalf("value = %d, want 42", n)
	}
}

func TestStopIterationValueEmptyArgsIsNone(t *testing.T) {
	exc := pyerrors.New(pyerrors.PyExc_StopIteration, nil)
	got := stopIterationValue(exc)
	if got != objects.None() {
		t.Fatalf("value = %v, want None", got)
	}
}

func TestStopIterationValueNonExceptionFallsBackToNone(t *testing.T) {
	if got := stopIterationValue(objects.NewInt(1)); got != objects.None() {
		t.Fatalf("value = %v, want None", got)
	}
}
