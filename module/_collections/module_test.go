// Tests for the _collections accelerator. Exercises deque, defaultdict,
// _tuplegetter, and _count_elements through the Go API.

package _collections

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// ---------------------------------------------------------------------------
// deque tests.
// ---------------------------------------------------------------------------

func TestDequeAppendAndPop(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	dequeAppendRight(dq, objects.NewInt(1))
	dequeAppendRight(dq, objects.NewInt(2))
	dequeAppendRight(dq, objects.NewInt(3))
	if dequeLen(dq) != 3 {
		t.Fatalf("want len 3, got %d", dequeLen(dq))
	}
	v, _ := dequePopMethod([]objects.Object{dq}, nil)
	n, _ := v.(*objects.Int).Int64()
	if n != 3 {
		t.Fatalf("pop: want 3, got %d", n)
	}
}

func TestDequeAppendLeftAndPopLeft(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	dequeAppendLeft(dq, objects.NewInt(1))
	dequeAppendLeft(dq, objects.NewInt(2))
	v, _ := dequePopLeftMethod([]objects.Object{dq}, nil)
	n, _ := v.(*objects.Int).Int64()
	if n != 2 {
		t.Fatalf("popleft: want 2, got %d", n)
	}
}

func TestDequeMaxlen(t *testing.T) {
	d, _ := dequeNew(DequeType, []objects.Object{
		objects.NewList(nil),
		objects.NewInt(3),
	}, nil)
	dq := d.(*dequeObject)
	for i := int64(0); i < 5; i++ {
		dequeAppendRight(dq, objects.NewInt(i))
	}
	if int64(dequeLen(dq)) != 3 {
		t.Fatalf("maxlen: want 3, got %d", dequeLen(dq))
	}
	// Leftmost should be 2.
	first := dq.items[dq.head]
	n, _ := first.(*objects.Int).Int64()
	if n != 2 {
		t.Fatalf("maxlen first item: want 2, got %d", n)
	}
}

func TestDequeRotate(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	for _, v := range []int64{1, 2, 3, 4, 5} {
		dequeAppendRight(dq, objects.NewInt(v))
	}
	dequeRotate(dq, 2)
	// After rotate(2): [4, 5, 1, 2, 3]
	expected := []int64{4, 5, 1, 2, 3}
	items := dq.items[dq.head:dq.tail]
	for i, item := range items {
		n, _ := item.(*objects.Int).Int64()
		if n != expected[i] {
			t.Fatalf("rotate[%d]: want %d, got %d", i, expected[i], n)
		}
	}
}

func TestDequeReverse(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	for _, v := range []int64{1, 2, 3} {
		dequeAppendRight(dq, objects.NewInt(v))
	}
	dequeReverseMethod([]objects.Object{dq}, nil)
	expected := []int64{3, 2, 1}
	items := dq.items[dq.head:dq.tail]
	for i, item := range items {
		n, _ := item.(*objects.Int).Int64()
		if n != expected[i] {
			t.Fatalf("reverse[%d]: want %d, got %d", i, expected[i], n)
		}
	}
}

func TestDequeCount(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	for _, v := range []int64{1, 2, 1, 3, 1} {
		dequeAppendRight(dq, objects.NewInt(v))
	}
	v, err := dequeCountMethod([]objects.Object{dq, objects.NewInt(1)}, nil)
	if err != nil {
		t.Fatalf("count error: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 3 {
		t.Fatalf("count(1): want 3, got %d", n)
	}
}

func TestDequeIterator(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	for _, v := range []int64{10, 20, 30} {
		dequeAppendRight(dq, objects.NewInt(v))
	}
	it := newDequeIter(dq)
	expected := []int64{10, 20, 30}
	for _, want := range expected {
		v, err := dequeIterNext(it)
		if err != nil {
			t.Fatalf("iter next: %v", err)
		}
		n, _ := v.(*objects.Int).Int64()
		if n != want {
			t.Fatalf("iter: want %d, got %d", want, n)
		}
	}
	_, err := dequeIterNext(it)
	if !errors.Is(err, objects.ErrStopIteration) {
		t.Fatalf("want StopIteration, got %v", err)
	}
}

func TestDequeReverseIterator(t *testing.T) {
	d, _ := dequeNew(DequeType, nil, nil)
	dq := d.(*dequeObject)
	for _, v := range []int64{10, 20, 30} {
		dequeAppendRight(dq, objects.NewInt(v))
	}
	it := newDequeRevIter(dq)
	expected := []int64{30, 20, 10}
	for _, want := range expected {
		v, err := dequeRevIterNext(it)
		if err != nil {
			t.Fatalf("reviter next: %v", err)
		}
		n, _ := v.(*objects.Int).Int64()
		if n != want {
			t.Fatalf("reviter: want %d, got %d", want, n)
		}
	}
	_, err := dequeRevIterNext(it)
	if !errors.Is(err, objects.ErrStopIteration) {
		t.Fatalf("want StopIteration, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// defaultdict tests.
// ---------------------------------------------------------------------------

func TestDefaultDictMissingCallsFactory(t *testing.T) {
	listFactory := objects.NewBuiltinFunction("list", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewList(nil), nil
	})
	dd, err := defaultDictNew(DefaultDictType, []objects.Object{listFactory}, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	v, err := defaultDictGetItem(dd, objects.NewStr("key"))
	if err != nil {
		t.Fatalf("getitem: %v", err)
	}
	if _, ok := v.(*objects.List); !ok {
		t.Fatalf("want *List, got %T", v)
	}
}

func TestDefaultDictMissingRaisesKeyErrorWhenNoFactory(t *testing.T) {
	dd, _ := defaultDictNew(DefaultDictType, nil, nil)
	_, err := defaultDictGetItem(dd, objects.NewStr("missing"))
	if err == nil {
		t.Fatal("want KeyError, got nil")
	}
}

// ---------------------------------------------------------------------------
// _tuplegetter tests.
// ---------------------------------------------------------------------------

func TestTupleGetterDescr(t *testing.T) {
	tg, err := tupleGetterNew(TupleGetterType, []objects.Object{
		objects.NewInt(1),
		objects.NewStr("second element"),
	}, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tup := objects.NewTuple([]objects.Object{
		objects.NewInt(10),
		objects.NewInt(20),
		objects.NewInt(30),
	})
	v, err := tupleGetterDescrGet(tg, tup, nil)
	if err != nil {
		t.Fatalf("descr_get: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 20 {
		t.Fatalf("want 20, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// _count_elements tests.
// ---------------------------------------------------------------------------

func TestCountElements(t *testing.T) {
	mapping := objects.NewDict()
	items := objects.NewList([]objects.Object{
		objects.NewStr("a"),
		objects.NewStr("b"),
		objects.NewStr("a"),
		objects.NewStr("c"),
		objects.NewStr("a"),
	})
	_, err := countElements([]objects.Object{mapping, items}, nil)
	if err != nil {
		t.Fatalf("count_elements: %v", err)
	}
	v, err := mapping.GetItem(objects.NewStr("a"))
	if err != nil {
		t.Fatalf("getitem a: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 3 {
		t.Fatalf("want count 3 for 'a', got %d", n)
	}
}
