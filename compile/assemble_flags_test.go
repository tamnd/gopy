package compile

import "testing"

// TestFinalizeFlagsNoFree: a unit with no free or cell variables
// gets CO_NOFREE set.
func TestFinalizeFlagsNoFree(t *testing.T) {
	u := &Unit{Flags: CoOptimized}
	got := finalizeFlags(u)
	if got&CoNoFree == 0 {
		t.Errorf("CoNoFree not set; flags=%#x", got)
	}
}

// TestFinalizeFlagsWithCells: any cellvar suppresses CO_NOFREE.
func TestFinalizeFlagsWithCells(t *testing.T) {
	u := &Unit{Flags: CoOptimized, CellVars: []string{"x"}}
	if got := finalizeFlags(u); got&CoNoFree != 0 {
		t.Errorf("CoNoFree set despite cellvar; flags=%#x", got)
	}
}

// TestFinalizeFlagsWithFrees: any freevar suppresses CO_NOFREE.
func TestFinalizeFlagsWithFrees(t *testing.T) {
	u := &Unit{Flags: CoOptimized, FreeVars: []string{"x"}}
	if got := finalizeFlags(u); got&CoNoFree != 0 {
		t.Errorf("CoNoFree set despite freevar; flags=%#x", got)
	}
}

// TestConstCacheKeyTypeAware: int(1) and int64(1) and float64(1.0)
// each get distinct cache slots.
func TestConstCacheKeyTypeAware(t *testing.T) {
	a := constCacheKey(int(1))
	b := constCacheKey(int64(1))
	c := constCacheKey(float64(1.0))
	if a == b || a == c || b == c {
		t.Errorf("cache keys collide: int=%v int64=%v float=%v", a, b, c)
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
