package objects

import (
	"errors"
	"testing"
)

func TestFilterWithPredicate(t *testing.T) {
	src := NewList([]Object{NewInt(1), NewInt(2), NewInt(3), NewInt(4)})
	called := 0
	pred := NewBuiltinFunction("isEven", func(args []Object, _ map[string]Object) (Object, error) {
		called++
		i := args[0].(*Int)
		v, _ := i.Int64()
		if v%2 == 0 {
			return True(), nil
		}
		return False(), nil
	})
	f, err := NewFilter(pred, src)
	if err != nil {
		t.Fatalf("NewFilter: %v", err)
	}
	got := drainIter(t, f)
	want := []int64{2, 4}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i, n := range want {
		v, _ := got[i].(*Int).Int64()
		if v != n {
			t.Fatalf("got[%d] = %d, want %d", i, v, n)
		}
	}
	if called != 4 {
		t.Fatalf("predicate calls = %d, want 4", called)
	}
}

func TestFilterNoneIsTruthShortcut(t *testing.T) {
	src := NewList([]Object{NewInt(0), NewInt(1), NewInt(0), NewInt(7)})
	f, err := NewFilter(None(), src)
	if err != nil {
		t.Fatalf("NewFilter: %v", err)
	}
	got := drainIter(t, f)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if v, _ := got[0].(*Int).Int64(); v != 1 {
		t.Fatalf("got[0] = %v, want 1", got[0])
	}
	if v, _ := got[1].(*Int).Int64(); v != 7 {
		t.Fatalf("got[1] = %v, want 7", got[1])
	}
}

func TestFilterRejectsNonIterable(t *testing.T) {
	if _, err := NewFilter(None(), NewInt(1)); err == nil {
		t.Fatal("filter(None, 1): want TypeError, got nil")
	}
}

func TestFilterIsItsOwnIterator(t *testing.T) {
	src := NewList([]Object{NewInt(1)})
	f, err := NewFilter(None(), src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.Type().Iter(f)
	if err != nil {
		t.Fatal(err)
	}
	if out != f {
		t.Fatalf("filter is not its own iterator")
	}
}

func drainIter(t *testing.T, it Object) []Object {
	t.Helper()
	var out []Object
	for {
		v, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return out
			}
			t.Fatalf("iter: %v", err)
		}
		out = append(out, v)
	}
}
