package objects

import "testing"

func TestNewDictStartsUnicodeKind(t *testing.T) {
	d := NewDict()
	if d.kind != dictKindUnicode {
		t.Errorf("kind = %d, want %d (Unicode)", d.kind, dictKindUnicode)
	}
}

func TestSetItemKeepsKindUnicodeForStrKeys(t *testing.T) {
	d := NewDict()
	for _, k := range []string{"a", "b", "c"} {
		_ = d.SetItem(NewStr(k), NewInt(1))
	}
	if d.kind != dictKindUnicode {
		t.Errorf("kind = %d, want Unicode after str-only inserts", d.kind)
	}
}

func TestSetItemDowngradesOnNonStrInsert(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	if d.kind != dictKindUnicode {
		t.Fatalf("kind should still be Unicode before non-str insert, got %d", d.kind)
	}
	_ = d.SetItem(NewInt(7), NewInt(2))
	if d.kind != dictKindGeneral {
		t.Errorf("kind = %d, want General after int key insert", d.kind)
	}
}

func TestKindStaysGeneralAfterDeletes(t *testing.T) {
	// CPython doesn't promote General → Unicode after a deletion either.
	d := NewDict()
	_ = d.SetItem(NewInt(7), NewInt(2))
	_ = d.DelItem(NewInt(7))
	if d.kind != dictKindGeneral {
		t.Errorf("kind = %d, want General to persist after the only non-str key was removed", d.kind)
	}
}

func TestDispatchUnicodeUnicodeFastPath(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(99))
	got, err := d.GetItem(NewStr("k"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(*Int).Int64(); v != 99 {
		t.Errorf("got %d, want 99", v)
	}
}

func TestDispatchUnicodeGenericPath(t *testing.T) {
	// Table holds only str keys; lookup with a non-str key should miss
	// without crashing and route through the unicode/generic variant.
	d := NewDict()
	_ = d.SetItem(NewStr("k"), NewInt(1))
	if _, err := d.GetItem(NewInt(0)); err != errKeyNotFound {
		t.Errorf("non-str lookup on str-only dict should miss; got err = %v", err)
	}
}

func TestDispatchGenericPath(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewInt(7), NewStr("seven"))
	_ = d.SetItem(NewStr("a"), NewInt(1))
	if d.kind != dictKindGeneral {
		t.Fatalf("kind should be General after mixed insert, got %d", d.kind)
	}
	got, err := d.GetItem(NewInt(7))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := got.(*strStub)
	if v.v != "seven" {
		t.Errorf("got %q, want seven", v.v)
	}
}

func TestLookupUnicodeUnicodeReturnsHit(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	h, _ := Hash(NewStr("a"))
	idx, ok, err := lookupUnicodeUnicode(d, NewStr("a"), h)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	if v, _ := d.entries[idx].value.(*Int).Int64(); v != 1 {
		t.Errorf("got %d, want 1", v)
	}
}

func TestLookupUnicodeUnicodeReturnsEmptyOnMiss(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	h, _ := Hash(NewStr("nope"))
	_, ok, err := lookupUnicodeUnicode(d, NewStr("nope"), h)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected miss")
	}
}

func TestLookupGenericFindsIntKey(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewInt(42), NewStr("answer"))
	h, _ := Hash(NewInt(42))
	idx, ok, err := lookupGeneric(d, NewInt(42), h)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	v, _ := d.entries[idx].value.(*strStub)
	if v.v != "answer" {
		t.Errorf("got %q, want answer", v.v)
	}
}

func TestLookupSplitRejectsNonStrKey(t *testing.T) {
	d := NewDict()
	d.kind = dictKindSplit
	_, _, err := lookupSplit(d, NewInt(0), 0)
	if err == nil {
		t.Error("split lookup with int key should SystemError")
	}
}

func TestProbingMatchesCpythonAfterCollisions(t *testing.T) {
	// Insert enough keys to force probe-chain collisions, then verify
	// each key still round-trips through the dispatcher.
	d := NewDict()
	for i := 0; i < 64; i++ {
		_ = d.SetItem(NewStr(string(rune('a'+i%26))+string(rune('0'+i/26))), NewInt(int64(i)))
	}
	for i := 0; i < 64; i++ {
		k := NewStr(string(rune('a'+i%26)) + string(rune('0'+i/26)))
		got, err := d.GetItem(k)
		if err != nil {
			t.Fatalf("missing key %d: %v", i, err)
		}
		if v, _ := got.(*Int).Int64(); v != int64(i) {
			t.Errorf("key %d round-trip = %d, want %d", i, v, i)
		}
	}
}
