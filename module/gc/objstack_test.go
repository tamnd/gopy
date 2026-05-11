// Tests for the _PyObjectStack port. Drives push/pop across a
// chunk boundary to make sure chunk allocation and unlink behave,
// then exercises clear/merge/size.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestObjectStackPushPop(t *testing.T) {
	var s objectStack
	if got := s.pop(); got != nil {
		t.Fatalf("empty pop = %v, want nil", got)
	}
	a := objects.NewStr("a")
	b := objects.NewStr("b")
	s.push(a)
	s.push(b)
	if got := s.size(); got != 2 {
		t.Fatalf("size = %d, want 2", got)
	}
	if got := s.pop(); got != b {
		t.Fatalf("pop = %v, want %v", got, b)
	}
	if got := s.pop(); got != a {
		t.Fatalf("pop = %v, want %v", got, a)
	}
	if got := s.pop(); got != nil {
		t.Fatalf("drained pop = %v, want nil", got)
	}
}

func TestObjectStackChunkBoundary(t *testing.T) {
	var s objectStack
	n := objectStackChunkSize + 5
	want := make([]objects.Object, 0, n)
	for range n {
		o := objects.NewStr("v")
		want = append(want, o)
		s.push(o)
	}
	if got := s.size(); got != n {
		t.Fatalf("size = %d, want %d", got, n)
	}
	for i := n - 1; i >= 0; i-- {
		got := s.pop()
		if got != want[i] {
			t.Fatalf("pop %d: got %v, want %v", i, got, want[i])
		}
	}
	if s.size() != 0 {
		t.Fatalf("size after drain = %d, want 0", s.size())
	}
}

func TestObjectStackClear(t *testing.T) {
	var s objectStack
	for range 10 {
		s.push(objects.NewStr("x"))
	}
	s.clear()
	if s.size() != 0 {
		t.Fatalf("size after clear = %d, want 0", s.size())
	}
	if s.pop() != nil {
		t.Fatal("pop after clear should be nil")
	}
}

func TestObjectStackMergeFromEmpty(t *testing.T) {
	var dst, src objectStack
	dst.push(objects.NewStr("a"))
	dst.merge(&src)
	if dst.size() != 1 {
		t.Fatalf("dst size = %d, want 1", dst.size())
	}
}

func TestObjectStackMerge(t *testing.T) {
	var dst, src objectStack
	dst.push(objects.NewStr("d1"))
	dst.push(objects.NewStr("d2"))

	src.push(objects.NewStr("s1"))
	src.push(objects.NewStr("s2"))
	src.push(objects.NewStr("s3"))

	dst.merge(&src)

	if dst.size() != 5 {
		t.Fatalf("dst size = %d, want 5", dst.size())
	}
	if src.size() != 0 {
		t.Fatalf("src size = %d, want 0", src.size())
	}
	if src.head != nil {
		t.Fatal("src.head must be nil after merge")
	}
}
