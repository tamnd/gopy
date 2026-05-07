package objects

import (
	"errors"
	"testing"
)

func TestSeqIterOverTuple(t *testing.T) {
	src := NewTuple([]Object{NewInt(10), NewInt(20), NewInt(30)})
	it := NewSeqIter(src)
	var got []int64
	for {
		v, err := it.Type().IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			t.Fatalf("IterNext: %v", err)
		}
		n, _ := v.(*Int).Int64()
		got = append(got, n)
	}
	if len(got) != 3 || got[0] != 10 || got[2] != 30 {
		t.Fatalf("got %v, want [10 20 30]", got)
	}
}

func TestCallIterStopsOnSentinel(t *testing.T) {
	count := 0
	bf := NewBuiltinFunction("counter", func(_ []Object, _ map[string]Object) (Object, error) {
		count++
		return NewInt(int64(count)), nil
	})
	it := NewCallIter(bf, NewInt(3))
	var got []int64
	for {
		v, err := it.Type().IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			t.Fatalf("IterNext: %v", err)
		}
		n, _ := v.(*Int).Int64()
		got = append(got, n)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v, want [1 2] (sentinel = 3 stops iteration)", got)
	}
}
