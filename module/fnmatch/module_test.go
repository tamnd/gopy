// Tests for the fnmatch Go port. Two layers:
//
//   - translate() byte-equivalence with CPython for representative
//     patterns (the regex strings, even though gopy's re port cannot
//     execute them yet).
//   - matchGlob() behavioral equivalence with CPython fnmatch
//     semantics for the patterns the spec calls out.
//
// CPython: Lib/test/test_fnmatch.py the upstream test set

package fnmatch

import "testing"

func TestTranslateByteEquivalent(t *testing.T) {
	cases := []struct {
		pat, want string
	}{
		// CPython: `python3 -c "import fnmatch; print(fnmatch.translate('*.py'))"`
		{`*.py`, `(?s:.*\.py)\z`},
		{`test_[a-c]?.py`, `(?s:test_[a-c].\.py)\z`},
		{`[!abc]*`, `(?s:[^abc].*)\z`},
		{``, `(?s:)\z`},
		{`?`, `(?s:.)\z`},
		{`a`, `(?s:a)\z`},
		// Multiple stars compressed to a single `.*` at the end.
		{`a***b`, `(?s:a.*b)\z`},
	}
	for _, c := range cases {
		got := translate(c.pat)
		if got != c.want {
			t.Errorf("translate(%q) = %q, want %q", c.pat, got, c.want)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		name, pat string
		want      bool
	}{
		{"test_one.py", "test*.py", true},
		{"Test.py", "test*.py", false},
		{"a.py", "*.py", true},
		{"b.txt", "*.py", false},
		{"abc", "?bc", true},
		{"abc", "[abc]bc", true},
		{"dbc", "[abc]bc", false},
		{"dbc", "[!abc]bc", true},
		{"abc", "[!abc]bc", false},
		{"abc", "a[b-d]c", true},
		{"aec", "a[b-d]c", false},
		// Unterminated bracket: literal `[`.
		{"[a", "[a", true},
		// Compressed stars.
		{"abcdef", "a**f", true},
	}
	for _, c := range cases {
		got := matchGlob(c.name, c.pat)
		if got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.name, c.pat, got, c.want)
		}
	}
}

func TestIndexInRange(t *testing.T) {
	cases := []struct {
		s, sub          string
		start, end, exp int
	}{
		{"abc-def", "-", 0, 7, 3},
		{"abc-def", "-", 4, 7, -1},
		{"abc-def-ghi", "-", 4, 11, 7},
		{"abc", "-", 0, 3, -1},
		{"abc", "-", 5, 3, -1},
	}
	for _, c := range cases {
		got := indexInRange(c.s, c.sub, c.start, c.end)
		if got != c.exp {
			t.Errorf("indexInRange(%q,%q,%d,%d) = %d, want %d", c.s, c.sub, c.start, c.end, got, c.exp)
		}
	}
}
