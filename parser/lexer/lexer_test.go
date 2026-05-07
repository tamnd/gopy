package lexer

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/token"
)

func tokenize_(t *testing.T, src string) []Tok {
	t.Helper()
	s := FromString(src, ModeFile)
	var out []Tok
	for i := 0; i < 200; i++ {
		tk := s.Get()
		out = append(out, tk)
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			return out
		}
	}
	t.Fatalf("token stream did not terminate after 200 tokens")
	return out
}

func kinds(toks []Tok) []token.Type {
	out := make([]token.Type, len(toks))
	for i, t := range toks {
		out[i] = t.Kind
	}
	return out
}

func TestSimpleAssignment(t *testing.T) {
	got := kinds(tokenize_(t, "x = 1\n"))
	want := []token.Type{
		token.NAME, token.OP, token.NUMBER, token.NEWLINE, token.ENDMARKER,
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
	if len(got) != 1 || got[0] != token.ENDMARKER {
		t.Fatalf("empty input should yield only ENDMARKER, got %v", got)
	}
}

func TestComment(t *testing.T) {
	// CPython parser mode (tok_extra_tokens=0) drops comment-only lines
	// entirely via `goto nextline` (Parser/lexer/lexer.c:817), so a file
	// containing only `# hello\n` reaches the parser as just ENDMARKER.
	got := kinds(tokenize_(t, "# hello\n"))
	want := []token.Type{token.ENDMARKER}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestCommentExtraTokens(t *testing.T) {
	s := FromString("# hello\n", ModeFile)
	s.SetExtraTokens(true)
	var got []token.Type
	for i := 0; i < 20; i++ {
		tk := s.Get()
		got = append(got, tk.Kind)
		if tk.Kind == token.ENDMARKER {
			break
		}
	}
	want := []token.Type{token.COMMENT, token.NL, token.ENDMARKER}
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
	got := kinds(tokenize_(t, `"hello" + 'world'`+"\n"))
	want := []token.Type{
		token.STRING, token.OP, token.STRING, token.NEWLINE, token.ENDMARKER,
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
		if tk.Kind == token.INDENT {
			hasIndent = true
		}
		if tk.Kind == token.DEDENT {
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
	want := []token.Type{
		token.NAME, token.OP, token.NAME, token.NEWLINE, token.ENDMARKER,
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
	// In parser mode (tok_extra_tokens=0), an in-paren '\n' goes
	// through `goto nextline` (Parser/lexer/lexer.c:817) and produces
	// no NL token. The python-level tokenize module flips
	// tok_extra_tokens to 1, which is what TestParenContinuationExtraTokens
	// pins below.
	got := kinds(tokenize_(t, "(a +\n b)\n"))
	want := []token.Type{
		token.OP, token.NAME, token.OP,
		token.NAME, token.OP, token.NEWLINE, token.ENDMARKER,
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

func TestParenContinuationExtraTokens(t *testing.T) {
	s := FromString("(a +\n b)\n", ModeFile)
	s.SetExtraTokens(true)
	var got []token.Type
	for i := 0; i < 20; i++ {
		tk := s.Get()
		got = append(got, tk.Kind)
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			break
		}
	}
	want := []token.Type{
		token.OP, token.NAME, token.OP, token.NL,
		token.NAME, token.OP, token.NEWLINE, token.ENDMARKER,
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
	got := kinds(tokenize_(t, `f"hi {name}!"`+"\n"))
	want := []token.Type{
		token.FSTRING_START, token.FSTRING_MIDDLE,
		token.OP, token.NAME, token.OP,
		token.FSTRING_MIDDLE, token.FSTRING_END,
		token.NEWLINE, token.ENDMARKER,
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
	got := kinds(tokenize_(t, `t"x={x}"`+"\n"))
	if got[0] != token.TSTRING_START {
		t.Fatalf("expected TSTRING_START first, got %v", got)
	}
	saw := false
	for _, k := range got {
		if k == token.TSTRING_END {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected TSTRING_END in stream, got %v", got)
	}
}

func TestFStringEmpty(t *testing.T) {
	got := kinds(tokenize_(t, `f""`+"\n"))
	want := []token.Type{
		token.FSTRING_START, token.FSTRING_END,
		token.NEWLINE, token.ENDMARKER,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTypeComment(t *testing.T) {
	s := FromString("x = 1  # type: int\n", ModeFile)
	s.SetTypeComments(true)
	var got []token.Type
	for i := 0; i < 50; i++ {
		tk := s.Get()
		got = append(got, tk.Kind)
		if tk.Kind == token.ENDMARKER {
			break
		}
	}
	saw := false
	for _, k := range got {
		if k == token.TYPE_COMMENT {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected TYPE_COMMENT in stream, got %v", got)
	}
}

func TestReaderDriver(t *testing.T) {
	s := FromReader(strings.NewReader("a = 1\nb = 2\n"), ModeFile)
	var got []token.Type
	for i := 0; i < 50; i++ {
		tk := s.Get()
		got = append(got, tk.Kind)
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
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
	want := []token.Type{token.NAME, token.OP, token.NUMBER, token.NEWLINE, token.ENDMARKER}
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
		want := []token.Type{token.NUMBER, token.NEWLINE, token.ENDMARKER}
		if len(got) != len(want) || got[0] != token.NUMBER {
			t.Errorf("src %q: got %v", src, got)
		}
	}
}
