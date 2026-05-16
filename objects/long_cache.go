// Small-int cache for -5..256. CPython preallocates these as
// singletons so common literals (loop counters, indices, booleans
// converted to int) skip the allocator and compare cheaply by
// pointer.
//
// CPython: Objects/longobject.c:19 _PyLong_SMALL_INTS

package objects

import "math/big"

const (
	// smallIntMin is the lower bound of the preallocated int cache.
	//
	// CPython: Include/internal/pycore_global_objects.h:35 _PY_NSMALLNEGINTS
	smallIntMin = -5
	// smallIntMax is the upper bound (inclusive) of the preallocated int cache.
	//
	// CPython: Include/internal/pycore_global_objects.h:36 _PY_NSMALLPOSINTS
	smallIntMax = 256
)

// smallInts is the cache slot table. Populated by initSmallInts so
// the singletons exist before any concrete Int is constructed.
//
// CPython: Objects/longobject.c:19 _PyLong_SMALL_INTS
var smallInts [smallIntMax - smallIntMin + 1]*Int

// initSmallInts fills the cache. Called from int.go's init() because
// it depends on IntType being constructed (the slot wiring there must
// run first).
//
// CPython: Objects/longobject.c:6209 _PyLong_Init
func initSmallInts() {
	for i := range smallInts {
		o := &Int{}
		o.init(IntType)
		o.v.SetInt64(int64(smallIntMin + i))
		smallInts[i] = o
	}
}

// SmallInt returns the cached singleton at offset into the small-int
// table. Mirrors CPython's `_PyLong_SMALL_INTS[offset]` lookup used
// by LOAD_SMALL_INT (offset = _PY_NSMALLNEGINTS + oparg).
//
// CPython: Objects/longobject.c:19 _PyLong_SMALL_INTS
func SmallInt(offset int) Object {
	if offset < 0 || offset >= len(smallInts) {
		return nil
	}
	return smallInts[offset]
}

// smallIntFromInt64 returns the cached singleton for x, or nil if x
// is outside the cache window. Callers fall back to allocating a
// fresh Int.
//
// CPython: Objects/longobject.c:323 PyLong_FromLong (small-int branch)
func smallIntFromInt64(x int64) *Int {
	if x < smallIntMin || x > smallIntMax {
		return nil
	}
	return smallInts[x-smallIntMin]
}

// smallIntFromBig returns the cached singleton for b when it fits
// the int64-and-small-int window.
//
// CPython: Objects/longobject.c:156 _PyLong_FromBytes (small-int branch)
func smallIntFromBig(b *big.Int) *Int {
	if !b.IsInt64() {
		return nil
	}
	return smallIntFromInt64(b.Int64())
}
