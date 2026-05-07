package objects

import (
	"errors"
	"testing"
)

func TestMappingCheckDict(t *testing.T) {
	if !MappingCheck(NewDict()) {
		t.Error("dict should pass MappingCheck")
	}
	if MappingCheck(NewList(nil)) {
		t.Error("list has no mp_subscript so MappingCheck should be false")
	}
	if MappingCheck(nil) {
		t.Error("nil should not pass MappingCheck")
	}
}

func TestMappingSize(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	got, err := MappingSize(d)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("size = %d, want 2", got)
	}
}

func TestMappingSizeRejectsSequence(t *testing.T) {
	if _, err := MappingSize(NewList(nil)); err == nil {
		t.Error("list should fail MappingSize (it is a sequence, not a mapping)")
	}
}

func TestMappingSizeNoSlot(t *testing.T) {
	if _, err := MappingSize(NewInt(1)); err == nil {
		t.Error("int should fail MappingSize")
	}
}

func TestMappingGetItemString(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(99))
	got, err := MappingGetItemString(d, "k")
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 99 {
		t.Errorf("got %d, want 99", intVal(got, t))
	}
}

func TestMappingSetItemString(t *testing.T) {
	d := NewDict()
	if err := MappingSetItemString(d, "k", NewInt(7)); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetItem(NewStr("k"))
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 7 {
		t.Errorf("got %d, want 7", intVal(got, t))
	}
}

func TestMappingDelItemString(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(1))
	if err := MappingDelItemString(d, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetItem(NewStr("k")); err == nil {
		t.Error("expected KeyError after delete")
	}
}

func TestMappingGetOptionalItemHit(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(5))
	v, ok, err := MappingGetOptionalItem(d, NewStr("k"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if intVal(v, t) != 5 {
		t.Errorf("got %d, want 5", intVal(v, t))
	}
}

func TestMappingGetOptionalItemMissDictFastPath(t *testing.T) {
	d := NewDict()
	v, ok, err := MappingGetOptionalItem(d, NewStr("absent"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected ok=false on miss")
	}
	if v != nil {
		t.Error("value should be nil on miss")
	}
}

func TestMappingGetOptionalItemPropagatesNonKeyError(t *testing.T) {
	want := errors.New("boom")
	tp := NewType("custmap", []*Type{objectType})
	tp.Mapping = &MappingMethods{
		GetItem: func(o, key Object) (Object, error) { return nil, want },
	}
	holder := &Header{}
	holder.init(tp)
	if _, _, err := MappingGetOptionalItem(holder, NewStr("k")); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestMappingHasKey(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(1))
	if !MappingHasKey(d, NewStr("k")) {
		t.Error("k should be present")
	}
	if MappingHasKey(d, NewStr("absent")) {
		t.Error("absent key should report false")
	}
}

func TestMappingHasKeyString(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(1))
	if !MappingHasKeyString(d, "k") {
		t.Error("k should be present")
	}
}

func TestMappingHasKeyWithErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	tp := NewType("custmap2", []*Type{objectType})
	tp.Mapping = &MappingMethods{
		GetItem: func(o, key Object) (Object, error) { return nil, want },
	}
	holder := &Header{}
	holder.init(tp)
	if _, err := MappingHasKeyWithError(holder, NewStr("k")); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestMappingKeysDictFastPath(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	keys, err := MappingKeys(d)
	if err != nil {
		t.Fatal(err)
	}
	if keys.Len() != 2 {
		t.Errorf("len = %d, want 2", keys.Len())
	}
}

func TestMappingValuesDictFastPath(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(11))
	_ = d.SetItem(NewStr("b"), NewInt(22))
	vals, err := MappingValues(d)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Len() != 2 {
		t.Errorf("len = %d, want 2", vals.Len())
	}
	sum := int64(0)
	for i := 0; i < vals.Len(); i++ {
		sum += intVal(vals.Item(i), t)
	}
	if sum != 33 {
		t.Errorf("sum = %d, want 33", sum)
	}
}

func TestMappingItemsDictFastPath(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	items, err := MappingItems(d)
	if err != nil {
		t.Fatal(err)
	}
	if items.Len() != 1 {
		t.Fatalf("len = %d, want 1", items.Len())
	}
	pair, ok := items.Item(0).(*Tuple)
	if !ok {
		t.Fatalf("item should be tuple, got %T", items.Item(0))
	}
	if pair.Len() != 2 {
		t.Errorf("pair len = %d, want 2", pair.Len())
	}
	if intVal(pair.Item(1), t) != 1 {
		t.Errorf("value = %d, want 1", intVal(pair.Item(1), t))
	}
}
