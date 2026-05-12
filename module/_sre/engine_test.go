package _sre

import "testing"

// runeInput converts a Go string to []int32 the engine expects.
func runeInput(s string) []int32 {
	r := []rune(s)
	out := make([]int32, len(r))
	for i, c := range r {
		out[i] = int32(c)
	}
	return out
}

// runMatch is a tiny harness: build a state, run match at code[0],
// return ok plus the consumed position.
func runMatch(t *testing.T, input string, code []uint32, groups int) (int, int) {
	t.Helper()
	s := newState(runeInput(input), code, groups, 0, len(runeInput(input)), false)
	r, err := match(s, 0, true)
	if err != nil {
		t.Fatalf("match err: %v", err)
	}
	return r, s.ptr
}

// runSearch likewise for search().
func runSearch(t *testing.T, input string, code []uint32, groups int) (int, int) {
	t.Helper()
	s := newState(runeInput(input), code, groups, 0, len(runeInput(input)), false)
	r, err := search(s, 0)
	if err != nil {
		t.Fatalf("search err: %v", err)
	}
	return r, s.start
}

// TestEngineLiteral covers OpLiteral + OpSuccess: the simplest
// regex 'a' against "abc".
func TestEngineLiteral(t *testing.T) {
	code := []uint32{OpLiteral, 'a', OpSuccess}
	r, ptr := runMatch(t, "abc", code, 0)
	if r != 1 {
		t.Fatalf("expected match, got r=%d", r)
	}
	if ptr != 1 {
		t.Fatalf("expected ptr=1 after consuming 'a', got %d", ptr)
	}
}

// TestEngineLiteralFail covers literal mismatch.
func TestEngineLiteralFail(t *testing.T) {
	code := []uint32{OpLiteral, 'a', OpSuccess}
	r, _ := runMatch(t, "bcd", code, 0)
	if r != 0 {
		t.Fatalf("expected no match, got r=%d", r)
	}
}

// TestEngineCharset covers OpIn + OpRange + OpFailure terminator:
// [a-z] against "z".
func TestEngineCharset(t *testing.T) {
	// IN <skip=5> RANGE 'a' 'z' FAILURE SUCCESS
	code := []uint32{OpIn, 5, OpRange, 'a', 'z', OpFailure, OpSuccess}
	r, ptr := runMatch(t, "z", code, 0)
	if r != 1 || ptr != 1 {
		t.Fatalf("[a-z] vs 'z': r=%d ptr=%d", r, ptr)
	}
	r, _ = runMatch(t, "0", code, 0)
	if r != 0 {
		t.Fatalf("[a-z] vs '0' should fail, got r=%d", r)
	}
}

// TestEngineRepeatOne covers OpRepeatOne + OpAny: '.+' against "abc".
func TestEngineRepeatOne(t *testing.T) {
	// REPEAT_ONE <skip=5> <min=1> <max=MaxRepeat> ANY SUCCESS SUCCESS
	code := []uint32{OpRepeatOne, 5, 1, MaxRepeat, OpAny, OpSuccess, OpSuccess}
	r, ptr := runMatch(t, "abc", code, 0)
	if r != 1 || ptr != 3 {
		t.Fatalf(".+ vs 'abc': r=%d ptr=%d", r, ptr)
	}
}

// TestEngineBranch covers OpBranch alternation: 'a|b' against "b".
// Layout matches what _compiler.py emits: per-alt skip = total alt size
// including the trailing JUMP+offset, and each JUMP lands past the
// BRANCH terminator.
func TestEngineBranch(t *testing.T) {
	// pos: 0  BRANCH
	//      1  5             (skip to next alt)
	//      2  LITERAL
	//      3  'a'
	//      4  JUMP
	//      5  7             (lands at pos 12)
	//      6  5             (skip)
	//      7  LITERAL
	//      8  'b'
	//      9  JUMP
	//     10  2             (lands at pos 12)
	//     11  0             (terminator)
	//     12  SUCCESS
	code := []uint32{
		OpBranch,
		5, OpLiteral, 'a', OpJump, 7,
		5, OpLiteral, 'b', OpJump, 2,
		0,
		OpSuccess,
	}
	r, ptr := runMatch(t, "b", code, 0)
	if r != 1 || ptr != 1 {
		t.Fatalf("a|b vs 'b': r=%d ptr=%d", r, ptr)
	}
	r, ptr = runMatch(t, "a", code, 0)
	if r != 1 || ptr != 1 {
		t.Fatalf("a|b vs 'a': r=%d ptr=%d", r, ptr)
	}
	r, _ = runMatch(t, "c", code, 0)
	if r != 0 {
		t.Fatalf("a|b vs 'c' should fail, got r=%d", r)
	}
}

// TestEngineMark covers OpMark: '(a)' against "a", verifying the
// mark[0]/mark[1] capture-group bounds.
func TestEngineMark(t *testing.T) {
	// MARK 0 LITERAL 'a' MARK 1 SUCCESS
	code := []uint32{
		OpMark, 0,
		OpLiteral, 'a',
		OpMark, 1,
		OpSuccess,
	}
	s := newState(runeInput("a"), code, 1, 0, 1, false)
	r, err := match(s, 0, true)
	if err != nil {
		t.Fatalf("match err: %v", err)
	}
	if r != 1 {
		t.Fatalf("expected match")
	}
	if s.mark[0] != 0 || s.mark[1] != 1 {
		t.Fatalf("mark[0]=%d mark[1]=%d; expected 0,1", s.mark[0], s.mark[1])
	}
}

// TestEngineGroupref covers OpGroupref: '(a)\1' against "aa".
func TestEngineGroupref(t *testing.T) {
	// MARK 0 LITERAL 'a' MARK 1 GROUPREF 0 SUCCESS
	code := []uint32{
		OpMark, 0,
		OpLiteral, 'a',
		OpMark, 1,
		OpGroupref, 0,
		OpSuccess,
	}
	r, ptr := runMatch(t, "aa", code, 1)
	if r != 1 || ptr != 2 {
		t.Fatalf("(a)\\1 vs 'aa': r=%d ptr=%d", r, ptr)
	}
	r, _ = runMatch(t, "ab", code, 1)
	if r != 0 {
		t.Fatalf("(a)\\1 vs 'ab' should fail, got r=%d", r)
	}
}

// TestEngineAtBoundary covers OpAt + AtBoundary: '\b' between "a"
// and " ".
func TestEngineAtBoundary(t *testing.T) {
	// AT AtBoundary LITERAL 'a' AT AtBoundary SUCCESS
	code := []uint32{
		OpAt, AtBoundary,
		OpLiteral, 'a',
		OpAt, AtBoundary,
		OpSuccess,
	}
	r, ptr := runMatch(t, "a ", code, 0)
	if r != 1 || ptr != 1 {
		t.Fatalf("\\ba\\b vs 'a ': r=%d ptr=%d", r, ptr)
	}
	r, _ = runMatch(t, "ab", code, 0)
	if r != 0 {
		t.Fatalf("\\ba\\b vs 'ab' should fail, got r=%d", r)
	}
}

// TestEngineSearch covers search() walking past a non-matching prefix
// to find 'b' inside "aabc".
func TestEngineSearch(t *testing.T) {
	code := []uint32{OpLiteral, 'b', OpSuccess}
	r, start := runSearch(t, "aabc", code, 0)
	if r != 1 || start != 2 {
		t.Fatalf("search('b','aabc'): r=%d start=%d", r, start)
	}
}

// TestEngineLiteralIgnore covers case-insensitive ASCII literal:
// 'a' (lowered) against "A".
func TestEngineLiteralIgnore(t *testing.T) {
	code := []uint32{OpLiteralIgnore, 'a', OpSuccess}
	r, ptr := runMatch(t, "A", code, 0)
	if r != 1 || ptr != 1 {
		t.Fatalf("(?i)a vs 'A': r=%d ptr=%d", r, ptr)
	}
}
