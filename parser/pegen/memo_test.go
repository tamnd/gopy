package pegen

import (
	"testing"

	"github.com/tamnd/gopy/parser/lexer"
)

func newParser(t *testing.T, src string) *Parser {
	t.Helper()
	st := lexer.FromBytes([]byte(src), lexer.ModeFile)
	return New(st, StartFile, 0)
}

func TestMemoInsertAndLookup(t *testing.T) {
	p := newParser(t, "x = 1\n")
	if _, ok := p.IsMemoized(42); ok {
		t.Errorf("empty memo should miss")
	}
	mark := p.Mark()
	p.mark = mark + 2
	p.InsertMemo(mark, 42, "cached")
	p.mark = mark
	got, ok := p.IsMemoized(42)
	if !ok || got.(string) != "cached" {
		t.Errorf("IsMemoized = %v, %v", got, ok)
	}
	if p.mark != mark+2 {
		t.Errorf("mark not advanced to cached end: %d", p.mark)
	}
}

func TestMemoUpdateOverwrites(t *testing.T) {
	p := newParser(t, "x\n")
	mark := p.Mark()
	p.Peek()
	p.InsertMemo(mark, 7, "v1")
	p.UpdateMemo(mark, 7, "v2")
	got, _ := p.IsMemoized(7)
	if got.(string) != "v2" {
		t.Errorf("UpdateMemo did not overwrite: %v", got)
	}
}

func TestLookaheadRestoresMark(t *testing.T) {
	p := newParser(t, "x\n")
	startMark := p.Mark()
	matched := p.Lookahead(true, func(p *Parser) any {
		p.Peek()
		p.mark++
		return "ok"
	})
	if !matched {
		t.Errorf("positive lookahead should match a non-nil result")
	}
	if p.mark != startMark {
		t.Errorf("lookahead leaked mark: %d", p.mark)
	}
}

func TestNegativeLookaheadOnNil(t *testing.T) {
	p := newParser(t, "x\n")
	matched := p.Lookahead(false, func(p *Parser) any { return nil })
	if !matched {
		t.Errorf("negative lookahead should match a nil result")
	}
}

func TestExpectSoftKeyword(t *testing.T) {
	p := newParser(t, "match x\n")
	if p.ExpectSoftKeyword("case") != nil {
		t.Errorf("should not match wrong kw")
	}
	if p.ExpectSoftKeyword("match") == nil {
		t.Errorf("should match 'match'")
	}
}
