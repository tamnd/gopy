package objects

import "testing"

// ASCII strings dispatch to the byte-equal fast path. Each table
// entry pairs an ASCII haystack + needle with the codepoint index
// CPython returns. The fast path must match StrFind on the same
// inputs (it is the same answer, just allocation-free).
//
// CPython: Objects/unicodeobject.c:11964 unicode_find_impl
func TestStrFindKind_ASCIIFastPath(t *testing.T) {
	cases := []struct {
		s, needle  string
		start, end int
		want       int
	}{
		{"hello world", "world", 0, 11, 6},
		{"hello world", "xyz", 0, 11, -1},
		{"abcabc", "b", 0, 6, 1},
		{"abcabc", "b", 2, 6, 4},
		{"", "x", 0, 0, -1},
		{"x", "", 0, 1, 0},
		{"abc", "", 0, 3, 0},
	}
	for _, c := range cases {
		u := NewStr(c.s).(*Unicode)
		if !u.IsASCII() {
			t.Fatalf("setup: %q expected ASCII, got kind=%d ascii=%v", c.s, u.Kind(), u.IsASCII())
		}
		got := strFindKind(u, c.needle, c.start, c.end)
		if got != c.want {
			t.Errorf("strFindKind(%q,%q,%d,%d) = %d, want %d",
				c.s, c.needle, c.start, c.end, got, c.want)
		}
	}
}

// Non-ASCII strings (Latin-1 supplement, BMP, full Unicode) must fall
// back to the existing rune-walk path. Result parity matters; this
// test pins it.
//
// CPython: Objects/unicodeobject.c:9680 any_find_slice (kind > 1 path)
func TestStrFindKind_NonASCIIFallback(t *testing.T) {
	cases := []struct {
		s, needle  string
		start, end int
		want       int
	}{
		{"café au lait", "au", 0, 12, 5},
		{"中文中文", "文", 0, 4, 1},
		{"abcdef", "def", 0, 7, 4},  // Latin-1 supplement triggers kind=1 ascii=false
		{"abc\U0001F600def", "def", 0, 7, 4}, // astral triggers kind=4
	}
	for _, c := range cases {
		u := NewStr(c.s).(*Unicode)
		if u.IsASCII() {
			t.Fatalf("setup: %q expected non-ASCII, got ascii=true", c.s)
		}
		got := strFindKind(u, c.needle, c.start, c.end)
		if got != c.want {
			t.Errorf("strFindKind(%q,%q) = %d, want %d", c.s, c.needle, got, c.want)
		}
	}
}

// rfind ASCII fast path: last occurrence.
//
// CPython: Objects/unicodeobject.c:13048 unicode_rfind_impl
func TestStrRFindKind_ASCIIFastPath(t *testing.T) {
	u := NewStr("abcabc").(*Unicode)
	if got := strRFindKind(u, "b", 0, 6); got != 4 {
		t.Errorf("rfind 'abcabc','b' = %d, want 4", got)
	}
	if got := strRFindKind(u, "x", 0, 6); got != -1 {
		t.Errorf("rfind 'abcabc','x' = %d, want -1", got)
	}
}

// count fast path: empty needle returns len+1 (CPython's split-point
// semantics).
//
// CPython: Objects/unicodeobject.c:11779 unicode_count_impl
func TestStrCountKind_ASCIIFastPath(t *testing.T) {
	cases := []struct {
		s, needle  string
		start, end int
		want       int
	}{
		{"aaaa", "a", 0, 4, 4},
		{"aaaa", "aa", 0, 4, 2}, // non-overlapping
		{"abc", "", 0, 3, 4},    // empty needle: len+1
		{"abcabc", "b", 1, 5, 2}, // "bcab" contains "b" twice
		{"abcabc", "b", 2, 5, 1}, // "cab" contains "b" once
	}
	for _, c := range cases {
		u := NewStr(c.s).(*Unicode)
		got := strCountKind(u, c.needle, c.start, c.end)
		if got != c.want {
			t.Errorf("strCountKind(%q,%q,%d,%d) = %d, want %d",
				c.s, c.needle, c.start, c.end, got, c.want)
		}
	}
}

// startswith/endswith ASCII fast path.
//
// CPython: Objects/unicodeobject.c:13617 unicode_startswith_impl
// CPython: Objects/unicodeobject.c:13673 unicode_endswith_impl
func TestStrStartsEndsKind_ASCIIFastPath(t *testing.T) {
	u := NewStr("hello world").(*Unicode)
	if !strStartsWithKind(u, "hello", 0, 11) {
		t.Error("'hello world'.startswith('hello') should be true")
	}
	if strStartsWithKind(u, "world", 0, 11) {
		t.Error("'hello world'.startswith('world') should be false")
	}
	if !strEndsWithKind(u, "world", 0, 11) {
		t.Error("'hello world'.endswith('world') should be true")
	}
	if strEndsWithKind(u, "hello", 0, 11) {
		t.Error("'hello world'.endswith('hello') should be false")
	}
}

// index variants raise ValueError on miss, return the codepoint index
// on hit. Both ASCII and non-ASCII paths must agree.
//
// CPython: Objects/unicodeobject.c:12027 unicode_index_impl
// CPython: Objects/unicodeobject.c:13069 unicode_rindex_impl
func TestStrIndexKind(t *testing.T) {
	ascii := NewStr("hello").(*Unicode)
	if i, err := strIndexKind(ascii, "ll", 0, 5); err != nil || i != 2 {
		t.Errorf("hello.index(ll) = (%d,%v), want (2,nil)", i, err)
	}
	if _, err := strIndexKind(ascii, "z", 0, 5); err == nil {
		t.Error("hello.index(z) should error")
	}
	if i, err := strRIndexKind(ascii, "l", 0, 5); err != nil || i != 3 {
		t.Errorf("hello.rindex(l) = (%d,%v), want (3,nil)", i, err)
	}
}

// unicodeGetItemKind ASCII fast path: byte index == codepoint index,
// so the per-call walk vanishes. Negative indices wrap.
//
// CPython: Objects/unicodeobject.c:1848 unicode_getitem
func TestUnicodeGetItemKind_ASCII(t *testing.T) {
	u := NewStr("hello").(*Unicode)
	tests := []struct {
		i    int
		want string
	}{
		{0, "h"}, {1, "e"}, {4, "o"}, {-1, "o"}, {-5, "h"},
	}
	for _, c := range tests {
		got, err := unicodeGetItemKind(u, c.i)
		if err != nil {
			t.Errorf("getitem(%d) err: %v", c.i, err)
			continue
		}
		if got.(*Unicode).Value() != c.want {
			t.Errorf("getitem(%d) = %q, want %q", c.i, got.(*Unicode).Value(), c.want)
		}
	}
	if _, err := unicodeGetItemKind(u, 5); err == nil {
		t.Error("getitem(5) on 'hello' should be IndexError")
	}
	if _, err := unicodeGetItemKind(u, -6); err == nil {
		t.Error("getitem(-6) on 'hello' should be IndexError")
	}
}

// Non-ASCII getitem still walks the UTF-8 sequence. Pin the result so
// future fast-path work for kind=1 Latin-1 / kind=2 BMP / kind=4 doesn't
// regress.
//
// CPython: Objects/unicodeobject.c:1848 unicode_getitem (kind > 1 path)
func TestUnicodeGetItemKind_NonASCII(t *testing.T) {
	u := NewStr("café").(*Unicode)
	if u.IsASCII() {
		t.Fatal("setup: 'café' should be non-ASCII")
	}
	got, err := unicodeGetItemKind(u, 3)
	if err != nil {
		t.Fatalf("getitem(3) err: %v", err)
	}
	if got.(*Unicode).Value() != "é" {
		t.Errorf("getitem(3) = %q, want é", got.(*Unicode).Value())
	}
}

// ASCII strings should now round-trip find / count / startswith /
// endswith without re-classifying the haystack on each call. This
// guards the codepoint-vs-byte alignment invariant: every IsASCII
// string has byte length equal to codepoint length.
//
// CPython: Include/cpython/unicodeobject.h PyUnicode_IS_ASCII
// (length(bytes) == length(codepoints) when the ASCII flag is set)
func TestStrKindASCIIInvariant(t *testing.T) {
	for _, s := range []string{"", "a", "hello world", "\x00\x7f"} {
		u := NewStr(s).(*Unicode)
		if !u.IsASCII() {
			t.Errorf("%q expected ASCII", s)
			continue
		}
		if u.Length() != len(u.Value()) {
			t.Errorf("%q: Length=%d, len(v)=%d (ASCII must match)",
				s, u.Length(), len(u.Value()))
		}
	}
}
