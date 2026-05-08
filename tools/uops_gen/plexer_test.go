package main

import (
	"strings"
	"testing"
)

// kinds returns just the Kind field of every token (handy for asserting
// the shape of the stream without matching whitespace or columns).
func kinds(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Kind
	}
	return out
}

func TestPLexer_NextSkipsComments(t *testing.T) {
	p, err := NewPLexer("x // c\ny", "<test>")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := p.Next(false)
	b, _ := p.Next(false)
	if a.Text != "x" || b.Text != "y" {
		t.Errorf("got (%q,%q), want (x,y) with comment skipped", a.Text, b.Text)
	}
	if _, ok := p.Next(false); ok {
		t.Error("expected EOF after y")
	}
}

func TestPLexer_NextRawIncludesComments(t *testing.T) {
	p, _ := NewPLexer("x // c\ny", "<test>")
	var got []string
	for {
		tok, ok := p.Next(true)
		if !ok {
			break
		}
		got = append(got, tok.Kind)
	}
	if got[1] != TokComment {
		t.Errorf("kinds = %v, want COMMENT in slot 1", got)
	}
}

func TestPLexer_BackupAndPeek(t *testing.T) {
	p, _ := NewPLexer("a b c", "<test>")
	first, _ := p.Peek(false)
	if first.Text != "a" {
		t.Fatalf("peek = %q, want a", first.Text)
	}
	if p.Pos != 0 {
		t.Errorf("Peek advanced cursor to %d", p.Pos)
	}
	p.Next(false)
	p.Next(false)
	p.Backup()
	tok, _ := p.Peek(false)
	if tok.Text != "b" {
		t.Errorf("after backup peek = %q, want b", tok.Text)
	}
}

func TestPLexer_ExpectMatchAndMismatch(t *testing.T) {
	p, _ := NewPLexer("foo;", "<test>")
	if _, ok := p.Expect(TokIdentifier); !ok {
		t.Fatal("Expect IDENTIFIER on 'foo' should match")
	}
	if _, ok := p.Expect(TokIdentifier); ok {
		t.Fatal("Expect IDENTIFIER on ';' should not match")
	}
	if tok, _ := p.Peek(false); tok.Kind != TokSemi {
		t.Errorf("after failed Expect, peek kind = %s, want SEMI", tok.Kind)
	}
}

func TestPLexer_RequireRaisesOnMismatch(t *testing.T) {
	p, _ := NewPLexer("123", "<test>")
	_, err := p.Require(TokIdentifier)
	if err == nil {
		t.Fatal("Require IDENTIFIER on NUMBER should error")
	}
	if !strings.Contains(err.Error(), "Expected") {
		t.Errorf("error = %v, want message to mention Expected", err)
	}
}

func TestPLexer_ConsumeToHandlesNestedParens(t *testing.T) {
	p, _ := NewPLexer("foo(a, (b, c)); next", "<test>")
	p.Next(false) // foo
	p.Next(false) // (
	got, err := p.ConsumeTo(TokRParen)
	if err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if last.Kind != TokRParen {
		t.Errorf("last kind = %s, want RPAREN", last.Kind)
	}
	if next, _ := p.Next(false); next.Kind != TokSemi {
		t.Errorf("after consume_to, next kind = %s, want SEMI", next.Kind)
	}
}

func TestPLexer_ConsumeToReachesEOF(t *testing.T) {
	p, _ := NewPLexer("foo bar", "<test>")
	if _, err := p.ConsumeTo(TokSemi); err == nil {
		t.Fatal("ConsumeTo on missing terminator should error")
	}
}

func TestPLexer_ExtractLine(t *testing.T) {
	p, _ := NewPLexer("alpha\nbeta\ngamma", "<test>")
	if got := p.ExtractLine(2); got != "beta" {
		t.Errorf("ExtractLine(2) = %q, want beta", got)
	}
	if got := p.ExtractLine(99); got != "" {
		t.Errorf("ExtractLine(99) = %q, want empty", got)
	}
}

func TestPLexer_SetPosRoundTrip(t *testing.T) {
	p, _ := NewPLexer("a b c", "<test>")
	save := p.GetPos()
	p.Next(false)
	p.Next(false)
	p.SetPos(save)
	tok, _ := p.Peek(false)
	if tok.Text != "a" {
		t.Errorf("after SetPos, peek = %q, want a", tok.Text)
	}
}

func TestPLexer_KindsMatchOnSimpleIf(t *testing.T) {
	p, _ := NewPLexer("if (x) { x.foo; // comment\n}", "<test>")
	var toks []Token
	for {
		tok, ok := p.Next(true)
		if !ok {
			break
		}
		toks = append(toks, tok)
	}
	got := kinds(toks)
	want := []string{
		TokIf, TokLParen, TokIdentifier, TokRParen,
		TokLBrace, TokIdentifier, TokPeriod, TokIdentifier, TokSemi,
		TokComment, TokRBrace,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}
