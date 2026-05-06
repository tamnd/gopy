package objects

import (
	"strings"
	"testing"
)

func TestMapSingleIterable(t *testing.T) {
	src := NewList([]Object{NewInt(1), NewInt(2), NewInt(3)})
	double := NewBuiltinFunction("dbl", func(args []Object, _ map[string]Object) (Object, error) {
		i := args[0].(*Int)
		v, _ := i.Int64()
		return NewInt(v * 2), nil
	})
	m, err := NewMap(double, []Object{src}, false)
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	got := drainIter(t, m)
	want := []int64{2, 4, 6}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i, n := range want {
		v, _ := got[i].(*Int).Int64()
		if v != n {
			t.Fatalf("got[%d] = %d, want %d", i, v, n)
		}
	}
}

func TestMapMultipleIterablesShortest(t *testing.T) {
	a := NewList([]Object{NewInt(1), NewInt(2), NewInt(3)})
	b := NewList([]Object{NewInt(10), NewInt(20)})
	add := NewBuiltinFunction("add", func(args []Object, _ map[string]Object) (Object, error) {
		x, _ := args[0].(*Int).Int64()
		y, _ := args[1].(*Int).Int64()
		return NewInt(x + y), nil
	})
	m, err := NewMap(add, []Object{a, b}, false)
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	got := drainIter(t, m)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
}

func TestMapStrictShorter(t *testing.T) {
	a := NewList([]Object{NewInt(1), NewInt(2), NewInt(3)})
	b := NewList([]Object{NewInt(10), NewInt(20)})
	add := NewBuiltinFunction("add", func(args []Object, _ map[string]Object) (Object, error) {
		x, _ := args[0].(*Int).Int64()
		y, _ := args[1].(*Int).Int64()
		return NewInt(x + y), nil
	})
	m, err := NewMap(add, []Object{a, b}, true)
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	if _, err := IterNext(m); err != nil {
		t.Fatalf("first next: %v", err)
	}
	if _, err := IterNext(m); err != nil {
		t.Fatalf("second next: %v", err)
	}
	_, err = IterNext(m)
	if err == nil || !strings.Contains(err.Error(), "shorter") {
		t.Fatalf("third next err = %v, want ValueError shorter", err)
	}
}

func TestMapStrictLonger(t *testing.T) {
	a := NewList([]Object{NewInt(1)})
	b := NewList([]Object{NewInt(10), NewInt(20)})
	add := NewBuiltinFunction("add", func(args []Object, _ map[string]Object) (Object, error) {
		return NewInt(0), nil
	})
	m, err := NewMap(add, []Object{a, b}, true)
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	if _, err := IterNext(m); err != nil {
		t.Fatalf("first next: %v", err)
	}
	_, err = IterNext(m)
	if err == nil || !strings.Contains(err.Error(), "longer") {
		t.Fatalf("err = %v, want ValueError longer", err)
	}
}

func TestMapNeedsAtLeastOneIterable(t *testing.T) {
	if _, err := NewMap(None(), nil, false); err == nil {
		t.Fatal("NewMap(None, []): want TypeError, got nil")
	}
}

func TestMapPropagatesNonIterable(t *testing.T) {
	if _, err := NewMap(None(), []Object{NewInt(1)}, false); err == nil {
		t.Fatal("NewMap(None, [1]): want TypeError, got nil")
	}
}
