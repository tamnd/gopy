// Package _functools is the gopy port of CPython's _functools C
// accelerator. The pure-Python wrapper at Lib/functools.py imports
// these names through `from _functools import ...`: the partial type,
// the cmp_to_key key class, reduce, and the _lru_cache_wrapper
// internal type used by lru_cache.
//
// CPython: Modules/_functoolsmodule.c

package _functools

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_functools", buildModule)
}

// buildModule materializes the _functools module dict. Mirrors the
// PyInit__functools entry point.
//
// CPython: Modules/_functoolsmodule.c:1830 _functools_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_functools")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"partial", PartialType},
		{"_PlaceholderType", PlaceholderType},
		{"Placeholder", Placeholder},
		{"cmp_to_key", objects.NewBuiltinFunction("cmp_to_key", cmpToKey)},
		{"reduce", objects.NewBuiltinFunction("reduce", reduceFunc)},
		{"_lru_cache_wrapper", LruCacheWrapperType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Placeholder singleton.
// ---------------------------------------------------------------------------

// PlaceholderType backs functools.Placeholder. CPython exposes a
// singleton instance whose only purpose is to act as a sentinel inside
// partial.args (and to be checked by identity).
//
// CPython: Modules/_functoolsmodule.c:128 placeholder_type_spec
var PlaceholderType = objects.NewType("_PlaceholderType", []*objects.Type{objects.ObjectType()})

// Placeholder is the singleton instance of PlaceholderType, mirroring
// the module-level Placeholder attribute set in _functools_exec.
//
// CPython: Modules/_functoolsmodule.c:1848 Placeholder
var Placeholder objects.Object

type placeholderObj struct {
	objects.Header
}

func init() {
	PlaceholderType.Repr = func(_ objects.Object) (string, error) { return "Placeholder", nil }
	PlaceholderType.Str = PlaceholderType.Repr
	// Build singleton, then wire TpNew to return it.
	p := &placeholderObj{}
	p.Init(PlaceholderType)
	Placeholder = p
	PlaceholderType.TpNew = placeholderNew
}

// placeholderNew is the tp_new slot: always returns the module-level
// Placeholder singleton.
//
// CPython: Modules/_functoolsmodule.c:120 placeholder_new
func placeholderNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	_ = cls
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: PlaceholderType takes no arguments")
	}
	return Placeholder, nil
}

// ---------------------------------------------------------------------------
// partial type.
// ---------------------------------------------------------------------------

// PartialType is functools.partial. Instances bundle a callable with a
// frozen prefix of positional and keyword arguments; calling the
// partial spreads those plus the call-site args into the underlying
// function. Placeholder sentinels in args reserve slots that the call
// site fills in left-to-right.
//
// CPython: Modules/_functoolsmodule.c:796 partial_type_spec
var PartialType = newPartialType()

// Partial is the concrete Go shape backing a partial instance. The
// fields mirror partialobject in the C source.
//
// CPython: Modules/_functoolsmodule.c:136 partialobject
type Partial struct {
	objects.Header
	Fn       objects.Object
	Args     *objects.Tuple
	Keywords *objects.Dict
	Dict     *objects.Dict
	Phcount  int
}

func newPartialType() *objects.Type {
	t := objects.NewType("partial", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = partialGetattr
	t.Setattro = partialSetattr
	t.Repr = partialRepr
	t.Str = partialRepr
	t.Call = partialCall
	t.TpNew = partialNew
	t.DescrGet = partialDescrGet
	return t
}

// partialNew implements partial.__new__. Mirrors partial_new: validate
// the callable, scan args for placeholders, fold an inner partial's
// arguments together, and stamp the resulting object.
//
// CPython: Modules/_functoolsmodule.c:167 partial_new
func partialNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: type 'partial' takes at least one argument")
	}
	fn := args[0]
	newArgs := append([]objects.Object{}, args[1:]...)
	if !isCallable(fn) {
		return nil, fmt.Errorf("TypeError: the first argument must be callable")
	}
	if n := len(newArgs); n > 0 && newArgs[n-1] == Placeholder {
		return nil, fmt.Errorf("TypeError: trailing Placeholders are not allowed")
	}
	for _, v := range kwargs {
		if v == Placeholder {
			return nil, fmt.Errorf("TypeError: Placeholder cannot be passed as a keyword argument")
		}
	}

	var (
		ptoArgs  *objects.Tuple
		ptoKw    *objects.Dict
		phcount  int
		innerFn  objects.Object
		isInner  bool
		ptoInner *Partial
	)
	if inner, ok := fn.(*Partial); ok && inner.Dict == nil {
		ptoInner = inner
		isInner = true
		ptoArgs = inner.Args
		ptoKw = inner.Keywords
		innerFn = inner.Fn
	}

	// Count placeholders in newArgs.
	for i := 0; i < len(newArgs)-1; i++ {
		if newArgs[i] == Placeholder {
			phcount++
		}
	}

	var totArgs *objects.Tuple
	if isInner && ptoInner.Phcount > 0 && len(newArgs) > 0 {
		// Merge: fill each Placeholder in pto.args with the next entry
		// from newArgs, append any leftover newArgs at the tail.
		npargs := ptoArgs.Len()
		remainingPh := ptoInner.Phcount
		totNargs := npargs
		if len(newArgs) > remainingPh {
			totNargs += len(newArgs) - remainingPh
		}
		out := make([]objects.Object, totNargs)
		j := 0
		for i := 0; i < totNargs; i++ {
			var item objects.Object
			if i < npargs {
				item = ptoArgs.Item(i)
				if j < len(newArgs) && item == Placeholder {
					item = newArgs[j]
					j++
					remainingPh--
				}
			} else {
				item = newArgs[j]
				j++
			}
			out[i] = item
		}
		totArgs = objects.NewTuple(out)
		phcount += remainingPh
		fn = innerFn
	} else if ptoArgs == nil {
		totArgs = objects.NewTuple(newArgs)
	} else {
		// Concatenate existing args with newArgs.
		merged := make([]objects.Object, 0, ptoArgs.Len()+len(newArgs))
		for i := 0; i < ptoArgs.Len(); i++ {
			merged = append(merged, ptoArgs.Item(i))
		}
		merged = append(merged, newArgs...)
		totArgs = objects.NewTuple(merged)
		phcount += ptoInner.Phcount
		fn = innerFn
	}

	// Merge keywords: pto.kw is the base, kwargs overrides.
	totKw := objects.NewDict()
	if ptoKw != nil {
		for _, k := range ptoKw.Keys() {
			v, err := ptoKw.GetItem(k)
			if err != nil {
				return nil, err
			}
			if err := totKw.SetItem(k, v); err != nil {
				return nil, err
			}
		}
	}
	for k, v := range kwargs {
		if err := totKw.SetItem(objects.NewStr(k), v); err != nil {
			return nil, err
		}
	}

	p := &Partial{
		Fn:       fn,
		Args:     totArgs,
		Keywords: totKw,
		Phcount:  phcount,
	}
	p.Init(cls)
	return p, nil
}

// partialCall is the tp_call slot. Mirrors partial_call: merge the
// stored args (replacing each Placeholder with a fresh arg) plus the
// trailing call-site args, merge the stored keywords plus the
// call-site keywords, then forward to the wrapped function.
//
// CPython: Modules/_functoolsmodule.c:497 partial_call
func partialCall(o objects.Object, callArgs []objects.Object, callKwargs map[string]objects.Object) (objects.Object, error) {
	p := o.(*Partial)
	nargs := len(callArgs)
	if nargs < p.Phcount {
		return nil, fmt.Errorf("TypeError: missing positional arguments in 'partial' call; expected at least %d, got %d", p.Phcount, nargs)
	}

	// Merge keyword arguments.
	totKw := callKwargs
	if p.Keywords != nil && p.Keywords.Len() > 0 {
		totKw = make(map[string]objects.Object, p.Keywords.Len()+len(callKwargs))
		for _, k := range p.Keywords.Keys() {
			v, err := p.Keywords.GetItem(k)
			if err != nil {
				return nil, err
			}
			ks, err := objects.Str(k)
			if err != nil {
				return nil, err
			}
			totKw[ks] = v
		}
		for k, v := range callKwargs {
			totKw[k] = v
		}
	}

	// Merge positional arguments.
	var totArgs []objects.Object
	ptoArgs := p.Args
	ptoNargs := ptoArgs.Len()
	if p.Phcount > 0 {
		totNargs := ptoNargs + nargs - p.Phcount
		totArgs = make([]objects.Object, 0, totNargs)
		j := 0
		for i := 0; i < ptoNargs; i++ {
			item := ptoArgs.Item(i)
			if item == Placeholder {
				item = callArgs[j]
				j++
			}
			totArgs = append(totArgs, item)
		}
		for i := j; i < nargs; i++ {
			totArgs = append(totArgs, callArgs[i])
		}
	} else {
		totArgs = make([]objects.Object, 0, ptoNargs+nargs)
		for i := 0; i < ptoNargs; i++ {
			totArgs = append(totArgs, ptoArgs.Item(i))
		}
		totArgs = append(totArgs, callArgs...)
	}

	tup := objects.NewTuple(totArgs)
	var kwDict *objects.Dict
	if len(totKw) > 0 {
		kwDict = objects.NewDict()
		for k, v := range totKw {
			if err := kwDict.SetItem(objects.NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return objects.Call(p.Fn, tup, kwDict)
}

// partialRepr renders partial(func, args..., kw=val, ...). Mirrors
// partial_repr; recursion guard is omitted because gopy does not yet
// expose Py_ReprEnter.
//
// CPython: Modules/_functoolsmodule.c:610 partial_repr
func partialRepr(o objects.Object) (string, error) {
	p := o.(*Partial)
	fnRepr, err := objects.Repr(p.Fn)
	if err != nil {
		return "", err
	}
	out := "functools.partial(" + fnRepr
	for i := 0; i < p.Args.Len(); i++ {
		ar, err := objects.Repr(p.Args.Item(i))
		if err != nil {
			return "", err
		}
		out += ", " + ar
	}
	if p.Keywords != nil {
		for _, k := range p.Keywords.Keys() {
			ks, err := objects.Str(k)
			if err != nil {
				return "", err
			}
			v, err := p.Keywords.GetItem(k)
			if err != nil {
				return "", err
			}
			vr, err := objects.Repr(v)
			if err != nil {
				return "", err
			}
			out += ", " + ks + "=" + vr
		}
	}
	out += ")"
	return out, nil
}

// partialGetattr exposes the .func / .args / .keywords / .__dict__
// fields as read-only attributes, plus methods like __reduce__ and
// __setstate__. Falls back to generic attribute access for anything
// else (e.g. user-stamped attributes in the per-instance dict).
//
// CPython: Modules/_functoolsmodule.c:588 partial_memberlist
func partialGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	p := o.(*Partial)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "func":
		return p.Fn, nil
	case "args":
		return p.Args, nil
	case "keywords":
		return p.Keywords, nil
	case "__dict__":
		if p.Dict == nil {
			p.Dict = objects.NewDict()
		}
		return p.Dict, nil
	}
	if p.Dict != nil {
		if v, err := p.Dict.GetItem(name); err == nil {
			return v, nil
		}
	}
	return objects.GenericGetAttr(o, name)
}

// partialSetattr stores user attributes in the per-instance __dict__,
// rejecting writes to the read-only fields. value==nil signals delete.
//
// CPython: Modules/_functoolsmodule.c:588 partial_memberlist (Py_READONLY)
func partialSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	p := o.(*Partial)
	n, err := objects.Str(name)
	if err != nil {
		return err
	}
	switch n {
	case "func", "args", "keywords":
		return fmt.Errorf("AttributeError: readonly attribute '%s'", n)
	}
	if p.Dict == nil {
		p.Dict = objects.NewDict()
	}
	if value == nil {
		return p.Dict.DelItem(name)
	}
	return p.Dict.SetItem(name, value)
}

// partialDescrGet wires partial as a descriptor: accessing it through
// an instance binds the instance as a method, exactly the way
// partial_descr_get does.
//
// CPython: Modules/_functoolsmodule.c:362 partial_descr_get
func partialDescrGet(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
	if owner == nil || objects.IsNone(owner) {
		return descr, nil
	}
	return objects.NewBoundMethod(descr, owner), nil
}

// ---------------------------------------------------------------------------
// cmp_to_key key class.
// ---------------------------------------------------------------------------

// KeyObjectType is the keyobject class returned by cmp_to_key. Each
// instance carries the user's cmp function plus an obj; rich
// comparisons call cmp(self.obj, other.obj) and compare the integer
// result to zero.
//
// CPython: Modules/_functoolsmodule.c:881 keyobject_type_spec
var KeyObjectType = objects.NewType("functools.KeyWrapper", []*objects.Type{objects.ObjectType()})

// KeyObject backs an instance of the cmp_to_key K class.
type KeyObject struct {
	objects.Header
	Cmp objects.Object
	Obj objects.Object
}

func init() {
	KeyObjectType.Getattro = keyObjectGetattr
	KeyObjectType.Call = keyObjectCall
	KeyObjectType.RichCmp = keyObjectRichCmp
}

// isCallable returns true when o looks invokable: it has a Call slot,
// a Vectorcall slot, or (for types) a TpNew slot. Mirrors PyCallable_Check.
//
// CPython: Objects/call.c:235 PyCallable_Check
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
	// Types themselves are callable through TpNew.
	if _, ok := o.(*objects.Type); ok {
		return true
	}
	return false
}

// cmpToKey is the module-level cmp_to_key function. It freezes the
// user's cmp into a fresh KeyObject "factory" (Obj==nil); calling
// that factory with an obj returns a fully populated keyobject.
//
// CPython: Modules/_functoolsmodule.c:953 _functools_cmp_to_key_impl
func cmpToKey(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: cmp_to_key() takes exactly one argument (%d given)", len(args))
	}
	k := &KeyObject{Cmp: args[0]}
	k.Init(KeyObjectType)
	return k, nil
}

// keyObjectCall produces a new KeyObject bound to the supplied
// obj. Mirrors keyobject_call.
//
// CPython: Modules/_functoolsmodule.c:889 keyobject_call
func keyObjectCall(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	ko := o.(*KeyObject)
	var obj objects.Object
	if len(args) == 1 {
		obj = args[0]
	} else if v, ok := kwargs["obj"]; ok && len(args) == 0 && len(kwargs) == 1 {
		obj = v
	} else {
		return nil, fmt.Errorf("TypeError: K() takes exactly one argument (obj)")
	}
	k := &KeyObject{Cmp: ko.Cmp, Obj: obj}
	k.Init(KeyObjectType)
	return k, nil
}

// keyObjectGetattr exposes the .obj read-only field plus generic
// attribute access for fallback.
//
// CPython: Modules/_functoolsmodule.c:845 keyobject_members
func keyObjectGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	ko := o.(*KeyObject)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	if n == "obj" {
		if ko.Obj == nil {
			return nil, fmt.Errorf("AttributeError: 'KeyWrapper' object has no attribute 'obj'")
		}
		return ko.Obj, nil
	}
	return objects.GenericGetAttr(o, name)
}

// keyObjectRichCmp implements <, <=, ==, !=, >, >= by invoking the
// user-supplied cmp(self.obj, other.obj) and comparing the integer
// result against zero.
//
// CPython: Modules/_functoolsmodule.c:910 keyobject_richcompare
func keyObjectRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	lhs, ok := a.(*KeyObject)
	if !ok {
		return objects.NotImplemented(), nil
	}
	rhs, ok := b.(*KeyObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: other argument must be K instance")
	}
	if lhs.Obj == nil || rhs.Obj == nil {
		return nil, fmt.Errorf("AttributeError: object")
	}
	res, err := objects.Call(lhs.Cmp, objects.NewTuple([]objects.Object{lhs.Obj, rhs.Obj}), nil)
	if err != nil {
		return nil, err
	}
	return objects.RichCmp(res, objects.NewInt(0), op)
}

// ---------------------------------------------------------------------------
// reduce.
// ---------------------------------------------------------------------------

// reduceFunc implements functools.reduce: fold a 2-arg function over
// an iterable, optionally seeded by `initial`.
//
// CPython: Modules/_functoolsmodule.c:990 _functools_reduce_impl
func reduceFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
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

// ---------------------------------------------------------------------------
// lru_cache wrapper.
// ---------------------------------------------------------------------------

// LruCacheWrapperType is functools._lru_cache_wrapper. Instances are
// callable wrappers around a user function plus a key->result cache
// and an LRU recency list when maxsize is bounded.
//
// CPython: Modules/_functoolsmodule.c:1810 lru_cache_type_spec
var LruCacheWrapperType = newLruCacheWrapperType()

// kwdMark is a unique sentinel used inside lruMakeKey to separate the
// positional and keyword sections of a cache key. CPython allocates a
// fresh object() at module init; gopy uses a package-level singleton.
//
// CPython: Modules/_functoolsmodule.c:1834 state->kwd_mark
var kwdMark = func() objects.Object {
	o := objects.NewInstance(objects.NewType("kwd_mark_t", []*objects.Type{objects.ObjectType()}))
	return o
}()

// LruCacheWrapper is the runtime shape of an _lru_cache_wrapper. It
// folds the dict cache plus the doubly-linked recency list plus the
// hit/miss counters; see the C struct lru_cache_object for the
// CPython side.
//
// CPython: Modules/_functoolsmodule.c:1120 lru_cache_object
type LruCacheWrapper struct {
	objects.Header
	Func          objects.Object
	Maxsize       int64
	Unbounded     bool // true when maxsize is None.
	Typed         bool
	Cache         *objects.Dict
	CacheInfoType objects.Object
	Hits          int64
	Misses        int64
	// Per-instance attribute dict for user assignments like
	// `wrapper.cache_parameters = lambda ...` that lru_cache() makes.
	// Lazily allocated.
	Dict *objects.Dict
	// Doubly-linked list of cache entries in LRU order. root is a
	// dummy node so the list is never empty.
	root *lruListNode
}

// lruListNode is one entry in the LRU recency list. key is what the
// cache dict maps from; result is the cached return value.
//
// CPython: Modules/_functoolsmodule.c:1084 lru_list_elem
type lruListNode struct {
	prev, next *lruListNode
	key        objects.Object
	result     objects.Object
}

func newLruCacheWrapperType() *objects.Type {
	t := objects.NewType("functools._lru_cache_wrapper", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = lruCacheGetattr
	t.Setattro = lruCacheSetattr
	t.Call = lruCacheCall
	t.TpNew = lruCacheNew
	t.DescrGet = lruCacheDescrGet
	objects.SetTypeDescr(t, "cache_info", objects.NewMethodDescr(t, "cache_info", lruCacheInfo))
	objects.SetTypeDescr(t, "cache_clear", objects.NewMethodDescr(t, "cache_clear", lruCacheClear))
	objects.SetTypeDescr(t, "__copy__", objects.NewMethodDescr(t, "__copy__", lruCacheCopy))
	objects.SetTypeDescr(t, "__deepcopy__", objects.NewMethodDescr(t, "__deepcopy__", lruCacheCopy))
	return t
}

// lruCacheNew is _lru_cache_wrapper.__new__: validate args, pick a
// wrapper variant from maxsize, initialize the cache + list root.
//
// CPython: Modules/_functoolsmodule.c:1522 lru_cache_new
func lruCacheNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("TypeError: _lru_cache_wrapper requires (user_function, maxsize, typed, cache_info_type)")
	}
	fn := args[0]
	if !isCallable(fn) {
		return nil, fmt.Errorf("TypeError: the first argument must be callable")
	}
	maxsizeArg := args[1]
	typedArg := args[2]
	cacheInfoType := args[3]

	var (
		maxsize   int64
		unbounded bool
	)
	if objects.IsNone(maxsizeArg) {
		unbounded = true
		maxsize = -1
	} else {
		n, ok := maxsizeArg.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: maxsize should be integer or None")
		}
		v, fits := n.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: maxsize too large")
		}
		maxsize = v
		if maxsize < 0 {
			maxsize = 0
		}
	}
	typed, err := objects.IsTruthy(typedArg)
	if err != nil {
		return nil, err
	}

	w := &LruCacheWrapper{
		Func:          fn,
		Maxsize:       maxsize,
		Unbounded:     unbounded,
		Typed:         typed,
		Cache:         objects.NewDict(),
		CacheInfoType: cacheInfoType,
	}
	w.root = &lruListNode{}
	w.root.prev = w.root
	w.root.next = w.root
	w.Init(cls)
	return w, nil
}

// lruCacheCall is the tp_call slot. Picks one of three caching
// strategies based on maxsize and forwards to the wrapped function on
// a miss.
//
// CPython: Modules/_functoolsmodule.c:1647 lru_cache_call
func lruCacheCall(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	w := o.(*LruCacheWrapper)
	switch {
	case w.Maxsize == 0 && !w.Unbounded:
		return uncachedLruCacheWrapper(w, args, kwargs)
	case w.Unbounded:
		return infiniteLruCacheWrapper(w, args, kwargs)
	default:
		return boundedLruCacheWrapper(w, args, kwargs)
	}
}

// uncachedLruCacheWrapper is the maxsize==0 path: increments misses
// and forwards every call without caching.
//
// CPython: Modules/_functoolsmodule.c:1200 uncached_lru_cache_wrapper
func uncachedLruCacheWrapper(w *LruCacheWrapper, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	w.Misses++
	return callFunc(w.Func, args, kwargs)
}

// infiniteLruCacheWrapper is the maxsize==None path: cache without
// bounds and without LRU ordering.
//
// CPython: Modules/_functoolsmodule.c:1212 infinite_lru_cache_wrapper
func infiniteLruCacheWrapper(w *LruCacheWrapper, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	key, err := lruMakeKey(args, kwargs, w.Typed)
	if err != nil {
		return nil, err
	}
	if v, err := w.Cache.GetItem(key); err == nil {
		w.Hits++
		return v, nil
	}
	w.Misses++
	res, err := callFunc(w.Func, args, kwargs)
	if err != nil {
		return nil, err
	}
	if err := w.Cache.SetItem(key, res); err != nil {
		return nil, err
	}
	return res, nil
}

// boundedLruCacheWrapper is the size-limited path: cache with LRU
// eviction when len(cache) reaches maxsize. The cache maps keys to
// lruListNode pointers wrapped in capsules; promotion on hit moves
// the node to the tail of the recency list.
//
// CPython: Modules/_functoolsmodule.c:1491 bounded_lru_cache_wrapper
func boundedLruCacheWrapper(w *LruCacheWrapper, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	key, err := lruMakeKey(args, kwargs, w.Typed)
	if err != nil {
		return nil, err
	}
	if linkObj, err := w.Cache.GetItem(key); err == nil {
		link := linkObj.(*lruNodeHolder).node
		extractLink(link)
		appendLink(w, link)
		w.Hits++
		return link.result, nil
	}
	w.Misses++
	res, err := callFunc(w.Func, args, kwargs)
	if err != nil {
		return nil, err
	}
	// If we hit the bound, evict the LRU node (root.next).
	cacheSize := int64(w.Cache.Len())
	if cacheSize >= w.Maxsize && w.root.next != w.root {
		oldest := w.root.next
		extractLink(oldest)
		_ = w.Cache.DelItem(oldest.key)
	}
	link := &lruListNode{key: key, result: res}
	appendLink(w, link)
	if err := w.Cache.SetItem(key, newLruNodeHolder(link)); err != nil {
		return nil, err
	}
	return res, nil
}

// callFunc forwards the (args, kwargs) tuple to fn through the
// gopy Call helper.
func callFunc(fn objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	tup := objects.NewTuple(args)
	var kwd *objects.Dict
	if len(kwargs) > 0 {
		kwd = objects.NewDict()
		for k, v := range kwargs {
			if err := kwd.SetItem(objects.NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return objects.Call(fn, tup, kwd)
}

// extractLink unlinks node from its doubly-linked list.
//
// CPython: Modules/_functoolsmodule.c:1250 lru_cache_extract_link
func extractLink(node *lruListNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// appendLink inserts node at the tail of w.root's list (most recent).
//
// CPython: Modules/_functoolsmodule.c:1259 lru_cache_append_link
func appendLink(w *LruCacheWrapper, node *lruListNode) {
	last := w.root.prev
	last.next = node
	node.prev = last
	node.next = w.root
	w.root.prev = node
}

// lruNodeHolder is a tiny Object wrapper so an lruListNode can live
// inside the cache dict's value slot. The list itself is unowned by
// the dict; eviction unlinks the node before deleting.
type lruNodeHolder struct {
	objects.Header
	node *lruListNode
}

var lruNodeHolderType = objects.NewType("_lru_list_elem", []*objects.Type{objects.ObjectType()})

// newLruNodeHolder wraps an lru list node so it can live in the cache
// dict's value slot. The dict does not own the linked list; eviction
// unlinks the node before deleting.
func newLruNodeHolder(node *lruListNode) *lruNodeHolder {
	h := &lruNodeHolder{node: node}
	h.Init(lruNodeHolderType)
	return h
}

// lruMakeKey builds a hashable cache key from args + kwargs. Mirrors
// lru_cache_make_key: the short path returns the single scalar arg
// when typed==False; otherwise it builds a tuple that flattens
// positional args, an optional kwd marker, key/value pairs, and
// per-arg types when typed==True.
//
// CPython: Modules/_functoolsmodule.c:1139 lru_cache_make_key
func lruMakeKey(args []objects.Object, kwargs map[string]objects.Object, typed bool) (objects.Object, error) {
	if !typed && len(kwargs) == 0 {
		if len(args) == 1 {
			a := args[0]
			tp := a.Type()
			if tp == objects.StrType() || tp == objects.IntType {
				return a, nil
			}
		}
		return objects.NewTuple(args), nil
	}
	keySize := len(args)
	if len(kwargs) > 0 {
		keySize += len(kwargs)*2 + 1
	}
	if typed {
		keySize += len(args) + len(kwargs)
	}
	items := make([]objects.Object, 0, keySize)
	items = append(items, args...)
	// Stable order over kwargs to keep the cache deterministic. The
	// CPython version relies on dict insertion order; Go maps have
	// random iteration so we sort the keys.
	if len(kwargs) > 0 {
		items = append(items, kwdMark)
		keys := sortedKwargKeys(kwargs)
		for _, k := range keys {
			items = append(items, objects.NewStr(k))
			items = append(items, kwargs[k])
		}
	}
	if typed {
		for _, a := range args {
			items = append(items, a.Type())
		}
		if len(kwargs) > 0 {
			keys := sortedKwargKeys(kwargs)
			for _, k := range keys {
				items = append(items, kwargs[k].Type())
			}
		}
	}
	return objects.NewTuple(items), nil
}

func sortedKwargKeys(m map[string]objects.Object) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// insertion-sort is fine: kwargs are small.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// lruCacheInfo is _lru_cache_wrapper.cache_info. Returns a tuple-like
// instance of cache_info_type with (hits, misses, maxsize, currsize).
//
// CPython: Modules/_functoolsmodule.c:1672 _functools__lru_cache_wrapper_cache_info_impl
func lruCacheInfo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	w := args[0].(*LruCacheWrapper)
	var maxsizeObj objects.Object
	if w.Unbounded {
		maxsizeObj = objects.None()
	} else {
		maxsizeObj = objects.NewInt(w.Maxsize)
	}
	callArgs := []objects.Object{
		objects.NewInt(w.Hits),
		objects.NewInt(w.Misses),
		maxsizeObj,
		objects.NewInt(int64(w.Cache.Len())),
	}
	return objects.Call(w.CacheInfoType, objects.NewTuple(callArgs), nil)
}

// lruCacheClear is _lru_cache_wrapper.cache_clear. Drops every entry
// and zeroes hits/misses. Mirrors the C version's unlink-and-clear.
//
// CPython: Modules/_functoolsmodule.c:1699 _functools__lru_cache_wrapper_cache_clear_impl
func lruCacheClear(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	w := args[0].(*LruCacheWrapper)
	w.Hits = 0
	w.Misses = 0
	// Reset list and dict.
	w.root.prev = w.root
	w.root.next = w.root
	// Drop every entry from the cache dict.
	for _, k := range w.Cache.Keys() {
		_ = w.Cache.DelItem(k)
	}
	return objects.None(), nil
}

// lruCacheCopy implements __copy__ / __deepcopy__: both return self.
//
// CPython: Modules/_functoolsmodule.c:1723 lru_cache_copy / lru_cache_deepcopy
func lruCacheCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return args[0], nil
}

// lruCacheDescrGet wires the wrapper as a descriptor so an
// @lru_cache-decorated method binds correctly. Mirrors
// lru_cache_descr_get.
//
// CPython: Modules/_functoolsmodule.c:1656 lru_cache_descr_get
func lruCacheDescrGet(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
	if owner == nil || objects.IsNone(owner) {
		return descr, nil
	}
	return objects.NewBoundMethod(descr, owner), nil
}

// lruCacheGetattr serves attribute reads on _lru_cache_wrapper
// instances. The per-instance __dict__ holds names set from Python
// land (e.g. wrapper.cache_parameters = lambda ... in lru_cache()),
// so we consult it before falling through to the type's descriptors.
//
// CPython: Objects/object.c:1932 PyObject_GenericGetAttr
func lruCacheGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	w := o.(*LruCacheWrapper)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	if n == "__dict__" {
		if w.Dict == nil {
			w.Dict = objects.NewDict()
		}
		return w.Dict, nil
	}
	if n == "__wrapped__" {
		return w.Func, nil
	}
	if w.Dict != nil {
		if v, err := w.Dict.GetItem(name); err == nil {
			return v, nil
		}
	}
	return objects.GenericGetAttr(o, name)
}

// lruCacheSetattr routes attribute stores to the per-instance dict
// (lazily allocated), so functools.lru_cache() can stash
// cache_parameters and similar metadata directly on the wrapper.
// value==nil signals delete.
//
// CPython: Objects/object.c:2024 PyObject_GenericSetAttr
func lruCacheSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	w := o.(*LruCacheWrapper)
	if w.Dict == nil {
		w.Dict = objects.NewDict()
	}
	if value == nil {
		return w.Dict.DelItem(name)
	}
	return w.Dict.SetItem(name, value)
}
