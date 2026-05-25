// CPython: Parser/pegen_errors.c. Error pinning surface for the
// generated parser table. The PEG runtime tracks the deepest token
// position any rule reached and pins the SyntaxError there. Without
// this heuristic, PEG errors would always point at the start of the
// file because every rule alt eventually backtracks to position 0.

package pegen

import (
	"fmt"

	perrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/token"
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

// SetSyntaxError is the driver hook callers run after Dispatch fails.
// It mirrors CPython's _Pypegen_set_syntax_error: if a SyntaxError is
// already pinned, drain the lexer once to see whether the tokenizer
// can refine it (unclosed paren), then propagate. Otherwise build a
// fresh SyntaxError at the last token, using the ERRORTOKEN/EOF and
// INDENT/DEDENT shortcuts before falling back to "invalid syntax".
//
// CPython: Parser/pegen_errors.c:413 _Pypegen_set_syntax_error
func (p *Parser) SetSyntaxError() {
	if p.pinnedErr != nil {
		// Existing error: prioritize tokenizer errors raised on the
		// second phase only if the existing error came from the parser
		// (tokenizer is in OK/EOF/DONE state).
		if p.tok != nil {
			done := p.tok.Done()
			if done == lexer.DoneOK || done == lexer.DoneEOF {
				p.tokenizeFullSourceCheckForErrors()
			}
		}
		return
	}
	if p.fill == 0 {
		p.recordError(perrors.KindSyntax, perrors.Pos{}, "error at start before reading any input")
		return
	}
	last := p.tokens[p.fill-1]
	if last.Type == token.ERRORTOKEN && p.tok != nil && p.tok.Done() == lexer.DoneEOF {
		if p.tok.Level() > 0 {
			p.raiseUnclosedParenthesesError()
		} else {
			pos := perrors.Pos{Lineno: last.Lineno, ColOff: last.ColOff, EndLine: last.EndLine, EndCol: last.EndCol}
			p.recordError(perrors.KindSyntax, pos, "unexpected EOF while parsing")
		}
		return
	}
	if last.Type == token.INDENT || last.Type == token.DEDENT {
		msg := "unexpected indent"
		if last.Type == token.DEDENT {
			msg = "unexpected unindent"
		}
		pos := perrors.Pos{Lineno: last.Lineno, ColOff: last.ColOff, EndLine: last.EndLine, EndCol: last.EndCol}
		p.recordError(perrors.KindIndentation, pos, msg)
		return
	}
	pos := perrors.Pos{Lineno: last.Lineno, ColOff: last.ColOff, EndLine: last.EndLine, EndCol: last.EndCol}
	p.recordError(perrors.KindSyntax, pos, "invalid syntax")
	p.tokenizeFullSourceCheckForErrors()
}

// raiseUnclosedParenthesesError pins a SyntaxError at the line/col of
// the still-open paren on the lexer's parenstack.
//
// CPython: Parser/pegen_errors.c:60 raise_unclosed_parentheses_error
func (p *Parser) raiseUnclosedParenthesesError() {
	if p.tok == nil || p.tok.Level() == 0 {
		return
	}
	line, col, ch := p.tok.ParenInfo(p.tok.Level())
	pos := perrors.Pos{Lineno: line, ColOff: col, EndLine: line, EndCol: -1}
	// Side-step recordError's farthest-pos guard: the tokenizer-side
	// caret is authoritative.
	p.pinnedErr = &perrors.SyntaxError{
		Kind:     perrors.KindSyntax,
		Pos:      pos,
		Filename: p.tok.Filename(),
		Message:  fmt.Sprintf("'%c' was never closed", ch),
	}
	p.pinnedFromTokenizer = true
	p.errorIndicator = true
}

// tokenizeFullSourceCheckForErrors drains the lexer to surface any
// tokenizer-side errors the parser failed to consume. A new ERRORTOKEN
// with a recorded message (unterminated string, bad escape, ...)
// overrides whatever generic SyntaxError is currently pinned. An
// ERRORTOKEN at EOF with an unclosed paren raises the matching
// "'%c' was never closed" diagnostic.
//
// CPython: Parser/pegen_errors.c:164 _PyPegen_tokenize_full_source_to_check_for_errors
func (p *Parser) tokenizeFullSourceCheckForErrors() {
	if p.tok == nil {
		return
	}
	// CPython skips the drain for interactive input. gopy mirrors this
	// once the readline driver is wired through; today the
	// FromReadline path is the only place that needs the guard, and
	// it's already filtered by callers using ModeSingle.
	saved := p.pinnedErr
	savedFromTok := p.pinnedFromTokenizer
	currentErrLine := 0
	switch {
	case saved != nil:
		currentErrLine = saved.Pos.Lineno
	case p.knownErrToken != nil:
		currentErrLine = p.knownErrToken.Lineno
	case p.fill > 0:
		currentErrLine = p.tokens[p.fill-1].Lineno
	}
	// Speculatively clear so the lexer's new error can pin itself.
	p.pinnedErr = nil
	p.pinnedFromTokenizer = false
	raised := false
	for {
		tk := p.tok.Get()
		if tk.Kind == token.ERRORTOKEN {
			if p.tok.Err() != nil {
				// Inside an f-string the expression-level parser error
				// should propagate; a tokenizer error produced by draining
				// the rest of the source (e.g. the outer closing quote being
				// scanned as an unterminated inner string → "f-string:
				// expecting '}'") must NOT swallow it.
				//
				// CPython: Parser/pegen_errors.c:212
				// if (PyErr_Occurred() && p->tok->tok_mode_stack_index <= 0)
				if p.tok.InsideFString() {
					// In f-string context: restore the old error, not the
					// new tokenizer one.
					break
				}
				// Lexer pinned a structured error: lift it to the
				// parser surface, overriding the prior generic one.
				pos := perrors.Pos{Lineno: tk.Start.Line, ColOff: tk.Start.Col, EndLine: tk.End.Line, EndCol: tk.End.Col}
				ftok := &Token{Type: tk.Kind, Lineno: pos.Lineno, ColOff: pos.ColOff, EndLine: pos.EndLine, EndCol: pos.EndCol}
				p.pinnedErr = tokenizerSyntaxError(p.tok, ftok)
				p.pinnedFromTokenizer = true
				p.errorIndicator = true
				raised = true
				break
			}
			if p.tok.Level() > 0 {
				line, _, _ := p.tok.ParenInfo(p.tok.Level())
				if currentErrLine > line {
					p.raiseUnclosedParenthesesError()
					raised = true
					break
				}
			}
			break
		}
		if tk.Kind == token.ENDMARKER {
			break
		}
	}
	if !raised {
		// No new error: restore the previously pinned one.
		p.pinnedErr = saved
		p.pinnedFromTokenizer = savedFromTok
	}
}

func (p *Parser) recordError(kind perrors.Kind, pos perrors.Pos, msg string) {
	if p.pinnedFromTokenizer {
		// The tokenizer raised a structured error (unterminated
		// string, bad cookie, indentation, ...). Parser-side
		// refinement in the second pass must defer to it, same as
		// CPython's _Pypegen_set_syntax_error propagating the
		// pending exception instead of overwriting it.
		//
		// CPython: Parser/pegen_errors.c:416 _Pypegen_set_syntax_error
		return
	}
	if p.errorIndicator && p.pinnedErr != nil {
		// Once a parser-side raise has fired, subsequent raises in
		// the same alt-chain are no-ops. Mirrors CPython's
		// `if (p->error_indicator && PyErr_Occurred()) return NULL;`
		// guard so the first SyntaxError wins instead of the last.
		//
		// CPython: Parser/pegen_errors.c:323 _PyPegen_raise_error_known_location
		// CPython: Parser/pegen_errors.c:231 _PyPegen_raise_error
		return
	}
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
