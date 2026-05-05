// Pin selected SyntaxError messages so a casual edit cannot drift
// the user-visible text away from CPython 3.14. The exhaustive
// panel lives in parser/errors; this file is the smoke check.

package partest

import (
	"strings"
	"testing"

	perrors "github.com/tamnd/gopy/parser/errors"
)

func TestErrorBuilderPositions(t *testing.T) {
	e := perrors.Raise("x.py", perrors.Pos{Lineno: 7, ColOff: 12, EndLine: 7, EndCol: 14},
		"cannot assign to %s", "literal")
	if e.Message != "cannot assign to literal" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Pos.Lineno != 7 || e.Pos.ColOff != 12 || e.Pos.EndCol != 14 {
		t.Errorf("Pos = %+v", e.Pos)
	}
	if e.Filename != "x.py" {
		t.Errorf("Filename = %q", e.Filename)
	}
}

func TestErrorPanelTokenizerDispatch(t *testing.T) {
	cases := []struct {
		code   perrors.Errcode
		substr string
	}{
		{perrors.ErrToken, "invalid token"},
		{perrors.ErrDedent, "unindent does not match"},
		{perrors.ErrTabSpace, "inconsistent use of tabs and spaces"},
		{perrors.ErrOverflow, "too many levels of indentation"},
	}
	for _, c := range cases {
		e := perrors.FromTokenizer(c.code, "x.py", perrors.Pos{Lineno: 1})
		if !strings.Contains(e.Message, c.substr) {
			t.Errorf("code %d msg = %q; want substring %q", c.code, e.Message, c.substr)
		}
	}
}

func TestErrorPanelUnclosedParen(t *testing.T) {
	e := perrors.UnclosedParen("x.py", perrors.Pos{Lineno: 3}, '(')
	if e.Message != "'(' was never closed" {
		t.Errorf("UnclosedParen = %q", e.Message)
	}
}
