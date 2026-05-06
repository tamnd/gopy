package objects

import "testing"

func TestStructSeqAttributeAccess(t *testing.T) {
	tp := NewStructSeqType("PointInfo", []StructSeqField{
		{Name: "x"},
		{Name: "y"},
		{Name: "label"},
	})
	p := NewStructSeq(tp, []Object{NewInt(1), NewInt(2), NewStr("origin")})

	x, err := GetAttr(p, NewStr("x"))
	if err != nil {
		t.Fatalf("GetAttr x: %v", err)
	}
	if got, _ := x.(*Int).Int64(); got != 1 {
		t.Fatalf("x=%d, want 1", got)
	}
	lbl, err := GetAttr(p, NewStr("label"))
	if err != nil {
		t.Fatalf("GetAttr label: %v", err)
	}
	if s, _ := Str(lbl); s != "origin" {
		t.Fatalf("label=%q, want origin", s)
	}
	if _, err := GetAttr(p, NewStr("missing")); err == nil {
		t.Fatalf("expected AttributeError")
	}
}

func TestStructSeqIndexAccess(t *testing.T) {
	tp := NewStructSeqType("Pair", []StructSeqField{{Name: "a"}, {Name: "b"}})
	p := NewStructSeq(tp, []Object{NewInt(10), NewInt(20)})
	v, err := GetItem(p, NewInt(1))
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got, _ := v.(*Int).Int64(); got != 20 {
		t.Fatalf("got %d, want 20", got)
	}
	n, err := Length(p)
	if err != nil || n != 2 {
		t.Fatalf("Length=%d,%v", n, err)
	}
}

func TestStructSeqRepr(t *testing.T) {
	tp := NewStructSeqType("PT", []StructSeqField{{Name: "x"}, {Name: "y"}})
	p := NewStructSeq(tp, []Object{NewInt(3), NewInt(4)})
	r, err := Repr(p)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if r != "PT(x=3, y=4)" {
		t.Fatalf("got %q", r)
	}
}

func TestStructSeqEqualToTuple(t *testing.T) {
	tp := NewStructSeqType("Pair", []StructSeqField{{Name: "a"}, {Name: "b"}})
	p := NewStructSeq(tp, []Object{NewInt(1), NewInt(2)})
	tup := NewTuple([]Object{NewInt(1), NewInt(2)})
	eq, err := RichCmpBool(p, tup, CompareEQ)
	if err != nil || !eq {
		t.Fatalf("structseq != tuple: eq=%v err=%v", eq, err)
	}
}

func TestStructSeqMismatchPanics(t *testing.T) {
	tp := NewStructSeqType("X", []StructSeqField{{Name: "a"}})
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on field-count mismatch")
		}
	}()
	_ = NewStructSeq(tp, []Object{NewInt(1), NewInt(2)})
}
