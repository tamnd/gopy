package objects

import (
	"errors"
	"testing"
)

func TestRangeIteratorAscending(t *testing.T) {
	r, _ := NewRange(NewInt(0), NewInt(5), NewInt(1))
	it, _ := r.Type().Iter(r)
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
	if len(got) != 5 || got[0] != 0 || got[4] != 4 {
		t.Fatalf("got %v, want 0..4", got)
	}
}

func TestRangeIteratorLengthHint(t *testing.T) {
	r, _ := NewRange(NewInt(0), NewInt(10), NewInt(2))
	it, _ := r.Type().Iter(r)
	ri := it.(*rangeIterator)
	if v, _ := ri.LengthHint().Int64(); v != 5 {
		t.Fatalf("length_hint at start = %d, want 5", v)
	}
	_, _ = it.Type().IterNext(it)
	if v, _ := ri.LengthHint().Int64(); v != 4 {
		t.Fatalf("length_hint after one = %d, want 4", v)
	}
}

func TestRangeIteratorLengthHintBackward(t *testing.T) {
	r, _ := NewRange(NewInt(10), NewInt(0), NewInt(-1))
	it, _ := r.Type().Iter(r)
	ri := it.(*rangeIterator)
	if v, _ := ri.LengthHint().Int64(); v != 10 {
		t.Fatalf("length_hint backward = %d, want 10", v)
	}
}

func TestRangeIteratorLengthHintExhausted(t *testing.T) {
	r, _ := NewRange(NewInt(0), NewInt(2), NewInt(1))
	it, _ := r.Type().Iter(r)
	ri := it.(*rangeIterator)
	for {
		_, err := it.Type().IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
	}
	if v, _ := ri.LengthHint().Int64(); v != 0 {
		t.Fatalf("length_hint exhausted = %d, want 0", v)
	}
}
