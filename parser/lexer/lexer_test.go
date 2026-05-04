package lexer

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/tokenize"
)

func tokenize_(t *testing.T, src string) []Tok {
	t.Helper()
	s := FromString(src, ModeFile)
	var out []Tok
	for i := 0; i < 200; i++ {
		tk := s.Get()
		out = append(out, tk)
		if tk.Kind == tokenize.ENDMARKER || tk.Kind == tokenize.ERRORTOKEN {
			return out
		}
	}
	t.Fatalf("token stream did not terminate after 200 tokens")
	return out
}

func kinds(toks []Tok) []tokenize.Type {
	out := make([]tokenize.Type, len(toks))
	for i, t := range toks {
		out[i] = t.Kind
	}
	return out
}

func TestSimpleAssignment(t *testing.T) {
	got := kinds(tokenize_(t, "x = 1\n"))
	want := []tokenize.Type{
		tokenize.NAME, tokenize.OP, tokenize.NUMBER, tokenize.NEWLINE, tokenize.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestEmpty(t *testing.T) {
	got := kinds(tokenize_(t, ""))
	if len(got) != 1 || got[0] != tokenize.ENDMARKER {
		t.Fatalf("empty input should yield only ENDMARKER, got %v", got)
	}
}

func TestComment(t *testing.T) {
	got := kinds(tokenize_(t, "# hello\n"))
	want := []tokenize.Type{tokenize.NEWLINE, tokenize.ENDMARKER}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestStringLiteral(t *testing.T) {
	got := kinds(tokenize_(t, `"hello" + 'world'` + "\n"))
	want := []tokenize.Type{
		tokenize.STRING, tokenize.OP, tokenize.STRING, tokenize.NEWLINE, tokenize.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestIndentDedent(t *testing.T) {
	src := "if x:\n    a\n    b\nc\n"
	toks := tokenize_(t, src)
	var hasIndent, hasDedent bool
	for _, tk := range toks {
		if tk.Kind == tokenize.INDENT {
			hasIndent = true
		}
		if tk.Kind == tokenize.DEDENT {
			hasDedent = true
		}
	}
	if !hasIndent {
		t.Errorf("expected INDENT in stream, got %v", kinds(toks))
	}
	if !hasDedent {
		t.Errorf("expected DEDENT in stream, got %v", kinds(toks))
	}
}

func TestLineContinuation(t *testing.T) {
	got := kinds(tokenize_(t, "a + \\\n  b\n"))
	want := []tokenize.Type{
		tokenize.NAME, tokenize.OP, tokenize.NAME, tokenize.NEWLINE, tokenize.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParenContinuation(t *testing.T) {
	got := kinds(tokenize_(t, "(a +\n b)\n"))
	want := []tokenize.Type{
		tokenize.OP, tokenize.NAME, tokenize.OP, tokenize.NL,
		tokenize.NAME, tokenize.OP, tokenize.NEWLINE, tokenize.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestFStringSimple(t *testing.T) {
	got := kinds(tokenize_(t, `f"hi {name}!"` + "\n"))
	want := []tokenize.Type{
		tokenize.FSTRING_START, tokenize.FSTRING_MIDDLE,
		tokenize.OP, tokenize.NAME, tokenize.OP,
		tokenize.FSTRING_MIDDLE, tokenize.FSTRING_END,
		tokenize.NEWLINE, tokenize.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTStringSimple(t *testing.T) {
	got := kinds(tokenize_(t, `t"x={x}"` + "\n"))
	if got[0] != tokenize.TSTRING_START {
		t.Fatalf("expected TSTRING_START first, got %v", got)
	}
	saw := false
	for _, k := range got {
		if k == tokenize.TSTRING_END {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected TSTRING_END in stream, got %v", got)
	}
}

func TestFStringEmpty(t *testing.T) {
	got := kinds(tokenize_(t, `f""` + "\n"))
	want := []tokenize.Type{
		tokenize.FSTRING_START, tokenize.FSTRING_END,
		tokenize.NEWLINE, tokenize.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTypeComment(t *testing.T) {
	s := FromString("x = 1  # type: int\n", ModeFile)
	s.SetTypeComments(true)
	var got []tokenize.Type
	for i := 0; i < 50; i++ {
		tk := s.Get()
		got = append(got, tk.Kind)
		if tk.Kind == tokenize.ENDMARKER {
			break
		}
	}
	saw := false
	for _, k := range got {
		if k == tokenize.TYPE_COMMENT {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected TYPE_COMMENT in stream, got %v", got)
	}
}

func TestReaderDriver(t *testing.T) {
	s := FromReader(strings.NewReader("a = 1\nb = 2\n"), ModeFile)
	var got []tokenize.Type
	for i := 0; i < 50; i++ {
		tk := s.Get()
		got = append(got, tk.Kind)
		if tk.Kind == tokenize.ENDMARKER || tk.Kind == tokenize.ERRORTOKEN {
			break
		}
	}
	if len(got) < 5 {
		t.Fatalf("expected non-trivial token stream, got %v", got)
	}
}

func TestBOMStripped(t *testing.T) {
	src := "\xef\xbb\xbfx = 1\n"
	got := kinds(tokenize_(t, src))
	want := []tokenize.Type{tokenize.NAME, tokenize.OP, tokenize.NUMBER, tokenize.NEWLINE, tokenize.ENDMARKER}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestNumberKinds(t *testing.T) {
	cases := []string{"0", "0x1f", "0o77", "0b101", "1.5", "1.5e10", "10j"}
	for _, src := range cases {
		got := kinds(tokenize_(t, src+"\n"))
		want := []tokenize.Type{tokenize.NUMBER, tokenize.NEWLINE, tokenize.ENDMARKER}
		if len(got) != len(want) || got[0] != tokenize.NUMBER {
			t.Errorf("src %q: got %v", src, got)
		}
	}
}
