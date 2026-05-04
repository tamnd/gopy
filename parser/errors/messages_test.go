package errors

import "testing"

// Pin a sample of the message panel so a casual edit cannot drift
// the user-visible text away from CPython 3.14. Refresh by reading
// the relevant raise site in pegen_errors.c.
func TestMessagePanel(t *testing.T) {
	cases := []struct{ got, want string }{
		{MsgInvalidSyntax, "invalid syntax"},
		{MsgUnexpectedEOF, "unexpected EOF while parsing"},
		{MsgUnexpectedIndent, "unexpected indent"},
		{MsgUnexpectedUnindent, "unexpected unindent"},
		{MsgInconsistentDedent, "unindent does not match any outer indentation level"},
		{MsgTabSpace, "inconsistent use of tabs and spaces in indentation"},
		{MsgTooDeep, "too many levels of indentation"},
		{MsgInvalidToken, "invalid token"},
		{MsgLineCont, "unexpected character after line continuation character"},
		{MsgUnclosedParen, "'%c' was never closed"},
		{MsgErrorAtStart, "error at start before reading any input"},
		{MsgStackOverflow, "Parser stack overflowed - Python source too complex to parse"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("message drift: got %q want %q", c.got, c.want)
		}
	}
}

func TestBuilderShape(t *testing.T) {
	e := Raise("x.py", Pos{Lineno: 3, ColOff: 5}, MsgUnclosedParen, '(')
	if e.Kind != KindSyntax {
		t.Errorf("kind = %v want SyntaxError", e.Kind)
	}
	if e.Pos.Lineno != 3 || e.Pos.ColOff != 5 {
		t.Errorf("position = %+v", e.Pos)
	}
	if e.Message != "'(' was never closed" {
		t.Errorf("message = %q", e.Message)
	}
}

func TestFromTokenizerDispatch(t *testing.T) {
	cases := []struct {
		code Errcode
		want string
		kind Kind
	}{
		{ErrToken, MsgInvalidToken, KindSyntax},
		{ErrDedent, MsgInconsistentDedent, KindIndentation},
		{ErrTabSpace, MsgTabSpace, KindTab},
		{ErrOverflow, MsgTooDeep, KindIndentation},
		{ErrColumnOverflow, MsgColumnOverflow, KindOverflow},
	}
	for _, c := range cases {
		e := FromTokenizer(c.code, "x.py", Pos{Lineno: 1})
		if e.Message != c.want {
			t.Errorf("code %d message %q want %q", c.code, e.Message, c.want)
		}
		if e.Kind != c.kind {
			t.Errorf("code %d kind %v want %v", c.code, e.Kind, c.kind)
		}
	}
}
