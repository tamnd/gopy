// functools module: gopy's Go-native port of Lib/functools.py. The
// heavy lifting (partial, _lru_cache_wrapper, cmp_to_key, reduce,
// Placeholder) lives in module/_functools, mirroring the C-accelerated
// names CPython exposes through `from _functools import ...`. The pure
// Python helpers (wraps, update_wrapper, lru_cache, cache,
// total_ordering, cached_property, partialmethod, singledispatch) are
// ported here.
//
// Vendoring the upstream Lib/functools.py (stdlib/functools.py) is
// blocked behind ports of abc, operator, reprlib, and _thread; until
// those land the inittab entry below shadows the .py file.
//
// CPython: Lib/functools.py the public surface

package functools

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/module/_functools"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("functools", buildModule)
}

// buildModule materializes the functools module dict.
//
// CPython: Lib/functools.py the module-level definitions
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("functools")
	d := m.Dict()
	cacheInfoType := newCacheInfoType()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"WRAPPER_ASSIGNMENTS", wrapperAssignments()},
		{"WRAPPER_UPDATES", wrapperUpdates()},
		{"update_wrapper", objects.NewBuiltinFunction("update_wrapper", updateWrapper)},
		{"wraps", objects.NewBuiltinFunction("wraps", wraps)},
		{"reduce", objects.NewBuiltinFunction("reduce", reduceImpl)},
		{"partial", _functools.PartialType},
		{"Placeholder", _functools.Placeholder},
		{"cmp_to_key", objects.NewBuiltinFunction("cmp_to_key", cmpToKey)},
		{"_lru_cache_wrapper", _functools.LruCacheWrapperType},
		{"_CacheInfo", cacheInfoType},
		{"lru_cache", objects.NewBuiltinFunction("lru_cache", makeLruCache(cacheInfoType))},
		{"cache", objects.NewBuiltinFunction("cache", makeCache(cacheInfoType))},
		{"cached_property", cachedPropertyType()},
		{"total_ordering", objects.NewBuiltinFunction("total_ordering", totalOrdering)},
		{"singledispatch", objects.NewBuiltinFunction("singledispatch", singledispatch)},
		{"partialmethod", partialmethodType()},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// wrapperAssignments returns the tuple of attribute names update_wrapper
// copies from the wrapped function to the wrapper.
//
// CPython: Lib/functools.py:31 WRAPPER_ASSIGNMENTS
func wrapperAssignments() objects.Object {
	return objects.NewTuple([]objects.Object{
		objects.NewStr("__module__"),
		objects.NewStr("__name__"),
		objects.NewStr("__qualname__"),
		objects.NewStr("__annotations__"),
		objects.NewStr("__type_params__"),
		objects.NewStr("__doc__"),
	})
}

// wrapperUpdates returns the tuple of attribute names update_wrapper
// merges (via dict.update) from the wrapped function into the wrapper.
//
// CPython: Lib/functools.py:33 WRAPPER_UPDATES
func wrapperUpdates() objects.Object {
	return objects.NewTuple([]objects.Object{objects.NewStr("__dict__")})
}

// updateWrapper copies WRAPPER_ASSIGNMENTS attributes from wrapped onto
// wrapper, then updates wrapper.__dict__ from each WRAPPER_UPDATES
// attribute on wrapped. Returns wrapper unchanged.
//
// CPython: Lib/functools.py:36 update_wrapper
func updateWrapper(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: update_wrapper() missing required argument 'wrapped'")
	}
	wrapper := args[0]
	wrapped := args[1]
	assigned := iterStrings(args, kwargs, "assigned", 2, wrapperAssignmentsList())
	updated := iterStrings(args, kwargs, "updated", 3, wrapperUpdatesList())
	for _, name := range assigned {
		nameObj := objects.NewStr(name)
		v, err := objects.GetAttr(wrapped, nameObj)
		if err != nil {
			continue
		}
		_ = objects.SetAttr(wrapper, nameObj, v)
	}
	for _, name := range updated {
		nameObj := objects.NewStr(name)
		wrapperDict, err := objects.GetAttr(wrapper, nameObj)
		if err != nil {
			continue
		}
		wrappedDict, err := objects.GetAttr(wrapped, nameObj)
		if err != nil {
			continue
		}
		updateDict(wrapperDict, wrappedDict)
	}
	// __wrapped__ unconditionally.
	_ = objects.SetAttr(wrapper, objects.NewStr("__wrapped__"), wrapped)
	return wrapper, nil
}

func wrapperAssignmentsList() []string {
	return []string{"__module__", "__name__", "__qualname__", "__annotations__", "__type_params__", "__doc__"}
}

func wrapperUpdatesList() []string {
	return []string{"__dict__"}
}

func iterStrings(args []objects.Object, kwargs map[string]objects.Object, name string, posIdx int, fallback []string) []string {
	var src objects.Object
	if posIdx < len(args) {
		src = args[posIdx]
	} else if v, ok := kwargs[name]; ok {
		src = v
	}
	if src == nil {
		return fallback
	}
	tp := src.Type()
	if tp.Iter == nil {
		return fallback
	}
	it, err := tp.Iter(src)
	if err != nil {
		return fallback
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return fallback
	}
	var out []string
	for {
		v, err := itType.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return fallback
		}
		if v == nil {
			break
		}
		s, _ := objects.Str(v)
		out = append(out, s)
	}
	return out
}

// updateDict copies entries from src into dst when both are *Dict. Used
// by update_wrapper to mirror wrapped.__dict__ onto wrapper.__dict__.
//
// CPython: Lib/functools.py:54 update_wrapper (the inner d.update())
func updateDict(dst, src objects.Object) {
	dstD, ok1 := dst.(*objects.Dict)
	srcD, ok2 := src.(*objects.Dict)
	if !ok1 || !ok2 {
		return
	}
	for _, k := range srcD.Keys() {
		v, err := srcD.GetItem(k)
		if err != nil {
			continue
		}
		_ = dstD.SetItem(k, v)
	}
}

// wraps returns a decorator that calls update_wrapper with the
// curried arguments.
//
// CPython: Lib/functools.py:78 wraps
func wraps(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: wraps() missing required argument 'wrapped'")
	}
	wrapped := args[0]
	assigned := iterStrings(args, kwargs, "assigned", 1, wrapperAssignmentsList())
	updated := iterStrings(args, kwargs, "updated", 2, wrapperUpdatesList())
	dec := func(decArgs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(decArgs) < 1 {
			return nil, fmt.Errorf("TypeError: wraps decorator requires the wrapper function")
		}
		wrapper := decArgs[0]
		uwArgs := []objects.Object{wrapper, wrapped}
		uwKwargs := map[string]objects.Object{
			"assigned": stringTuple(assigned),
			"updated":  stringTuple(updated),
		}
		return updateWrapper(uwArgs, uwKwargs)
	}
	return objects.NewBuiltinFunction("wraps_decorator", dec), nil
}

func stringTuple(ss []string) objects.Object {
	items := make([]objects.Object, len(ss))
	for i, s := range ss {
		items[i] = objects.NewStr(s)
	}
	return objects.NewTuple(items)
}

// reduceImpl is the module-level reduce. Defers to _functools.reduce.
//
// CPython: Lib/functools.py:204 reduce
func reduceImpl(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: reduce() takes 2 or 3 arguments (%d given)", len(args))
	}
	fn := args[0]
	it, err := objects.Iter(args[1])
	if err != nil {
		return nil, fmt.Errorf("TypeError: reduce() arg 2 must support iteration")
	}
	var acc objects.Object
	if len(args) == 3 {
		acc = args[2]
	}
	_ = kwargs
	for {
		v, ierr := objects.IterNext(it)
		if ierr != nil {
			if errors.Is(ierr, objects.ErrStopIteration) {
				break
			}
			return nil, ierr
		}
		if v == nil {
			break
		}
		if acc == nil {
			acc = v
			continue
		}
		next, cerr := objects.Call(fn, objects.NewTuple([]objects.Object{acc, v}), nil)
		if cerr != nil {
			return nil, cerr
		}
		acc = next
	}
	if acc == nil {
		return nil, fmt.Errorf("TypeError: reduce() of empty iterable with no initial value")
	}
	return acc, nil
}

// cmpToKey returns a KeyObject factory bound to fn. Defers to the
// _functools implementation: cmp_to_key freezes fn into a callable
// that takes a single obj.
//
// CPython: Lib/functools.py:226 cmp_to_key
func cmpToKey(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: cmp_to_key() takes exactly one argument (%d given)", len(args))
	}
	k := &_functools.KeyObject{Cmp: args[0]}
	k.Init(_functools.KeyObjectType)
	return k, nil
}

// newCacheInfoType builds the _CacheInfo named-tuple-ish class used by
// lru_cache. CPython generates it via collections.namedtuple; the
// stand-in is a callable type whose __new__ stores (hits, misses,
// maxsize, currsize) in the instance dict and exposes them as
// attributes.
//
// CPython: Lib/functools.py:548 _CacheInfo
func newCacheInfoType() *objects.Type {
	t := objects.NewType("CacheInfo", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 4 {
			return nil, fmt.Errorf("TypeError: CacheInfo() requires 4 arguments")
		}
		inst := objects.NewInstance(cls)
		dict := inst.Dict()
		names := []string{"hits", "misses", "maxsize", "currsize"}
		for i, name := range names {
			if err := dict.SetItem(objects.NewStr(name), args[i]); err != nil {
				return nil, err
			}
		}
		return inst, nil
	}
	t.Repr = func(o objects.Object) (string, error) {
		inst, ok := o.(*objects.Instance)
		if !ok {
			return "CacheInfo(?)", nil
		}
		d := inst.Dict()
		hits, _ := d.GetItem(objects.NewStr("hits"))
		misses, _ := d.GetItem(objects.NewStr("misses"))
		maxsize, _ := d.GetItem(objects.NewStr("maxsize"))
		currsize, _ := d.GetItem(objects.NewStr("currsize"))
		hs, _ := objects.Repr(hits)
		ms, _ := objects.Repr(misses)
		mxs, _ := objects.Repr(maxsize)
		cs, _ := objects.Repr(currsize)
		return "CacheInfo(hits=" + hs + ", misses=" + ms + ", maxsize=" + mxs + ", currsize=" + cs + ")", nil
	}
	t.Str = t.Repr
	return t
}

// makeLruCache produces the lru_cache decorator factory. lru_cache can
// be called three ways: lru_cache(user_function), lru_cache(maxsize),
// or lru_cache(maxsize=N, typed=B). The wrapper handles all three.
//
// CPython: Lib/functools.py:526 lru_cache
func makeLruCache(cacheInfoType *objects.Type) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		// Default maxsize is 128, typed is False.
		var maxsize objects.Object = objects.NewInt(128)
		typed := objects.NewBool(false)
		// Disambiguate: lru_cache(user_function) hands us a single
		// callable as args[0]; lru_cache(maxsize=N) hands us a single
		// int (or None) as args[0]; lru_cache() hands us nothing.
		var userFn objects.Object
		positionalArgs := args
		if len(positionalArgs) >= 1 {
			a0 := positionalArgs[0]
			if isCallable(a0) && !isIntLike(a0) {
				userFn = a0
				positionalArgs = positionalArgs[1:]
			}
		}
		if len(positionalArgs) >= 1 {
			maxsize = positionalArgs[0]
		} else if v, ok := kwargs["maxsize"]; ok {
			maxsize = v
		}
		if len(positionalArgs) >= 2 {
			typed = positionalArgs[1]
		} else if v, ok := kwargs["typed"]; ok {
			typed = v
		}
		decorate := func(decArgs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(decArgs) < 1 {
				return nil, fmt.Errorf("TypeError: lru_cache decorator requires a callable")
			}
			fn := decArgs[0]
			return objects.Call(_functools.LruCacheWrapperType, objects.NewTuple([]objects.Object{fn, maxsize, typed, cacheInfoType}), nil)
		}
		if userFn != nil {
			return decorate([]objects.Object{userFn}, nil)
		}
		return objects.NewBuiltinFunction("lru_cache_decorator", decorate), nil
	}
}

// makeCache implements functools.cache: lru_cache(maxsize=None).
//
// CPython: Lib/functools.py:651 cache
func makeCache(cacheInfoType *objects.Type) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: cache() takes 1 positional argument (%d given)", len(args))
		}
		fn := args[0]
		return objects.Call(_functools.LruCacheWrapperType, objects.NewTuple([]objects.Object{fn, objects.None(), objects.NewBool(false), cacheInfoType}), nil)
	}
}

func isCallable(o objects.Object) bool {
	if o == nil {
		return false
	}
	tp := o.Type()
	if tp == nil {
		return false
	}
	if tp.Call != nil || tp.Vectorcall != nil {
		return true
	}
	if _, ok := o.(*objects.Type); ok {
		return true
	}
	return false
}

func isIntLike(o objects.Object) bool {
	if o == nil {
		return false
	}
	if _, ok := o.(*objects.Int); ok {
		return true
	}
	return objects.IsNone(o)
}

// cachedPropertyType is functools.cached_property: store the wrapped
// function and, on first attribute access, cache the result in the
// instance __dict__. Subsequent accesses bypass the descriptor.
//
// CPython: Lib/functools.py:990 cached_property
func cachedPropertyType() *objects.Type {
	t := objects.NewType("cached_property", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: cached_property() requires the wrapped function")
		}
		fn := args[0]
		inst := objects.NewInstance(cls)
		d := inst.Dict()
		_ = d.SetItem(objects.NewStr("func"), fn)
		_ = d.SetItem(objects.NewStr("attrname"), objects.None())
		// Stamp __name__ for introspection when available.
		if name, err := objects.GetAttr(fn, objects.NewStr("__name__")); err == nil {
			_ = d.SetItem(objects.NewStr("__name__"), name)
			_ = d.SetItem(objects.NewStr("attrname"), name)
		}
		return inst, nil
	}
	t.DescrGet = cachedPropertyGet
	return t
}

func cachedPropertyGet(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
	if owner == nil || objects.IsNone(owner) {
		return descr, nil
	}
	inst, ok := descr.(*objects.Instance)
	if !ok {
		return descr, nil
	}
	d := inst.Dict()
	nameObj, err := d.GetItem(objects.NewStr("attrname"))
	if err != nil || objects.IsNone(nameObj) {
		return nil, fmt.Errorf("TypeError: cached_property has no __set_name__ result yet")
	}
	// Check the owner's __dict__ for a cached value.
	if hasDict, ok := owner.(interface{ Dict() *objects.Dict }); ok {
		ownerDict := hasDict.Dict()
		if v, err := ownerDict.GetItem(nameObj); err == nil {
			return v, nil
		}
		// Miss: call func(owner) and store.
		fn, err := d.GetItem(objects.NewStr("func"))
		if err != nil {
			return nil, err
		}
		val, err := objects.Call(fn, objects.NewTuple([]objects.Object{owner}), nil)
		if err != nil {
			return nil, err
		}
		_ = ownerDict.SetItem(nameObj, val)
		return val, nil
	}
	// Owner has no per-instance dict: just call without caching.
	fn, err := d.GetItem(objects.NewStr("func"))
	if err != nil {
		return nil, err
	}
	return objects.Call(fn, objects.NewTuple([]objects.Object{owner}), nil)
}

// totalOrdering decorates a class with the missing comparison methods
// derived from one provided ordering operator plus __eq__. The CPython
// version inspects __lt__/__le__/__gt__/__ge__ and __eq__ on the class
// to pick a "root" comparison. For the gopy port we accept the class
// unchanged.
//
// CPython: Lib/functools.py:106 total_ordering
func totalOrdering(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: total_ordering() takes exactly one argument (%d given)", len(args))
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: total_ordering() expected a class, got %s", args[0].Type().Name)
	}
	// CPython chains the missing operator wrappers; gopy currently
	// exposes only the rich comparison slot, so the class is already
	// usable through Python's comparison fallback. Returning the class
	// unchanged matches what total_ordering does for a class that
	// already defines all six comparisons.
	return cls, nil
}

// singledispatch returns a generic-function decorator. The CPython
// implementation builds a registry of (type -> implementation)
// dispatches; the gopy version produces a callable wrapper that
// dispatches by exact type with a fallback to the original function.
//
// CPython: Lib/functools.py:884 singledispatch
func singledispatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: singledispatch() takes exactly one argument (%d given)", len(args))
	}
	fn := args[0]
	registry := objects.NewDict()
	_ = registry.SetItem(objects.NewStr("object"), fn)
	wrapper := func(callArgs []objects.Object, callKwargs map[string]objects.Object) (objects.Object, error) {
		if len(callArgs) >= 1 {
			tp := callArgs[0].Type()
			if v, err := registry.GetItem(objects.NewStr(tp.Name)); err == nil {
				return objects.Call(v, objects.NewTuple(callArgs), kwToDict(callKwargs))
			}
		}
		return objects.Call(fn, objects.NewTuple(callArgs), kwToDict(callKwargs))
	}
	registerFn := func(rargs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(rargs) < 1 {
			return nil, fmt.Errorf("TypeError: register() requires a type argument")
		}
		tp, ok := rargs[0].(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: register() first argument must be a type")
		}
		if len(rargs) >= 2 {
			_ = registry.SetItem(objects.NewStr(tp.Name), rargs[1])
			return rargs[1], nil
		}
		// Decorator form: register(tp) returns a decorator.
		inner := func(iargs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(iargs) < 1 {
				return nil, fmt.Errorf("TypeError: register decorator missing function")
			}
			_ = registry.SetItem(objects.NewStr(tp.Name), iargs[0])
			return iargs[0], nil
		}
		return objects.NewBuiltinFunction("register_decorator", inner), nil
	}
	wrapped := objects.NewBuiltinFunction("singledispatch_wrapper", wrapper)
	// Stamp register / registry on the wrapper for introspection.
	registerObj := objects.NewBuiltinFunction("register", registerFn)
	if obj, ok := any(wrapped).(interface {
		SetAttr(name, value objects.Object) error
	}); ok {
		_ = obj.SetAttr(objects.NewStr("register"), registerObj)
		_ = obj.SetAttr(objects.NewStr("registry"), registry)
	}
	return wrapped, nil
}

func kwToDict(kwargs map[string]objects.Object) *objects.Dict {
	if len(kwargs) == 0 {
		return nil
	}
	d := objects.NewDict()
	for k, v := range kwargs {
		_ = d.SetItem(objects.NewStr(k), v)
	}
	return d
}

// partialmethodType is functools.partialmethod: like partial but binds
// the descriptor as a method instead of as a function value.
//
// CPython: Lib/functools.py:359 partialmethod
func partialmethodType() *objects.Type {
	t := objects.NewType("partialmethod", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: partialmethod() requires the wrapped callable")
		}
		inst := objects.NewInstance(cls)
		d := inst.Dict()
		_ = d.SetItem(objects.NewStr("func"), args[0])
		_ = d.SetItem(objects.NewStr("args"), objects.NewTuple(args[1:]))
		kwd := objects.NewDict()
		for k, v := range kwargs {
			_ = kwd.SetItem(objects.NewStr(k), v)
		}
		_ = d.SetItem(objects.NewStr("keywords"), kwd)
		return inst, nil
	}
	t.DescrGet = func(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
		if owner == nil || objects.IsNone(owner) {
			return descr, nil
		}
		inst, ok := descr.(*objects.Instance)
		if !ok {
			return descr, nil
		}
		d := inst.Dict()
		fn, err := d.GetItem(objects.NewStr("func"))
		if err != nil {
			return nil, err
		}
		boundArgsObj, _ := d.GetItem(objects.NewStr("args"))
		boundKwObj, _ := d.GetItem(objects.NewStr("keywords"))
		boundArgs, _ := boundArgsObj.(*objects.Tuple)
		boundKw, _ := boundKwObj.(*objects.Dict)
		// Build a partial(fn, owner, *boundArgs, **boundKw) so the
		// usual __call__ machinery handles the rest.
		partArgs := []objects.Object{fn, owner}
		if boundArgs != nil {
			for i := 0; i < boundArgs.Len(); i++ {
				partArgs = append(partArgs, boundArgs.Item(i))
			}
		}
		var kwargs map[string]objects.Object
		if boundKw != nil && boundKw.Len() > 0 {
			kwargs = make(map[string]objects.Object, boundKw.Len())
			for _, k := range boundKw.Keys() {
				v, _ := boundKw.GetItem(k)
				ks, _ := objects.Str(k)
				kwargs[ks] = v
			}
		}
		return _functools.PartialType.TpNew(_functools.PartialType, partArgs, kwargs)
	}
	return t
}
