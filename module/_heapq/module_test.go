package _heapq

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestHeappushHeappop verifies a basic push/pop round-trip: items pushed
// in arbitrary order come back in ascending order.
func TestHeappushHeappop(t *testing.T) {
	heap := objects.NewList(nil)

	vals := []int64{5, 3, 8, 1, 4}
	for _, v := range vals {
		_, err := heappush([]objects.Object{heap, objects.NewInt(v)}, nil)
		if err != nil {
			t.Fatalf("heappush(%d): %v", v, err)
		}
	}

	expected := []int64{1, 3, 4, 5, 8}
	for _, want := range expected {
		got, err := heappop([]objects.Object{heap}, nil)
		if err != nil {
			t.Fatalf("heappop: %v", err)
		}
		i, ok := got.(*objects.Int)
		if !ok {
			t.Fatalf("heappop returned non-int: %T", got)
		}
		v, _ := i.Int64()
		if v != want {
			t.Errorf("heappop got %d, want %d", v, want)
		}
	}
}

// TestHeapify verifies that heapify turns an arbitrary list into a valid
// min-heap: the minimum element must be at index 0 after heapify.
func TestHeapify(t *testing.T) {
	items := []objects.Object{
		objects.NewInt(9),
		objects.NewInt(2),
		objects.NewInt(7),
		objects.NewInt(1),
		objects.NewInt(5),
	}
	heap := objects.NewList(items)
	_, err := heapify([]objects.Object{heap}, nil)
	if err != nil {
		t.Fatalf("heapify: %v", err)
	}
	top := heap.Item(0).(*objects.Int)
	v, _ := top.Int64()
	if v != 1 {
		t.Errorf("after heapify, heap[0] = %d, want 1", v)
	}
}

// TestHeapreplace verifies that heapreplace returns the old root and
// places the new item correctly.
func TestHeapreplace(t *testing.T) {
	heap := objects.NewList(nil)
	for _, v := range []int64{1, 3, 5} {
		if _, err := heappush([]objects.Object{heap, objects.NewInt(v)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	old, err := heapreplace([]objects.Object{heap, objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatalf("heapreplace: %v", err)
	}
	ov, _ := old.(*objects.Int).Int64()
	if ov != 1 {
		t.Errorf("heapreplace returned %d, want 1", ov)
	}
	// New root should be 2.
	top, _ := heap.Item(0).(*objects.Int).Int64()
	if top != 2 {
		t.Errorf("heap[0] after heapreplace = %d, want 2", top)
	}
}
