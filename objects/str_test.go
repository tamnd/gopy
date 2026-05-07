package objects

import (
	"testing"
)

func TestUnicodeKindAscii(t *testing.T) {
	s := NewStr("hello").(*Unicode)
	if s.Kind() != StrKind1Byte {
		t.Errorf("kind = %d, want %d", s.Kind(), StrKind1Byte)
	}
	if !s.IsASCII() {
		t.Error("'hello' should classify as ASCII")
	}
	if s.Length() != 5 {
		t.Errorf("length = %d, want 5", s.Length())
	}
}

func TestUnicodeKindLatin1(t *testing.T) {
	// é (U+00E9) is < 0x100 but >= 0x80, so kind=1 but not ascii.
	s := NewStr("café").(*Unicode)
	if s.Kind() != StrKind1Byte {
		t.Errorf("kind = %d, want 1", s.Kind())
	}
	if s.IsASCII() {
		t.Error("'café' should not classify as ASCII")
	}
	if s.Length() != 4 {
		t.Errorf("length = %d codepoints, want 4", s.Length())
	}
}

func TestUnicodeKindBmp(t *testing.T) {
	// 中 is U+4E2D, fits in UCS-2.
	s := NewStr("中文").(*Unicode)
	if s.Kind() != StrKind2Byte {
		t.Errorf("kind = %d, want 2", s.Kind())
	}
	if s.IsASCII() {
		t.Error("'中文' is not ASCII")
	}
	if s.Length() != 2 {
		t.Errorf("length = %d, want 2", s.Length())
	}
}

func TestUnicodeKindAstral(t *testing.T) {
	// 🐍 is U+1F40D, requires UCS-4.
	s := NewStr("🐍").(*Unicode)
	if s.Kind() != StrKind4Byte {
		t.Errorf("kind = %d, want 4", s.Kind())
	}
	if s.Length() != 1 {
		t.Errorf("length = %d codepoints, want 1", s.Length())
	}
}

func TestUnicodeEmpty(t *testing.T) {
	s := NewStr("").(*Unicode)
	if s.Kind() != StrKind1Byte {
		t.Errorf("empty string kind = %d, want 1", s.Kind())
	}
	if !s.IsASCII() {
		t.Error("empty string should be ascii")
	}
	if s.Length() != 0 {
		t.Errorf("empty length = %d, want 0", s.Length())
	}
}

func TestUnicodeReady(t *testing.T) {
	s := NewStr("x").(*Unicode)
	if !s.IsReady() {
		t.Error("freshly built str should be ready")
	}
}

func TestUnicodeHashCached(t *testing.T) {
	s := NewStr("widget").(*Unicode)
	if s.hash != -1 {
		t.Errorf("hash field starts as %d, want -1 sentinel", s.hash)
	}
	first, err := Hash(s)
	if err != nil {
		t.Fatal(err)
	}
	if s.hash != first {
		t.Errorf("after Hash, cache = %d, want %d", s.hash, first)
	}
	// Mutate the cache to verify the second call reads it instead of
	// recomputing.
	s.hash = 999
	again, _ := Hash(s)
	if again != 999 {
		t.Errorf("second Hash recomputed (got %d) instead of using cache (999)", again)
	}
}

func TestUnicodeHashEmpty(t *testing.T) {
	s := NewStr("")
	h, err := Hash(s)
	if err != nil {
		t.Fatal(err)
	}
	if h != 0 {
		t.Errorf("empty string hash = %d, want 0", h)
	}
}

func TestUnicodeRepr(t *testing.T) {
	r, err := Repr(NewStr("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if r != "'hi'" {
		t.Errorf("repr = %q, want 'hi'", r)
	}
}

func TestUnicodeStr(t *testing.T) {
	s, err := Str(NewStr("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if s != "hi" {
		t.Errorf("Str = %q, want hi", s)
	}
}

func TestUnicodeRichCmp(t *testing.T) {
	a := NewStr("hello")
	b := NewStr("hello")
	c := NewStr("world")

	eq, err := strType.RichCmp(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if eq != NewBool(true) {
		t.Error("equal strings should compare equal")
	}

	ne, _ := strType.RichCmp(a, c, CompareNE)
	if ne != NewBool(true) {
		t.Error("different strings should compare unequal")
	}
}

func TestUnicodeRichCmpDifferentType(t *testing.T) {
	a := NewStr("1")
	b := NewInt(1)
	got, _ := strType.RichCmp(a, b, CompareEQ)
	if got != NotImplemented() {
		t.Error("str vs int comparison should defer with NotImplemented")
	}
}

func TestUnicodeValueRoundtrip(t *testing.T) {
	cases := []string{"", "ascii", "café", "中文", "🐍 emoji"}
	for _, in := range cases {
		s := NewStr(in).(*Unicode)
		if s.Value() != in {
			t.Errorf("Value() = %q, want %q", s.Value(), in)
		}
	}
}

func TestStrTypeSingleton(t *testing.T) {
	if StrType() != strType {
		t.Error("StrType() should return the package singleton")
	}
	if StrType().Name != "str" {
		t.Errorf("type name = %q, want str", StrType().Name)
	}
}
