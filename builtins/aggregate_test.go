package builtins

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestSumOverInts pins the default-start path: starts at 0 and adds.
func TestSumOverInts(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
	})
	v, err := Sum([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 6 {
		t.Errorf("sum([1,2,3]) = %d, want 6", got)
	}
}

// TestSumWithStart pins the second positional argument.
func TestSumWithStart(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewInt(2), objects.NewInt(3)})
	v, err := Sum([]objects.Object{tup, objects.NewInt(10)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 15 {
		t.Errorf("sum([2,3], 10) = %d, want 15", got)
	}
}

// TestSumRejectsStringStart pins the "use ''.join()" guard.
func TestSumRejectsStringStart(t *testing.T) {
	tup := objects.NewTuple(nil)
	_, err := Sum([]objects.Object{tup, objects.NewStr("seed")}, nil)
	if err == nil || !strings.Contains(err.Error(), "''.join") {
		t.Fatalf("err = %v, want join() guidance", err)
	}
}

// TestMinOfTuple pins the iterable path.
func TestMinOfTuple(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(3), objects.NewInt(1), objects.NewInt(2),
	})
	v, err := MinOf([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 1 {
		t.Errorf("min([3,1,2]) = %d, want 1", got)
	}
}

// TestMaxOfPositionals pins the multi-positional path.
func TestMaxOfPositionals(t *testing.T) {
	v, err := MaxOf([]objects.Object{
		objects.NewInt(5), objects.NewInt(9), objects.NewInt(7),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 9 {
		t.Errorf("max(5,9,7) = %d, want 9", got)
	}
}

// TestMinDefaultOnEmpty pins the default kwarg.
func TestMinDefaultOnEmpty(t *testing.T) {
	tup := objects.NewTuple(nil)
	def := objects.NewInt(-1)
	v, err := MinOf([]objects.Object{tup}, map[string]objects.Object{"default": def})
	if err != nil {
		t.Fatal(err)
	}
	if v != def {
		t.Errorf("min([], default=-1) = %v, want -1", v)
	}
}

// TestMinEmptyNoDefaultRaises pins the ValueError shape.
func TestMinEmptyNoDefaultRaises(t *testing.T) {
	_, err := MinOf([]objects.Object{objects.NewTuple(nil)}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("err = %v, want ValueError", err)
	}
}

// TestAnyShortCircuit pins the True-on-first-truthy behavior.
func TestAnyShortCircuit(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(0), objects.NewInt(0), objects.NewInt(7),
	})
	v, err := Any([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.True() {
		t.Errorf("any([0,0,7]) = %v, want True", v)
	}
}

// TestAnyAllFalsy pins the False return when no element is truthy.
func TestAnyAllFalsy(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewInt(0), objects.NewInt(0)})
	v, _ := Any([]objects.Object{tup}, nil)
	if v != objects.False() {
		t.Errorf("any([0,0]) = %v, want False", v)
	}
}

// TestAllShortCircuit pins the False-on-first-falsy behavior.
func TestAllShortCircuit(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(1), objects.NewInt(0), objects.NewInt(1),
	})
	v, _ := All([]objects.Object{tup}, nil)
	if v != objects.False() {
		t.Errorf("all([1,0,1]) = %v, want False", v)
	}
}

// TestAllAllTruthy pins the True return when no element is falsy.
func TestAllAllTruthy(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	v, _ := All([]objects.Object{tup}, nil)
	if v != objects.True() {
		t.Errorf("all([1,2]) = %v, want True", v)
	}
}

// TestSortedAscending pins the default ordering.
func TestSortedAscending(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(3), objects.NewInt(1), objects.NewInt(2),
	})
	v, err := Sorted([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(*objects.List)
	want := []int64{1, 2, 3}
	for i, w := range want {
		gv, _ := got.Item(i).(*objects.Int).Int64()
		if gv != w {
			t.Errorf("sorted[%d] = %d, want %d", i, gv, w)
		}
	}
}

// TestSortedReverse pins the reverse= kwarg.
func TestSortedReverse(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(1), objects.NewInt(3), objects.NewInt(2),
	})
	v, err := Sorted([]objects.Object{tup}, map[string]objects.Object{"reverse": objects.True()})
	if err != nil {
		t.Fatal(err)
	}
	got := v.(*objects.List)
	want := []int64{3, 2, 1}
	for i, w := range want {
		gv, _ := got.Item(i).(*objects.Int).Int64()
		if gv != w {
			t.Errorf("sorted reverse[%d] = %d, want %d", i, gv, w)
		}
	}
}
