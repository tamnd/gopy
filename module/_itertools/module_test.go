// Tests for the itertools accelerator. Each iterator gets at least
// one positive case and one exhaustion case; constructors get
// argument-validation coverage where CPython rejects bad input.

package _itertools

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// callType invokes a type as a callable (TpNew dispatch).
func callType(t *testing.T, tp *objects.Type, args ...objects.Object) objects.Object {
	t.Helper()
	out, err := objects.Call(tp, objects.NewTuple(args), nil)
	if err != nil {
		t.Fatalf("call %s: %v", tp.Name, err)
	}
	return out
}

// drain pulls every value out of it via the IterNext slot, stopping at
// StopIteration or after cap items.
func drain(t *testing.T, it objects.Object, cap int) []objects.Object {
	t.Helper()
	out := []objects.Object{}
	for i := 0; i < cap; i++ {
		v, err := objects.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				return out
			}
			t.Fatalf("IterNext: %v", err)
		}
		out = append(out, v)
	}
	return out
}

// exhausted asserts that the iterator yields StopIteration on the
// next call.
func exhausted(t *testing.T, it objects.Object) {
	t.Helper()
	_, err := objects.IterNext(it)
	if !errors.Is(err, objects.ErrStopIteration) {
		t.Fatalf("expected StopIteration, got %v", err)
	}
}

func kwDict(t *testing.T, kv map[string]objects.Object) *objects.Dict {
	t.Helper()
	d := objects.NewDict()
	for k, v := range kv {
		if err := d.SetItem(objects.NewStr(k), v); err != nil {
			t.Fatalf("kw set %s: %v", k, err)
		}
	}
	return d
}

func intsTuple(xs ...int64) *objects.Tuple {
	out := make([]objects.Object, len(xs))
	for i, x := range xs {
		out[i] = objects.NewInt(x)
	}
	return objects.NewTuple(out)
}

func intsList(xs ...int64) *objects.List {
	out := make([]objects.Object, len(xs))
	for i, x := range xs {
		out[i] = objects.NewInt(x)
	}
	return objects.NewList(out)
}

func wantInts(t *testing.T, got []objects.Object, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, g := range got {
		x, ok := g.(*objects.Int)
		if !ok {
			t.Fatalf("got[%d] = %T, want *Int", i, g)
		}
		v, _ := x.Int64()
		if v != want[i] {
			t.Fatalf("got[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func wantTupleInts(t *testing.T, got objects.Object, want ...int64) {
	t.Helper()
	tup, ok := got.(*objects.Tuple)
	if !ok {
		t.Fatalf("want *Tuple, got %T", got)
	}
	if tup.Len() != len(want) {
		t.Fatalf("tuple len = %d, want %d", tup.Len(), len(want))
	}
	for i, w := range want {
		x, ok := tup.Item(i).(*objects.Int)
		if !ok {
			t.Fatalf("tuple[%d] = %T, want *Int", i, tup.Item(i))
		}
		v, _ := x.Int64()
		if v != w {
			t.Fatalf("tuple[%d] = %d, want %d", i, v, w)
		}
	}
}

func TestBuildModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	for _, name := range []string{
		"accumulate", "batched", "chain", "combinations",
		"combinations_with_replacement", "compress", "count", "cycle",
		"dropwhile", "filterfalse", "groupby", "_grouper", "islice",
		"pairwise", "permutations", "product", "repeat", "starmap",
		"takewhile", "_tee", "_tee_dataobject", "zip_longest", "tee",
	} {
		if _, err := m.Dict().GetItem(objects.NewStr(name)); err != nil {
			t.Errorf("module missing %q: %v", name, err)
		}
	}
}

func TestBatched(t *testing.T) {
	it := callType(t, BatchedType, intsList(1, 2, 3, 4, 5), objects.NewInt(2))
	got := drain(t, it, 100)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantTupleInts(t, got[0], 1, 2)
	wantTupleInts(t, got[1], 3, 4)
	wantTupleInts(t, got[2], 5)
	exhausted(t, it)
}

func TestBatchedZero(t *testing.T) {
	if _, err := objects.Call(BatchedType, objects.NewTuple([]objects.Object{intsList(1, 2), objects.NewInt(0)}), nil); err == nil {
		t.Fatalf("batched with n=0 should error")
	}
}

func TestPairwise(t *testing.T) {
	it := callType(t, PairwiseType, intsList(1, 2, 3, 4))
	got := drain(t, it, 100)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	wantTupleInts(t, got[0], 1, 2)
	wantTupleInts(t, got[1], 2, 3)
	wantTupleInts(t, got[2], 3, 4)
	exhausted(t, it)
}

func TestPairwiseShort(t *testing.T) {
	it := callType(t, PairwiseType, intsList(1))
	exhausted(t, it)
}

func TestGroupby(t *testing.T) {
	it := callType(t, GroupbyType, intsList(1, 1, 2, 2, 2, 3))
	wantKey := []int64{1, 2, 3}
	wantVals := [][]int64{{1, 1}, {2, 2, 2}, {3}}
	for i := 0; i < 3; i++ {
		g, err := objects.IterNext(it)
		if err != nil {
			t.Fatalf("group %d: %v", i, err)
		}
		tup := g.(*objects.Tuple)
		k, _ := tup.Item(0).(*objects.Int).Int64()
		if k != wantKey[i] {
			t.Fatalf("key[%d] = %d, want %d", i, k, wantKey[i])
		}
		vals := drain(t, tup.Item(1), 100)
		wantInts(t, vals, wantVals[i]...)
	}
	exhausted(t, it)
}

func TestCycle(t *testing.T) {
	it := callType(t, CycleType, intsList(1, 2, 3))
	got := drain(t, it, 7)
	wantInts(t, got, 1, 2, 3, 1, 2, 3, 1)
}

func TestCycleEmpty(t *testing.T) {
	it := callType(t, CycleType, intsList())
	exhausted(t, it)
}

func TestDropwhile(t *testing.T) {
	pred := objects.NewBuiltinFunction("lt3", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		v, _ := args[0].(*objects.Int).Int64()
		return objects.NewBool(v < 3), nil
	})
	it := callType(t, DropwhileType, pred, intsList(1, 2, 3, 4, 1, 2))
	wantInts(t, drain(t, it, 100), 3, 4, 1, 2)
	exhausted(t, it)
}

func TestTakewhile(t *testing.T) {
	pred := objects.NewBuiltinFunction("lt3", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		v, _ := args[0].(*objects.Int).Int64()
		return objects.NewBool(v < 3), nil
	})
	it := callType(t, TakewhileType, pred, intsList(1, 2, 3, 4, 1, 2))
	wantInts(t, drain(t, it, 100), 1, 2)
	exhausted(t, it)
}

func TestIsliceStop(t *testing.T) {
	it := callType(t, IsliceType, intsList(0, 1, 2, 3, 4, 5), objects.NewInt(3))
	wantInts(t, drain(t, it, 100), 0, 1, 2)
	exhausted(t, it)
}

func TestIsliceStartStopStep(t *testing.T) {
	it := callType(t, IsliceType, intsList(0, 1, 2, 3, 4, 5, 6, 7, 8, 9),
		objects.NewInt(1), objects.NewInt(8), objects.NewInt(2))
	wantInts(t, drain(t, it, 100), 1, 3, 5, 7)
	exhausted(t, it)
}

func TestStarmap(t *testing.T) {
	add := objects.NewBuiltinFunction("add", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a + b), nil
	})
	data := objects.NewList([]objects.Object{
		intsTuple(1, 2), intsTuple(3, 4), intsTuple(5, 6),
	})
	it := callType(t, StarmapType, add, data)
	wantInts(t, drain(t, it, 100), 3, 7, 11)
	exhausted(t, it)
}

func TestChain(t *testing.T) {
	it := callType(t, ChainType, intsList(1, 2), intsList(3), intsList(4, 5))
	wantInts(t, drain(t, it, 100), 1, 2, 3, 4, 5)
	exhausted(t, it)
}

func TestChainEmpty(t *testing.T) {
	it := callType(t, ChainType)
	exhausted(t, it)
}

func TestChainFromIterable(t *testing.T) {
	fromIter, err := objects.GetAttr(ChainType, objects.NewStr("from_iterable"))
	if err != nil {
		t.Fatalf("from_iterable lookup: %v", err)
	}
	outer := objects.NewList([]objects.Object{intsList(1, 2), intsList(3), intsList(4, 5)})
	it, err := objects.Call(fromIter, objects.NewTuple([]objects.Object{outer}), nil)
	if err != nil {
		t.Fatalf("from_iterable call: %v", err)
	}
	wantInts(t, drain(t, it, 100), 1, 2, 3, 4, 5)
}

func TestProduct(t *testing.T) {
	it := callType(t, ProductType, intsList(1, 2), intsList(3, 4))
	got := drain(t, it, 100)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	wantTupleInts(t, got[0], 1, 3)
	wantTupleInts(t, got[1], 1, 4)
	wantTupleInts(t, got[2], 2, 3)
	wantTupleInts(t, got[3], 2, 4)
	exhausted(t, it)
}

func TestProductRepeat(t *testing.T) {
	kw := map[string]objects.Object{"repeat": objects.NewInt(2)}
	it, err := objects.Call(ProductType, objects.NewTuple([]objects.Object{intsList(0, 1)}), kwDict(t, kw))
	if err != nil {
		t.Fatalf("product call: %v", err)
	}
	got := drain(t, it, 100)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestCombinations(t *testing.T) {
	it := callType(t, CombinationsType, intsList(1, 2, 3, 4), objects.NewInt(2))
	got := drain(t, it, 100)
	if len(got) != 6 {
		t.Fatalf("len = %d", len(got))
	}
	wantTupleInts(t, got[0], 1, 2)
	wantTupleInts(t, got[5], 3, 4)
	exhausted(t, it)
}

func TestCombinationsWithReplacement(t *testing.T) {
	it := callType(t, CombinationsWithReplacementType, intsList(1, 2, 3), objects.NewInt(2))
	got := drain(t, it, 100)
	if len(got) != 6 {
		t.Fatalf("len = %d", len(got))
	}
	wantTupleInts(t, got[0], 1, 1)
	wantTupleInts(t, got[5], 3, 3)
	exhausted(t, it)
}

func TestPermutations(t *testing.T) {
	it := callType(t, PermutationsType, intsList(1, 2, 3))
	got := drain(t, it, 100)
	if len(got) != 6 {
		t.Fatalf("len = %d", len(got))
	}
	wantTupleInts(t, got[0], 1, 2, 3)
	wantTupleInts(t, got[5], 3, 2, 1)
	exhausted(t, it)
}

func TestPermutationsR(t *testing.T) {
	it := callType(t, PermutationsType, intsList(1, 2, 3), objects.NewInt(2))
	got := drain(t, it, 100)
	if len(got) != 6 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestAccumulate(t *testing.T) {
	it := callType(t, AccumulateType, intsList(1, 2, 3, 4))
	wantInts(t, drain(t, it, 100), 1, 3, 6, 10)
	exhausted(t, it)
}

func TestAccumulateInitial(t *testing.T) {
	kw := map[string]objects.Object{"initial": objects.NewInt(100)}
	it, err := objects.Call(AccumulateType, objects.NewTuple([]objects.Object{intsList(1, 2, 3)}), kwDict(t, kw))
	if err != nil {
		t.Fatalf("accumulate call: %v", err)
	}
	wantInts(t, drain(t, it, 100), 100, 101, 103, 106)
	exhausted(t, it)
}

func TestCompress(t *testing.T) {
	it := callType(t, CompressType,
		intsList(1, 2, 3, 4, 5),
		objects.NewList([]objects.Object{
			objects.NewBool(true), objects.NewBool(false),
			objects.NewBool(true), objects.NewBool(false),
			objects.NewBool(true),
		}))
	wantInts(t, drain(t, it, 100), 1, 3, 5)
	exhausted(t, it)
}

func TestFilterfalse(t *testing.T) {
	pred := objects.NewBuiltinFunction("even", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		v, _ := args[0].(*objects.Int).Int64()
		return objects.NewBool(v%2 == 0), nil
	})
	it := callType(t, FilterfalseType, pred, intsList(1, 2, 3, 4, 5))
	wantInts(t, drain(t, it, 100), 1, 3, 5)
	exhausted(t, it)
}

func TestCount(t *testing.T) {
	it := callType(t, CountType)
	wantInts(t, drain(t, it, 5), 0, 1, 2, 3, 4)
}

func TestCountStartStep(t *testing.T) {
	it := callType(t, CountType, objects.NewInt(10), objects.NewInt(3))
	wantInts(t, drain(t, it, 4), 10, 13, 16, 19)
}

func TestRepeat(t *testing.T) {
	it := callType(t, RepeatType, objects.NewInt(7), objects.NewInt(3))
	wantInts(t, drain(t, it, 100), 7, 7, 7)
	exhausted(t, it)
}

func TestRepeatInfinite(t *testing.T) {
	it := callType(t, RepeatType, objects.NewInt(9))
	wantInts(t, drain(t, it, 4), 9, 9, 9, 9)
}

func TestZipLongest(t *testing.T) {
	kw := map[string]objects.Object{"fillvalue": objects.NewInt(0)}
	it, err := objects.Call(ZipLongestType,
		objects.NewTuple([]objects.Object{intsList(1, 2, 3), intsList(10, 20)}),
		kwDict(t, kw))
	if err != nil {
		t.Fatalf("zip_longest call: %v", err)
	}
	got := drain(t, it, 100)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	wantTupleInts(t, got[0], 1, 10)
	wantTupleInts(t, got[1], 2, 20)
	wantTupleInts(t, got[2], 3, 0)
	exhausted(t, it)
}

func TestZipLongestEmpty(t *testing.T) {
	it := callType(t, ZipLongestType)
	exhausted(t, it)
}

func TestTee(t *testing.T) {
	mod, _ := buildModule()
	teeFn, _ := mod.Dict().GetItem(objects.NewStr("tee"))
	out, err := objects.Call(teeFn, objects.NewTuple([]objects.Object{intsList(1, 2, 3)}), nil)
	if err != nil {
		t.Fatalf("tee call: %v", err)
	}
	tup, ok := out.(*objects.Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("tee() = %T len=%d", out, tup.Len())
	}
	wantInts(t, drain(t, tup.Item(0), 100), 1, 2, 3)
	wantInts(t, drain(t, tup.Item(1), 100), 1, 2, 3)
}

func TestTeeN(t *testing.T) {
	mod, _ := buildModule()
	teeFn, _ := mod.Dict().GetItem(objects.NewStr("tee"))
	out, err := objects.Call(teeFn, objects.NewTuple([]objects.Object{intsList(1, 2), objects.NewInt(3)}), nil)
	if err != nil {
		t.Fatalf("tee call: %v", err)
	}
	tup := out.(*objects.Tuple)
	if tup.Len() != 3 {
		t.Fatalf("tee n=3 returned %d", tup.Len())
	}
	for i := 0; i < 3; i++ {
		wantInts(t, drain(t, tup.Item(i), 100), 1, 2)
	}
}

func TestCountRepr(t *testing.T) {
	c := callType(t, CountType, objects.NewInt(2), objects.NewInt(3))
	s, err := objects.Repr(c)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if s != "count(2, 3)" {
		t.Fatalf("repr = %q", s)
	}
}

func TestRepeatRepr(t *testing.T) {
	r := callType(t, RepeatType, objects.NewInt(5), objects.NewInt(2))
	s, err := objects.Repr(r)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if s != "repeat(5, 2)" {
		t.Fatalf("repr = %q", s)
	}
}

func TestRepeatLengthHint(t *testing.T) {
	r := callType(t, RepeatType, objects.NewInt(5), objects.NewInt(4))
	lh, err := objects.GetAttr(r, objects.NewStr("__length_hint__"))
	if err != nil {
		t.Fatalf("getattr __length_hint__: %v", err)
	}
	out, err := objects.Call(lh, objects.NewTuple(nil), nil)
	if err != nil {
		t.Fatalf("call __length_hint__: %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 4 {
		t.Fatalf("length_hint = %d, want 4", v)
	}
}
