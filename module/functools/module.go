// functools module: minimal Go-backed surface. Lib/functools.py is
// 1100 lines (lru_cache, singledispatch, partial, reduce, the wraps
// decorator family); this stub covers the names unittest reaches for
// at module load (`wraps` decorator, `partial`, `reduce`) so the
// imports resolve. Real ports follow when the call sites need them.
//
// CPython: Lib/functools.py the public surface

package functools

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("functools", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("functools")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"wraps", objects.NewBuiltinFunction("wraps", wraps)},
		{"update_wrapper", objects.NewBuiltinFunction("update_wrapper", updateWrapper)},
		{"partial", partialType},
		{"reduce", objects.NewBuiltinFunction("reduce", reduce)},
		{"lru_cache", objects.NewBuiltinFunction("lru_cache", lruCache)},
		{"cache", objects.NewBuiltinFunction("cache", lruCache)},
		{"cached_property", cachedPropertyType},
		{"total_ordering", objects.NewBuiltinFunction("total_ordering", identityDecorator)},
		{"singledispatch", objects.NewBuiltinFunction("singledispatch", identityDecorator)},
		{"WRAPPER_ASSIGNMENTS", objects.NewTuple(nil)},
		{"WRAPPER_UPDATES", objects.NewTuple(nil)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// wraps returns a decorator that returns its argument unchanged. The
// real Lib/functools.wraps copies __module__/__name__/__qualname__ etc
// from the wrapped function; the simplification keeps decorated names
// callable, which is what import-time code paths require.
//
// CPython: Lib/functools.py:65 wraps
func wraps(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBuiltinFunction("decorator", identityDecorator), nil
}

func identityDecorator(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}

// updateWrapper ports functools.update_wrapper as a no-op pass-through.
//
// CPython: Lib/functools.py:36 update_wrapper
func updateWrapper(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}

// reduce ports functools.reduce: fold f over iterable.
//
// CPython: Lib/functools.py:204 reduce
func reduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: reduce() takes at least 2 arguments")
	}
	fn := args[0]
	tp := args[1].Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: reduce() arg 2 must be iterable")
	}
	it, err := tp.Iter(args[1])
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	var acc objects.Object
	if len(args) >= 3 {
		acc = args[2]
	} else {
		v, ierr := itType.IterNext(it)
		if ierr != nil || v == nil {
			return nil, fmt.Errorf("TypeError: reduce() of empty sequence with no initial value")
		}
		acc = v
	}
	for {
		v, ierr := itType.IterNext(it)
		if ierr != nil {
			if errors.Is(ierr, objects.ErrStopIteration) {
				break
			}
			return nil, ierr
		}
		if v == nil {
			break
		}
		next, err := objects.Call(fn, objects.NewTuple([]objects.Object{acc, v}), nil)
		if err != nil {
			return nil, err
		}
		acc = next
	}
	return acc, nil
}

// lruCache returns a decorator that returns its argument. Without
// caching it still satisfies @lru_cache uses for import-time code.
//
// CPython: Lib/functools.py:546 lru_cache
func lruCache(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBuiltinFunction("decorator", identityDecorator), nil
}

// partialType is a minimal functools.partial: callable that captures
// args and kwargs, prepending them on each invocation.
//
// CPython: Lib/functools.py:282 partial
var partialType = newPartialType()

func newPartialType() *objects.Type {
	t := objects.NewType("partial", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: partial() requires at least one positional argument")
		}
		fn := args[0]
		bound := append([]objects.Object{}, args[1:]...)
		boundKwargs := kwargs
		inst := objects.NewInstance(cls)
		_ = inst.Dict().SetItem(objects.NewStr("func"), fn)
		_ = inst.Dict().SetItem(objects.NewStr("args"), objects.NewTuple(bound))
		_ = inst.Dict().SetItem(objects.NewStr("keywords"), objects.NewDict())
		t.Call = func(o objects.Object, callArgs []objects.Object, callKwargs map[string]objects.Object) (objects.Object, error) {
			full := append(append([]objects.Object{}, bound...), callArgs...)
			merged := map[string]objects.Object{}
			for k, v := range boundKwargs {
				merged[k] = v
			}
			for k, v := range callKwargs {
				merged[k] = v
			}
			var kwd *objects.Dict
			if len(merged) > 0 {
				kwd = objects.NewDict()
				for k, v := range merged {
					if err := kwd.SetItem(objects.NewStr(k), v); err != nil {
						return nil, err
					}
				}
			}
			_ = o
			return objects.Call(fn, objects.NewTuple(full), kwd)
		}
		return inst, nil
	}
	return t
}

// cachedPropertyType is a stand-in for functools.cached_property used
// as a decorator. Returns the underlying function unchanged.
//
// CPython: Lib/functools.py:892 cached_property
var cachedPropertyType = newCachedPropertyType()

func newCachedPropertyType() *objects.Type {
	t := objects.NewType("cached_property", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		_ = cls
		if len(args) >= 1 {
			return args[0], nil
		}
		return objects.None(), nil
	}
	return t
}
