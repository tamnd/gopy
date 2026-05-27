// Kind-dispatched fast paths for str search / index / count /
// startswith / endswith / __getitem__. Mirrors CPython's
// `any_find_slice` dispatch tower which switches on
// `PyUnicode_KIND(s)` and routes ASCII-only haystacks through the
// `asciilib_*` family (memchr-fast) rather than the generic UCS scan.
//
// The Unicode object already carries an `ascii` flag set by
// classify() at construction (str.go:312 classify). When the flag is
// set, every codepoint is < 0x80 so its UTF-8 encoding is one byte
// and codepoint index equals byte index. That lets the fast path
// hand the raw Go string straight to strings.Index / Count /
// HasPrefix / HasSuffix without materializing a []rune. The
// allocation savings dominate the pyperformance string benchmarks
// (regex_compile, html5lib, mako, django_template) which work on
// ASCII source.
//
// Non-ASCII strings (kind=1 Latin-1 with codepoints 0x80-0xFF,
// kind=2 BMP, kind=4 full unicode) fall back to the rune-walk path
// in str_methods.go. A follow-up will route those through an
// allocation-free UTF-8 walk too; for now the slow path matches the
// pre-port behavior so correctness is preserved.
//
// CPython: Objects/unicodeobject.c:9680 any_find_slice (kind dispatch)
// CPython: Objects/unicodeobject.c:11779 unicode_count_impl
// CPython: Objects/unicodeobject.c:11964 unicode_find_impl
// CPython: Objects/unicodeobject.c:13617 unicode_startswith_impl
// CPython: Objects/unicodeobject.c:13673 unicode_endswith_impl
// CPython: Objects/unicodeobject.c:1848 unicode_getitem
// CPython: Objects/stringlib/asciilib.h asciilib_find_slice
// CPython: Include/cpython/unicodeobject.h PyUnicode_IS_ASCII

package objects

import (
	"errors"
	"fmt"
	"strings"
)

// strFindKind returns the leftmost codepoint index of needle in
// s[start:end], or -1 if not found. start/end are codepoint indices.
//
// CPython: Objects/unicodeobject.c:11964 unicode_find_impl
func strFindKind(s *Unicode, needle string, start, end int) int {
	if s.ascii {
		return strFindASCII(s.v, needle, start, end)
	}
	return StrFind(s.v, needle, start, end)
}

// strRFindKind is the rightmost variant.
//
// CPython: Objects/unicodeobject.c:13048 unicode_rfind_impl
func strRFindKind(s *Unicode, needle string, start, end int) int {
	if s.ascii {
		return strRFindASCII(s.v, needle, start, end)
	}
	return StrRFind(s.v, needle, start, end)
}

// strIndexKind is strFindKind that raises ValueError on miss.
//
// CPython: Objects/unicodeobject.c:12027 unicode_index_impl
func strIndexKind(s *Unicode, needle string, start, end int) (int, error) {
	i := strFindKind(s, needle, start, end)
	if i < 0 {
		return 0, errors.New("ValueError: substring not found")
	}
	return i, nil
}

// strRIndexKind is the rightmost variant.
//
// CPython: Objects/unicodeobject.c:13069 unicode_rindex_impl
func strRIndexKind(s *Unicode, needle string, start, end int) (int, error) {
	i := strRFindKind(s, needle, start, end)
	if i < 0 {
		return 0, errors.New("ValueError: substring not found")
	}
	return i, nil
}

// strCountKind returns the number of non-overlapping occurrences of
// needle in s[start:end]. Empty needle returns codepoint count + 1.
//
// CPython: Objects/unicodeobject.c:11779 unicode_count_impl
func strCountKind(s *Unicode, needle string, start, end int) int {
	if s.ascii {
		return strCountASCII(s.v, needle, start, end)
	}
	return StrCount(s.v, needle, start, end)
}

// strStartsWithKind reports whether s[start:end] begins with prefix.
//
// CPython: Objects/unicodeobject.c:13617 unicode_startswith_impl
func strStartsWithKind(s *Unicode, prefix string, start, end int) bool {
	if s.ascii {
		return strStartsWithASCII(s.v, prefix, start, end)
	}
	return StrStartsWith(s.v, prefix, start, end)
}

// strEndsWithKind reports whether s[start:end] ends with suffix.
//
// CPython: Objects/unicodeobject.c:13673 unicode_endswith_impl
func strEndsWithKind(s *Unicode, suffix string, start, end int) bool {
	if s.ascii {
		return strEndsWithASCII(s.v, suffix, start, end)
	}
	return StrEndsWith(s.v, suffix, start, end)
}

// unicodeGetItemKind returns the i-th codepoint as a one-char string.
// Handles negative indices. Every kind reads its pre-encoded slab in
// O(1); no UTF-8 walk. Single codepoints < 0x100 route through the
// latin1 singleton cache (`get_latin1_char` in CPython), so the fast
// path returns the shared instance and skips Unicode allocation.
//
// CPython: Objects/unicodeobject.c:1848 unicode_getitem
// CPython: Objects/unicodeobject.c:1958 get_latin1_char
func unicodeGetItemKind(s *Unicode, i int) (Object, error) {
	if i < 0 {
		i += s.length
	}
	if i < 0 || i >= s.length {
		return nil, fmt.Errorf("IndexError: string index out of range")
	}
	if s.ascii {
		return latin1Cache[s.v[i]], nil
	}
	switch s.kind {
	case StrKind1Byte:
		return latin1Cache[s.data1[i]], nil
	case StrKind2Byte:
		r := rune(s.data2[i])
		if r < 0x100 {
			return latin1Cache[r], nil
		}
		return NewStr(runeToStr(r)), nil
	case StrKind4Byte:
		r := rune(s.data4[i])
		if r < 0x100 {
			return latin1Cache[r], nil
		}
		return NewStr(runeToStr(r)), nil
	}
	return nil, fmt.Errorf("IndexError: string index out of range")
}

// strFindASCII is the kind-1 ASCII fast path: codepoint index equals
// byte index so strings.Index runs directly on the Go-string view
// without materializing a rune slice.
//
// CPython: Objects/stringlib/asciilib.h asciilib_find_slice
func strFindASCII(s, needle string, start, end int) int {
	n := len(s)
	start, end = clampStrSlice(n, start, end)
	if end-start < len(needle) {
		return -1
	}
	idx := strings.Index(s[start:end], needle)
	if idx < 0 {
		return -1
	}
	return start + idx
}

// strRFindASCII is the kind-1 ASCII rfind fast path.
//
// CPython: Objects/stringlib/asciilib.h asciilib_rfind_slice
func strRFindASCII(s, needle string, start, end int) int {
	n := len(s)
	start, end = clampStrSlice(n, start, end)
	if end-start < len(needle) {
		return -1
	}
	idx := strings.LastIndex(s[start:end], needle)
	if idx < 0 {
		return -1
	}
	return start + idx
}

// strCountASCII is the kind-1 ASCII count fast path. Empty needle
// returns end-start+1 to match CPython's "split-point" semantics.
//
// CPython: Objects/stringlib/asciilib.h asciilib_count
func strCountASCII(s, needle string, start, end int) int {
	n := len(s)
	start, end = clampStrSlice(n, start, end)
	view := s[start:end]
	if needle == "" {
		return len(view) + 1
	}
	return strings.Count(view, needle)
}

// strStartsWithASCII is the kind-1 ASCII startswith fast path.
//
// CPython: Objects/unicodeobject.c:13617 unicode_startswith_impl
func strStartsWithASCII(s, prefix string, start, end int) bool {
	n := len(s)
	start, end = clampStrSlice(n, start, end)
	return strings.HasPrefix(s[start:end], prefix)
}

// strEndsWithASCII is the kind-1 ASCII endswith fast path.
//
// CPython: Objects/unicodeobject.c:13673 unicode_endswith_impl
func strEndsWithASCII(s, suffix string, start, end int) bool {
	n := len(s)
	start, end = clampStrSlice(n, start, end)
	return strings.HasSuffix(s[start:end], suffix)
}
