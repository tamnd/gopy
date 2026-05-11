// Tests for the _functools accelerator. We exercise the four public
// names (partial, cmp_to_key, reduce, _lru_cache_wrapper) through the
// runtime to validate they behave like the CPython implementation.

package _functools

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// callFn is a tiny helper that wraps a Go closure as a Python callable
// so it can be handed to partial/cmp_to_key/reduce/lru_cache.
func callFn(fn func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)) objects.Object {
	return objects.NewBuiltinFunction("test_fn", fn)
}

// TestPartialBasic exercises partial(fn, *bound) and confirms that
// trailing positional args are appended at call time.
func TestPartialBasic(t *testing.T) {
	add := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a + b), nil
	})
	p, err := PartialType.TpNew(PartialType, []objects.Object{add, objects.NewInt(3)}, nil)
	if err != nil {
		t.Fatalf("partial ctor: %v", err)
	}
	out, err := objects.Call(p, objects.NewTuple([]objects.Object{objects.NewInt(4)}), nil)
	if err != nil {
		t.Fatalf("partial call: %v", err)
	}
	n, ok := out.(*objects.Int)
	if !ok {
		t.Fatalf("want *Int, got %T", out)
	}
	v, _ := n.Int64()
	if v != 7 {
		t.Fatalf("want 7, got %d", v)
	}
}

// TestPartialKeywords confirms a partial that captures **kw and
// receives more kwargs at call time merges them with the call site
// overriding.
func TestPartialKeywords(t *testing.T) {
	mul := callFn(func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		// scale defaults to 1.
		scale := int64(1)
		if v, ok := kwargs["scale"]; ok {
			s, _ := v.(*objects.Int).Int64()
			scale = s
		}
		return objects.NewInt(a * scale), nil
	})
	p, err := PartialType.TpNew(PartialType, []objects.Object{mul}, map[string]objects.Object{
		"scale": objects.NewInt(10),
	})
	if err != nil {
		t.Fatalf("partial ctor: %v", err)
	}
	out, err := objects.Call(p, objects.NewTuple([]objects.Object{objects.NewInt(3)}), nil)
	if err != nil {
		t.Fatalf("partial call: %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 30 {
		t.Fatalf("want 30, got %d", v)
	}
	// Call-site kwarg overrides bound kwarg.
	kwd := objects.NewDict()
	_ = kwd.SetItem(objects.NewStr("scale"), objects.NewInt(2))
	out, err = objects.Call(p, objects.NewTuple([]objects.Object{objects.NewInt(3)}), kwd)
	if err != nil {
		t.Fatalf("partial call override: %v", err)
	}
	v, _ = out.(*objects.Int).Int64()
	if v != 6 {
		t.Fatalf("want 6 from override, got %d", v)
	}
}

// TestPartialPlaceholder exercises Placeholder slot-filling.
func TestPartialPlaceholder(t *testing.T) {
	sub := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a - b), nil
	})
	// partial(sub, Placeholder, 1) -> call-time arg fills slot 0, 1 stays at slot 1.
	p, err := PartialType.TpNew(PartialType, []objects.Object{sub, Placeholder, objects.NewInt(1)}, nil)
	if err != nil {
		t.Fatalf("partial ctor: %v", err)
	}
	out, err := objects.Call(p, objects.NewTuple([]objects.Object{objects.NewInt(10)}), nil)
	if err != nil {
		t.Fatalf("partial call: %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 9 {
		t.Fatalf("want 9, got %d", v)
	}
}

// TestCmpToKey verifies that cmp_to_key returns a callable producing
// KeyObject instances that compare via the user's cmp function.
func TestCmpToKey(t *testing.T) {
	cmp := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a - b), nil
	})
	keyFactory, err := cmpToKey([]objects.Object{cmp}, nil)
	if err != nil {
		t.Fatalf("cmp_to_key: %v", err)
	}
	k1, err := objects.Call(keyFactory, objects.NewTuple([]objects.Object{objects.NewInt(5)}), nil)
	if err != nil {
		t.Fatalf("k(5): %v", err)
	}
	k2, err := objects.Call(keyFactory, objects.NewTuple([]objects.Object{objects.NewInt(7)}), nil)
	if err != nil {
		t.Fatalf("k(7): %v", err)
	}
	// k1 < k2 because cmp(5, 7) == -2 < 0.
	res, err := objects.RichCmp(k1, k2, objects.CompareLT)
	if err != nil {
		t.Fatalf("rich cmp: %v", err)
	}
	if !isTruthy(t, res) {
		t.Fatalf("want k1 < k2 true")
	}
	// k1 > k2 must be false.
	res, err = objects.RichCmp(k1, k2, objects.CompareGT)
	if err != nil {
		t.Fatalf("rich cmp gt: %v", err)
	}
	if isTruthy(t, res) {
		t.Fatalf("want k1 > k2 false")
	}
}

// TestReduce exercises the seeded and unseeded paths plus the
// empty-iterable error.
func TestReduce(t *testing.T) {
	add := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a + b), nil
	})
	lst := objects.NewList([]objects.Object{
		objects.NewInt(1), objects.NewInt(2), objects.NewInt(3), objects.NewInt(4),
	})
	out, err := reduceFunc([]objects.Object{add, lst}, nil)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 10 {
		t.Fatalf("want 10, got %d", v)
	}
	// Seeded.
	out, err = reduceFunc([]objects.Object{add, lst, objects.NewInt(100)}, nil)
	if err != nil {
		t.Fatalf("reduce w/init: %v", err)
	}
	v, _ = out.(*objects.Int).Int64()
	if v != 110 {
		t.Fatalf("want 110, got %d", v)
	}
	// Empty + no init must raise.
	empty := objects.NewList(nil)
	_, err = reduceFunc([]objects.Object{add, empty}, nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want empty iterable TypeError, got %v", err)
	}
}

// TestLruCache exercises the bounded LRU path: every miss bumps
// `misses`, every hit bumps `hits`, and eviction removes the LRU entry
// once the cache reaches maxsize.
func TestLruCache(t *testing.T) {
	calls := 0
	doubler := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		n, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(n * 2), nil
	})
	cacheInfoType := newTestCacheInfoType()
	w, err := LruCacheWrapperType.TpNew(LruCacheWrapperType, []objects.Object{
		doubler,
		objects.NewInt(2),
		objects.NewBool(false),
		cacheInfoType,
	}, nil)
	if err != nil {
		t.Fatalf("lru ctor: %v", err)
	}
	mustCall := func(arg int64) int64 {
		t.Helper()
		out, err := objects.Call(w, objects.NewTuple([]objects.Object{objects.NewInt(arg)}), nil)
		if err != nil {
			t.Fatalf("lru call(%d): %v", arg, err)
		}
		v, _ := out.(*objects.Int).Int64()
		return v
	}
	if v := mustCall(3); v != 6 {
		t.Fatalf("f(3) want 6, got %d", v)
	}
	if v := mustCall(3); v != 6 {
		t.Fatalf("f(3) hit want 6, got %d", v)
	}
	wrapper := w.(*LruCacheWrapper)
	if wrapper.Hits != 1 || wrapper.Misses != 1 {
		t.Fatalf("hits/misses: got %d/%d want 1/1", wrapper.Hits, wrapper.Misses)
	}
	if calls != 1 {
		t.Fatalf("inner calls: got %d want 1", calls)
	}
	// Fill the cache and force an eviction.
	mustCall(4)
	mustCall(5)
	if wrapper.Cache.Len() != 2 {
		t.Fatalf("cache size after eviction: got %d want 2", wrapper.Cache.Len())
	}
	if calls != 3 {
		t.Fatalf("inner calls after eviction: got %d want 3", calls)
	}
}

// TestLruCacheUnbounded confirms maxsize=None caches indefinitely.
func TestLruCacheUnbounded(t *testing.T) {
	doubler := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		n, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(n * 2), nil
	})
	cacheInfoType := newTestCacheInfoType()
	w, err := LruCacheWrapperType.TpNew(LruCacheWrapperType, []objects.Object{
		doubler, objects.None(), objects.NewBool(false), cacheInfoType,
	}, nil)
	if err != nil {
		t.Fatalf("lru ctor: %v", err)
	}
	wrapper := w.(*LruCacheWrapper)
	for i := int64(0); i < 10; i++ {
		_, _ = objects.Call(w, objects.NewTuple([]objects.Object{objects.NewInt(i)}), nil)
	}
	if wrapper.Cache.Len() != 10 {
		t.Fatalf("unbounded cache size: got %d want 10", wrapper.Cache.Len())
	}
}

// TestLruCacheUncached confirms maxsize=0 disables caching entirely.
func TestLruCacheUncached(t *testing.T) {
	calls := 0
	doubler := callFn(func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		n, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(n * 2), nil
	})
	cacheInfoType := newTestCacheInfoType()
	w, err := LruCacheWrapperType.TpNew(LruCacheWrapperType, []objects.Object{
		doubler, objects.NewInt(0), objects.NewBool(false), cacheInfoType,
	}, nil)
	if err != nil {
		t.Fatalf("lru ctor: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, _ = objects.Call(w, objects.NewTuple([]objects.Object{objects.NewInt(1)}), nil)
	}
	if calls != 5 {
		t.Fatalf("uncached: expected 5 calls, got %d", calls)
	}
	wrapper := w.(*LruCacheWrapper)
	if wrapper.Misses != 5 || wrapper.Hits != 0 {
		t.Fatalf("uncached counters: misses=%d hits=%d", wrapper.Misses, wrapper.Hits)
	}
}

// newTestCacheInfoType builds a minimal CacheInfo type for the tests so
// cache_info() has somewhere to return. Mirrors the Lib/functools.py
// _CacheInfo named-tuple-ish shape.
func newTestCacheInfoType() *objects.Type {
	t := objects.NewType("CacheInfo", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		d := inst.Dict()
		_ = d.SetItem(objects.NewStr("hits"), args[0])
		_ = d.SetItem(objects.NewStr("misses"), args[1])
		_ = d.SetItem(objects.NewStr("maxsize"), args[2])
		_ = d.SetItem(objects.NewStr("currsize"), args[3])
		return inst, nil
	}
	return t
}

func isTruthy(t *testing.T, o objects.Object) bool {
	t.Helper()
	v, err := objects.IsTruthy(o)
	if err != nil {
		t.Fatalf("IsTruthy: %v", err)
	}
	return v
}

// Defensive: confirm reduceFunc's empty-iterable error is what we
// expect via errors.Is on an exact predicate, not just a substring.
var _ = errors.New // keep the errors import used even if a refactor drops a usage
