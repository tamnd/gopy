package _sre

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// literalAPattern compiles "a" against a no-group bytecode.
func literalAPattern(t *testing.T) objects.Object {
	t.Helper()
	code := []uint32{OpLiteral, 'a', OpSuccess}
	return compileFor(t, "a", code, 0)
}

// twoGroupABPattern compiles (a)(b) — same pattern test/match_test.go
// uses, replicated here so pattern_methods tests stay self-contained.
func twoGroupABPattern(t *testing.T) objects.Object {
	t.Helper()
	code := []uint32{
		OpMark, 0,
		OpLiteral, 'a',
		OpMark, 1,
		OpMark, 2,
		OpLiteral, 'b',
		OpMark, 3,
		OpSuccess,
	}
	return compileFor(t, "(a)(b)", code, 2)
}

// TestPatternFindallNoGroups pins findall returns a list of matched
// substrings when the pattern has no capture groups.
func TestPatternFindallNoGroups(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternFindall([]objects.Object{pat, objects.NewStr("aXaYaZ")}, nil)
	if err != nil {
		t.Fatalf("findall: %v", err)
	}
	l, ok := r.(*objects.List)
	if !ok {
		t.Fatalf("findall = %T; want List", r)
	}
	if l.Len() != 3 {
		t.Fatalf("findall len = %d; want 3", l.Len())
	}
	for i := 0; i < l.Len(); i++ {
		s, _ := objects.Str(l.Item(i))
		if s != "a" {
			t.Errorf("findall[%d] = %q; want %q", i, s, "a")
		}
	}
}

// TestPatternFindallTwoGroups pins findall returns a list of tuples
// when groups > 1.
func TestPatternFindallTwoGroups(t *testing.T) {
	pat := twoGroupABPattern(t)
	r, err := patternFindall([]objects.Object{pat, objects.NewStr("ababZab")}, nil)
	if err != nil {
		t.Fatalf("findall: %v", err)
	}
	l := r.(*objects.List)
	if l.Len() != 3 {
		t.Fatalf("findall len = %d; want 3", l.Len())
	}
	for i := 0; i < l.Len(); i++ {
		tu, ok := l.Item(i).(*objects.Tuple)
		if !ok {
			t.Fatalf("findall[%d] = %T; want Tuple", i, l.Item(i))
		}
		a, _ := objects.Str(tu.Item(0))
		b, _ := objects.Str(tu.Item(1))
		if a != "a" || b != "b" {
			t.Errorf("findall[%d] = (%q,%q); want (a,b)", i, a, b)
		}
	}
}

// TestPatternFinditer pins finditer yields Match objects.
func TestPatternFinditer(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternFinditer([]objects.Object{pat, objects.NewStr("axa")}, nil)
	if err != nil {
		t.Fatalf("finditer: %v", err)
	}
	l := r.(*objects.List)
	if l.Len() != 2 {
		t.Fatalf("finditer len = %d; want 2", l.Len())
	}
	// Confirm each item is a Match (Instance of MatchType).
	for i := 0; i < l.Len(); i++ {
		inst, ok := l.Item(i).(*objects.Instance)
		if !ok || inst.Type() != MatchType {
			t.Errorf("finditer[%d] = %T; want Match", i, l.Item(i))
		}
	}
}

// TestPatternSubString pins sub with a literal replacement template.
func TestPatternSubString(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternSub([]objects.Object{pat, objects.NewStr("X"), objects.NewStr("aba")}, nil)
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	s, _ := objects.Str(r)
	if s != "XbX" {
		t.Errorf("sub = %q; want %q", s, "XbX")
	}
}

// TestPatternSubCount pins the optional count argument limits the
// number of replacements.
func TestPatternSubCount(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternSub([]objects.Object{
		pat, objects.NewStr("X"), objects.NewStr("aaa"), objects.NewInt(2),
	}, nil)
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	s, _ := objects.Str(r)
	if s != "XXa" {
		t.Errorf("sub = %q; want %q", s, "XXa")
	}
}

// TestPatternSubBackref pins backreferences in the template render the
// captured groups.
func TestPatternSubBackref(t *testing.T) {
	pat := twoGroupABPattern(t)
	r, err := patternSub([]objects.Object{
		pat, objects.NewStr(`[\1-\2]`), objects.NewStr("Xab Yab"),
	}, nil)
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	s, _ := objects.Str(r)
	if s != "X[a-b] Y[a-b]" {
		t.Errorf("sub = %q; want %q", s, "X[a-b] Y[a-b]")
	}
}

// TestPatternSubn pins the return shape (result, n).
func TestPatternSubn(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternSubn([]objects.Object{pat, objects.NewStr("Y"), objects.NewStr("aXa")}, nil)
	if err != nil {
		t.Fatalf("subn: %v", err)
	}
	tu := r.(*objects.Tuple)
	if tu.Len() != 2 {
		t.Fatalf("subn len = %d; want 2", tu.Len())
	}
	s, _ := objects.Str(tu.Item(0))
	if s != "YXY" {
		t.Errorf("subn[0] = %q; want %q", s, "YXY")
	}
	n, _ := tu.Item(1).(*objects.Int).Int64()
	if n != 2 {
		t.Errorf("subn[1] = %d; want 2", n)
	}
}

// TestPatternSplit pins split without groups: ['x', 'y', 'z'].
func TestPatternSplit(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternSplit([]objects.Object{pat, objects.NewStr("xayaz")}, nil)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	l := r.(*objects.List)
	if l.Len() != 3 {
		t.Fatalf("split len = %d; want 3", l.Len())
	}
	for i, want := range []string{"x", "y", "z"} {
		s, _ := objects.Str(l.Item(i))
		if s != want {
			t.Errorf("split[%d] = %q; want %q", i, s, want)
		}
	}
}

// TestPatternSplitMaxsplit pins maxsplit caps the splits.
func TestPatternSplitMaxsplit(t *testing.T) {
	pat := literalAPattern(t)
	r, err := patternSplit([]objects.Object{
		pat, objects.NewStr("xayaz"), objects.NewInt(1),
	}, nil)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	l := r.(*objects.List)
	if l.Len() != 2 {
		t.Fatalf("split len = %d; want 2", l.Len())
	}
	s0, _ := objects.Str(l.Item(0))
	s1, _ := objects.Str(l.Item(1))
	if s0 != "x" || s1 != "yaz" {
		t.Errorf("split = [%q, %q]; want [\"x\", \"yaz\"]", s0, s1)
	}
}

// TestPatternSplitWithGroups pins captured groups are interleaved into
// the result list.
func TestPatternSplitWithGroups(t *testing.T) {
	pat := twoGroupABPattern(t)
	r, err := patternSplit([]objects.Object{pat, objects.NewStr("xabYabz")}, nil)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	l := r.(*objects.List)
	// Layout: ["x", "a", "b", "Y", "a", "b", "z"].
	wantLen := 7
	if l.Len() != wantLen {
		t.Fatalf("split len = %d; want %d", l.Len(), wantLen)
	}
	want := []string{"x", "a", "b", "Y", "a", "b", "z"}
	for i, w := range want {
		got, _ := objects.Str(l.Item(i))
		if got != w {
			t.Errorf("split[%d] = %q; want %q", i, got, w)
		}
	}
}
