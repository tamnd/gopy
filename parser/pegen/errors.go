// CPython: Parser/pegen_errors.c. Error pinning surface for the
// generated parser table. The PEG runtime tracks the deepest token
// position any rule reached and pins the SyntaxError there. Without
// this heuristic, PEG errors would always point at the start of the
// file because every rule alt eventually backtracks to position 0.

package pegen

import (
	"fmt"

	perrors "github.com/tamnd/gopy/parser/errors"
)

// RaiseSyntaxError pins a SyntaxError at the current parse position.
// The generated parser calls this from a failing rule body. The
// "farthest reached" heuristic keeps the latest, deepest record;
// earlier shallower errors are dropped.
//
// CPython: Parser/pegen_errors.c:228 _PyPegen_raise_error
func (p *Parser) RaiseSyntaxError(format string, args ...any) {
	t := p.Peek()
	pos := perrors.Pos{}
	if t != nil {
		pos = perrors.Pos{
			Lineno:  t.Lineno,
			ColOff:  t.ColOff,
			EndLine: t.EndLine,
			EndCol:  t.EndCol,
		}
	}
	p.recordError(perrors.KindSyntax, pos, fmt.Sprintf(format, args...))
}

// RaiseSyntaxErrorKnownLocation pins a SyntaxError at a caller-given
// span. Used by action helpers that already know where they want the
// caret to land (invalid LHS, "did you mean :=", paren mismatch).
//
// CPython: Parser/pegen_errors.c:317 _PyPegen_raise_error_known_location
func (p *Parser) RaiseSyntaxErrorKnownLocation(pos perrors.Pos, format string, args ...any) {
	p.recordError(perrors.KindSyntax, pos, fmt.Sprintf(format, args...))
}

// RaiseIndentationError pins an IndentationError. Mirrors
// RAISE_INDENTATION_ERROR in pegen.h.
//
// CPython: Parser/pegen.h RAISE_INDENTATION_ERROR
func (p *Parser) RaiseIndentationError(format string, args ...any) {
	t := p.Peek()
	pos := perrors.Pos{}
	if t != nil {
		pos = perrors.Pos{Lineno: t.Lineno, ColOff: t.ColOff, EndLine: t.EndLine, EndCol: t.EndCol}
	}
	p.recordError(perrors.KindIndentation, pos, fmt.Sprintf(format, args...))
}

// FarthestPos reports the deepest token index any rule has reached
// during this parse. Diagnostic; used by error pinning to decide
// whether a fresh error replaces the stored one.
//
// CPython: Parser/pegen.h farthest_pos
func (p *Parser) FarthestPos() int { return p.farthestPos }

// BumpFarthest is called by the generated parser at the head of
// every rule alt that consumes a token. Keeps p.farthestPos at the
// deepest mark seen so far.
func (p *Parser) BumpFarthest() {
	if p.mark > p.farthestPos {
		p.farthestPos = p.mark
	}
}

// PinnedError returns the SyntaxError currently pinned on the
// parser, or nil if none. The driver calls this once parsing fails
// to convert the indicator into a return value.
//
// CPython: Parser/pegen.c:1136 _PyPegen_run_parser error promotion
func (p *Parser) PinnedError() *perrors.SyntaxError { return p.pinnedErr }

func (p *Parser) recordError(kind perrors.Kind, pos perrors.Pos, msg string) {
	if p.pinnedErr != nil && p.mark < p.farthestPos {
		// A deeper error already won; ignore this shallower one.
		return
	}
	p.pinnedErr = &perrors.SyntaxError{
		Kind:    kind,
		Pos:     pos,
		Message: msg,
	}
	if p.mark > p.farthestPos {
		p.farthestPos = p.mark
	}
	p.errorIndicator = true
}
