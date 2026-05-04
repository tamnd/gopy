package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func strPtr(s string) *string { return &s }

// TestTryExceptBare covers `try: pass except: pass`. The body
// runs inside SETUP_FINALLY; the bare except handler runs inside a
// SETUP_CLEANUP and ends with POP_EXCEPT.
func TestTryExceptBare(t *testing.T) {
	body := &ast.Try{
		Body: []ast.Stmt{&ast.Pass{}},
		Handlers: []ast.Excepthandler{
			&ast.ExceptHandler{Body: []ast.Stmt{&ast.Pass{}}},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "SETUP_FINALLY", "PUSH_EXC_INFO", "POP_EXCEPT", "RERAISE")
}

// TestTryExceptTyped covers `try: pass except E: pass`. CHECK_EXC_MATCH
// + POP_JUMP_IF_FALSE chains the next handler arm.
func TestTryExceptTyped(t *testing.T) {
	body := &ast.Try{
		Body: []ast.Stmt{&ast.Pass{}},
		Handlers: []ast.Excepthandler{
			&ast.ExceptHandler{
				Type: nameLoad("E"),
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "CHECK_EXC_MATCH", "POP_JUMP_IF_FALSE")
}

// TestTryExceptAs covers `try: pass except E as e: pass`. The bound
// name e gets the second SETUP_CLEANUP wrapper that clears the name
// after the body runs.
func TestTryExceptAs(t *testing.T) {
	body := &ast.Try{
		Body: []ast.Stmt{&ast.Pass{}},
		Handlers: []ast.Excepthandler{
			&ast.ExceptHandler{
				Type: nameLoad("E"),
				Name: strPtr("e"),
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	// Two SETUP_CLEANUP ops: one for the handler chain, one around
	// the as-named body for cleanup.
	count := 0
	for _, op := range got {
		if op == "SETUP_CLEANUP" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 SETUP_CLEANUP, got %d in %v", count, got)
	}
}

// TestTryFinallyEmitsBodyTwice covers `try: pass finally: x = 1`.
// The finally body runs once on the success path and again under
// SETUP_CLEANUP for the exception path; both inline copies are
// produced by the codegen, the dedup happens at the bytecode
// optimization level.
func TestTryFinallyEmitsBodyTwice(t *testing.T) {
	body := &ast.Try{
		Body: []ast.Stmt{&ast.Pass{}},
		Finalbody: []ast.Stmt{
			&ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	stores := 0
	for _, op := range got {
		if op == "STORE_NAME" {
			stores++
		}
	}
	if stores != 2 {
		t.Errorf("expected STORE_NAME emitted twice for finally body, got %d in %v", stores, got)
	}
}

// TestTryStarExceptBuildsList covers `try: pass except* E: pass`.
// The PEP 654 path emits BUILD_LIST 0 / COPY 2 / CHECK_EG_MATCH
// before each handler.
func TestTryStarExceptBuildsList(t *testing.T) {
	body := &ast.TryStar{
		Body: []ast.Stmt{&ast.Pass{}},
		Handlers: []ast.Excepthandler{
			&ast.ExceptHandler{
				Type: nameLoad("E"),
				Body: []ast.Stmt{&ast.Pass{}},
			},
		},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	mustContain(t, got, "BUILD_LIST", "CHECK_EG_MATCH", "CALL_INTRINSIC_2")
}

// TestDefaultExceptMustBeLast rejects a default except followed by
// another handler. CPython raises a SyntaxError; we surface a
// compile-time error.
func TestDefaultExceptMustBeLast(t *testing.T) {
	body := &ast.Try{
		Body: []ast.Stmt{&ast.Pass{}},
		Handlers: []ast.Excepthandler{
			&ast.ExceptHandler{Body: []ast.Stmt{&ast.Pass{}}},
			&ast.ExceptHandler{Type: nameLoad("E"), Body: []ast.Stmt{&ast.Pass{}}},
		},
	}
	st := mustBuildSym(t, module(body))
	c := NewCompiler("<t>", 0, nil, st)
	if _, err := c.Codegen(st.Top, module(body)); err == nil {
		t.Fatalf("expected error for default except not last")
	}
}

func mustContain(t *testing.T, ops []string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		found := false
		for _, op := range ops {
			if op == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("op %q missing from %v", w, ops)
		}
	}
}
