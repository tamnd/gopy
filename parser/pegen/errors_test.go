package pegen

import (
	"testing"

	perrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/token"
)

func newPforTest(src string) *Parser {
	st := lexer.FromString(src, lexer.ModeFile)
	return New(st, StartFile, 0)
}

func TestRaiseSyntaxErrorPinsAtCurrentToken(t *testing.T) {
	p := newPforTest("foo bar\n")
	_ = p.Peek() // fill at least one token
	p.RaiseSyntaxError("bad %s", "thing")
	se := p.PinnedError()
	if se == nil {
		t.Fatal("PinnedError = nil after RaiseSyntaxError")
	}
	if se.Message != "bad thing" {
		t.Errorf("Message = %q", se.Message)
	}
	if se.Pos.Lineno != 1 {
		t.Errorf("Pos.Lineno = %d, want 1", se.Pos.Lineno)
	}
}

func TestFarthestPosTracksDeepest(t *testing.T) {
	p := newPforTest("a b c d\n")
	for range 3 {
		_ = p.Peek()
		p.mark++
		p.BumpFarthest()
	}
	if p.FarthestPos() != 3 {
		t.Errorf("FarthestPos = %d, want 3", p.FarthestPos())
	}
}

func TestShallowErrorDoesNotOverwriteDeeper(t *testing.T) {
	p := newPforTest("a b c d\n")
	_ = p.Peek()
	p.mark = 3
	p.BumpFarthest()
	p.RaiseSyntaxError("deep")
	p.mark = 0
	p.RaiseSyntaxError("shallow")
	if p.PinnedError() == nil || p.PinnedError().Message != "deep" {
		t.Errorf("PinnedError = %v, want deep", p.PinnedError())
	}
}

func TestExpectForcedPinsExpectedToken(t *testing.T) {
	p := newPforTest("foo\n")
	_ = p.Peek()
	if got := p.ExpectForced(token.COLON, ":"); got != nil {
		t.Fatalf("ExpectForced returned %v, want nil on miss", got)
	}
	se := p.PinnedError()
	if se == nil {
		t.Fatal("PinnedError = nil after ExpectForced miss")
	}
	if se.Message != "expected ':'" {
		t.Errorf("Message = %q, want \"expected ':'\"", se.Message)
	}
}

func TestRaiseKnownLocation(t *testing.T) {
	p := newPforTest("x\n")
	pos := perrors.Pos{Lineno: 9, ColOff: 3, EndLine: 9, EndCol: 5}
	p.RaiseSyntaxErrorKnownLocation(pos, "cannot %s", "assign")
	se := p.PinnedError()
	if se == nil || se.Pos != pos || se.Message != "cannot assign" {
		t.Errorf("PinnedError = %+v, want pos=%+v msg=cannot assign", se, pos)
	}
}
