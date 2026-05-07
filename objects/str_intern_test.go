package objects

import "testing"

func TestInternFromStringReturnsSameInstance(t *testing.T) {
	ClearInterned()
	a := InternFromString("hello").(*Unicode)
	b := InternFromString("hello").(*Unicode)
	if a != b {
		t.Error("InternFromString should return the same *Unicode for equal strings")
	}
}

func TestInternInPlaceRewrites(t *testing.T) {
	ClearInterned()
	canonical := InternFromString("widget").(*Unicode)

	// Build a fresh, separately-allocated *Unicode with the same value.
	dup := NewStr("widget")
	if dup == canonical {
		t.Fatal("NewStr unexpectedly returned the interned instance")
	}

	p := dup
	InternInPlace(&p)
	if p != canonical {
		t.Error("InternInPlace should rewrite p to the canonical instance")
	}
}

func TestInternInPlaceFirstInsert(t *testing.T) {
	ClearInterned()
	o := NewStr("brand-new").(*Unicode)
	p := Object(o)
	InternInPlace(&p)
	// First insert: the table now holds o, and p is unchanged.
	if p.(*Unicode) != o {
		t.Error("first InternInPlace should keep the original pointer")
	}
	again := InternFromString("brand-new").(*Unicode)
	if again != o {
		t.Error("InternFromString after InternInPlace should return the inserted instance")
	}
}

func TestInternInPlaceNonStringIsNoop(t *testing.T) {
	p := Object(NewInt(7))
	before := p
	InternInPlace(&p)
	if p != before {
		t.Error("InternInPlace on non-str should not mutate the slot")
	}
}

func TestInternInPlaceNilIsNoop(t *testing.T) {
	var p Object
	InternInPlace(&p)
	if p != nil {
		t.Error("nil slot should remain nil")
	}
	InternInPlace(nil)
}

func TestInternedCount(t *testing.T) {
	ClearInterned()
	if InternedCount() != 0 {
		t.Errorf("after clear: count = %d, want 0", InternedCount())
	}
	InternFromString("a")
	InternFromString("b")
	InternFromString("a") // repeat
	if got := InternedCount(); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
}

func TestUnicodeCtypeWrappers(t *testing.T) {
	if !IsLowerRune('a') || IsLowerRune('A') {
		t.Error("IsLowerRune")
	}
	if !IsUpperRune('A') || IsUpperRune('a') {
		t.Error("IsUpperRune")
	}
	if !IsAlphaRune('a') || IsAlphaRune('1') {
		t.Error("IsAlphaRune")
	}
	if !IsDigitRune('1') || IsDigitRune('a') {
		t.Error("IsDigitRune")
	}
	if !IsDecimalRune('1') {
		t.Error("IsDecimalRune")
	}
	if !IsSpaceRune(' ') || IsSpaceRune('x') {
		t.Error("IsSpaceRune")
	}
	if !IsPrintableRune(' ') || IsPrintableRune('\x00') {
		t.Error("IsPrintableRune")
	}
	if !IsXIDStartRune('_') || !IsXIDStartRune('a') || IsXIDStartRune('1') {
		t.Error("IsXIDStartRune")
	}
	if !IsXIDContinueRune('1') || !IsXIDContinueRune('_') {
		t.Error("IsXIDContinueRune")
	}
	if !IsCasedRune('a') || IsCasedRune('1') {
		t.Error("IsCasedRune")
	}
	if ToLowerRune('A') != 'a' {
		t.Error("ToLowerRune")
	}
	if ToUpperRune('a') != 'A' {
		t.Error("ToUpperRune")
	}
	if ToTitleRune('a') != 'A' {
		t.Error("ToTitleRune")
	}
}
