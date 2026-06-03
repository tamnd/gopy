package builtins

import (
	stderrors "errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestLenSequenceAndMapping pins len() against the three concrete
// containers v0.6 ships: tuple (sequence), list (sequence), dict
// (mapping). Each shape exercises a different protocol slot.
func TestLenSequenceAndMapping(t *testing.T) {
	cases := []struct {
		name string
		o    objects.Object
		want int64
	}{
		{"tuple", objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)}), 2},
		{"list", objects.NewList([]objects.Object{objects.NewInt(1)}), 1},
		{"dict", dictWith(objects.NewStr("k"), objects.NewInt(7)), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Len([]objects.Object{tc.o}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := v.(*objects.Int).Int64()
			if got != tc.want {
				t.Errorf("len(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestLenRejectsNonSized pins the TypeError shape: int has no length.
func TestLenRejectsNonSized(t *testing.T) {
	_, err := Len([]objects.Object{objects.NewInt(1)}, nil)
	if err == nil {
		t.Fatal("expected TypeError, got nil")
	}
}

// TestIterAndNextRoundTrip pins iter() returns the expected iterator
// type and next() walks the values to StopIteration.
func TestIterAndNextRoundTrip(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewInt(10), objects.NewInt(20)})
	it, err := Iter([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Next([]objects.Object{it}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*objects.Int).Int64(); got != 10 {
		t.Errorf("first next() = %d, want 10", got)
	}
	v, _ = Next([]objects.Object{it}, nil)
	if got, _ := v.(*objects.Int).Int64(); got != 20 {
		t.Errorf("second next() = %d, want 20", got)
	}
	_, err = Next([]objects.Object{it}, nil)
	if !stderrors.Is(err, objects.ErrStopIteration) {
		t.Errorf("third next() = %v, want StopIteration", err)
	}
}

// TestNextWithDefaultSwallowsStop pins the default-arg branch:
// StopIteration becomes the default value silently.
func TestNextWithDefaultSwallowsStop(t *testing.T) {
	it, _ := Iter([]objects.Object{objects.NewTuple(nil)}, nil)
	def := objects.NewInt(42)
	v, err := Next([]objects.Object{it, def}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 42 {
		t.Errorf("next(empty, 42) = %d, want 42", got)
	}
}

// TestRangeBuilderShape pins the three argument shapes.
func TestRangeBuilderShape(t *testing.T) {
	cases := []struct {
		name string
		args []objects.Object
		want []int64
	}{
		{"stop_only", []objects.Object{objects.NewInt(3)}, []int64{0, 1, 2}},
		{"start_stop", []objects.Object{objects.NewInt(2), objects.NewInt(5)}, []int64{2, 3, 4}},
		{"start_stop_step", []objects.Object{objects.NewInt(1), objects.NewInt(7), objects.NewInt(2)}, []int64{1, 3, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Range(tc.args, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := drain(t, r)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.want))
			}
			for i, w := range tc.want {
				v, _ := got[i].(*objects.Int).Int64()
				if v != w {
					t.Errorf("[%d] = %d, want %d", i, v, w)
				}
			}
		})
	}
}

// TestReversedOnTuple pins the sequence path.
func TestReversedOnTuple(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3)})
	rev, err := Reversed([]objects.Object{tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := drain(t, rev)
	want := []int64{3, 2, 1}
	for i, w := range want {
		v, _ := got[i].(*objects.Int).Int64()
		if v != w {
			t.Errorf("reversed[%d] = %d, want %d", i, v, w)
		}
	}
}

// TestEnumerateStartIndex pins the (iterable, start) shape: the
// counter begins at start and increments.
func TestEnumerateStartIndex(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.NewStr("a"), objects.NewStr("b")})
	e, err := Enumerate(objects.EnumerateType, []objects.Object{tup, objects.NewInt(10)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := drain(t, e)
	if len(out) != 2 {
		t.Fatalf("got %d items, want 2", len(out))
	}
	first := out[0].(*objects.Tuple)
	idx, _ := first.Item(0).(*objects.Int).Int64()
	if idx != 10 {
		t.Errorf("first index = %d, want 10", idx)
	}
}

// TestZipPairsEqualLength pins the standard case.
func TestZipPairsEqualLength(t *testing.T) {
	a := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	b := objects.NewTuple([]objects.Object{objects.NewStr("x"), objects.NewStr("y")})
	z, err := Zip([]objects.Object{a, b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := drain(t, z)
	if len(out) != 2 {
		t.Fatalf("zip yielded %d, want 2", len(out))
	}
	pair := out[1].(*objects.Tuple)
	if v, _ := pair.Item(0).(*objects.Int).Int64(); v != 2 {
		t.Errorf("zip[1][0] = %d, want 2", v)
	}
}

// TestZipStrictMismatch pins the strict=True ValueError when
// iterators have different lengths.
func TestZipStrictMismatch(t *testing.T) {
	a := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	b := objects.NewTuple([]objects.Object{objects.NewInt(1)})
	z, err := Zip([]objects.Object{a, b}, map[string]objects.Object{"strict": objects.True()})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = z.Type().IterNext(z)
	_, err = z.Type().IterNext(z)
	if err == nil {
		t.Fatal("expected ValueError, got nil")
	}
}

func drain(t *testing.T, o objects.Object) []objects.Object {
	t.Helper()
	it := o
	if it.Type().IterNext == nil {
		var err error
		it, err = it.Type().Iter(it)
		if err != nil {
			t.Fatal(err)
		}
	}
	out := []objects.Object{}
	for {
		v, err := it.Type().IterNext(it)
		if stderrors.Is(err, objects.ErrStopIteration) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
}

func dictWith(k, v objects.Object) *objects.Dict {
	d := objects.NewDict()
	_ = d.SetItem(k, v)
	return d
}
