package _sre

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// twoGroupPattern compiles a bytecode pattern matching two consecutive
// literal characters as two separate groups: (a)(b) against "ab".
//
//	MARK 0 LITERAL 'a' MARK 1 MARK 2 LITERAL 'b' MARK 3 SUCCESS, groups=2
func twoGroupPattern(t *testing.T) objects.Object {
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

// TestMatchGroupsTuple pins that groups() returns a tuple of all
// captured group strings (one entry per group, group 0 excluded).
func TestMatchGroupsTuple(t *testing.T) {
	pat := twoGroupPattern(t)
	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("expected match")
	}
	tup, err := matchGroups([]objects.Object{m}, nil)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	tu, ok := tup.(*objects.Tuple)
	if !ok {
		t.Fatalf("groups() = %T; want Tuple", tup)
	}
	if tu.Len() != 2 {
		t.Fatalf("groups() len = %d; want 2", tu.Len())
	}
	for i, want := range []string{"a", "b"} {
		got, _ := objects.Str(tu.Item(i))
		if got != want {
			t.Errorf("groups()[%d] = %q; want %q", i, got, want)
		}
	}
}

// TestMatchSpan pins that span() returns (start, end) for a group.
func TestMatchSpan(t *testing.T) {
	pat := twoGroupPattern(t)
	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	tup, err := matchSpan([]objects.Object{m, objects.NewInt(1)}, nil)
	if err != nil {
		t.Fatalf("span: %v", err)
	}
	tu, ok := tup.(*objects.Tuple)
	if !ok || tu.Len() != 2 {
		t.Fatalf("span() = %v; want 2-tuple", tup)
	}
	lo, _ := tu.Item(0).(*objects.Int).Int64()
	hi, _ := tu.Item(1).(*objects.Int).Int64()
	if lo != 0 || hi != 1 {
		t.Errorf("span(1) = (%d, %d); want (0, 1)", lo, hi)
	}
}

// TestMatchStartEnd pins start() and end() for individual groups.
func TestMatchStartEnd(t *testing.T) {
	pat := twoGroupPattern(t)
	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	st, err := matchStart([]objects.Object{m, objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	si, _ := st.(*objects.Int).Int64()
	if si != 1 {
		t.Errorf("start(2) = %d; want 1", si)
	}
	en, err := matchEnd([]objects.Object{m, objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	ei, _ := en.(*objects.Int).Int64()
	if ei != 2 {
		t.Errorf("end(2) = %d; want 2", ei)
	}
}

// TestMatchGroupByName pins that named-group lookup goes through the
// Pattern's groupindex dict.
func TestMatchGroupByName(t *testing.T) {
	pat := twoGroupPattern(t)
	patInst := pat.(*objects.Instance)
	gi := objects.NewDict()
	_ = gi.SetItem(objects.NewStr("first"), objects.NewInt(1))
	_ = gi.SetItem(objects.NewStr("second"), objects.NewInt(2))
	_ = patInst.Dict().SetItem(objects.NewStr("groupindex"), gi)

	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	g, err := matchGroup([]objects.Object{m, objects.NewStr("second")}, nil)
	if err != nil {
		t.Fatalf("group(second): %v", err)
	}
	s, _ := objects.Str(g)
	if s != "b" {
		t.Errorf("group('second') = %q; want %q", s, "b")
	}
}

// TestMatchGroupdict pins groupdict() against the pattern's named
// groups; the default kwarg covers unmatched entries.
func TestMatchGroupdict(t *testing.T) {
	pat := twoGroupPattern(t)
	patInst := pat.(*objects.Instance)
	gi := objects.NewDict()
	_ = gi.SetItem(objects.NewStr("first"), objects.NewInt(1))
	_ = gi.SetItem(objects.NewStr("second"), objects.NewInt(2))
	_ = patInst.Dict().SetItem(objects.NewStr("groupindex"), gi)

	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	d, err := matchGroupdict([]objects.Object{m}, nil)
	if err != nil {
		t.Fatalf("groupdict: %v", err)
	}
	dd, ok := d.(*objects.Dict)
	if !ok {
		t.Fatalf("groupdict() = %T; want Dict", d)
	}
	for k, want := range map[string]string{"first": "a", "second": "b"} {
		v, err := dd.GetItem(objects.NewStr(k))
		if err != nil {
			t.Fatalf("groupdict[%q]: %v", k, err)
		}
		got, _ := objects.Str(v)
		if got != want {
			t.Errorf("groupdict[%q] = %q; want %q", k, got, want)
		}
	}
}

// TestMatchExpandBackref pins template expansion: \\1 and \\2 reference
// captured groups, and unknown escapes fall through verbatim.
func TestMatchExpandBackref(t *testing.T) {
	pat := twoGroupPattern(t)
	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	out, err := matchExpand([]objects.Object{m, objects.NewStr(`\1-\2`)}, nil)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	s, _ := objects.Str(out)
	if s != "a-b" {
		t.Errorf("expand(\\1-\\2) = %q; want %q", s, "a-b")
	}
}

// TestMatchExpandNamed pins \\g<name> resolution through the Pattern's
// groupindex dict.
func TestMatchExpandNamed(t *testing.T) {
	pat := twoGroupPattern(t)
	patInst := pat.(*objects.Instance)
	gi := objects.NewDict()
	_ = gi.SetItem(objects.NewStr("x"), objects.NewInt(1))
	_ = patInst.Dict().SetItem(objects.NewStr("groupindex"), gi)

	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	out, err := matchExpand([]objects.Object{m, objects.NewStr(`<\g<x>>`)}, nil)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	s, _ := objects.Str(out)
	if s != "<a>" {
		t.Errorf("expand(\\g<x>) = %q; want %q", s, "<a>")
	}
}

// TestMatchRegsProperty pins the `regs` instance-dict slot makeMatch
// stamps after a successful match.
func TestMatchRegsProperty(t *testing.T) {
	pat := twoGroupPattern(t)
	m, err := patternMatch([]objects.Object{pat, objects.NewStr("ab")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	inst := m.(*objects.Instance)
	regs, err := inst.Dict().GetItem(objects.NewStr("regs"))
	if err != nil {
		t.Fatalf("regs slot missing: %v", err)
	}
	tup, ok := regs.(*objects.Tuple)
	if !ok {
		t.Fatalf("regs = %T; want Tuple", regs)
	}
	// regs = ((0,2), (0,1), (1,2)) for groups 0, 1, 2 over "ab".
	wantPairs := [][2]int64{{0, 2}, {0, 1}, {1, 2}}
	if tup.Len() != len(wantPairs) {
		t.Fatalf("regs len = %d; want %d", tup.Len(), len(wantPairs))
	}
	for i, want := range wantPairs {
		pair := tup.Item(i).(*objects.Tuple)
		lo, _ := pair.Item(0).(*objects.Int).Int64()
		hi, _ := pair.Item(1).(*objects.Int).Int64()
		if lo != want[0] || hi != want[1] {
			t.Errorf("regs[%d] = (%d,%d); want (%d,%d)", i, lo, hi, want[0], want[1])
		}
	}
}
