package builtins

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestIntFromString pins int("42") == 42.
func TestIntFromString(t *testing.T) {
	v, err := IntCtor([]objects.Object{objects.NewStr("42")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("int('42') = %d, want 42", got)
	}
}

// TestIntFromStringWithBase pins int("ff", 16) == 255.
func TestIntFromStringWithBase(t *testing.T) {
	v, err := IntCtor([]objects.Object{objects.NewStr("ff"), objects.NewInt(16)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 255 {
		t.Errorf("int('ff', 16) = %d, want 255", got)
	}
}

// TestIntFromFloat pins int(3.7) == 3 (truncation toward zero).
func TestIntFromFloat(t *testing.T) {
	v, err := IntCtor([]objects.Object{objects.NewFloat(3.7)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 3 {
		t.Errorf("int(3.7) = %d, want 3", got)
	}
}

// TestIntEmpty pins int() == 0.
func TestIntEmpty(t *testing.T) {
	v, err := IntCtor(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 0 {
		t.Errorf("int() = %d, want 0", got)
	}
}

// TestIntInvalidLiteral pins the ValueError shape.
func TestIntInvalidLiteral(t *testing.T) {
	_, err := IntCtor([]objects.Object{objects.NewStr("nope")}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("err = %v, want ValueError", err)
	}
}

// TestFloatFromString pins float("3.14") == 3.14.
func TestFloatFromString(t *testing.T) {
	v, err := FloatCtor([]objects.Object{objects.NewStr("3.14")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(*objects.Float).Float64(); got != 3.14 {
		t.Errorf("float('3.14') = %v, want 3.14", got)
	}
}

// TestFloatFromInt pins float(7) == 7.0.
func TestFloatFromInt(t *testing.T) {
	v, err := FloatCtor([]objects.Object{objects.NewInt(7)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(*objects.Float).Float64(); got != 7.0 {
		t.Errorf("float(7) = %v, want 7.0", got)
	}
}

// TestBoolEmpty pins bool() == False.
func TestBoolEmpty(t *testing.T) {
	v, _ := BoolCtor(nil, nil)
	if v != objects.False() {
		t.Errorf("bool() = %v, want False", v)
	}
}

// TestBoolFromInt pins bool(7) == True, bool(0) == False.
func TestBoolFromInt(t *testing.T) {
	v, _ := BoolCtor([]objects.Object{objects.NewInt(7)}, nil)
	if v != objects.True() {
		t.Errorf("bool(7) = %v, want True", v)
	}
	v, _ = BoolCtor([]objects.Object{objects.NewInt(0)}, nil)
	if v != objects.False() {
		t.Errorf("bool(0) = %v, want False", v)
	}
}

// TestListFromTuple pins list((1,2)) drains the iterable.
func TestListFromTuple(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	v, err := ListCtor([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	l := v.(*objects.List)
	if l.Len() != 2 {
		t.Fatalf("len = %d, want 2", l.Len())
	}
	if got, _ := l.Item(1).(*objects.Int).Int64(); got != 2 {
		t.Errorf("[1] = %d, want 2", got)
	}
}

// TestTupleFromList pins tuple([1,2,3]).
func TestTupleFromList(t *testing.T) {
	lst := objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3)})
	v, err := TupleCtor([]objects.Object{lst}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup := v.(*objects.Tuple)
	if tup.Len() != 3 {
		t.Fatalf("len = %d, want 3", tup.Len())
	}
}

// TestDictFromKwargs pins dict(a=1, b=2).
func TestDictFromKwargs(t *testing.T) {
	v, err := DictCtor(nil, map[string]objects.Object{
		"a": objects.NewInt(1),
		"b": objects.NewInt(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	d := v.(*objects.Dict)
	got, err := d.GetItem(objects.NewStr("a"))
	if err != nil {
		t.Fatal(err)
	}
	if val, _ := got.(*objects.Int).Int64(); val != 1 {
		t.Errorf("dict[a] = %d, want 1", val)
	}
}

// TestDictFromPairs pins dict([(k1,v1),(k2,v2)]).
func TestDictFromPairs(t *testing.T) {
	pairs := objects.NewList([]objects.Object{
		objects.NewTuple([]objects.Object{objects.NewStr("a"), objects.NewInt(1)}),
		objects.NewTuple([]objects.Object{objects.NewStr("b"), objects.NewInt(2)}),
	})
	v, err := DictCtor([]objects.Object{pairs}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := v.(*objects.Dict)
	got, _ := d.GetItem(objects.NewStr("b"))
	if val, _ := got.(*objects.Int).Int64(); val != 2 {
		t.Errorf("dict[b] = %d, want 2", val)
	}
}
