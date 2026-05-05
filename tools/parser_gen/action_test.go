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
		{"_PyAST_BinOp(a, Add, b, EXTRA)", "actionAstBinOp(p, a, ast.Add, b)", true},
		{"_PyAST_Constant(Py_True, NULL, EXTRA)", "actionAstConstant(p, pyTrueSentinel, nil)", true},
		{"_PyAST_Assign(a, b, NEW_TYPE_COMMENT(p, e), EXTRA)", "actionAstAssign(p, a, b, e)", true},
		{"_PyPegen_seq_insert_in_front(p, a, b)", "actionPgenSeqInsertInFront(p, p, a, b)", true},
		{"RAISE_SYNTAX_ERROR(\"oops\")", `raiseAction(p, "RAISE_SYNTAX_ERROR", "oops")`, true},
		{"a->lineno", "", false}, // member access on any-typed bound
		{"unknown_ident", "", false},
	}

	// extras for the patterns the def/comprehension/async_def gate
	// cases hit: NAME-id extraction, ternary fallback, type tags
	bound2 := map[string]bool{"n": true, "params": true, "b": true, "a": true, "tc": true, "t": true}
	extra := []struct {
		body string
		want string
	}{
		{"n->v.Name.id", "nameIdOf(n)"},
		{"(params) ? params : CHECK(arguments_ty, _PyPegen_empty_arguments(p))", "func() any { if truthy((params)) { return params }; return actionPgenEmptyArguments(p, p) }()"},
	}
	for _, c := range extra {
		got, ok := translateAction(c.body, bound2)
		if !ok {
			t.Errorf("translateAction(%q): ok=false, want true", c.body)
			continue
		}
		if got != c.want {
			t.Errorf("translateAction(%q):\n got %q\nwant %q", c.body, got, c.want)
		}
	}

	full := `_PyAST_FunctionDef(n->v.Name.id,
		(params) ? params : CHECK(arguments_ty, _PyPegen_empty_arguments(p)),
		b, NULL, a, NEW_TYPE_COMMENT(p, tc), t, EXTRA)`
	got, ok := translateAction(full, bound2)
	if !ok {
		t.Errorf("translateAction(full FunctionDef): ok=false")
	}
	t.Logf("FunctionDef translation: %s", got)

	// Also exercise the space-joined form the grammar parser actually
	// produces (each atom separated by a single space).
	joined := `_PyAST_FunctionDef ( n -> v . Name . id , ( params ) ? params : CHECK ( arguments_ty , _PyPegen_empty_arguments ( p ) ) , b , NULL , a , NEW_TYPE_COMMENT ( p , tc ) , t , EXTRA )`
	got2, ok2 := translateAction(joined, bound2)
	if !ok2 {
		t.Errorf("translateAction(joined FunctionDef): ok=false")
	}
	t.Logf("FunctionDef joined translation: %s", got2)

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
