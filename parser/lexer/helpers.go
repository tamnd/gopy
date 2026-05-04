// CPython: Parser/tokenizer/helpers.c.
//
// The C source's helpers split into two camps. The first camp formats
// errors using the live tokenizer state. The gopy port keeps that
// shape in syntaxError and indentError. The second camp deals with
// PEP 263 source decoding, which is handled in the file driver and so
// lives there rather than here.

package lexer

import (
	"fmt"

	"github.com/tamnd/gopy/tokenize"
)

// syntaxError records a SyntaxError at the current cursor position and
// returns an ERRORTOKEN. Mirrors _PyTokenizer_syntaxerror, which uses
// the current line range as the error location.
//
// CPython: Parser/tokenizer/helpers.c:66 _PyTokenizer_syntaxerror
func (s *State) syntaxError(format string, args ...any) Tok {
	s.recordError(fmt.Sprintf(format, args...))
	s.done = eSyntax
	return s.tokenSetup(tokenize.ERRORTOKEN, s.cur, s.cur)
}

// syntaxErrorKnownRange records a SyntaxError pinned to a specific
// column range. The PEG layer uses this when it has already consumed
// the bad token and can name the exact span.
//
// CPython: Parser/tokenizer/helpers.c:77 _PyTokenizer_syntaxerror_known_range
func (s *State) syntaxErrorKnownRange(startCol, endCol int, format string, args ...any) Tok {
	s.recordError(fmt.Sprintf(format, args...))
	if s.err != nil {
		s.err.Pos.Col = startCol
		s.err.EndPos.Col = endCol
	}
	s.done = eSyntax
	return s.tokenSetup(tokenize.ERRORTOKEN, s.cur, s.cur)
}

// indentError flags an inconsistent-tab/space situation. Mirrors the
// E_TABSPACE branch.
//
// CPython: Parser/tokenizer/helpers.c:88 _PyTokenizer_indenterror
func (s *State) indentError() Tok {
	s.done = eTabSpace
	s.cur = s.inp
	return s.tokenSetup(tokenize.ERRORTOKEN, s.cur, s.cur)
}

// warnInvalidEscape mirrors the deprecation warning the C tokenizer
// raises for unrecognised \X escapes inside string literals. The gopy
// surface just stashes the offender for the parser to surface.
//
// CPython: Parser/tokenizer/helpers.c:110 _PyTokenizer_warn_invalid_escape_sequence
func (s *State) warnInvalidEscape(c byte) {
	if !s.reportWarnings {
		return
	}
	// Held for the action_helpers port to surface as a SyntaxWarning.
	_ = c
}
