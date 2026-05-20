// PEP-393 slab dispatch parity + O(1) indexing tests for
// pre-encoded UCS1 / UCS2 / UCS4 storage.
//
// CPython stores str codepoints in a flat array keyed by
// PyUnicode_KIND(s): 1 byte for latin1, 2 bytes for BMP, 4 bytes for
// astral. PyUnicode_READ(kind, data, i) is one indexed load. gopy's
// canonical storage is Go-string UTF-8, but classify() builds the
// matching slab so unicodeGetItemKind and strIterator can index in
// O(1) instead of walking UTF-8.
//
// CPython: Objects/unicodeobject.c:1731 _PyUnicode_Ready (slab fill)
// CPython: Include/cpython/unicodeobject.h:151 PyUnicode_READ

package objects

import (
	"testing"
)

func TestStrSlabClassify(t *testing.T) {
	cases := []struct {
		s     string
		kind  byte
		ascii bool
		want  []rune
	}{
		{"hello", StrKind1Byte, true, []rune{'h', 'e', 'l', 'l', 'o'}},
		{"café", StrKind1Byte, false, []rune{'c', 'a', 'f', 'é'}},
		{"abc", StrKind1Byte, false, []rune{'a', 'b', 'c', 0x80}},
		{"中文", StrKind2Byte, false, []rune{0x4E2D, 0x6587}},
		{"a中b", StrKind2Byte, false, []rune{'a', 0x4E2D, 'b'}},
		{"\U0001F600", StrKind4Byte, false, []rune{0x1F600}},
		{"a\U0001F600b", StrKind4Byte, false, []rune{'a', 0x1F600, 'b'}},
	}
	for _, c := range cases {
		u := NewStr(c.s).(*Unicode)
		if u.Kind() != c.kind {
			t.Errorf("%q: kind=%d, want %d", c.s, u.Kind(), c.kind)
		}
		if u.IsASCII() != c.ascii {
			t.Errorf("%q: ascii=%v, want %v", c.s, u.IsASCII(), c.ascii)
		}
		if u.Length() != len(c.want) {
			t.Errorf("%q: length=%d, want %d", c.s, u.Length(), len(c.want))
		}
		for i, r := range c.want {
			if got := u.RuneAt(i); got != r {
				t.Errorf("%q: RuneAt(%d)=%U, want %U", c.s, i, got, r)
			}
		}
	}
}

func TestStrSlabPopulated(t *testing.T) {
	u1 := NewStr("café").(*Unicode)
	if u1.Data1() == nil {
		t.Error("kind-1 non-ASCII string should have data1 slab")
	}
	if u1.Data2() != nil || u1.Data4() != nil {
		t.Error("kind-1 string should not have data2/data4")
	}

	u2 := NewStr("中文").(*Unicode)
	if u2.Data2() == nil {
		t.Error("kind-2 string should have data2 slab")
	}
	if u2.Data1() != nil || u2.Data4() != nil {
		t.Error("kind-2 string should not have data1/data4")
	}

	u4 := NewStr("\U0001F600").(*Unicode)
	if u4.Data4() == nil {
		t.Error("kind-4 string should have data4 slab")
	}
	if u4.Data1() != nil || u4.Data2() != nil {
		t.Error("kind-4 string should not have data1/data2")
	}

	uA := NewStr("hello").(*Unicode)
	if uA.Data1() != nil || uA.Data2() != nil || uA.Data4() != nil {
		t.Error("ASCII string should have no slab (byte index == codepoint index)")
	}
}

func TestUnicodeGetItemKindSlabs(t *testing.T) {
	u1 := NewStr("café").(*Unicode)
	got, err := unicodeGetItemKind(u1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Unicode).Value() != "é" {
		t.Errorf("café[3] = %q, want é", got.(*Unicode).Value())
	}
	// chr(0xE9) is the latin1 cache singleton; identity must hold.
	if got != latin1Cache[0xE9] {
		t.Error("café[3] should return the cached latin1 singleton")
	}

	u2 := NewStr("a中b").(*Unicode)
	got, err = unicodeGetItemKind(u2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Unicode).Value() != "中" {
		t.Errorf("a中b[1] = %q, want 中", got.(*Unicode).Value())
	}

	u4 := NewStr("a\U0001F600b").(*Unicode)
	got, err = unicodeGetItemKind(u4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Unicode).Value() != "\U0001F600" {
		t.Errorf("astral[1] = %q, want emoji", got.(*Unicode).Value())
	}
}

// Latin1 singleton 0x80..0xFF must carry data1 so the slab dispatch
// invariant holds: kind=1 + !ascii implies data1 != nil.
//
// CPython: Objects/unicodeobject.c:1958 get_latin1_char
func TestLatin1CacheSlabInvariant(t *testing.T) {
	for i := 0x80; i < 0x100; i++ {
		u := latin1Cache[i]
		if u.Data1() == nil {
			t.Errorf("latin1Cache[%#x] missing data1", i)
			continue
		}
		if len(u.Data1()) != 1 || u.Data1()[0] != uint8(i) {
			t.Errorf("latin1Cache[%#x] data1=%v", i, u.Data1())
		}
		got, err := unicodeGetItemKind(u, 0)
		if err != nil {
			t.Errorf("latin1Cache[%#x][0] err: %v", i, err)
			continue
		}
		if got != u {
			t.Errorf("latin1Cache[%#x][0] should be self-identical", i)
		}
	}
}

func TestStrIteratorSlabs(t *testing.T) {
	cases := []struct {
		s    string
		want []string
	}{
		{"abc", []string{"a", "b", "c"}},
		{"café", []string{"c", "a", "f", "é"}},
		{"中文", []string{"中", "文"}},
		{"a\U0001F600", []string{"a", "\U0001F600"}},
	}
	for _, c := range cases {
		u, _ := NewStr(c.s).(*Unicode)
		it, err := strIter(u)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for {
			next, err := strIterType.IterNext(it)
			if err == ErrStopIteration {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, next.(*Unicode).Value())
		}
		if len(got) != len(c.want) {
			t.Errorf("iter(%q) len=%d, want %d", c.s, len(got), len(c.want))
			continue
		}
		for i, g := range got {
			if g != c.want[i] {
				t.Errorf("iter(%q)[%d] = %q, want %q", c.s, i, g, c.want[i])
			}
		}
	}
}

// O(1) indexing benchmark: indexing the last codepoint of a long
// non-ASCII string must not depend on string length once the slab is
// built.
//
// CPython: Include/cpython/unicodeobject.h:151 PyUnicode_READ (O(1))
func BenchmarkUnicodeGetItem_UCS2_Last(b *testing.B) {
	var sb []rune
	for range 4096 {
		sb = append(sb, '中')
	}
	u, _ := NewStr(string(sb)).(*Unicode)
	last := u.Length() - 1
	b.ResetTimer()
	for b.Loop() {
		if _, err := unicodeGetItemKind(u, last); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnicodeGetItem_UCS4_Last(b *testing.B) {
	var sb []rune
	for range 4096 {
		sb = append(sb, '\U0001F600')
	}
	u, _ := NewStr(string(sb)).(*Unicode)
	last := u.Length() - 1
	b.ResetTimer()
	for b.Loop() {
		if _, err := unicodeGetItemKind(u, last); err != nil {
			b.Fatal(err)
		}
	}
}
