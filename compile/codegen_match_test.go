package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestMatchValueComparesAndBranches covers `match x: case 1: pass`.
// A MatchValue lowers to the subject + literal + COMPARE_OP Eq +
// TO_BOOL + POP_JUMP_IF_FALSE.
func TestMatchValueComparesAndBranches(t *testing.T) {
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchValue{Value: cnst(int64(1))},
				Body:    []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "COMPARE_OP", "TO_BOOL", "POP_JUMP_IF_FALSE")
}

// TestMatchSingletonUsesIs covers `case None`. Singletons match by
// identity: LOAD_CONST None + IS_OP 0.
func TestMatchSingletonUsesIs(t *testing.T) {
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchSingleton{Value: nil},
				Body:    []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "IS_OP", "POP_JUMP_IF_FALSE")
}

// TestMatchWildcardOnly covers `match x: case _: pass`. The single
// wildcard arm just pops the subject and runs the body.
func TestMatchWildcardOnly(t *testing.T) {
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchAs{},
				Body:    []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	// Just verify it compiles: wildcard means no compare ops emitted
	// for the pattern itself.
	for _, op := range got {
		if op == "COMPARE_OP" || op == "IS_OP" {
			t.Errorf("wildcard-only match should not emit %s, got ops %v", op, got)
		}
	}
}

// TestMatchWildcardCaptureBindsName covers `case x: pass`. The bare
// name acts as a capture and must produce a STORE for x.
func TestMatchWildcardCaptureBindsName(t *testing.T) {
	name := "y"
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchAs{Name: &name},
				Body:    []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	hasStore := false
	for _, op := range got {
		if op == "STORE_NAME" {
			hasStore = true
		}
	}
	if !hasStore {
		t.Errorf("expected STORE_NAME for capture, got ops %v", got)
	}
}

// TestMatchSequenceUsesMatchSequence covers `case [a, b]`. The
// sequence pattern emits MATCH_SEQUENCE + GET_LEN + UNPACK_SEQUENCE.
func TestMatchSequenceUsesMatchSequence(t *testing.T) {
	a := "a"
	b := "b"
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchSequence{
					Patterns: []ast.Pattern{
						&ast.MatchAs{Name: &a},
						&ast.MatchAs{Name: &b},
					},
				},
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "MATCH_SEQUENCE", "GET_LEN", "UNPACK_SEQUENCE")
}

// TestMatchMappingEmitsMatchMapping covers `case {1: a}`. Mapping
// pattern uses MATCH_MAPPING + MATCH_KEYS.
func TestMatchMappingEmitsMatchMapping(t *testing.T) {
	a := "a"
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchMapping{
					Keys:     []ast.Expr{cnst(int64(1))},
					Patterns: []ast.Pattern{&ast.MatchAs{Name: &a}},
				},
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "MATCH_MAPPING", "MATCH_KEYS")
}

// TestMatchClassEmitsMatchClass covers `case Foo(a)`. MATCH_CLASS
// gates the type and runs the kw-attr extraction.
func TestMatchClassEmitsMatchClass(t *testing.T) {
	a := "a"
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchClass{
					Cls:      nameLoad("Foo"),
					Patterns: []ast.Pattern{&ast.MatchAs{Name: &a}},
				},
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "MATCH_CLASS", "UNPACK_SEQUENCE")
}

// TestMatchOrAcceptsAlternatives covers `case 1 | 2`.
func TestMatchOrAcceptsAlternatives(t *testing.T) {
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchOr{
					Patterns: []ast.Pattern{
						&ast.MatchValue{Value: cnst(int64(1))},
						&ast.MatchValue{Value: cnst(int64(2))},
					},
				},
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	count := 0
	for _, op := range got {
		if op == "COMPARE_OP" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("or-pattern with 2 value alts should produce >=2 COMPARE_OPs, got %d in %v", count, got)
	}
}

// TestMatchTwoCasesBranches covers two refutable cases. The first
// must emit COPY 1 to preserve the subject for the second.
func TestMatchTwoCasesBranches(t *testing.T) {
	body := &ast.Match{
		Subject: nameLoad("x"),
		Cases: []*ast.MatchCase{
			{
				Pattern: &ast.MatchValue{Value: cnst(int64(1))},
				Body:    []ast.Stmt{&ast.Pass{}},
			},
			{
				Pattern: &ast.MatchValue{Value: cnst(int64(2))},
				Body:    []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	hasCopy := false
	for _, op := range got {
		if op == "COPY" {
			hasCopy = true
		}
	}
	if !hasCopy {
		t.Errorf("two-case match should emit COPY for subject preservation, got %v", got)
	}
}
