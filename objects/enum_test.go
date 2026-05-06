package objects

import (
	"errors"
	"testing"
)

func TestEnumerateDefaultStart(t *testing.T) {
	src := NewTuple([]Object{NewInt(100), NewInt(200), NewInt(300)})
	e, err := NewEnumerate(src, nil)
	if err != nil {
		t.Fatalf("NewEnumerate: %v", err)
	}
	want := [][2]int64{{0, 100}, {1, 200}, {2, 300}}
	for i, w := range want {
		v, err := e.Type().IterNext(e)
		if err != nil {
			t.Fatalf("IterNext %d: %v", i, err)
		}
		tup := v.(*Tuple)
		idx, _ := tup.Item(0).(*Int).Int64()
		val, _ := tup.Item(1).(*Int).Int64()
		if idx != w[0] || val != w[1] {
			t.Fatalf("step %d = (%d,%d), want (%d,%d)", i, idx, val, w[0], w[1])
		}
	}
	if _, err := e.Type().IterNext(e); !errors.Is(err, ErrStopIteration) {
		t.Fatalf("trailing IterNext = %v, want StopIteration", err)
	}
}

func TestEnumerateCustomStart(t *testing.T) {
	src := NewTuple([]Object{NewInt(7), NewInt(8)})
	e, err := NewEnumerate(src, NewInt(10))
	if err != nil {
		t.Fatalf("NewEnumerate: %v", err)
	}
	v, _ := e.Type().IterNext(e)
	idx, _ := v.(*Tuple).Item(0).(*Int).Int64()
	if idx != 10 {
		t.Fatalf("first index = %d, want 10", idx)
	}
}

func TestReversedOverTuple(t *testing.T) {
	src := NewTuple([]Object{NewInt(1), NewInt(2), NewInt(3)})
	r, err := NewReversed(src)
	if err != nil {
		t.Fatalf("NewReversed: %v", err)
	}
	var got []int64
	for {
		v, err := r.Type().IterNext(r)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			t.Fatalf("IterNext: %v", err)
		}
		n, _ := v.(*Int).Int64()
		got = append(got, n)
	}
	if len(got) != 3 || got[0] != 3 || got[2] != 1 {
		t.Fatalf("got %v, want [3 2 1]", got)
	}
}

func TestReversedRejectsNonSequence(t *testing.T) {
	if _, err := NewReversed(None()); err == nil {
		t.Fatal("reversed(None) must error")
	}
}
