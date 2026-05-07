package objects

import (
	"errors"
	"strings"
	"testing"
)

func TestDictIterReturnsKeysInTableOrder(t *testing.T) {
	d := NewDict()
	keys := []string{"a", "b", "c"}
	for _, k := range keys {
		_ = d.SetItem(NewStr(k), NewInt(1))
	}
	it, err := Iter(d)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for {
		v, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		s, _ := Str(v)
		seen[s] = true
	}
	for _, k := range keys {
		if !seen[k] {
			t.Errorf("missing %q from iter", k)
		}
	}
}

func TestDictIterRaisesOnSizeChangeDuringIteration(t *testing.T) {
	d := NewDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = d.SetItem(NewStr(k), NewInt(1))
	}
	it, err := Iter(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IterNext(it); err != nil {
		t.Fatal(err)
	}
	_ = d.SetItem(NewStr("d"), NewInt(2))
	_, err = IterNext(it)
	if err == nil || !strings.Contains(err.Error(), "dictionary changed size during iteration") {
		t.Errorf("expected RuntimeError on size change, got %v", err)
	}
}

func TestDictIterRaisesOnDeleteDuringIteration(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	it, err := Iter(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IterNext(it); err != nil {
		t.Fatal(err)
	}
	_ = d.DelItem(NewStr("b"))
	_, err = IterNext(it)
	if err == nil || !strings.Contains(err.Error(), "dictionary changed size during iteration") {
		t.Errorf("expected RuntimeError on delete during iter, got %v", err)
	}
}

func TestDictIterValueReplacementDoesNotRaise(t *testing.T) {
	// CPython only catches size changes (ma_used). Replacing an existing
	// key's value leaves used unchanged and iteration continues.
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	it, err := Iter(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := IterNext(it); err != nil {
		t.Fatal(err)
	}
	_ = d.SetItem(NewStr("a"), NewInt(99)) // value-only mutation
	if _, err := IterNext(it); err != nil {
		t.Fatalf("value replacement should not raise, got %v", err)
	}
}

func TestKeysViewIterAndLen(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	view := d.KeysView()
	n, err := Length(view)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
	got := drain(t, view)
	if len(got) != 2 {
		t.Errorf("iter yielded %d items, want 2", len(got))
	}
}

func TestKeysViewContains(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	view := d.KeysView()
	yes, err := Contains(view, NewStr("a"))
	if err != nil || !yes {
		t.Errorf("'a' should be in keys view; ok=%v err=%v", yes, err)
	}
	no, err := Contains(view, NewStr("z"))
	if err != nil || no {
		t.Errorf("'z' should not be in keys view; ok=%v err=%v", no, err)
	}
}

func TestValuesViewIterAndLen(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(10))
	_ = d.SetItem(NewStr("b"), NewInt(20))
	view := d.ValuesView()
	got := drain(t, view)
	sum := int64(0)
	for _, v := range got {
		n, _ := v.(*Int).Int64()
		sum += n
	}
	if sum != 30 {
		t.Errorf("sum of values = %d, want 30", sum)
	}
}

func TestItemsViewYieldsTuples(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	view := d.ItemsView()
	got := drain(t, view)
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1", len(got))
	}
	tup, ok := got[0].(*Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("item should be a 2-tuple, got %T", got[0])
	}
	k, _ := Str(tup.Item(0))
	v, _ := tup.Item(1).(*Int).Int64()
	if k != "a" || v != 1 {
		t.Errorf("got (%q, %d), want (a, 1)", k, v)
	}
}

func TestItemsViewContainsMatchesValue(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	view := d.ItemsView()
	yes, err := Contains(view, NewTuple([]Object{NewStr("a"), NewInt(1)}))
	if err != nil || !yes {
		t.Errorf("(a, 1) should be in items view; ok=%v err=%v", yes, err)
	}
	// Same key, different value — should miss.
	no, err := Contains(view, NewTuple([]Object{NewStr("a"), NewInt(99)}))
	if err != nil || no {
		t.Errorf("(a, 99) should not be in items view; ok=%v err=%v", no, err)
	}
}

func TestKeysViewIntersection(t *testing.T) {
	a, b := NewDict(), NewDict()
	_ = a.SetItem(NewStr("x"), NewInt(1))
	_ = a.SetItem(NewStr("y"), NewInt(2))
	_ = b.SetItem(NewStr("y"), NewInt(20))
	_ = b.SetItem(NewStr("z"), NewInt(30))
	got, err := NumberAnd(a.KeysView(), b.KeysView())
	if err != nil {
		t.Fatal(err)
	}
	s := got.(*Set)
	if s.Len() != 1 {
		t.Errorf("|a&b| = %d, want 1", s.Len())
	}
	if ok, _ := s.Contains(NewStr("y")); !ok {
		t.Error("intersection should contain 'y'")
	}
}

func TestKeysViewUnion(t *testing.T) {
	a, b := NewDict(), NewDict()
	_ = a.SetItem(NewStr("x"), NewInt(1))
	_ = b.SetItem(NewStr("y"), NewInt(2))
	got, err := NumberOr(a.KeysView(), b.KeysView())
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Set).Len() != 2 {
		t.Errorf("|a|b| = %d, want 2", got.(*Set).Len())
	}
}

func TestKeysViewDifference(t *testing.T) {
	a, b := NewDict(), NewDict()
	_ = a.SetItem(NewStr("x"), NewInt(1))
	_ = a.SetItem(NewStr("y"), NewInt(2))
	_ = b.SetItem(NewStr("y"), NewInt(20))
	got, err := NumberSubtract(a.KeysView(), b.KeysView())
	if err != nil {
		t.Fatal(err)
	}
	s := got.(*Set)
	if s.Len() != 1 {
		t.Errorf("|a-b| = %d, want 1", s.Len())
	}
	if ok, _ := s.Contains(NewStr("x")); !ok {
		t.Error("difference should contain 'x'")
	}
}

func TestKeysViewSymmetricDifference(t *testing.T) {
	a, b := NewDict(), NewDict()
	_ = a.SetItem(NewStr("x"), NewInt(1))
	_ = a.SetItem(NewStr("y"), NewInt(2))
	_ = b.SetItem(NewStr("y"), NewInt(20))
	_ = b.SetItem(NewStr("z"), NewInt(30))
	got, err := NumberXor(a.KeysView(), b.KeysView())
	if err != nil {
		t.Fatal(err)
	}
	s := got.(*Set)
	if s.Len() != 2 {
		t.Errorf("|a^b| = %d, want 2 (x and z)", s.Len())
	}
}

func TestKeysViewBinopAcceptsArbitraryIterable(t *testing.T) {
	// CPython's view set ops accept any iterable on the right.
	a := NewDict()
	_ = a.SetItem(NewStr("x"), NewInt(1))
	_ = a.SetItem(NewStr("y"), NewInt(2))
	other := NewList([]Object{NewStr("y"), NewStr("z")})
	got, err := NumberAnd(a.KeysView(), other)
	if err != nil {
		t.Fatal(err)
	}
	s := got.(*Set)
	if ok, _ := s.Contains(NewStr("y")); !ok || s.Len() != 1 {
		t.Errorf("a.keys() & list got len=%d contains-y=%v, want {y}", s.Len(), ok)
	}
}

func TestDictViewRepr(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	r, err := Repr(d.KeysView())
	if err != nil {
		t.Fatal(err)
	}
	if r != "dict_keys(['a'])" {
		t.Errorf("repr = %q, want dict_keys(['a'])", r)
	}
}

func drain(t *testing.T, view Object) []Object {
	t.Helper()
	it, err := Iter(view)
	if err != nil {
		t.Fatal(err)
	}
	var out []Object
	for {
		v, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
}
