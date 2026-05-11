// Tests for the gc_list_* port. Mirrors the operations exercised by
// CPython's collector entry points: append at tail, remove from
// middle, move between lists, merge, size, and clear-collecting.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func collect(list *gcHead) []objects.Object {
	out := []objects.Object{}
	for g := list.next; g != list; g = g.next {
		out = append(out, g.obj)
	}
	return out
}

func TestListInitIsEmpty(t *testing.T) {
	h := newListHead()
	if !listIsEmpty(h) {
		t.Fatal("fresh head must be empty")
	}
	if h.next != h || h.prev != h {
		t.Fatal("fresh head must point at itself")
	}
}

func TestListAppendTail(t *testing.T) {
	h := newListHead()
	a := &gcHead{obj: objects.NewStr("a")}
	b := &gcHead{obj: objects.NewStr("b")}
	c := &gcHead{obj: objects.NewStr("c")}
	listAppend(a, h)
	listAppend(b, h)
	listAppend(c, h)
	if listIsEmpty(h) {
		t.Fatal("list became empty after appends")
	}
	if got, want := listSize(h), 3; got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}
	got := collect(h)
	if len(got) != 3 || got[0] != a.obj || got[1] != b.obj || got[2] != c.obj {
		t.Fatalf("order wrong: %v", got)
	}
}

func TestListRemove(t *testing.T) {
	h := newListHead()
	a := &gcHead{obj: objects.NewStr("a")}
	b := &gcHead{obj: objects.NewStr("b")}
	c := &gcHead{obj: objects.NewStr("c")}
	listAppend(a, h)
	listAppend(b, h)
	listAppend(c, h)
	listRemove(b)
	if listSize(h) != 2 {
		t.Fatalf("size after remove = %d, want 2", listSize(h))
	}
	got := collect(h)
	if got[0] != a.obj || got[1] != c.obj {
		t.Fatalf("after remove(b): %v", got)
	}
	if b.prev != nil || b.next != nil {
		t.Fatal("removed node must have nil prev/next")
	}
}

func TestListMove(t *testing.T) {
	src := newListHead()
	dst := newListHead()
	a := &gcHead{obj: objects.NewStr("a")}
	b := &gcHead{obj: objects.NewStr("b")}
	listAppend(a, src)
	listAppend(b, src)
	listMove(a, dst)
	if listSize(src) != 1 || listSize(dst) != 1 {
		t.Fatalf("sizes = %d/%d, want 1/1", listSize(src), listSize(dst))
	}
	if collect(dst)[0] != a.obj {
		t.Fatal("moved node should sit on dst")
	}
	if collect(src)[0] != b.obj {
		t.Fatal("source should still hold b")
	}
}

func TestListMerge(t *testing.T) {
	from := newListHead()
	to := newListHead()
	a := &gcHead{obj: objects.NewStr("a")}
	b := &gcHead{obj: objects.NewStr("b")}
	c := &gcHead{obj: objects.NewStr("c")}
	listAppend(a, to)
	listAppend(b, from)
	listAppend(c, from)
	listMerge(from, to)
	if !listIsEmpty(from) {
		t.Fatal("from must be empty after merge")
	}
	if listSize(to) != 3 {
		t.Fatalf("to size = %d, want 3", listSize(to))
	}
	got := collect(to)
	if got[0] != a.obj || got[1] != b.obj || got[2] != c.obj {
		t.Fatalf("merge order: %v", got)
	}
}

func TestListMergeEmptyFrom(t *testing.T) {
	from := newListHead()
	to := newListHead()
	listAppend(&gcHead{obj: objects.NewStr("a")}, to)
	listMerge(from, to)
	if listSize(to) != 1 {
		t.Fatalf("to size = %d, want 1", listSize(to))
	}
}

func TestListClearCollecting(t *testing.T) {
	h := newListHead()
	a := &gcHead{obj: objects.NewStr("a"), flags: gcCollecting | gcFinalized}
	b := &gcHead{obj: objects.NewStr("b"), flags: gcCollecting}
	listAppend(a, h)
	listAppend(b, h)
	listClearCollecting(h)
	if a.flags&gcCollecting != 0 || b.flags&gcCollecting != 0 {
		t.Fatal("listClearCollecting must drop the COLLECTING bit")
	}
	if a.flags&gcFinalized == 0 {
		t.Fatal("FINALIZED bit must be preserved")
	}
}
