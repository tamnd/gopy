package objects

import (
	"fmt"
	"sort"
	"testing"
)

func intsListFromInts(xs []int64) *List {
	items := make([]Object, len(xs))
	for i, v := range xs {
		items[i] = NewInt(v)
	}
	return NewList(items)
}

func intsListToInts(t *testing.T, l *List) []int64 {
	t.Helper()
	out := make([]int64, l.Len())
	for i := 0; i < l.Len(); i++ {
		v, ok := l.Item(i).(*Int).Int64()
		if !ok {
			t.Fatalf("list[%d] is not a compact int", i)
		}
		out[i] = v
	}
	return out
}

func TestSortAscending(t *testing.T) {
	l := intsListFromInts([]int64{5, 1, 4, 2, 3})
	if err := l.Sort(nil, false); err != nil {
		t.Fatal(err)
	}
	got := intsListToInts(t, l)
	want := []int64{1, 2, 3, 4, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sort got %v, want %v", got, want)
			return
		}
	}
}

func TestSortAlreadySorted(t *testing.T) {
	in := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	l := intsListFromInts(in)
	if err := l.Sort(nil, false); err != nil {
		t.Fatal(err)
	}
	got := intsListToInts(t, l)
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("already-sorted got %v, want %v", got, in)
			return
		}
	}
}

func TestSortReversedInput(t *testing.T) {
	// Triggers count_run's descending-run path and the in-place reverse.
	in := []int64{9, 8, 7, 6, 5, 4, 3, 2, 1}
	l := intsListFromInts(in)
	if err := l.Sort(nil, false); err != nil {
		t.Fatal(err)
	}
	got := intsListToInts(t, l)
	for i, v := range got {
		if v != int64(i+1) {
			t.Errorf("reversed input got %v", got)
			return
		}
	}
}

func TestSortReverseFlag(t *testing.T) {
	in := []int64{5, 1, 4, 2, 3}
	l := intsListFromInts(in)
	if err := l.Sort(nil, true); err != nil {
		t.Fatal(err)
	}
	got := intsListToInts(t, l)
	want := []int64{5, 4, 3, 2, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reverse=true got %v, want %v", got, want)
			return
		}
	}
}

func TestSortLargeRandom(t *testing.T) {
	// Beyond MAX_MINRUN, so this exercises real run-finding and merging
	// rather than just binary insertion sort.
	const N = 1000
	in := make([]int64, N)
	for i := 0; i < N; i++ {
		in[i] = int64((i*31337 + 7) % 997)
	}
	l := intsListFromInts(in)
	if err := l.Sort(nil, false); err != nil {
		t.Fatal(err)
	}
	got := intsListToInts(t, l)
	want := append([]int64(nil), in...)
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at %d: got %d, want %d", i, got[i], want[i])
			return
		}
	}
}

func TestSortStability(t *testing.T) {
	// Pair (key, original_index). After sort by key alone, ties must
	// keep their original-index order. CPython promises this; the
	// merge_lo/merge_hi compare uses strict-less only, which is why
	// equality preserves order.
	const N = 200
	items := make([]Object, N)
	for i := 0; i < N; i++ {
		key := NewInt(int64(i % 7))
		idx := NewInt(int64(i))
		items[i] = NewTuple([]Object{key, idx})
	}
	l := NewList(items)
	def := &MethodDef{
		Name:  "first",
		Flags: MethO,
		O: func(_ Object, arg Object) (Object, error) {
			return arg.(*Tuple).Item(0), nil
		},
	}
	keyfn, err := NewCFunction(def, None(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Sort(keyfn, false); err != nil {
		t.Fatal(err)
	}
	prevKey := int64(-1)
	prevIdx := int64(-1)
	for i := 0; i < N; i++ {
		tup := l.Item(i).(*Tuple)
		k, _ := tup.Item(0).(*Int).Int64()
		idx, _ := tup.Item(1).(*Int).Int64()
		if k < prevKey {
			t.Errorf("not sorted by key at %d: %d < %d", i, k, prevKey)
			return
		}
		if k == prevKey && idx < prevIdx {
			t.Errorf("not stable at %d: key=%d idx=%d came after idx=%d",
				i, k, idx, prevIdx)
			return
		}
		prevKey, prevIdx = k, idx
	}
}

func TestSortKeyfuncCalledOnce(t *testing.T) {
	// CPython evaluates the key once per element and never again. If
	// our port ever started reading via the key during the merge it
	// would re-evaluate, which would be wrong.
	calls := 0
	def := &MethodDef{
		Name:  "neg",
		Flags: MethO,
		O: func(_ Object, arg Object) (Object, error) {
			calls++
			v, _ := arg.(*Int).Int64()
			return NewInt(-v), nil
		},
	}
	keyfn, err := NewCFunction(def, None(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	in := []int64{5, 1, 4, 2, 3}
	l := intsListFromInts(in)
	if err := l.Sort(keyfn, false); err != nil {
		t.Fatal(err)
	}
	if calls != len(in) {
		t.Errorf("keyfunc calls = %d, want %d", calls, len(in))
	}
	got := intsListToInts(t, l)
	want := []int64{5, 4, 3, 2, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key sorted got %v, want %v", got, want)
			return
		}
	}
}

func TestSortKeyfuncErrorPropagates(t *testing.T) {
	def := &MethodDef{
		Name:  "boom",
		Flags: MethO,
		O: func(_ Object, _ Object) (Object, error) {
			return nil, fmt.Errorf("boom")
		},
	}
	keyfn, _ := NewCFunction(def, None(), nil, nil)
	l := intsListFromInts([]int64{3, 1, 2})
	err := l.Sort(keyfn, false)
	if err == nil || err.Error() != "boom" {
		t.Errorf("expected boom from key, got %v", err)
	}
	// List must be restored to its original ordering even on error.
	got := intsListToInts(t, l)
	want := []int64{3, 1, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("after key error, list = %v, want %v", got, want)
			return
		}
	}
}

func TestSortEmptyAndSingle(t *testing.T) {
	l0 := NewList(nil)
	if err := l0.Sort(nil, false); err != nil {
		t.Fatal(err)
	}
	if l0.Len() != 0 {
		t.Errorf("empty sort should remain empty, got len=%d", l0.Len())
	}
	l1 := intsListFromInts([]int64{42})
	if err := l1.Sort(nil, true); err != nil {
		t.Fatal(err)
	}
	if v, _ := l1.Item(0).(*Int).Int64(); v != 42 {
		t.Errorf("single-element sort lost its element")
	}
}

func TestMergeComputeMinrunPin(t *testing.T) {
	// Pin the minrun curve against CPython's published table.
	// listsort.txt: n < 64 → n; powers of two: 32; else round to
	// closest-from-below to n/k power of two with k in [32,64].
	cases := []struct {
		n, want int
	}{
		{0, 0},
		{1, 1},
		{63, 63},
		{64, 32},
		{65, 33},
		{127, 64},
		{128, 32},
		{1000, 63},
		{1024, 32},
		{1025, 33},
	}
	for _, c := range cases {
		got := mergeComputeMinrun(c.n)
		if got != c.want {
			t.Errorf("mergeComputeMinrun(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestSortAllEqualKeys(t *testing.T) {
	// All-equal input is a worst case for any compare-based sort; here
	// it should produce zero swaps in an ideal Timsort and certainly
	// keep stable ordering.
	const N = 100
	items := make([]Object, N)
	for i := 0; i < N; i++ {
		items[i] = NewTuple([]Object{NewInt(7), NewInt(int64(i))})
	}
	l := NewList(items)
	def := &MethodDef{
		Name: "first", Flags: MethO,
		O: func(_ Object, arg Object) (Object, error) {
			return arg.(*Tuple).Item(0), nil
		},
	}
	keyfn, _ := NewCFunction(def, None(), nil, nil)
	if err := l.Sort(keyfn, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < N; i++ {
		tup := l.Item(i).(*Tuple)
		idx, _ := tup.Item(1).(*Int).Int64()
		if idx != int64(i) {
			t.Errorf("equal-key stability broken at %d: idx=%d", i, idx)
			return
		}
	}
}
