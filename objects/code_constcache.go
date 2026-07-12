package objects

import (
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// constCache merges equal constants across every code object produced
// by a single compile() call. CPython keeps a per-compile dict
// (compiler.c_const_cache) and routes every constant, plus each code
// object's co_consts / co_names / co_linetable / co_exceptiontable /
// co_code through it, so two functions with identical constants or
// identical location tables share one object. gopy materializes
// constants late (the lift from compile.Code to objects.Code), so the
// merge runs there instead: InternCodeConstants walks the freshly
// lifted code tree once and interns each piece through a shared cache.
//
// CPython: Python/compile.c:98 c_const_cache
// CPython: Python/compile.c:318 const_cache_insert
type constCache struct {
	byKey map[string]Object
}

// InternCodeConstants merges the constant / location-table objects of a
// freshly lifted code tree so co_consts, co_linetable, co_exceptiontable
// and co_filename return stable, shared objects. The whole tree compiled
// in one compile() call shares one cache, matching CPython's per-compile
// const_cache; callers invoke this once on the top-level code object.
//
// CPython: Python/compile.c:1707 (const_cache created per compile, then
// every makecode merges its tables through it).
func InternCodeConstants(top *Code) {
	if top == nil {
		return
	}
	cc := &constCache{byKey: make(map[string]Object)}
	cc.internCode(top, nil)
}

// internCode interns one code object's tables in place. filenameObj is
// the already-interned co_filename of the enclosing code, threaded down
// so every nested code object shares one filename object exactly as
// CPython passes a single filename PyObject to each makecode call.
func (cc *constCache) internCode(c *Code, filenameObj Object) {
	if filenameObj == nil {
		filenameObj = cc.intern(NewStr(c.Filename))
	}
	// Pin each cached table on the code object. co_filename / co_linetable
	// / co_exceptiontable / co_consts are un-counted Go fields, so a merged
	// object shared across two code units would otherwise sit at a refcount
	// that the first transient Decref drops to zero, tearing the storage
	// down while a later co_consts read still points at it. CPython holds
	// each of these as a counted reference for the code's lifetime; the
	// Incref here is that reference (mirrors SyncConstObjs pinning the
	// per-code consts tuple).
	c.filenameObj = filenameObj
	Incref(filenameObj)
	c.linetableObj = cc.intern(NewBytes(c.Linetable))
	Incref(c.linetableObj)
	c.exceptiontableObj = cc.intern(NewBytes(c.ExceptionTable))
	Incref(c.exceptiontableObj)

	items := make([]Object, len(c.Consts))
	for i, v := range c.Consts {
		obj := wrapConstAttr(v)
		if nested, ok := obj.(*Code); ok {
			cc.internCode(nested, filenameObj)
		}
		items[i] = cc.intern(obj)
	}
	if t, ok := cc.intern(NewTuple(items)).(*Tuple); ok {
		c.constsObj = t
		Incref(t)
	}
}

// intern returns the canonical object for o, merging equal constants.
// Recurses into tuples and frozensets so their elements are interned
// too, mirroring const_cache_insert's recursive=true mode.
//
// CPython: Python/compile.c:318 const_cache_insert
func (cc *constCache) intern(o Object) Object {
	// None and Ellipsis are singletons; no key needed.
	if IsNone(o) {
		return o
	}
	if _, ok := o.(*ellipsisObject); ok {
		return o
	}
	switch x := o.(type) {
	case *Tuple:
		items := make([]Object, x.Len())
		for i := 0; i < x.Len(); i++ {
			items[i] = cc.intern(x.Item(i))
		}
		merged := NewTuple(items)
		key := constKey(merged)
		if v, ok := cc.byKey[key]; ok {
			return v
		}
		cc.byKey[key] = merged
		return merged
	case *Set:
		if x.frozen {
			items := x.Items()
			interned := make([]Object, len(items))
			for i, it := range items {
				interned[i] = cc.intern(it)
			}
			key := frozensetKey(interned)
			if v, ok := cc.byKey[key]; ok {
				return v
			}
			// Rebuild the frozenset so its elements are the canonical
			// interned objects: the test relies on a tuple stored both as
			// a standalone const and as a frozenset member being one
			// object. const_cache_insert merges recursively, so the
			// member must point at the same interned tuple.
			merged, err := NewFrozenset(interned)
			if err != nil {
				cc.byKey[key] = o
				return o
			}
			cc.byKey[key] = merged
			return merged
		}
	}
	key := constKey(o)
	if v, ok := cc.byKey[key]; ok {
		return v
	}
	cc.byKey[key] = o
	return o
}

// constKey builds a structural key that distinguishes constants the way
// CPython's _PyCode_ConstantKey does: by exact type and value, so
// True / 1 / 1.0 never collapse, -0.0 differs from 0.0, and NaN keys on
// its bit pattern. Containers recurse on element keys.
//
// CPython: Objects/codeobject.c:2634 _PyCode_ConstantKey
func constKey(o Object) string {
	switch x := o.(type) {
	case *ellipsisObject:
		return "..."
	case *Bool:
		if x.Sign() != 0 {
			return "bool:1"
		}
		return "bool:0"
	case *Int:
		return "int:" + x.BigInt().String()
	case *Float:
		return "float:" + strconv.FormatUint(math.Float64bits(x.Float64()), 16)
	case *Complex:
		return "complex:" + strconv.FormatUint(math.Float64bits(x.Real()), 16) +
			":" + strconv.FormatUint(math.Float64bits(x.Imag()), 16)
	case *Unicode:
		return "str:" + x.Value()
	case *Bytes:
		return "bytes:" + string(x.Bytes())
	case *Tuple:
		parts := make([]string, x.Len())
		for i := 0; i < x.Len(); i++ {
			parts[i] = constKey(x.Item(i))
		}
		return "tuple(" + strings.Join(parts, ",") + ")"
	case *Set:
		if x.frozen {
			return frozensetKey(x.Items())
		}
	}
	if IsNone(o) {
		return "None"
	}
	// Code objects and anything else are never merged: key on identity.
	return "id:" + strconv.FormatUint(uint64(reflect.ValueOf(o).Pointer()), 16)
}

// frozensetKey is order-independent so {0, 1} and {1, 0} collapse.
func frozensetKey(items []Object) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = constKey(it)
	}
	sort.Strings(parts)
	return "frozenset{" + strings.Join(parts, ",") + "}"
}
