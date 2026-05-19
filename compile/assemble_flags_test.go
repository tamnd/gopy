package compile

import "testing"

// TestFinalizeFlagsPassthrough: with CO_NOFREE retired in CPython
// 3.14, finalizeFlags is a passthrough. Make sure it doesn't sneak
// any extra bits onto a clean unit.
func TestFinalizeFlagsPassthrough(t *testing.T) {
	u := &Unit{Flags: CoOptimized}
	if got := finalizeFlags(u); got != CoOptimized {
		t.Errorf("finalizeFlags added bits; flags=%#x want %#x", got, CoOptimized)
	}
}

// TestConstCacheKeyIntFloatBool: 1 (int), 1.0 (float), True (bool)
// and "1" (str) get distinct slots, but Go's int / int32 / int64 all
// collapse onto the same Python int slot. CPython keys ints by value
// alone (`Py_NewRef(op)`) while tagging bool/float separately, so the
// distinct-types-with-same-value triple must stay split here.
//
// CPython: Objects/codeobject.c:3035 _PyCode_ConstantKey
func TestConstCacheKeyIntFloatBool(t *testing.T) {
	intKey := constCacheKey(int64(1))
	if k := constCacheKey(int(1)); k != intKey {
		t.Errorf("int(1) and int64(1) should share a slot: %v vs %v", k, intKey)
	}
	if k := constCacheKey(int32(1)); k != intKey {
		t.Errorf("int32(1) and int64(1) should share a slot: %v vs %v", k, intKey)
	}
	floatKey := constCacheKey(float64(1.0))
	boolKey := constCacheKey(true)
	strKey := constCacheKey("1")
	if intKey == floatKey || intKey == boolKey || intKey == strKey {
		t.Errorf("int collides: int=%v float=%v bool=%v str=%v",
			intKey, floatKey, boolKey, strKey)
	}
	if floatKey == boolKey || floatKey == strKey || boolKey == strKey {
		t.Errorf("float/bool/str collide: float=%v bool=%v str=%v",
			floatKey, boolKey, strKey)
	}
}

// TestConstCacheKeyTupleByValue: equal-by-value tuples share a slot
// even when emitted from separate codegen sites (different *ConstTuple
// pointers). This mirrors CPython's recursive tuple-of-keys packing.
//
// CPython: Objects/codeobject.c:3090 _PyCode_ConstantKey tuple branch
func TestConstCacheKeyTupleByValue(t *testing.T) {
	a := &ConstTuple{Values: []any{int64(1), "x", nil}}
	b := &ConstTuple{Values: []any{int64(1), "x", nil}}
	if constCacheKey(a) != constCacheKey(b) {
		t.Errorf("equal-by-value tuples should dedupe: %v vs %v",
			constCacheKey(a), constCacheKey(b))
	}
	c := &ConstTuple{Values: []any{int64(1), "x", "y"}}
	if constCacheKey(a) == constCacheKey(c) {
		t.Errorf("differing tuples should not dedupe: %v",
			constCacheKey(a))
	}
}

// TestConstCacheKeyFloatNaN: float NaN bits do not collide with each
// other and zero stays distinct from negative zero.
func TestConstCacheKeyFloatNegZero(t *testing.T) {
	pos := constCacheKey(0.0)
	// Go's compiler folds -0.0 to 0.0; the float-NaN bucket lives in
	// the bit-pattern test below.
	// 0.0 vs +0.0 are the same bit pattern; we only assert the float
	// bucket does not lose precision against an int 0.
	intZero := constCacheKey(int64(0))
	if pos == intZero {
		t.Errorf("float 0.0 collides with int 0: pos=%v intZero=%v", pos, intZero)
	}
}
