package lexer

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/token"
)

// TestVerifyEndOfNumberWarnsOnKeywordAdjacency pins that a literal
// immediately followed by `and` / `or` / `if` / `in` / `is` / `else`
// / `for` / `not` records a SyntaxWarning instead of a SyntaxError,
// matching CPython's verify_end_of_number behavior at lexer.c:343.
func TestVerifyEndOfNumberWarnsOnKeywordAdjacency(t *testing.T) {
	cases := []string{
		"1and 2\n",
		"1or 2\n",
		"1if x else 2\n",
		"1in x\n",
		"1is None\n",
		"1not in x\n",
	}
	for _, src := range cases {
		st := FromString(src, ModeFile)
		for range 100 {
			tk := st.Get()
			if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
				break
			}
		}
		if st.Err() != nil {
			t.Errorf("source %q: Err() = %v, want nil (warning, not error)", src, st.Err())
		}
		ws := st.Warnings()
		if len(ws) == 0 {
			t.Errorf("source %q: Warnings() empty, want SyntaxWarning", src)
			continue
		}
		if ws[0].Category != "SyntaxWarning" {
			t.Errorf("source %q: Category = %q, want SyntaxWarning", src, ws[0].Category)
		}
		if !strings.Contains(ws[0].Message, "invalid") {
			t.Errorf("source %q: Message = %q, want to mention \"invalid\"", src, ws[0].Message)
		}
	}
}

// TestVerifyEndOfNumberErrorsOnPlainIdentifier pins that the
// non-keyword identifier follow (e.g. `1foo`) still surfaces as a
// hard SyntaxError, matching the else branch in
// verify_end_of_number.
func TestVerifyEndOfNumberErrorsOnPlainIdentifier(t *testing.T) {
	src := "1foo\n"
	st := FromString(src, ModeFile)
	sawError := false
	for range 100 {
		tk := st.Get()
		if tk.Kind == token.ERRORTOKEN {
			sawError = true
		}
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			break
		}
	}
	if !sawError {
		t.Errorf("source %q: never saw ERRORTOKEN", src)
	}
	if st.Err() == nil {
		t.Errorf("source %q: Err() = nil, want SyntaxError", src)
	}
}
