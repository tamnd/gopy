package _sre

// Phase 7 verification: drive the bytecode engine against the exact
// programs Lib/re/_compiler.py would emit for the three gate patterns
// listed in spec 1703 Phase 7. The CLI gate (`gopy -c 'import re; ...'`)
// blocks on `enum` import, which lives outside spec 1703; this test
// bridges the gap by exercising the engine directly so a regression in
// _sre cannot hide behind an unrelated import failure.

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// gateGroupsCode reproduces _compiler.py's bytecode for r"(\d+)-(\d+)":
//
//	INFO 4 0 3 MAXREPEAT
//	MARK 0
//	REPEAT_ONE 9 1 MAXREPEAT
//	  IN 4 CATEGORY CATEGORY_UNI_DIGIT FAILURE
//	  SUCCESS
//	MARK 1
//	LITERAL '-'
//	MARK 2
//	REPEAT_ONE 9 1 MAXREPEAT
//	  IN 4 CATEGORY CATEGORY_UNI_DIGIT FAILURE
//	  SUCCESS
//	MARK 3
//	SUCCESS
func gateGroupsCode() []uint32 {
	return []uint32{
		OpInfo, 4, 0, 3, MaxRepeat,
		OpMark, 0,
		OpRepeatOne, 9, 1, MaxRepeat,
		OpIn, 4, OpCategory, CategoryUniDigit, OpFailure,
		OpSuccess,
		OpMark, 1,
		OpLiteral, '-',
		OpMark, 2,
		OpRepeatOne, 9, 1, MaxRepeat,
		OpIn, 4, OpCategory, CategoryUniDigit, OpFailure,
		OpSuccess,
		OpMark, 3,
		OpSuccess,
	}
}

// TestPhase7GateGroups pins the first gate from spec 1703:
//
//	re.match(r"(\d+)-(\d+)", "12-34").groups() == ("12", "34")
func TestPhase7GateGroups(t *testing.T) {
	pat := compileFor(t, `(\d+)-(\d+)`, gateGroupsCode(), 2)
	m, err := patternMatch([]objects.Object{pat, objects.NewStr("12-34")}, nil)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("expected Match, got None")
	}
	tup, err := matchGroups([]objects.Object{m}, nil)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	tu := tup.(*objects.Tuple)
	if tu.Len() != 2 {
		t.Fatalf("groups len = %d; want 2", tu.Len())
	}
	for i, want := range []string{"12", "34"} {
		s, _ := objects.Str(tu.Item(i))
		if s != want {
			t.Errorf("groups()[%d] = %q; want %q", i, s, want)
		}
	}
}

// gateFindallCode reproduces _compiler.py's bytecode for r"\w+":
//
//	INFO 4 0 1 MAXREPEAT
//	REPEAT_ONE 9 1 MAXREPEAT
//	  IN 4 CATEGORY CATEGORY_UNI_WORD FAILURE
//	  SUCCESS
//	SUCCESS
func gateFindallCode() []uint32 {
	return []uint32{
		OpInfo, 4, 0, 1, MaxRepeat,
		OpRepeatOne, 9, 1, MaxRepeat,
		OpIn, 4, OpCategory, CategoryUniWord, OpFailure,
		OpSuccess,
		OpSuccess,
	}
}

// TestPhase7GateFindall pins gate two:
//
//	re.findall(r"\w+", "a b c") == ["a", "b", "c"]
func TestPhase7GateFindall(t *testing.T) {
	pat := compileFor(t, `\w+`, gateFindallCode(), 0)
	r, err := patternFindall([]objects.Object{pat, objects.NewStr("a b c")}, nil)
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
	for i, want := range []string{"a", "b", "c"} {
		s, _ := objects.Str(l.Item(i))
		if s != want {
			t.Errorf("findall[%d] = %q; want %q", i, s, want)
		}
	}
}

// gateSubCode reproduces _compiler.py's bytecode for r"\d":
//
//	INFO 7 4 1 1 CATEGORY CATEGORY_UNI_DIGIT FAILURE
//	IN 4 CATEGORY CATEGORY_UNI_DIGIT FAILURE
//	SUCCESS
func gateSubCode() []uint32 {
	return []uint32{
		OpInfo, 7, 4, 1, 1, OpCategory, CategoryUniDigit, OpFailure,
		OpIn, 4, OpCategory, CategoryUniDigit, OpFailure,
		OpSuccess,
	}
}

// TestPhase7GateSub pins gate three:
//
//	re.sub(r"\d", "_", "a1b2") == "a_b_"
func TestPhase7GateSub(t *testing.T) {
	pat := compileFor(t, `\d`, gateSubCode(), 0)
	r, err := patternSub([]objects.Object{
		pat, objects.NewStr("_"), objects.NewStr("a1b2"),
	}, nil)
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	s, _ := objects.Str(r)
	if s != "a_b_" {
		t.Errorf("sub = %q; want %q", s, "a_b_")
	}
}
