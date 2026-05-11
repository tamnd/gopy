// Tests for the functools module. Exercises the Python-facing surface
// end-to-end: import the module, pull the public names, call them, and
// check the results. This also indirectly covers module/_functools
// (the C-accelerator port) because Go's ./... wildcard skips directories
// with a leading underscore so the _functools tests would otherwise not
// run on CI.

package functools

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// importFunctools materializes the functools module the same way
// `import functools` would at runtime: pull the inittab init function
// and call it once.
func importFunctools(t *testing.T) *objects.Module {
	t.Helper()
	initFn := imp.FindInitFunc("functools")
	if initFn == nil {
		t.Fatal("functools not in inittab")
	}
	m, err := initFn()
	if err != nil {
		t.Fatalf("functools init: %v", err)
	}
	return m
}

func getAttr(t *testing.T, m *objects.Module, name string) objects.Object {
	t.Helper()
	v, err := m.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("functools.%s missing: %v", name, err)
	}
	return v
}

// TestModuleSurface confirms the public names exist after import.
func TestModuleSurface(t *testing.T) {
	m := importFunctools(t)
	for _, name := range []string{
		"partial", "reduce", "cmp_to_key", "lru_cache", "cache",
		"wraps", "update_wrapper", "WRAPPER_ASSIGNMENTS", "WRAPPER_UPDATES",
		"total_ordering", "singledispatch", "cached_property", "partialmethod",
		"_lru_cache_wrapper", "_CacheInfo", "Placeholder",
	} {
		if _, err := m.Dict().GetItem(objects.NewStr(name)); err != nil {
			t.Errorf("functools.%s missing: %v", name, err)
		}
	}
}

// TestReduce calls functools.reduce(add, [1,2,3,4]) and expects 10.
func TestReduce(t *testing.T) {
	m := importFunctools(t)
	reduce := getAttr(t, m, "reduce")
	add := objects.NewBuiltinFunction("add", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a + b), nil
	})
	lst := objects.NewList([]objects.Object{
		objects.NewInt(1), objects.NewInt(2), objects.NewInt(3), objects.NewInt(4),
	})
	out, err := objects.Call(reduce, objects.NewTuple([]objects.Object{add, lst}), nil)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 10 {
		t.Fatalf("reduce sum: got %d want 10", v)
	}
}

// TestPartial calls functools.partial(fn, 3)(4).
func TestPartial(t *testing.T) {
	m := importFunctools(t)
	partial := getAttr(t, m, "partial")
	add := objects.NewBuiltinFunction("add", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a + b), nil
	})
	p, err := objects.Call(partial, objects.NewTuple([]objects.Object{add, objects.NewInt(3)}), nil)
	if err != nil {
		t.Fatalf("partial: %v", err)
	}
	out, err := objects.Call(p, objects.NewTuple([]objects.Object{objects.NewInt(4)}), nil)
	if err != nil {
		t.Fatalf("partial call: %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 7 {
		t.Fatalf("partial(add, 3)(4): got %d want 7", v)
	}
}

// TestCmpToKey routes a small list through cmp_to_key and checks the
// comparisons fire.
func TestCmpToKey(t *testing.T) {
	m := importFunctools(t)
	cmpToKeyFn := getAttr(t, m, "cmp_to_key")
	cmp := objects.NewBuiltinFunction("cmp", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		a, _ := args[0].(*objects.Int).Int64()
		b, _ := args[1].(*objects.Int).Int64()
		return objects.NewInt(a - b), nil
	})
	factory, err := objects.Call(cmpToKeyFn, objects.NewTuple([]objects.Object{cmp}), nil)
	if err != nil {
		t.Fatalf("cmp_to_key: %v", err)
	}
	k1, _ := objects.Call(factory, objects.NewTuple([]objects.Object{objects.NewInt(5)}), nil)
	k2, _ := objects.Call(factory, objects.NewTuple([]objects.Object{objects.NewInt(7)}), nil)
	lt, err := objects.RichCmp(k1, k2, objects.CompareLT)
	if err != nil {
		t.Fatalf("rich cmp: %v", err)
	}
	truth, _ := objects.IsTruthy(lt)
	if !truth {
		t.Fatalf("want k1 < k2 true")
	}
}

// TestLruCacheDecorator exercises lru_cache(maxsize=2) over a counted
// function: same arg twice should call the inner once, and the second
// call must register as a hit.
func TestLruCacheDecorator(t *testing.T) {
	m := importFunctools(t)
	lruCache := getAttr(t, m, "lru_cache")
	calls := 0
	doubler := objects.NewBuiltinFunction("doubler", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		n, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(n * 2), nil
	})
	// lru_cache(maxsize=2) returns a decorator that wraps doubler.
	kwd := objects.NewDict()
	_ = kwd.SetItem(objects.NewStr("maxsize"), objects.NewInt(2))
	decorator, err := objects.Call(lruCache, objects.NewTuple(nil), kwd)
	if err != nil {
		t.Fatalf("lru_cache(maxsize=2): %v", err)
	}
	wrapper, err := objects.Call(decorator, objects.NewTuple([]objects.Object{doubler}), nil)
	if err != nil {
		t.Fatalf("decorator(doubler): %v", err)
	}
	for i := 0; i < 3; i++ {
		_, err := objects.Call(wrapper, objects.NewTuple([]objects.Object{objects.NewInt(3)}), nil)
		if err != nil {
			t.Fatalf("wrapper(3) call #%d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("inner calls: got %d want 1", calls)
	}
	// cache_info() should report 2 hits and 1 miss.
	infoFn, err := objects.GetAttr(wrapper, objects.NewStr("cache_info"))
	if err != nil {
		t.Fatalf("cache_info attr: %v", err)
	}
	info, err := objects.Call(infoFn, objects.NewTuple(nil), nil)
	if err != nil {
		t.Fatalf("cache_info(): %v", err)
	}
	repr, _ := objects.Repr(info)
	if !strings.Contains(repr, "hits=2") || !strings.Contains(repr, "misses=1") {
		t.Fatalf("cache_info repr unexpected: %s", repr)
	}
}

// TestLruCacheNoArgs confirms lru_cache(user_function) (the bare form
// without parentheses) returns a wrapped callable directly.
func TestLruCacheNoArgs(t *testing.T) {
	m := importFunctools(t)
	lruCache := getAttr(t, m, "lru_cache")
	calls := 0
	tripler := objects.NewBuiltinFunction("tripler", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		n, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(n * 3), nil
	})
	wrapper, err := objects.Call(lruCache, objects.NewTuple([]objects.Object{tripler}), nil)
	if err != nil {
		t.Fatalf("lru_cache(tripler): %v", err)
	}
	out1, _ := objects.Call(wrapper, objects.NewTuple([]objects.Object{objects.NewInt(4)}), nil)
	out2, _ := objects.Call(wrapper, objects.NewTuple([]objects.Object{objects.NewInt(4)}), nil)
	v1, _ := out1.(*objects.Int).Int64()
	v2, _ := out2.(*objects.Int).Int64()
	if v1 != 12 || v2 != 12 {
		t.Fatalf("tripler outputs: got %d, %d want 12, 12", v1, v2)
	}
	if calls != 1 {
		t.Fatalf("inner called %d times; expected hit on second call", calls)
	}
}

// TestCache calls functools.cache(fn) (alias for lru_cache(maxsize=None)).
func TestCache(t *testing.T) {
	m := importFunctools(t)
	cache := getAttr(t, m, "cache")
	fn := objects.NewBuiltinFunction("fn", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		n, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(n + 1), nil
	})
	wrapper, err := objects.Call(cache, objects.NewTuple([]objects.Object{fn}), nil)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	out, _ := objects.Call(wrapper, objects.NewTuple([]objects.Object{objects.NewInt(5)}), nil)
	v, _ := out.(*objects.Int).Int64()
	if v != 6 {
		t.Fatalf("cache(fn)(5): got %d want 6", v)
	}
}
