package objects

import (
	"errors"
	"strings"
	"testing"
)

func TestOrderedDictPreservesInsertionOrder(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"c", "a", "b"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	got := drainOdict(t, od)
	want := []string{"c", "a", "b"}
	if !sliceEq(got, want) {
		t.Errorf("iter order = %v, want %v", got, want)
	}
}

func TestOrderedDictReplacingValueLeavesOrderAlone(t *testing.T) {
	od := NewOrderedDict()
	_ = od.SetItem(NewStr("a"), NewInt(1))
	_ = od.SetItem(NewStr("b"), NewInt(2))
	_ = od.SetItem(NewStr("a"), NewInt(99))
	got := drainOdict(t, od)
	if !sliceEq(got, []string{"a", "b"}) {
		t.Errorf("re-set should not move key; got %v", got)
	}
	v, _ := od.GetItem(NewStr("a"))
	if n, _ := v.(*Int).Int64(); n != 99 {
		t.Errorf("a = %d, want 99 after re-set", n)
	}
}

func TestOrderedDictMoveToEndLast(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	if err := od.MoveToEnd(NewStr("a"), true); err != nil {
		t.Fatal(err)
	}
	got := drainOdict(t, od)
	if !sliceEq(got, []string{"b", "c", "a"}) {
		t.Errorf("move_to_end last=true: got %v, want [b c a]", got)
	}
}

func TestOrderedDictMoveToEndFirst(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	if err := od.MoveToEnd(NewStr("c"), false); err != nil {
		t.Fatal(err)
	}
	got := drainOdict(t, od)
	if !sliceEq(got, []string{"c", "a", "b"}) {
		t.Errorf("move_to_end last=false: got %v, want [c a b]", got)
	}
}

func TestOrderedDictMoveToEndOnEnd(t *testing.T) {
	// Already at the requested end: no error, no order change.
	od := NewOrderedDict()
	for _, k := range []string{"a", "b"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	if err := od.MoveToEnd(NewStr("b"), true); err != nil {
		t.Fatal(err)
	}
	got := drainOdict(t, od)
	if !sliceEq(got, []string{"a", "b"}) {
		t.Errorf("no-op move kept order? got %v", got)
	}
}

func TestOrderedDictMoveToEndMissingRaises(t *testing.T) {
	od := NewOrderedDict()
	_ = od.SetItem(NewStr("a"), NewInt(1))
	err := od.MoveToEnd(NewStr("missing"), true)
	if !errors.Is(err, errKeyNotFound) {
		t.Errorf("missing key err = %v, want errKeyNotFound", err)
	}
}

func TestOrderedDictPopItemLast(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	k, _, err := od.PopItem(true)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := Str(k)
	if s != "c" {
		t.Errorf("popitem(last=true) = %q, want c", s)
	}
	if od.Len() != 2 {
		t.Errorf("len after pop = %d, want 2", od.Len())
	}
}

func TestOrderedDictPopItemFirst(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	k, _, err := od.PopItem(false)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := Str(k)
	if s != "a" {
		t.Errorf("popitem(last=false) = %q, want a", s)
	}
}

func TestOrderedDictPopItemOnEmptyRaises(t *testing.T) {
	od := NewOrderedDict()
	_, _, err := od.PopItem(true)
	if err == nil || !strings.Contains(err.Error(), "KeyError") {
		t.Errorf("expected KeyError on empty popitem, got %v", err)
	}
}

func TestOrderedDictDelItemUnlinks(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	if err := od.DelItem(NewStr("b")); err != nil {
		t.Fatal(err)
	}
	got := drainOdict(t, od)
	if !sliceEq(got, []string{"a", "c"}) {
		t.Errorf("after delete got %v, want [a c]", got)
	}
}

func TestOrderedDictEqualOrderMatters(t *testing.T) {
	a := NewOrderedDict()
	b := NewOrderedDict()
	_ = a.SetItem(NewStr("x"), NewInt(1))
	_ = a.SetItem(NewStr("y"), NewInt(2))
	_ = b.SetItem(NewStr("y"), NewInt(2))
	_ = b.SetItem(NewStr("x"), NewInt(1))
	out, err := RichCmp(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := IsTruthy(out)
	if yes {
		t.Error("OrderedDicts with same pairs but different order should NOT be ==")
	}
}

func TestOrderedDictEqualSameOrder(t *testing.T) {
	a := NewOrderedDict()
	b := NewOrderedDict()
	for _, k := range []string{"x", "y"} {
		_ = a.SetItem(NewStr(k), NewInt(1))
		_ = b.SetItem(NewStr(k), NewInt(1))
	}
	out, err := RichCmp(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := IsTruthy(out)
	if !yes {
		t.Error("OrderedDicts with same pairs and order should be ==")
	}
}

func TestOrderedDictEqualPlainDictIgnoresOrder(t *testing.T) {
	od := NewOrderedDict()
	_ = od.SetItem(NewStr("x"), NewInt(1))
	_ = od.SetItem(NewStr("y"), NewInt(2))
	d := NewDict()
	_ = d.SetItem(NewStr("y"), NewInt(2))
	_ = d.SetItem(NewStr("x"), NewInt(1))
	out, err := RichCmp(od, d, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := IsTruthy(out)
	if !yes {
		t.Error("OrderedDict vs plain dict should ignore order on ==")
	}
}

func TestOrderedDictIterMutationRaises(t *testing.T) {
	od := NewOrderedDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = od.SetItem(NewStr(k), NewInt(1))
	}
	it, err := Iter(od)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IterNext(it); err != nil {
		t.Fatal(err)
	}
	_ = od.SetItem(NewStr("d"), NewInt(2))
	if _, err := IterNext(it); err == nil ||
		!strings.Contains(err.Error(), "OrderedDict mutated during iteration") {
		t.Errorf("expected RuntimeError on mid-iter mutation, got %v", err)
	}
}

func TestOrderedDictRepr(t *testing.T) {
	od := NewOrderedDict()
	_ = od.SetItem(NewStr("a"), NewInt(1))
	_ = od.SetItem(NewStr("b"), NewInt(2))
	r, err := Repr(od)
	if err != nil {
		t.Fatal(err)
	}
	want := "OrderedDict({'a': 1, 'b': 2})"
	if r != want {
		t.Errorf("repr = %q, want %q", r, want)
	}
	empty, _ := Repr(NewOrderedDict())
	if empty != "OrderedDict()" {
		t.Errorf("empty repr = %q, want OrderedDict()", empty)
	}
}

func drainOdict(t *testing.T, od *OrderedDict) []string {
	t.Helper()
	it, err := Iter(od)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for {
		v, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		s, _ := Str(v)
		out = append(out, s)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
