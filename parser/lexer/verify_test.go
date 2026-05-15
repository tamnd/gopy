package lexer

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/token"
)

// hasError reports whether the tokenizer surfaced an ERRORTOKEN whose
// recorded message contains substr. Drives the negative-path tests for
// verify_end_of_number, verify_identifier, and prefix conflicts.
func hasError(toks []Tok, s *State, substr string) bool {
	_ = toks
	return s.Err() != nil && strings.Contains(s.Err().Message, substr)
}

func runTok(t *testing.T, src string) ([]Tok, *State) {
	t.Helper()
	s := FromString(src, ModeFile)
	var out []Tok
	for i := 0; i < 300; i++ {
		tk := s.Get()
		out = append(out, tk)
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			return out, s
		}
	}
	t.Fatalf("did not terminate")
	return out, s
}

func TestVerifyEndOfNumber_InvalidLiteral(t *testing.T) {
	toks, s := runTok(t, "1foo\n")
	if !hasError(toks, s, "invalid decimal literal") {
		t.Fatalf("expected invalid decimal literal, got %v err=%v",
			kinds(toks), s.Err())
	}
}

func TestVerifyEndOfNumber_KeywordAfterLiteral(t *testing.T) {
	// `1and` / `1or` / `1if` should tokenize, not error. CPython warns
	// but accepts; gopy currently accepts without warning.
	for _, src := range []string{"1and x\n", "1or x\n", "1if x else y\n", "2in x\n", "3is x\n", "4not x\n"} {
		toks, s := runTok(t, src)
		for _, tk := range toks {
			if tk.Kind == token.ERRORTOKEN {
				t.Errorf("src=%q unexpectedly errored: %v", src, s.Err())
				break
			}
		}
	}
}

func TestStringPrefix_Compatible(t *testing.T) {
	// rb / rf / rt and their reversed forms are valid.
	for _, src := range []string{`rb"x"`, `br"x"`, `rf"x"`, `fr"x"`, `rt"x"`, `tr"x"`} {
		toks, s := runTok(t, src+"\n")
		for _, tk := range toks {
			if tk.Kind == token.ERRORTOKEN {
				t.Errorf("src=%q unexpectedly errored: %v", src, s.Err())
				break
			}
		}
	}
}

func TestStringPrefix_Incompatible(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`ub"x"`, "'u' and 'b' prefixes are incompatible"},
		{`ur"x"`, "'u' and 'r' prefixes are incompatible"},
		{`uf"x"`, "'u' and 'f' prefixes are incompatible"},
		{`bf"x"`, "'b' and 'f' prefixes are incompatible"},
		{`ft"x"`, "'f' and 't' prefixes are incompatible"},
		{`bt"x"`, "'b' and 't' prefixes are incompatible"},
	}
	for _, c := range cases {
		toks, s := runTok(t, c.src+"\n")
		if !hasError(toks, s, c.want) {
			t.Errorf("src=%q: expected %q, err=%v", c.src, c.want, s.Err())
		}
	}
}

func TestLookahead_RewindsCleanly(t *testing.T) {
	// `andx` -> NAME(andx). `lookahead("nd")` triggered by leading `a`
	// path inside verifyEndOfNumber must not consume bytes from a NAME.
	toks, _ := runTok(t, "andx\n")
	want := []token.Type{token.NAME, token.NEWLINE, token.ENDMARKER}
	if len(toks) != len(want) {
		t.Fatalf("got %v, want %v", kinds(toks), want)
	}
	if string(toks[0].Bytes) != "andx" {
		t.Errorf("name=%q want andx", string(toks[0].Bytes))
	}
}
