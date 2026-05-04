package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestWithSingleItemNoAs covers `with cm: pass`. The opening sequence
// is COPY 1 / LOAD_SPECIAL __exit__ / SWAP 2 / SWAP 3 / LOAD_SPECIAL
// __enter__ / CALL 0 / SETUP_WITH; on no-as the enter result is
// dropped with POP_TOP.
func TestWithSingleItemNoAs(t *testing.T) {
	body := &ast.With{
		Items: []*ast.Withitem{{ContextExpr: nameLoad("cm")}},
		Body:  []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",    // cm
		"COPY",         // dup for __exit__ load
		"LOAD_SPECIAL", // __exit__
		"SWAP",         //
		"SWAP",         //
		"LOAD_SPECIAL", // __enter__
		"CALL",         // ()
		"SETUP_WITH",   // -> final
		"POP_TOP",      // discard enter result
		"NOP",          // pass body
		"POP_BLOCK",
		"LOAD_CONST", "LOAD_CONST", "LOAD_CONST", // None x3 for __exit__
		"CALL",
		"POP_TOP", // exit return
		"JUMP",    // -> exit
		"SETUP_CLEANUP",
		"PUSH_EXC_INFO",
		"WITH_EXCEPT_START",
		"TO_BOOL",
		"POP_JUMP_IF_TRUE",
		"RERAISE",
		"POP_TOP", "POP_BLOCK", "POP_EXCEPT", "POP_TOP", "POP_TOP", "POP_TOP",
		"JUMP_NO_INTERRUPT",
		"COPY", "POP_EXCEPT", "RERAISE",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops mismatch\n got = %v\nwant = %v", got, want)
	}
}

// TestWithSingleItemWithAs covers `with cm as v: pass`. The store
// must replace the POP_TOP of the no-as form.
func TestWithSingleItemWithAs(t *testing.T) {
	body := &ast.With{
		Items: []*ast.Withitem{{
			ContextExpr:  nameLoad("cm"),
			OptionalVars: nameStore("v"),
		}},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	// Inspect the enter-result storage point: after SETUP_WITH the
	// next op should be STORE_NAME instead of POP_TOP.
	idx := -1
	for i, op := range got {
		if op == "SETUP_WITH" {
			idx = i + 1
			break
		}
	}
	if idx < 0 || got[idx] != "STORE_NAME" {
		t.Errorf("expected STORE_NAME after SETUP_WITH, got %v", got)
	}
}

// TestAsyncWithSingleItem covers `async with cm: pass` inside an
// async def. The opening sequence inserts GET_AWAITABLE 1 / SEND
// loop around the __aenter__ call.
func TestAsyncWithSingleItem(t *testing.T) {
	withStmt := &ast.AsyncWith{
		Items: []*ast.Withitem{{ContextExpr: nameLoad("cm")}},
		Body:  []ast.Stmt{&ast.Pass{}},
	}
	fn := &ast.AsyncFunctionDef{
		Name: "f",
		Args: emptyArgs(),
		Body: []ast.Stmt{withStmt},
	}
	u := compileMod(t, module(fn))
	inner := findInnerUnit(t, u)
	got := opNames(inner)
	// Look for the aenter-await section: LOAD_SPECIAL __aenter__,
	// CALL, GET_AWAITABLE 1, LOAD_CONST None, SEND.
	found := false
	for i := 0; i+5 < len(got); i++ {
		if got[i] == "LOAD_SPECIAL" && got[i+1] == "CALL" &&
			got[i+2] == "GET_AWAITABLE" && got[i+3] == "LOAD_CONST" &&
			got[i+4] == "SEND" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("async-with aenter await sequence missing in %v", got)
	}
}

// TestWithMultipleItemsNests covers `with a, b: pass` which lowers
// to a SETUP_WITH for `a` and a nested SETUP_WITH for `b` inside.
func TestWithMultipleItemsNests(t *testing.T) {
	body := &ast.With{
		Items: []*ast.Withitem{
			{ContextExpr: nameLoad("a")},
			{ContextExpr: nameLoad("b")},
		},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	count := 0
	for _, op := range got {
		if op == "SETUP_WITH" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 SETUP_WITH ops, got %d in %v", count, got)
	}
}
