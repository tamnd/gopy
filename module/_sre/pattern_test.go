package _sre

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// makeCodeList wraps a uint32 bytecode buffer in a Python list of
// ints, matching what _compiler.py hands to _sre.compile.
func makeCodeList(code []uint32) *objects.List {
	items := make([]objects.Object, len(code))
	for i, v := range code {
		items[i] = objects.NewInt(int64(v))
	}
	return objects.NewList(items)
}

// compileFor wraps sreCompile with the standard
// (pattern, flags, code, groups, groupindex, indexgroup) arg shape.
func compileFor(t *testing.T, pattern string, code []uint32, groups int) objects.Object {
	t.Helper()
	args := []objects.Object{
		objects.NewStr(pattern),
		objects.NewInt(0),
		makeCodeList(code),
		objects.NewInt(int64(groups)),
		objects.NewDict(),
		objects.None(),
	}
	p, err := sreCompile(args, nil)
	if err != nil {
		t.Fatalf("sreCompile: %v", err)
	}
	return p
}

// TestPatternCompileMatch covers the simplest end-to-end shape: hand
// _sre.compile a bytecode list, call Pattern.match, get back a Match
// whose group(0) is the matched substring.
func TestPatternCompileMatch(t *testing.T) {
	code := []uint32{OpLiteral, 'a', OpSuccess}
	pat := compileFor(t, "a", code, 0)

	m, err := patternMatch([]objects.Object{pat, objects.NewStr("abc")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("expected Match, got None")
	}
	g, err := matchGroup([]objects.Object{m}, nil)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	s, _ := objects.Str(g)
	if s != "a" {
		t.Fatalf("group(0) = %q; want %q", s, "a")
	}
}

// TestPatternCompileMatchFail confirms a non-matching prefix returns
// None instead of an error.
func TestPatternCompileMatchFail(t *testing.T) {
	code := []uint32{OpLiteral, 'a', OpSuccess}
	pat := compileFor(t, "a", code, 0)

	m, err := patternMatch([]objects.Object{pat, objects.NewStr("bcd")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if !objects.IsNone(m) {
		t.Fatal("expected None")
	}
}

// TestPatternSearch confirms search() walks the input to find a match.
func TestPatternSearch(t *testing.T) {
	code := []uint32{OpLiteral, 'b', OpSuccess}
	pat := compileFor(t, "b", code, 0)

	m, err := patternSearch([]objects.Object{pat, objects.NewStr("aabc")}, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("expected Match")
	}
	st, err := matchStart([]objects.Object{m}, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	si, _ := st.(*objects.Int).Int64()
	if si != 2 {
		t.Fatalf("start = %d; want 2", si)
	}
}

// TestPatternFullmatch confirms fullmatch insists the whole input is
// consumed.
func TestPatternFullmatch(t *testing.T) {
	// 'a' literal: matches "a" fully, not "ab".
	code := []uint32{OpLiteral, 'a', OpSuccess}
	pat := compileFor(t, "a", code, 0)

	m, err := patternFullmatch([]objects.Object{pat, objects.NewStr("a")}, nil)
	if err != nil {
		t.Fatalf("fullmatch: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("expected fullmatch on 'a'")
	}

	m, err = patternFullmatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("fullmatch: %v", err)
	}
	if !objects.IsNone(m) {
		t.Fatal("expected None for 'ab' (not full)")
	}
}

// TestPatternGroupCapture confirms mark[]-based group capture survives
// the round trip through makeMatch -> matchGroup.
func TestPatternGroupCapture(t *testing.T) {
	// (a) against "a": MARK 0 LITERAL 'a' MARK 1 SUCCESS, groups=1.
	code := []uint32{
		OpMark, 0,
		OpLiteral, 'a',
		OpMark, 1,
		OpSuccess,
	}
	pat := compileFor(t, "(a)", code, 1)

	m, err := patternMatch([]objects.Object{pat, objects.NewStr("a")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("expected match")
	}
	g, err := matchGroup([]objects.Object{m, objects.NewInt(1)}, nil)
	if err != nil {
		t.Fatalf("group(1): %v", err)
	}
	s, _ := objects.Str(g)
	if s != "a" {
		t.Fatalf("group(1) = %q; want %q", s, "a")
	}
}
