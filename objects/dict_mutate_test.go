package objects

import "testing"

func TestUsableFractionMatchesCpythonCurve(t *testing.T) {
	// USABLE_FRACTION(n) = 2n/3
	cases := map[int]int{8: 5, 16: 10, 32: 21, 64: 42, 128: 85}
	for n, want := range cases {
		if got := usableFraction(n); got != want {
			t.Errorf("usableFraction(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestNextDictPow2Floors(t *testing.T) {
	cases := map[int]int{0: 8, 1: 8, 7: 8, 8: 8, 9: 16, 16: 16, 17: 32, 33: 64}
	for n, want := range cases {
		if got := nextDictPow2(n); got != want {
			t.Errorf("nextDictPow2(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestGrowthRateIsThreeUsed(t *testing.T) {
	d := NewDict()
	for i := 0; i < 4; i++ {
		_ = d.SetItem(NewStr(string(rune('a'+i))), NewInt(int64(i)))
	}
	if got := growthRate(d); got != 12 {
		t.Errorf("growthRate = %d, want 12", got)
	}
}

func TestSetItemResizesAtTwoThirdsLoad(t *testing.T) {
	// dictMinSize=8 → load ceiling 5 (= 2*8/3). Sixth insert should grow.
	d := NewDict()
	for i := 0; i < 5; i++ {
		_ = d.SetItem(NewStr(string(rune('a'+i))), NewInt(int64(i)))
	}
	if len(d.entries) != 8 {
		t.Fatalf("after 5 inserts cap = %d, want 8", len(d.entries))
	}
	_ = d.SetItem(NewStr("f"), NewInt(5))
	if len(d.entries) <= 8 {
		t.Errorf("after 6th insert cap = %d, want > 8", len(d.entries))
	}
}

func TestDelItemLeavesDummySlot(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	idx, _, _ := d.lookup(mustHash(t, NewStr("a")), NewStr("a"))
	if err := d.DelItem(NewStr("a")); err != nil {
		t.Fatal(err)
	}
	if d.entries[idx].used {
		t.Error("deleted slot still marked used")
	}
	if !d.entries[idx].dummy {
		t.Error("deleted slot should carry dummy marker for probe-chain continuity")
	}
}

func TestDelItemMissingKey(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	if err := d.DelItem(NewStr("nope")); err != errKeyNotFound {
		t.Errorf("missing-key delete err = %v, want errKeyNotFound", err)
	}
}

func TestProbeChainSurvivesDeleteOfMiddleEntry(t *testing.T) {
	// A hash collision puts both keys in the same probe chain. Deleting
	// the first one must leave a dummy so the second one is still findable.
	d := NewDict()
	_ = d.SetItem(NewInt(0), NewStr("zero"))
	_ = d.SetItem(NewInt(8), NewStr("eight")) // hash 8 maps to slot 0 in an 8-slot table
	if err := d.DelItem(NewInt(0)); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetItem(NewInt(8))
	if err != nil {
		t.Fatalf("eight should still be findable through dummy slot: %v", err)
	}
	if v, _ := got.(*strStub); v == nil || v.v != "eight" {
		t.Errorf("got %v, want eight", got)
	}
}

func TestSetItemReusesDummySlot(t *testing.T) {
	// After delete + reinsert of the same key, the slot count stays the
	// same: the insert lands in the dummy, not in a fresh empty.
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.DelItem(NewStr("a"))
	_ = d.SetItem(NewStr("a"), NewInt(2))
	if d.used != 1 {
		t.Errorf("used = %d, want 1 after delete+reinsert", d.used)
	}
	got, err := d.GetItem(NewStr("a"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(*Int).Int64(); v != 2 {
		t.Errorf("got %d, want 2", v)
	}
}

func TestResizeCompactsDummies(t *testing.T) {
	// Fill close to load ceiling, churn deletes/inserts so dummies pile
	// up, then trigger one more insert that crosses the threshold.
	// Resize should rebuild without dummies.
	d := NewDict()
	for i := 0; i < 4; i++ {
		_ = d.SetItem(NewStr(string(rune('a'+i))), NewInt(int64(i)))
	}
	for i := 0; i < 4; i++ {
		_ = d.DelItem(NewStr(string(rune('a' + i))))
	}
	dummyCount := 0
	for _, e := range d.entries {
		if e.dummy {
			dummyCount++
		}
	}
	if dummyCount != 4 {
		t.Fatalf("expected 4 dummies after deletes, got %d", dummyCount)
	}
	// Force a resize by reinserting enough to cross the load ceiling.
	for i := 0; i < 6; i++ {
		_ = d.SetItem(NewStr(string(rune('A'+i))), NewInt(int64(i)))
	}
	for _, e := range d.entries {
		if e.dummy {
			t.Error("resize should compact away all dummy slots")
		}
	}
}

func TestResizePreservesAllLiveKeys(t *testing.T) {
	// Insert past several resize thresholds and verify every key
	// round-trips. The probing chain must rebuild correctly under the
	// new mask.
	d := NewDict()
	const N = 50
	for i := 0; i < N; i++ {
		_ = d.SetItem(NewInt(int64(i)), NewInt(int64(i*10)))
	}
	for i := 0; i < N; i++ {
		got, err := d.GetItem(NewInt(int64(i)))
		if err != nil {
			t.Fatalf("missing key %d: %v", i, err)
		}
		if v, _ := got.(*Int).Int64(); v != int64(i*10) {
			t.Errorf("key %d → %d, want %d", i, v, i*10)
		}
	}
}

func TestFillCountsDummiesUntilResize(t *testing.T) {
	// fill must include dummies so a delete-heavy dict still triggers
	// a resize once the probe chains get clogged. used drops on
	// delete; fill does not.
	d := NewDict()
	for i := 0; i < 5; i++ {
		_ = d.SetItem(NewStr(string(rune('a'+i))), NewInt(int64(i)))
	}
	if d.fill != 5 || d.used != 5 {
		t.Fatalf("after 5 inserts fill=%d used=%d, want 5/5", d.fill, d.used)
	}
	for i := 0; i < 5; i++ {
		_ = d.DelItem(NewStr(string(rune('a' + i))))
	}
	if d.fill != 5 || d.used != 0 {
		t.Errorf("after 5 deletes fill=%d used=%d, want 5/0", d.fill, d.used)
	}
}

func TestResizeShrinksAfterMassDeleteAndChurn(t *testing.T) {
	// Once fill crosses the load ceiling we resize, and growthRate is
	// 3*used (not 3*fill), so a dict whose live count has dropped far
	// below its allocated table compacts on the next resize-triggering
	// insert. With fill=5 in cap=8 → next insert triggers resize;
	// used=0 → growth target 0 → floored at dictMinSize=8.
	d := NewDict()
	for i := 0; i < 5; i++ {
		_ = d.SetItem(NewStr(string(rune('a'+i))), NewInt(int64(i)))
	}
	for i := 0; i < 5; i++ {
		_ = d.DelItem(NewStr(string(rune('a' + i))))
	}
	bigCap := len(d.entries)
	_ = d.SetItem(NewStr("z"), NewInt(0)) // fill=5+1=6 > usableFraction(8)=5
	if d.fill != 1 {
		t.Errorf("after resize+insert fill=%d, want 1 (dummies compacted)", d.fill)
	}
	if len(d.entries) > bigCap {
		t.Errorf("expected at most same-size resize, cap %d > %d", len(d.entries), bigCap)
	}
}

func TestLoadAtCapacityBoundary(t *testing.T) {
	// Used+1 must STAY <= 2n/3 to skip resize. Boundary of 5/8 (2*8/3=5):
	// used=4 → next insert (used=5) is fine. used=5 → next would push to 6.
	if loadAtCapacity(4, 8) {
		t.Error("4/8 should not trigger resize on next insert")
	}
	if !loadAtCapacity(5, 8) {
		t.Error("5/8 should trigger resize on next insert")
	}
}

func mustHash(t *testing.T, o Object) int64 {
	t.Helper()
	h, err := Hash(o)
	if err != nil {
		t.Fatalf("Hash(%v) failed: %v", o, err)
	}
	return h
}
