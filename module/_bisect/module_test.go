package _bisect

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// makeIntList builds a *objects.List from a slice of int64 values.
func makeIntList(vals []int64) *objects.List {
	items := make([]objects.Object, len(vals))
	for i, v := range vals {
		items[i] = objects.NewInt(v)
	}
	return objects.NewList(items)
}

// TestBisectLeftFindsCorrectIndex checks that bisect_left returns the
// leftmost position where x would be inserted in a sorted list.
func TestBisectLeftFindsCorrectIndex(t *testing.T) {
	a := makeIntList([]int64{1, 3, 3, 5, 7})
	tests := []struct {
		x    int64
		want int64
	}{
		{0, 0},  // before all
		{1, 0},  // leftmost for existing element
		{3, 1},  // leftmost of duplicate 3s
		{4, 3},  // between 3 and 5
		{5, 3},  // exact match
		{8, 5},  // after all
	}
	for _, tc := range tests {
		got, err := bisectLeft([]objects.Object{a, objects.NewInt(tc.x)}, nil)
		if err != nil {
			t.Fatalf("bisect_left(%d): %v", tc.x, err)
		}
		idx, _ := got.(*objects.Int).Int64()
		if idx != tc.want {
			t.Errorf("bisect_left(%d) = %d, want %d", tc.x, idx, tc.want)
		}
	}
}

// TestBisectRightFindsCorrectIndex checks that bisect_right returns the
// rightmost position for insertion.
func TestBisectRightFindsCorrectIndex(t *testing.T) {
	a := makeIntList([]int64{1, 3, 3, 5, 7})
	tests := []struct {
		x    int64
		want int64
	}{
		{0, 0},
		{1, 1},
		{3, 3}, // rightmost past all 3s
		{4, 3},
		{5, 4},
		{8, 5},
	}
	for _, tc := range tests {
		got, err := bisectRight([]objects.Object{a, objects.NewInt(tc.x)}, nil)
		if err != nil {
			t.Fatalf("bisect_right(%d): %v", tc.x, err)
		}
		idx, _ := got.(*objects.Int).Int64()
		if idx != tc.want {
			t.Errorf("bisect_right(%d) = %d, want %d", tc.x, idx, tc.want)
		}
	}
}

// TestInsortLeftKeepsSorted inserts into a sorted list and verifies the
// list remains sorted with the new element at the leftmost valid position.
func TestInsortLeftKeepsSorted(t *testing.T) {
	a := makeIntList([]int64{1, 3, 5})
	if _, err := insortLeft([]objects.Object{a, objects.NewInt(3)}, nil); err != nil {
		t.Fatalf("insort_left: %v", err)
	}
	if a.Len() != 4 {
		t.Fatalf("expected length 4, got %d", a.Len())
	}
	// New 3 should be at index 1 (left of existing 3).
	v, _ := a.Item(1).(*objects.Int).Int64()
	if v != 3 {
		t.Errorf("a[1] = %d, want 3", v)
	}
	// Verify full order.
	expected := []int64{1, 3, 3, 5}
	for i, want := range expected {
		got, _ := a.Item(i).(*objects.Int).Int64()
		if got != want {
			t.Errorf("a[%d] = %d, want %d", i, got, want)
		}
	}
}
