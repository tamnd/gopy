// CPython: Parser/lexer/state.c:131 _PyLexer_token_setup,
// Parser/lexer/state.c:118 _PyLexer_type_comment_token_setup, plus the
// tok_get / _PyTokenizer_Get dispatch at Parser/lexer/lexer.c:1616.

package lexer

import "github.com/tamnd/gopy/token"

// tokenSetup fills the boundary fields on a Tok the FSM has been
// staging, then returns it. The C source mutates a caller-owned token
// and returns the kind; the gopy variant returns Tok by value because
// the FSM works in offsets rather than pointers.
//
// CPython: Parser/lexer/state.c:131 _PyLexer_token_setup
func (s *State) tokenSetup(kind token.Type, start, end int) Tok {
	t := Tok{
		Kind:        kind,
		Level:       s.level,
		StartOffset: start,
		EndOffset:   end,
	}
	if start >= 0 && end >= start && end <= len(s.buf) {
		t.Bytes = s.buf[start:end]
	}
	if isStringLit(kind) {
		t.Start.Line = s.firstLine
	} else {
		t.Start.Line = s.lineno
	}
	t.End.Line = s.lineno
	t.Start.Col = -1
	t.End.Col = -1
	if start >= 0 && end >= 0 {
		t.Start.Col = s.startCol
		t.End.Col = s.col
	}
	return t
}

// typeCommentTokenSetup mirrors the type-comment variant: same boundary
// fields, but col offsets come in directly from the caller because the
// scanner needs to span the # ... newline range explicitly.
//
// CPython: Parser/lexer/state.c:118 _PyLexer_type_comment_token_setup
func (s *State) typeCommentTokenSetup(kind token.Type, startCol, endCol, start, end int) Tok {
	return Tok{
		Kind:        kind,
		Level:       s.level,
		Bytes:       s.buf[start:end],
		Start:       Pos{Line: s.lineno, Col: startCol},
		End:         Pos{Line: s.lineno, Col: endCol},
		StartOffset: start,
		EndOffset:   end,
	}
}

// isStringLit matches CPython's ISSTRINGLIT: STRING, FSTRING_*,
// TSTRING_*. Used so multi-line string tokens report the line where the
// opening quote sat instead of where the closing quote sat.
//
// CPython: Include/internal/pycore_token.h:96 ISSTRINGLIT
func isStringLit(t token.Type) bool {
	switch t {
	case token.STRING,
		token.FSTRING_START, token.FSTRING_MIDDLE, token.FSTRING_END,
		token.TSTRING_START, token.TSTRING_MIDDLE, token.TSTRING_END:
		return true
	}
	return false
}

// Get is the public entry point. One call returns one token, or sets
// s.done / s.err and returns an ERRORTOKEN / ENDMARKER.
//
// CPython: Parser/lexer/lexer.c:1626 _PyTokenizer_Get
func (s *State) Get() Tok {
	t := s.tokGet()
	return t
}

// tokGet dispatches to the regular-mode or f-string-mode scanner based
// on the active tokenizer_mode entry.
//
// CPython: Parser/lexer/lexer.c:1616 tok_get
func (s *State) tokGet() Tok {
	m := s.curMode()
	if m.kind == tokRegularMode {
		return s.tokGetNormalMode()
	}
	return s.tokGetFStringMode()
}
