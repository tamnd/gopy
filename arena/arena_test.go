package arena

import (
	"testing"
	"unsafe"
)

func TestMallocZero(t *testing.T) {
	a := New()
	if got := a.Malloc(0); got != nil {
		t.Fatalf("Malloc(0) = %v, want nil", got)
	}
	if a.cur.off != 0 {
		t.Fatalf("Malloc(0) advanced offset to %d", a.cur.off)
	}
}

func TestMallocAlignment(t *testing.T) {
	a := New()
	for _, n := range []int{1, 7, 8, 9, 16, 17, 100} {
		before := a.cur.off
		a.Malloc(n)
		got := a.cur.off - before
		want := roundUp(n, alignment)
		if got != want {
			t.Fatalf("Malloc(%d) advanced offset by %d, want %d", n, got, want)
		}
	}
}

func TestMallocSliceCapped(t *testing.T) {
	a := New()
	first := a.Malloc(8)
	if cap(first) != len(first) {
		t.Fatalf("returned slice cap %d != len %d", cap(first), len(first))
	}
}

func TestMallocNonOverlap(t *testing.T) {
	a := New()
	x := a.Malloc(16)
	y := a.Malloc(16)
	if &x[0] == &y[0] {
		t.Fatal("two allocations share an address")
	}
	if uintptr(unsafe.Pointer(&y[0]))-uintptr(unsafe.Pointer(&x[0])) < 16 {
		t.Fatal("allocations overlap")
	}
}

func TestMallocLargerThanBlock(t *testing.T) {
	a := New()
	big := a.Malloc(defaultBlockSize * 3)
	if len(big) != defaultBlockSize*3 {
		t.Fatalf("len(big) = %d, want %d", len(big), defaultBlockSize*3)
	}
	// The big allocation lives in its own block, linked after the
	// initial empty one.
	if a.head == a.cur {
		t.Fatal("expected a new block to be linked")
	}
}

func TestMallocGrowsBlockList(t *testing.T) {
	a := New()
	for range 4 * defaultBlockSize / 8 {
		a.Malloc(8)
	}
	count := 0
	for b := a.head; b != nil; b = b.next {
		count++
	}
	if count < 2 {
		t.Fatalf("expected multiple blocks, got %d", count)
	}
}

func TestAddObjectRetains(t *testing.T) {
	a := New()
	type box struct{ x int }
	b := &box{x: 42}
	if err := a.AddObject(b); err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	if len(a.objects) != 1 {
		t.Fatalf("len(objects) = %d, want 1", len(a.objects))
	}
	if a.objects[0].(*box) != b {
		t.Fatal("AddObject did not retain the original pointer")
	}
}

func TestFreeZeroesArena(t *testing.T) {
	a := New()
	a.Malloc(64)
	_ = a.AddObject("x")
	a.Free()
	if a.head != nil || a.cur != nil || a.objects != nil {
		t.Fatal("Free did not clear arena")
	}
}

func TestRoundUp(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0}, {1, 8}, {7, 8}, {8, 8}, {9, 16}, {15, 16}, {16, 16},
	}
	for _, c := range cases {
		if got := roundUp(c.in, alignment); got != c.want {
			t.Errorf("roundUp(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
