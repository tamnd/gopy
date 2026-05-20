package objects

import (
	"reflect"
	"testing"
)

func TestStrFind(t *testing.T) {
	cases := []struct {
		s, needle  string
		start, end int
		want       int
	}{
		{"hello world", "world", 0, 11, 6},
		{"hello world", "xyz", 0, 11, -1},
		{"abcabc", "b", 0, 6, 1},
		{"abcabc", "b", 2, 6, 4},
		{"café au lait", "au", 0, 12, 5},
		{"中文中文", "文", 0, 4, 1},
	}
	for _, c := range cases {
		got := StrFind(c.s, c.needle, c.start, c.end)
		if got != c.want {
			t.Errorf("StrFind(%q, %q, %d, %d) = %d, want %d",
				c.s, c.needle, c.start, c.end, got, c.want)
		}
	}
}

func TestStrRFind(t *testing.T) {
	if got := StrRFind("abcabc", "b", 0, 6); got != 4 {
		t.Errorf("rfind 'abcabc','b' = %d, want 4", got)
	}
	if got := StrRFind("abc", "x", 0, 3); got != -1 {
		t.Errorf("rfind 'abc','x' = %d, want -1", got)
	}
}

func TestStrIndexErrors(t *testing.T) {
	if _, err := StrIndex("abc", "x", 0, 3); err == nil {
		t.Error("StrIndex on miss should error")
	}
	if _, err := StrRIndex("abc", "x", 0, 3); err == nil {
		t.Error("StrRIndex on miss should error")
	}
	if got, err := StrIndex("abc", "b", 0, 3); err != nil || got != 1 {
		t.Errorf("StrIndex hit = (%d,%v), want (1,nil)", got, err)
	}
}

func TestStrCount(t *testing.T) {
	if got := StrCount("abcabcabc", "abc", 0, 9); got != 3 {
		t.Errorf("count = %d, want 3", got)
	}
	if got := StrCount("aaaa", "aa", 0, 4); got != 2 {
		t.Errorf("non-overlap count = %d, want 2", got)
	}
	if got := StrCount("abc", "", 0, 3); got != 4 {
		t.Errorf("empty needle count = %d, want 4", got)
	}
}

func TestStrStartsEndsWith(t *testing.T) {
	if !StrStartsWith("hello", "he", 0, 5) {
		t.Error("hello startswith he")
	}
	if StrStartsWith("hello", "lo", 0, 5) {
		t.Error("hello does not start with lo")
	}
	if !StrEndsWith("hello", "lo", 0, 5) {
		t.Error("hello endswith lo")
	}
	if StrEndsWith("hello", "he", 0, 5) {
		t.Error("hello does not end with he")
	}
}

func TestStrSplit(t *testing.T) {
	got, _ := StrSplit("a,b,c", ",", -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("split = %v", got)
	}
	got, _ = StrSplit("a,b,c", ",", 1)
	if !reflect.DeepEqual(got, []string{"a", "b,c"}) {
		t.Errorf("split maxsplit=1 = %v", got)
	}
	got, _ = StrSplit("  a  b  c  ", "", -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("whitespace split = %v", got)
	}
	got, _ = StrSplit("  a  b  c  ", "", 1)
	if !reflect.DeepEqual(got, []string{"a", "b  c  "}) {
		t.Errorf("whitespace split maxsplit=1 = %v", got)
	}
}

func TestStrRSplit(t *testing.T) {
	got, _ := StrRSplit("a,b,c", ",", 1)
	if !reflect.DeepEqual(got, []string{"a,b", "c"}) {
		t.Errorf("rsplit maxsplit=1 = %v", got)
	}
	got, _ = StrRSplit("a,b,c", ",", -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("rsplit unlimited = %v", got)
	}
	got, _ = StrRSplit("  a  b  c  ", "", 1)
	if !reflect.DeepEqual(got, []string{"  a  b", "c"}) {
		t.Errorf("rsplit ws maxsplit=1 = %v", got)
	}
}

// TestStrSplitWhitespaceASCIIControls pins the broader Py_UNICODE_ISSPACE
// set (0x09-0x0D, 0x1C-0x1F, 0x20) which CPython recognizes for the
// sep=None mode. Go's unicode.IsSpace misses 0x1C-0x1F (FS/GS/RS/US)
// so a naive port via that helper drops the file/group/record/unit
// separator chars on the floor.
//
// CPython: Objects/unicodetype_db.h:6676 _PyUnicode_IsWhitespace
func TestStrSplitWhitespaceASCIIControls(t *testing.T) {
	got, _ := StrSplit("a\x1cb\x1dc\x1ed\x1fe", "", -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c", "d", "e"}) {
		t.Errorf("split with 0x1c-0x1f = %v", got)
	}
	got, _ = StrSplit("\x1c\x1f a\tb \nc\v\f", "", -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("split mixed ASCII whitespace = %v", got)
	}
}

// TestStrSplitWhitespaceNonASCII covers the rune-walk slow path.
// Latin-1 NBSP (0xA0) and the Unicode line/para separators must split.
func TestStrSplitWhitespaceNonASCII(t *testing.T) {
	got, _ := StrSplit("a b c", "", -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("split with NBSP / LSEP = %v", got)
	}
}

func TestStrSplitLines(t *testing.T) {
	got := StrSplitLines("a\nb\r\nc\rd", false)
	if !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("splitlines = %v", got)
	}
	got = StrSplitLines("a\nb\r\nc", true)
	if !reflect.DeepEqual(got, []string{"a\n", "b\r\n", "c"}) {
		t.Errorf("splitlines keepends = %v", got)
	}
	got = StrSplitLines("", false)
	if got != nil {
		t.Errorf("splitlines empty = %v, want nil", got)
	}
	got = StrSplitLines("a b", false)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("splitlines U+2028 = %v", got)
	}
}

func TestStrPartition(t *testing.T) {
	a, b, c, _ := StrPartition("a-b-c", "-")
	if a != "a" || b != "-" || c != "b-c" {
		t.Errorf("partition = (%q,%q,%q)", a, b, c)
	}
	a, b, c, _ = StrPartition("abc", "-")
	if a != "abc" || b != "" || c != "" {
		t.Errorf("partition miss = (%q,%q,%q)", a, b, c)
	}
	if _, _, _, err := StrPartition("abc", ""); err == nil {
		t.Error("empty sep should error")
	}
}

func TestStrRPartition(t *testing.T) {
	a, b, c, _ := StrRPartition("a-b-c", "-")
	if a != "a-b" || b != "-" || c != "c" {
		t.Errorf("rpartition = (%q,%q,%q)", a, b, c)
	}
	a, b, c, _ = StrRPartition("abc", "-")
	if a != "" || b != "" || c != "abc" {
		t.Errorf("rpartition miss = (%q,%q,%q)", a, b, c)
	}
}

func TestStrJoin(t *testing.T) {
	parts := []Object{NewStr("a"), NewStr("b"), NewStr("c")}
	got, err := StrJoin(", ", parts)
	if err != nil || got != "a, b, c" {
		t.Errorf("join = (%q,%v)", got, err)
	}
	bad := []Object{NewStr("a"), NewInt(1)}
	if _, err := StrJoin(",", bad); err == nil {
		t.Error("non-str item should error")
	}
}

func TestStrReplace(t *testing.T) {
	if StrReplace("aaa", "a", "b", -1) != "bbb" {
		t.Error("replace all")
	}
	if StrReplace("aaa", "a", "b", 2) != "bba" {
		t.Error("replace count=2")
	}
	if StrReplace("aaa", "a", "b", 0) != "aaa" {
		t.Error("replace count=0")
	}
}

func TestStrStrip(t *testing.T) {
	if StrStrip("  hi  ", "") != "hi" {
		t.Error("strip ws")
	}
	if StrStrip("xxhixx", "x") != "hi" {
		t.Error("strip chars")
	}
	if StrLStrip("  hi  ", "") != "hi  " {
		t.Error("lstrip")
	}
	if StrRStrip("  hi  ", "") != "  hi" {
		t.Error("rstrip")
	}
}

// TestStrStripASCIIControls pins the 0x1C-0x1F semantic fix:
// _PyUnicode_IsWhitespace recognises FS/GS/RS/US as whitespace, so
// CPython strip drops them. Pre-port gopy used unicode.IsSpace which
// misses them, so " \x1c\x1dhi\x1e\x1f " would have kept the controls.
//
// CPython: Objects/unicodetype_db.h:6676 _PyUnicode_IsWhitespace
func TestStrStripASCIIControls(t *testing.T) {
	in := " \x1c\x1dhi\x1e\x1f "
	if got := StrStrip(in, ""); got != "hi" {
		t.Errorf("StrStrip(%q) = %q, want %q", in, got, "hi")
	}
	if got := StrLStrip(in, ""); got != "hi\x1e\x1f " {
		t.Errorf("StrLStrip(%q) = %q", in, got)
	}
	if got := StrRStrip(in, ""); got != " \x1c\x1dhi" {
		t.Errorf("StrRStrip(%q) = %q", in, got)
	}
	// Pure controls strip down to the empty string.
	if got := StrStrip("\x1c\x1d\x1e\x1f", ""); got != "" {
		t.Errorf("StrStrip(all-controls) = %q", got)
	}
}

// TestStrStripNonASCII keeps the rune slow path honest. NBSP (U+00A0)
// is whitespace under _PyUnicode_IsWhitespace, so it must be trimmed
// from both ends, including around non-ASCII payload.
//
// CPython: Objects/unicodeobject.c:11744 _PyUnicode_XStrip
func TestStrStripNonASCII(t *testing.T) {
	in := "  héllo  "
	if got := StrStrip(in, ""); got != "héllo" {
		t.Errorf("StrStrip(%q) = %q", in, got)
	}
	if got := StrLStrip(in, ""); got != "héllo  " {
		t.Errorf("StrLStrip(%q) = %q", in, got)
	}
	if got := StrRStrip(in, ""); got != "  héllo" {
		t.Errorf("StrRStrip(%q) = %q", in, got)
	}
}

func TestStrCase(t *testing.T) {
	if StrLower("Hello") != "hello" {
		t.Error("lower")
	}
	if StrUpper("hello") != "HELLO" {
		t.Error("upper")
	}
	if StrCaseFold("HELLO") != "hello" {
		t.Error("casefold")
	}
	if StrSwapCase("Hello") != "hELLO" {
		t.Error("swapcase")
	}
	if StrCapitalize("hello world") != "Hello world" {
		t.Error("capitalize")
	}
	if StrTitle("hello world") != "Hello World" {
		t.Error("title")
	}
	if StrTitle("they're") != "They'Re" {
		// CPython behavior: apostrophe is non-cased so it breaks the word
		t.Errorf("title apostrophe = %q", StrTitle("they're"))
	}
}

func TestStrPad(t *testing.T) {
	if StrCenter("hi", 6, '*') != "**hi**" {
		t.Errorf("center = %q", StrCenter("hi", 6, '*'))
	}
	if StrCenter("hi", 5, '*') != "**hi*" {
		// CPython: extra char goes on the right when width is odd-but-with-even-pad,
		// or on the left when both width and pad are odd.
		t.Errorf("center 5 = %q", StrCenter("hi", 5, '*'))
	}
	if StrLJust("hi", 5, '.') != "hi..." {
		t.Errorf("ljust = %q", StrLJust("hi", 5, '.'))
	}
	if StrRJust("hi", 5, '.') != "...hi" {
		t.Errorf("rjust = %q", StrRJust("hi", 5, '.'))
	}
	if StrCenter("hello", 3, '*') != "hello" {
		t.Error("center smaller width returns original")
	}
}

func TestStrZFill(t *testing.T) {
	if StrZFill("42", 5) != "00042" {
		t.Errorf("zfill = %q", StrZFill("42", 5))
	}
	if StrZFill("-42", 5) != "-0042" {
		t.Errorf("zfill negative = %q", StrZFill("-42", 5))
	}
	if StrZFill("+42", 5) != "+0042" {
		t.Errorf("zfill positive = %q", StrZFill("+42", 5))
	}
	if StrZFill("12345", 3) != "12345" {
		t.Error("zfill smaller width returns original")
	}
}

func TestStrExpandTabs(t *testing.T) {
	if StrExpandTabs("a\tb", 8) != "a       b" {
		t.Errorf("expandtabs basic = %q", StrExpandTabs("a\tb", 8))
	}
	if StrExpandTabs("\t\t", 4) != "        " {
		t.Errorf("expandtabs double = %q", StrExpandTabs("\t\t", 4))
	}
	if StrExpandTabs("a\nb\tc", 4) != "a\nb   c" {
		t.Errorf("expandtabs newline-resets = %q", StrExpandTabs("a\nb\tc", 4))
	}
}

func TestStrTranslate(t *testing.T) {
	tab, err := MakeStrTrans("ab", "xy", "c")
	if err != nil {
		t.Fatal(err)
	}
	if got := StrTranslate("abc", tab); got != "xy" {
		t.Errorf("translate = %q, want xy", got)
	}
	if _, err := MakeStrTrans("abc", "xy", ""); err == nil {
		t.Error("length mismatch should error")
	}
}

func TestStrIsASCII(t *testing.T) {
	if !StrIsASCII("hello") {
		t.Error("hello is ascii")
	}
	if StrIsASCII("café") {
		t.Error("café is not ascii")
	}
	if !StrIsASCII("") {
		t.Error("empty string is ascii (matches CPython)")
	}
}

func TestStrIsLower(t *testing.T) {
	if !StrIsLower("hello") {
		t.Error("hello islower")
	}
	if StrIsLower("Hello") {
		t.Error("Hello not islower")
	}
	if StrIsLower("123") {
		t.Error("123 has no cased chars, not islower")
	}
	if !StrIsLower("hello123") {
		t.Error("hello123 islower")
	}
}

func TestStrIsUpper(t *testing.T) {
	if !StrIsUpper("HELLO") {
		t.Error("HELLO isupper")
	}
	if StrIsUpper("Hello") {
		t.Error("Hello not isupper")
	}
}

func TestStrIsTitle(t *testing.T) {
	if !StrIsTitle("Hello World") {
		t.Error("Hello World istitle")
	}
	if StrIsTitle("Hello world") {
		t.Error("Hello world not istitle")
	}
	if StrIsTitle("") {
		t.Error("empty not istitle")
	}
}

func TestStrIsSpace(t *testing.T) {
	if !StrIsSpace("   ") {
		t.Error("'   ' isspace")
	}
	if StrIsSpace("") {
		t.Error("empty not isspace")
	}
	if StrIsSpace(" a ") {
		t.Error("' a ' not isspace")
	}
}

// TestStrIsSpaceASCIIControls pins the 0x1C-0x1F semantic fix: CPython
// _PyUnicode_IsWhitespace returns true for FS/GS/RS/US, so str.isspace
// on a string of just those four bytes is True. Go's unicode.IsSpace
// drops them and the pre-port code returned False.
//
// CPython: Objects/unicodeobject.c:12209 unicode_isspace_impl
func TestStrIsSpaceASCIIControls(t *testing.T) {
	if !StrIsSpace("\x1c\x1d\x1e\x1f") {
		t.Error("FS/GS/RS/US isspace")
	}
	if !StrIsSpace(" \t\n\v\f\r\x1c\x1d\x1e\x1f") {
		t.Error("mixed ASCII whitespace isspace")
	}
	if StrIsSpace("a\x1c") {
		t.Error("payload + control not isspace")
	}
}

// TestStrIsSpaceNonASCII confirms the rune slow path uses the broader
// Unicode whitespace set: NBSP (U+00A0) is whitespace under
// _PyUnicode_IsWhitespace and Go's unicode.IsSpace; ogham space mark
// (U+1680) and line/paragraph separators (U+2028 / U+2029) are too.
func TestStrIsSpaceNonASCII(t *testing.T) {
	if !StrIsSpace("   ") {
		t.Error("nbsp + line/para sep isspace")
	}
	if StrIsSpace("a ") {
		t.Error("payload + nbsp not isspace")
	}
}

func TestStrIsAlpha(t *testing.T) {
	if !StrIsAlpha("abc") {
		t.Error("abc isalpha")
	}
	if StrIsAlpha("abc1") {
		t.Error("abc1 not isalpha")
	}
	if StrIsAlpha("") {
		t.Error("empty not isalpha")
	}
}

func TestStrIsAlnum(t *testing.T) {
	if !StrIsAlnum("abc123") {
		t.Error("abc123 isalnum")
	}
	if StrIsAlnum("abc 1") {
		t.Error("space breaks isalnum")
	}
}

func TestStrIsDecimal(t *testing.T) {
	if !StrIsDecimal("12345") {
		t.Error("12345 isdecimal")
	}
	if StrIsDecimal("12.5") {
		t.Error("12.5 not isdecimal")
	}
	if StrIsDecimal("") {
		t.Error("empty not isdecimal")
	}
}

func TestStrIsDigitNumeric(t *testing.T) {
	if !StrIsDigit("123") {
		t.Error("123 isdigit")
	}
	if !StrIsNumeric("123") {
		t.Error("123 isnumeric")
	}
	if StrIsDigit("") {
		t.Error("empty not isdigit")
	}
}

func TestStrIsIdentifier(t *testing.T) {
	if !StrIsIdentifier("foo_bar") {
		t.Error("foo_bar isidentifier")
	}
	if !StrIsIdentifier("_x") {
		t.Error("_x isidentifier")
	}
	if !StrIsIdentifier("foo123") {
		t.Error("foo123 isidentifier")
	}
	if StrIsIdentifier("123abc") {
		t.Error("123abc not isidentifier (digit-leading)")
	}
	if StrIsIdentifier("") {
		t.Error("empty not isidentifier")
	}
	if StrIsIdentifier("foo bar") {
		t.Error("space breaks isidentifier")
	}
}

func TestStrIsPrintable(t *testing.T) {
	if !StrIsPrintable("hello") {
		t.Error("hello isprintable")
	}
	if !StrIsPrintable("hello world") {
		t.Error("space is printable")
	}
	if StrIsPrintable("a\tb") {
		t.Error("tab not printable")
	}
	if !StrIsPrintable("") {
		t.Error("empty isprintable (matches CPython)")
	}
}
