package objects

import "testing"

// TestListGrowthCurve pins the CPython overallocation table:
// new_allocated = (newsize + (newsize >> 3) + 6) & ~3 for the
// append-one-at-a-time path.
//
// CPython: Objects/listobject.c:55 list_resize
func TestListGrowthCurve(t *testing.T) {
	cases := []struct {
		oldsize, newsize, want int
	}{
		{0, 0, 0},
		{0, 1, 4},
		{4, 5, 8},
		{8, 9, 16},
		{16, 17, 24},
		{24, 25, 32},
		{32, 33, 40},
		{40, 41, 52},
		{52, 53, 64},
		{64, 65, 76},
	}
	for _, c := range cases {
		got := listGrowthCurve(c.oldsize, c.newsize)
		if got != c.want {
			t.Errorf("growthCurve(old=%d, new=%d) = %d, want %d",
				c.oldsize, c.newsize, got, c.want)
		}
	}
}

// TestListResizeShrinkPath pins that a shrink that drops below half
// the current allocation reallocates, while a small drop reuses the
// existing buffer.
func TestListResizeShrinkPath(t *testing.T) {
	l := NewList(make([]Object, 16))
	// Force a known allocation by extending and dropping.
	l.listResize(16)
	cap1 := cap(l.items)
	l.listResize(15) // small drop, should reuse buffer
	if cap(l.items) != cap1 {
		t.Errorf("small drop reallocated: cap %d -> %d", cap1, cap(l.items))
	}
	l.listResize(3) // drop below half, should reallocate smaller
	if cap(l.items) > cap1 {
		t.Errorf("shrink grew capacity: %d -> %d", cap1, cap(l.items))
	}
}

// TestListResizeKeepsItems pins that items survive a grow.
func TestListResizeKeepsItems(t *testing.T) {
	l := NewList([]Object{NewInt(1), NewInt(2), NewInt(3)})
	l.listResize(10)
	if l.Len() != 10 {
		t.Fatalf("Len = %d, want 10", l.Len())
	}
	for i, want := range []int64{1, 2, 3} {
		v, _ := l.Item(i).(*Int).Int64()
		if v != want {
			t.Errorf("items[%d] = %d, want %d", i, v, want)
		}
	}
}
