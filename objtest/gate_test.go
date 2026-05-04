// Package objtest exercises the v0.2 gate from
// notes/Spec/1600/1608_gopy_objects_overview.md: build a dict, hash a
// tuple, iterate a list. The harness doubles as a smoke test for
// every concrete type that v0.2 ships.
package objtest

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/abstract"
	"github.com/tamnd/gopy/objects"
)

func TestGateBuildDict(t *testing.T) {
	d := objects.NewDict()
	one := objects.NewInt(1)
	two := objects.NewInt(2)
	hello := objects.NewInt(100)

	if err := d.SetItem(one, hello); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if err := d.SetItem(two, objects.NewInt(200)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}

	got, err := d.GetItem(one)
	if err != nil {
		t.Fatalf("GetItem(1): %v", err)
	}
	if got != hello {
		t.Fatalf("GetItem(1) returned %v, want %v", got, hello)
	}

	if d.Len() != 2 {
		t.Fatalf("Len = %d, want 2", d.Len())
	}

	if err := d.DelItem(one); err != nil {
		t.Fatalf("DelItem: %v", err)
	}
	if d.Len() != 1 {
		t.Fatalf("Len after DelItem = %d, want 1", d.Len())
	}

	if _, err := d.GetItem(one); err == nil {
		t.Fatal("GetItem after DelItem returned no error")
	}
}

func TestGateHashTuple(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(1),
		objects.NewInt(2),
		objects.None(),
	})
	h, err := objects.Hash(tup)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h == 0 {
		t.Fatal("Hash returned 0; expected nonzero")
	}

	again, err := objects.Hash(tup)
	if err != nil {
		t.Fatalf("Hash second call: %v", err)
	}
	if h != again {
		t.Fatalf("Hash not deterministic: %d vs %d", h, again)
	}

	other := objects.NewTuple([]objects.Object{
		objects.NewInt(1),
		objects.NewInt(2),
		objects.None(),
	})
	otherH, err := objects.Hash(other)
	if err != nil {
		t.Fatal(err)
	}
	if h != otherH {
		t.Fatalf("equal tuples hash differently: %d vs %d", h, otherH)
	}
}

func TestGateIterList(t *testing.T) {
	l := objects.NewList([]objects.Object{
		objects.NewInt(10),
		objects.NewInt(20),
		objects.NewInt(30),
	})
	it, err := abstract.Iter(l)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}

	sum := int64(0)
	for {
		v, err := abstract.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			t.Fatalf("IterNext: %v", err)
		}
		i, ok := v.(*objects.Int)
		if !ok {
			t.Fatalf("IterNext returned %T, want *Int", v)
		}
		x, ok := i.Int64()
		if !ok {
			t.Fatal("Int64 conversion failed")
		}
		sum += x
	}
	if sum != 60 {
		t.Fatalf("sum = %d, want 60", sum)
	}
}

func TestSmallIntCache(t *testing.T) {
	a := objects.NewInt(5)
	b := objects.NewInt(5)
	if a != b {
		t.Fatal("NewInt(5) returned distinct pointers; small-int cache failed")
	}

	c := objects.NewInt(1000)
	d := objects.NewInt(1000)
	if c == d {
		t.Fatal("NewInt(1000) returned cached pointer; small-int cache too wide")
	}
}

func TestEmptyTupleSingleton(t *testing.T) {
	a := objects.NewTuple(nil)
	b := objects.NewTuple([]objects.Object{})
	if a != b {
		t.Fatal("empty tuple is not a singleton")
	}
}

func TestNoneSingleton(t *testing.T) {
	if objects.None() != objects.None() {
		t.Fatal("None is not a singleton")
	}
	if !objects.IsNone(objects.None()) {
		t.Fatal("IsNone(None) returned false")
	}
}

func TestBoolSingletons(t *testing.T) {
	if objects.NewBool(true) != objects.True() {
		t.Fatal("NewBool(true) is not True")
	}
	if objects.NewBool(false) != objects.False() {
		t.Fatal("NewBool(false) is not False")
	}
}

func TestIntArithmetic(t *testing.T) {
	a := objects.NewInt(2)
	b := objects.NewInt(3)
	sum, err := abstract.Add(a, b)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := sum.(*objects.Int).Int64()
	if got != 5 {
		t.Fatalf("2 + 3 = %d, want 5", got)
	}

	prod, err := abstract.Multiply(a, b)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = prod.(*objects.Int).Int64()
	if got != 6 {
		t.Fatalf("2 * 3 = %d, want 6", got)
	}
}

func TestRangeIter(t *testing.T) {
	r, err := objects.NewRange(objects.NewInt(0), objects.NewInt(5), objects.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	it, err := abstract.Iter(r)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for {
		v, err := abstract.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			t.Fatal(err)
		}
		x, _ := v.(*objects.Int).Int64()
		got = append(got, x)
	}
	want := []int64{0, 1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("range iter got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("range iter[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestTypeMRO(t *testing.T) {
	mro := objects.BoolType.MRO
	want := []*objects.Type{objects.BoolType, objects.IntType, objects.ObjectType()}
	if len(mro) != len(want) {
		t.Fatalf("BoolType MRO len = %d, want %d", len(mro), len(want))
	}
	for i := range want {
		if mro[i] != want[i] {
			t.Fatalf("BoolType MRO[%d] = %v, want %v", i, mro[i].Name, want[i].Name)
		}
	}
}

func TestRepr(t *testing.T) {
	cases := []struct {
		o    objects.Object
		want string
	}{
		{objects.NewInt(42), "42"},
		{objects.NewFloat(3.5), "3.5"},
		{objects.True(), "True"},
		{objects.False(), "False"},
		{objects.None(), "None"},
		{objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)}), "(1, 2)"},
		{objects.NewTuple([]objects.Object{objects.NewInt(1)}), "(1,)"},
		{objects.NewTuple(nil), "()"},
		{objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2)}), "[1, 2]"},
	}
	for _, tc := range cases {
		got, err := objects.Repr(tc.o)
		if err != nil {
			t.Errorf("Repr(%v): %v", tc.o, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Repr(%v) = %q, want %q", tc.o, got, tc.want)
		}
	}
}
