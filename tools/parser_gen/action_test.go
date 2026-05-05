// Tests pin the small set of action shapes the translator handles
// today: bare bound idents, NULL/EXTRA constants, _PyAST_ and
// _PyPegen_ helper calls, CHECK pass-through, and RAISE_ macros.
// Anything outside this set must return ok=false so the alt falls
// back to the []any names form.

package main

import "testing"

func TestTranslateActionShapes(t *testing.T) {
	bound := map[string]bool{"a": true, "b": true, "e": true}

	cases := []struct {
		body string
		want string
		ok   bool
	}{
		{"", "placeholderMatched", true},
		{"a", "a", true},
		{"NULL", "nil", true},
		{"_PyAST_Module(a, EXTRA)", "actionAstModule(p, a)", true},
		{"_PyAST_Pass(EXTRA)", "actionAstPass(p)", true},
		{"_PyAST_Return(a, EXTRA)", "actionAstReturn(p, a)", true},
		{"_PyAST_BinOp(a, Add, b, EXTRA)", "", false}, // Add not bound
		{"_PyPegen_seq_insert_in_front(p, a, b)", "actionPgenSeq_insert_in_front(p, p, a, b)", true},
		{"RAISE_SYNTAX_ERROR(\"oops\")", `raiseAction(p, "RAISE_SYNTAX_ERROR", "oops")`, true},
		{"a->lineno", "", false}, // member access on any-typed bound
		{"unknown_ident", "", false},
	}

	for _, c := range cases {
		got, ok := translateAction(c.body, bound)
		if ok != c.ok {
			t.Errorf("translateAction(%q): ok=%v want %v", c.body, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("translateAction(%q):\n got %q\nwant %q", c.body, got, c.want)
		}
	}
}
