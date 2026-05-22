// CPython: Parser/lexer/lexer.c.
//
// This file ports the regular-mode FSM. The f-string mode FSM lives in
// fstring.go. The normal-mode entry point is tokGetNormalMode at
// Parser/lexer/lexer.c:501; the helpers nextC / backup / lineCont track
// position and refill, and the ASCII char-class predicates mirror the
// is_potential_identifier_* macros at the top of lexer.c.
//
// Function map (lexer.c -> gopy):
//
//	TOK_GET_MODE / TOK_NEXT_MODE                  -> State.curMode / State.nextMode (state.go)
//	contains_null_bytes                           -> inline byte check at nextC refill
//	tok_nextc                                     -> State.nextC
//	tok_backup                                    -> State.backup
//	set_ftstring_expr                             -> setFTStringExpr (fstring.go) [task #618]
//	lookahead                                     -> inline at call sites in tokGetNormalMode
//	verify_end_of_number                          -> [task #612, pending]
//	verify_identifier                             -> [task #612, pending]
//	tok_decimal_tail                              -> inlined in scanNumber
//	tok_continuation_line                         -> inlined in nextC line-continuation branch
//	maybe_raise_syntax_error_for_string_prefixes  -> [task #617, pending]
//	tok_get_normal_mode                           -> State.tokGetNormalMode
//	tok_get_fstring_mode                          -> State.tokGetFStringMode (fstring.go)
//	tok_get                                       -> State.Get (state.go)

package lexer

import (
	"fmt"
	"slices"
	"unicode"

	"github.com/tamnd/gopy/token"
)

// eof is the sentinel returned by nextC at end of input. CPython uses
// EOF (-1) from <stdio.h>; gopy uses -1 the same way.
const eof = -1

// isPotentialIdentifierStart matches the C macro: ASCII letter, '_', or
// any non-ASCII byte (the FSM revalidates the UTF-8 sequence later).
//
// CPython: Parser/lexer/lexer.c:12 is_potential_identifier_start
func isPotentialIdentifierStart(c int) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		c == '_' || c >= 128
}

// isPotentialIdentifierChar matches the continuation form: starts plus
// ASCII digits.
//
// CPython: Parser/lexer/lexer.c:18 is_potential_identifier_char
func isPotentialIdentifierChar(c int) bool {
	return isPotentialIdentifierStart(c) || (c >= '0' && c <= '9')
}

// nextC returns the next byte and advances cur. Refills the buffer from
// the driver when cur catches up to inp.
//
// CPython: Parser/lexer/lexer.c:60 tok_nextc
func (s *State) nextC() int {
	for {
		if s.cur != s.inp {
			if s.pendingLineno != 0 {
				s.lineno += s.pendingLineno
				s.pendingLineno = 0
			}
			s.col++
			c := int(s.buf[s.cur])
			s.cur++
			return c
		}
		if s.done != eOK {
			return eof
		}
		if s.underflow == nil || !s.underflow(s) {
			s.cur = s.inp
			return eof
		}
		s.lineStart = s.cur
		// CPython: Parser/lexer/lexer.c:89 contains_null_bytes
		if containsNullBytes(s.buf[s.lineStart:s.inp]) {
			s.recordError("source code cannot contain null bytes")
			s.done = eSyntax
			s.cur = s.inp
			return eof
		}
	}
}

// containsNullBytes mirrors Parser/lexer/lexer.c:53 contains_null_bytes.
func containsNullBytes(p []byte) bool {
	return slices.Contains(p, 0)
}

// backup undoes the previous nextC. Symmetric with the C source.
//
// CPython: Parser/lexer/lexer.c:99 tok_backup
func (s *State) backup(c int) {
	if c == eof {
		return
	}
	s.cur--
	s.col--
}

// peek returns the next byte without consuming it.
func (s *State) peek() int {
	if s.cur >= s.inp {
		if s.underflow == nil || !s.underflow(s) {
			return eof
		}
	}
	return int(s.buf[s.cur])
}

// tokGetNormalMode is the regular-mode scanner. Pulls one token from
// the source and returns it. The C source mutates a caller-owned token
// and returns the kind; gopy returns Tok by value.
//
// The body is wrapped in a for loop so the '\n' branch can `continue`
// to mirror CPython's `goto nextline`: a blank or comment-only line
// resets state and rescans the next line instead of emitting a
// spurious NEWLINE token.
//
// CPython: Parser/lexer/lexer.c:501 tok_get_normal_mode
func (s *State) tokGetNormalMode() Tok {
	for {
		s.start = s.cur
		s.startCol = s.col
		s.blankline = false

		if s.atbol {
			s.atbol = false
			if t, emit := s.indentNL(); emit {
				return t
			}
		}

		if s.pendin != 0 {
			// CPython only fills p_start/p_end for INDENT/DEDENT when
			// tok_extra_tokens is set (Parser/lexer/lexer.c:619, 627);
			// otherwise NULL falls through to _PyLexer_token_setup and
			// the row/col fields end up at -1.
			start, end := -1, -1
			if s.tokExtraTokens {
				start = s.cur
				end = s.cur
			}
			if s.pendin < 0 {
				s.pendin++
				return s.tokenSetup(token.DEDENT, start, end)
			}
			s.pendin--
			if s.tokExtraTokens {
				start = s.lineStart
			}
			return s.tokenSetup(token.INDENT, start, end)
		}

		c := s.nextC()
		for c == ' ' || c == '\t' || c == '\014' {
			c = s.nextC()
		}

		// Line continuation: a backslash at end of line joins the next
		// line into the current one without emitting NEWLINE.
		//
		// CPython: Parser/lexer/lexer.c:1205 (continuation branch)
		for c == '\\' && s.peek() == '\n' {
			s.nextC()
			s.pendingLineno++
			s.col = 0
			s.lineStart = s.cur
			s.contLine = true
			c = s.nextC()
			for c == ' ' || c == '\t' || c == '\014' {
				c = s.nextC()
			}
		}

		s.start = s.cur - 1
		s.startCol = s.col - 1

		if c == '#' {
			commentStart := s.cur - 1
			for c != '\n' && c != eof {
				c = s.nextC()
			}
			// Type comment recognition (`# type: ...`).
			//
			// CPython: Parser/lexer/lexer.c:830 type-comment branch
			if s.typeComments {
				if t, ok := s.maybeTypeComment(commentStart, s.cur); ok {
					return t
				}
			}
			if s.tokExtraTokens {
				// CPython: Parser/lexer/lexer.c:721 tok_backup(tok, c).
				// Hand the trailing '\n' (or EOF) back so the next
				// call hits the '\n' branch and emits NL.
				end := s.cur
				if c == '\n' || c == eof {
					s.backup(c)
					end = s.cur
				}
				tok := s.tokenSetup(token.COMMENT, commentStart, end)
				s.commentNewline = s.blankline
				return tok
			}
			if c == eof {
				return s.endmarker()
			}
		}

		if c == '\n' {
			// CPython emits NL with p_end = tok->cur (includes the
			// trailing '\n') and NEWLINE with p_end = tok->cur - 1
			// (excludes it). The Python-tokenize wrapper later
			// recomputes col_offset / end_col_offset from p_start and
			// p_end via _get_col_offsets, so the raw token must carry
			// columns derived from byte offsets, not from live
			// s.col (which is the post-newline value, one past
			// p_end for NEWLINE). The line-state bump runs after the
			// Tok is built so s.lineno still points at the closing
			// line.
			//
			// CPython: Parser/lexer/lexer.c:805
			start := s.start
			endNL := s.cur
			endNEWLINE := s.cur - 1
			bump := func() {
				s.atbol = true
				s.pendingLineno++
				s.col = 0
				s.lineStart = s.cur
			}
			// Blank line or in-paren continuation: drop the newline
			// unless extra-tokens mode wants the NL.
			if s.blankline || s.level > 0 {
				if s.tokExtraTokens {
					if s.commentNewline {
						s.commentNewline = false
					}
					tok := s.newlineTokenSetup(token.NL, start, endNL)
					bump()
					return tok
				}
				bump()
				continue
			}
			// CPython: Parser/lexer/lexer.c:819. Comment-only line in
			// extra-tokens mode flushes a trailing NL after the COMMENT.
			if s.commentNewline && s.tokExtraTokens {
				s.commentNewline = false
				tok := s.newlineTokenSetup(token.NL, start, endNL)
				bump()
				return tok
			}
			tok := s.newlineTokenSetup(token.NEWLINE, start, endNEWLINE)
			bump()
			return tok
		}

		if c == eof {
			return s.endmarker()
		}

		if isPotentialIdentifierStart(c) {
			return s.scanName(c)
		}
		if c >= '0' && c <= '9' {
			return s.scanNumber(c)
		}
		if c == '"' || c == '\'' {
			return s.scanString(c)
		}
		return s.scanOperator(c)
	}
}

// indentNL handles beginning-of-line column counting and emits INDENT
// or DEDENT when the column changes versus the indent stack. Returns
// (tok, true) if a token should be emitted now, (zero, false) to fall
// through to normal scanning.
//
// CPython: Parser/lexer/lexer.c:515 tok_get_normal_mode (atbol branch)
func (s *State) indentNL() (Tok, bool) {
	col := 0
	altcol := 0
	contLineCol := 0
	var c int
loop:
	for {
		c = s.nextC()
		switch c {
		case ' ':
			col++
			altcol++
		case '\t':
			col = (col/s.tabSize + 1) * s.tabSize
			altcol = (altcol/altTabSize + 1) * altTabSize
		case '\014':
			col = 0
			altcol = 0
		case '\\':
			// Indentation cannot be split over multiple physical
			// lines using backslashes. The first `\` we see pins the
			// indentation level for whatever follows the
			// continuation.
			//
			// CPython: Parser/lexer/lexer.c:532
			if contLineCol == 0 {
				contLineCol = col
			}
			if _, ok := s.continuationLine(); !ok {
				return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur), true
			}
		default:
			s.backup(c)
			break loop
		}
	}

	// Blank line, comment-only line, or in-paren continuation: do not
	// adjust indent stack.
	//
	// CPython: Parser/lexer/lexer.c:550 sets blankline=1 when the
	// indent loop lands on '#', '\n', or '\r'. The '\n' branch later
	// uses blankline to drop the newline via `goto nextline` so blank
	// and comment-only lines don't reach the parser as NEWLINE tokens.
	c = s.peek()
	if c == '#' || c == '\n' || c == eof {
		s.blankline = true
	}
	if c == '#' || c == '\n' || c == eof || s.level > 0 {
		return Tok{}, false
	}

	// CPython preserves the column captured before the first `\\`
	// (cont_line_col) so that backslash-continued indentation reports
	// at the original whitespace count rather than at the column on
	// the post-continuation physical line.
	//
	// CPython: Parser/lexer/lexer.c:572
	if contLineCol != 0 {
		col = contLineCol
		altcol = contLineCol
	}

	if col == s.indstack[s.indent] {
		// Same level.
		return Tok{}, false
	}
	if col > s.indstack[s.indent] {
		if s.indent+1 >= maxIndent {
			s.done = eToodeep
			s.recordError("too many levels of indentation")
			return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur), true
		}
		s.pendin++
		s.indent++
		s.indstack[s.indent] = col
		s.altstack[s.indent] = altcol
		return Tok{}, false
	}
	for s.indent > 0 && col < s.indstack[s.indent] {
		s.pendin--
		s.indent--
	}
	if col != s.indstack[s.indent] {
		s.done = eDedent
		s.recordError("unindent does not match any outer indentation level")
		return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur), true
	}
	return Tok{}, false
}

// continuationLine consumes the `\n` that follows a backslash inside
// the indent loop, advances tokenizer state to the next physical
// line, and returns the peeked first byte of that line (or eof). The
// returned ok=false signals an E_LINECONT / E_EOF style abort.
//
// CPython: Parser/lexer/lexer.c:435 tok_continuation_line
func (s *State) continuationLine() (int, bool) {
	c := s.nextC()
	if c == '\r' {
		c = s.nextC()
	}
	if c != '\n' {
		s.done = eErrLine
		s.recordError("unexpected character after line continuation character")
		return c, false
	}
	// The `\n` was consumed: advance to the next physical line. The
	// preloaded buffer model defers the lineno bump until nextC
	// returns the first byte of the new line.
	s.pendingLineno++
	s.col = 0
	s.lineStart = s.cur
	s.contLine = true
	c = s.nextC()
	if c == eof {
		s.done = eEOF
		return c, false
	}
	s.backup(c)
	return c, true
}

// scanName scans an identifier starting at the byte already consumed
// into c. Mirrors CPython's tok_get_normal_mode identifier arm
// character-by-character: the string-prefix probe is interleaved with
// the identifier-char loop and breaks the moment a non-prefix letter
// (or a repeat of one already seen) appears. That ordering is what
// keeps `shrink"` from being mistaken for a string prefix.
//
// CPython: Parser/lexer/lexer.c:743 (identifier branch in tok_get_normal_mode)
func (s *State) scanName(c int) Tok {
	sawB, sawR, sawU, sawF, sawT := false, false, false, false, false
	for {
		switch {
		case !sawB && (c == 'b' || c == 'B'):
			sawB = true
		case !sawU && (c == 'u' || c == 'U'):
			sawU = true
		case !sawR && (c == 'r' || c == 'R'):
			sawR = true
		case !sawF && (c == 'f' || c == 'F'):
			sawF = true
		case !sawT && (c == 't' || c == 'T'):
			sawT = true
		default:
			goto identTail
		}
		c = s.nextC()
		if c == '"' || c == '\'' {
			// CPython: Parser/lexer/lexer.c:771 maybe_raise_syntax_error_for_string_prefixes
			if s.maybeRaiseSyntaxErrorForStringPrefixes(sawB, sawR, sawU, sawF, sawT) {
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			// CPython: Parser/lexer/lexer.c:778 (f/t-string entry)
			if sawF || sawT {
				return s.startFString(s.start, s.cur-1, c)
			}
			// CPython: Parser/lexer/lexer.c:781 goto letter_quote
			s.backup(c)
			c = s.nextC()
			return s.scanString(c)
		}
	}
identTail:
	// CPython: Parser/lexer/lexer.c:784 identifier-char tail
	for isPotentialIdentifierChar(c) {
		c = s.nextC()
	}
	s.backup(c)
	// CPython: Parser/lexer/lexer.c:364 verify_identifier
	// The full check needs the XID_Start / XID_Continue Unicode tables
	// generated from the same Unicode version CPython ships. gopy's
	// objects.IsXIDStartRune is itself approximate, so wiring it here
	// would just propagate that approximation. Pending tasks #612 +
	// unicodedata XID table port.
	if !s.verifyIdentifier() {
		return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
	}
	return s.tokenSetup(token.NAME, s.start, s.cur)
}

// scanNumber scans an integer or floating-point literal. Handles the
// 0x / 0o / 0b prefixes, decimal digits, optional fraction, optional
// exponent, optional 'j' / 'J' imaginary suffix.
//
// CPython: Parser/lexer/lexer.c:300 tok_decimal_tail and the number
// branch in tok_get_normal_mode.
func (s *State) scanNumber(c int) Tok {
	if c == '0' {
		c = s.nextC()
		if c == 'x' || c == 'X' {
			for isHexDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
			c = s.nextC()
			// CPython: Parser/lexer/lexer.c:875 verify_end_of_number("hexadecimal")
			if c != eof && !s.verifyEndOfNumber(c, "hexadecimal") {
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			s.backup(c)
			return s.tokenSetup(token.NUMBER, s.start, s.cur)
		}
		if c == 'o' || c == 'O' {
			for isOctDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
			c = s.nextC()
			// CPython: Parser/lexer/lexer.c:905 verify_end_of_number("octal")
			if c != eof && !s.verifyEndOfNumber(c, "octal") {
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			s.backup(c)
			return s.tokenSetup(token.NUMBER, s.start, s.cur)
		}
		if c == 'b' || c == 'B' {
			for isBinDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
			c = s.nextC()
			// CPython: Parser/lexer/lexer.c:932 verify_end_of_number("binary")
			if c != eof && !s.verifyEndOfNumber(c, "binary") {
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			s.backup(c)
			return s.tokenSetup(token.NUMBER, s.start, s.cur)
		}
		// Leading zero followed by decimal digits, '.', 'e', 'j' falls
		// through to the regular decimal path.
	}
	for c >= '0' && c <= '9' || c == '_' {
		c = s.nextC()
	}
	if c == '.' {
		c = s.nextC()
		for c >= '0' && c <= '9' || c == '_' {
			c = s.nextC()
		}
	}
	if c == 'e' || c == 'E' {
		c = s.nextC()
		if c == '+' || c == '-' {
			c = s.nextC()
		}
		for c >= '0' && c <= '9' || c == '_' {
			c = s.nextC()
		}
	}
	if c == 'j' || c == 'J' {
		c = s.nextC()
	}
	// verify_end_of_number is called with c still consumed; on success
	// the caller backs c up.
	// CPython: Parser/lexer/lexer.c:305 verify_end_of_number
	if c != eof && !s.verifyEndOfNumber(c, "decimal") {
		return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
	}
	s.backup(c)
	return s.tokenSetup(token.NUMBER, s.start, s.cur)
}

func isHexDigitOrUnderscore(c int) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'f') ||
		(c >= 'A' && c <= 'F') ||
		c == '_'
}

func isOctDigitOrUnderscore(c int) bool {
	return (c >= '0' && c <= '7') || c == '_'
}

func isBinDigitOrUnderscore(c int) bool {
	return c == '0' || c == '1' || c == '_'
}

// scanString scans a single- or triple-quoted string literal. f-strings
// and t-strings are detected by the prefix branch in scanName and
// handled by tokGetFStringMode; this routine is for plain b-, u-, r-,
// or unprefixed strings.
//
// CPython: Parser/lexer/lexer.c:900 (string branch in tok_get_normal_mode)
func (s *State) scanString(quote int) Tok {
	// Pin firstLine at the opening quote so multi-line strings report
	// the start line (CPython tok->first_lineno; ISSTRINGLIT branch in
	// _PyLexer_token_setup reads it).
	//
	// CPython: Parser/lexer/lexer.c:906 tok->first_lineno = tok->lineno
	s.firstLine = s.lineno
	// Detect triple quote.
	triple := false
	if s.peek() == quote {
		s.nextC()
		if s.peek() == quote {
			s.nextC()
			triple = true
		} else {
			// Empty string literal "".
			return s.tokenSetup(token.STRING, s.start, s.cur)
		}
	}
	for {
		c := s.nextC()
		switch c {
		case eof:
			s.done = eEOFS
			s.recordError("unterminated string literal")
			return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
		case '\\':
			escaped := s.nextC()
			if escaped == eof {
				s.done = eEOFS
				s.recordError("unterminated string literal")
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			// `\<newline>` inside a string literal still consumes a
			// physical line. CPython's tok_nextc bumps tok->lineno on
			// every '\n' regardless of context; gopy's scanString uses
			// the raw nextC for the escape byte and must track the
			// line bump itself.
			//
			// CPython: Parser/lexer/lexer.c:1205 (line counter in nextc)
			if escaped == '\n' {
				s.pendingLineno++
				s.col = 0
			}
			continue
		case '\n':
			if !triple {
				s.done = eEOLS
				s.recordError("unterminated string literal")
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			s.pendingLineno++
			s.col = 0
		}
		if c == quote {
			if !triple {
				return s.tokenSetup(token.STRING, s.start, s.cur)
			}
			if s.peek() == quote {
				s.nextC()
				if s.peek() == quote {
					s.nextC()
					return s.tokenSetup(token.STRING, s.start, s.cur)
				}
			}
		}
	}
}

// scanOperator scans an operator or punctuation token. Multi-byte
// operators (==, !=, ->, **, **=, //, //=, ...) are detected by
// peek-and-extend.
//
// CPython: Parser/lexer/lexer.c:1255 (punctuation branch in tok_get_normal_mode)
func (s *State) scanOperator(c int) Tok {
	// CPython's tok_get_normal_mode runs the f-string punctuation hook
	// (update_ftstring_expr + set_ftstring_expr) before the operator
	// dispatch, using curly_bracket_depth - (c != '{') as the cursor.
	// gopy folds the hook in here; the cursor check uses the depth
	// before the dispatch increments/decrements it for '{' and '}'.
	//
	// CPython: Parser/lexer/lexer.c:1258
	ftCursorValid := false
	if (c == ':' || c == '}' || c == '!' || c == '{') &&
		s.insideFString() && s.insideFStringExpr() {
		m := s.curMode()
		delta := 1
		if c == '{' {
			delta = 0
		}
		cursor := m.curlyBracketDepth - delta
		cursorInFormatWithDebug := cursor == 1 && (m.inDebug || m.inFormatSpec)
		ftCursorValid = cursor == 0 || cursorInFormatWithDebug
		if ftCursorValid {
			s.updateFtstringExpr(byte(c))
		}
	}
	var tok Tok
	switch c {
	case '(', '[', '{':
		s.pushParen(byte(c))
		// CPython bumps curly_bracket_depth on every opener, not just
		// `{`. The misleading name is upstream's: the counter tracks
		// nesting depth of any bracket while inside an f-string so the
		// `:` format-spec switch only triggers at the outermost level
		// (e.g. it must NOT fire for the slice colon in
		// f"{arr[1:2]}").
		//
		// CPython: Parser/lexer/lexer.c:1312
		if s.insideFString() {
			s.curMode().curlyBracketDepth++
		}
		tok = s.tokenSetup(oneCharOp(c), s.start, s.cur)
	case ')', ']', '}':
		s.popParen(byte(c))
		// Inside an f-string expression, after popping, a `}` that
		// brings curlyBracketDepth back down to exprStartDepth closes
		// the expression and re-enters f-string mode.
		//
		// CPython: Parser/lexer/lexer.c:1360 INSIDE_FSTRING decrement
		if s.insideFString() {
			m := s.curMode()
			m.curlyBracketDepth--
			if m.curlyBracketDepth < 0 {
				m.curlyBracketDepth = 0
			}
			if c == '}' && m.curlyBracketDepth == m.curlyBracketExprStartDepth {
				m.curlyBracketExprStartDepth--
				m.kind = tokFStringMode
				m.inFormatSpec = false
				m.inDebug = false
			}
		}
		tok = s.tokenSetup(oneCharOp(c), s.start, s.cur)
	case '*', '/', '<', '>', '=', '!':
		c2, c3 := 0, 0
		if s.peek() == '=' {
			c2 = s.peek()
			s.nextC()
		} else if (c == '*' || c == '/' || c == '<' || c == '>') && s.peek() == c {
			c2 = s.peek()
			s.nextC()
			if s.peek() == '=' {
				c3 = s.peek()
				s.nextC()
			}
		}
		tok = s.tokenSetup(classifyOp(c, c2, c3), s.start, s.cur)
	case '+', '%', '&', '|', '^', '@':
		c2 := 0
		if s.peek() == '=' {
			c2 = s.peek()
			s.nextC()
		}
		tok = s.tokenSetup(classifyOp(c, c2, 0), s.start, s.cur)
	case '-':
		c2 := 0
		if s.peek() == '=' || s.peek() == '>' {
			c2 = s.peek()
			s.nextC()
		}
		tok = s.tokenSetup(classifyOp(c, c2, 0), s.start, s.cur)
	case ':':
		c2 := 0
		if s.peek() == '=' {
			c2 = s.peek()
			s.nextC()
		}
		// Inside an f-string interpolation `{expr:fmt}`, a `:` at the
		// outer curly-bracket level switches the lexer back to fstring
		// mode for the format-spec body so `>10`, `>{w}`, etc. arrive
		// as FSTRING_MIDDLE rather than as separate operators.
		//
		// CPython: Parser/lexer/lexer.c:1271 (is_punctuation branch)
		if s.insideFString() && s.insideFStringExpr() {
			m := s.curMode()
			cursor := m.curlyBracketDepth - 1
			if cursor == m.curlyBracketExprStartDepth {
				m.kind = tokFStringMode
				m.inFormatSpec = true
			}
		}
		tok = s.tokenSetup(classifyOp(c, c2, 0), s.start, s.cur)
	case '.':
		// `...` is the only multi-dot operator; `..` is two separate
		// DOTs. CPython's lexer mirrors that: peek twice and only
		// commit to ELLIPSIS when both extra dots are there.
		//
		// CPython: Parser/lexer/lexer.c:832 (period branch)
		if s.peek() == '.' {
			s.nextC()
			if s.peek() == '.' {
				s.nextC()
				tok = s.tokenSetup(threeCharOp('.', '.', '.'), s.start, s.cur)
				break
			}
			s.backup('.')
			tok = s.tokenSetup(oneCharOp(c), s.start, s.cur)
		} else if d := s.peek(); d >= '0' && d <= '9' {
			return s.scanNumber('.')
		} else {
			tok = s.tokenSetup(oneCharOp(c), s.start, s.cur)
		}
	case ',', ';', '~':
		tok = s.tokenSetup(oneCharOp(c), s.start, s.cur)
	default:
		s.done = eToken
		s.recordError("invalid character")
		tok = s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
	}
	if ftCursorValid && c != '{' {
		s.setFtstringExpr(&tok, byte(c))
	}
	return tok
}

func (s *State) pushParen(c byte) {
	if s.level >= maxLevel {
		s.done = eToken
		s.recordError("too many nested parentheses")
		return
	}
	s.parenStack[s.level] = c
	s.parenLineno[s.level] = s.lineno
	s.parenCol[s.level] = s.col - 1
	s.level++
}

func (s *State) popParen(c byte) {
	if s.level == 0 {
		s.done = eToken
		// CPython: Parser/tokenizer/helpers.c:184 surfaces "unmatched ')'"
		// pinned to the closing-bracket location.
		s.recordError(fmt.Sprintf("unmatched '%c'", c))
		return
	}
	s.level--
	open := s.parenStack[s.level]
	want := byte(0)
	switch open {
	case '(':
		want = ')'
	case '[':
		want = ']'
	case '{':
		want = '}'
	}
	if c != want {
		s.done = eToken
		// CPython: Parser/tokenizer/helpers.c:201 surfaces the long form
		// "closing parenthesis '%c' does not match opening parenthesis
		// '%c' on line %d".
		s.recordError(fmt.Sprintf(
			"closing parenthesis '%c' does not match opening parenthesis '%c' on line %d",
			c, open, s.parenLineno[s.level],
		))
	}
}

// endmarker emits the terminal ENDMARKER, also flushing any pending
// dedents back to indent level 0. CPython leaves p_start / p_end NULL
// on the EOF branch (Parser/lexer/lexer.c:738), so col_offset and
// end_col_offset stay -1; the (lineno+1, 0) reshape happens later in
// the Python-tokenize wrapper when extra_tokens is on.
//
// s.done is set to eEOF on the first call so the DEDENT-at-EOF
// tokens that flush ahead of ENDMARKER report E_EOF to the wrapper.
// In CPython this happens in the underflow (file/string/utf8
// tokenizer) before the indent-unwind branch queues DEDENTs via
// tok->pendin; gopy collapses that into endmarker() because the
// buffer is already fully loaded.
//
// CPython: Parser/lexer/lexer.c:734 EOF branch in tok_get_normal_mode
func (s *State) endmarker() Tok {
	s.done = eEOF
	if s.indent > 0 {
		s.indent--
		start, end := -1, -1
		if s.tokExtraTokens {
			start, end = s.cur, s.cur
		}
		return s.tokenSetup(token.DEDENT, start, end)
	}
	return s.tokenSetup(token.ENDMARKER, -1, -1)
}

// maybeTypeComment inspects a comment span and emits a TYPE_COMMENT
// token if it matches the `# type: ` prefix. Returns the token and
// true if matched, zero/false otherwise.
//
// CPython: Parser/lexer/lexer.c:50 type_comment_prefix
func (s *State) maybeTypeComment(start, end int) (Tok, bool) {
	const prefix = "# type:"
	if end-start < len(prefix) {
		return Tok{}, false
	}
	for i := 0; i < len(prefix); i++ {
		if s.buf[start+i] != prefix[i] {
			return Tok{}, false
		}
	}
	body := start + len(prefix)
	for body < end && (s.buf[body] == ' ' || s.buf[body] == '\t') {
		body++
	}
	return s.typeCommentTokenSetup(token.TYPE_COMMENT, s.startCol, s.col, body, end), true
}

// tokGetFStringMode scans inside an f-string or t-string body. Stub:
// the FSM re-entry for interpolation expressions is the bulk of
// Parser/lexer/lexer.c:1393 and lands in fstring.go.
//
// CPython: Parser/lexer/lexer.c:1393 tok_get_fstring_mode
func (s *State) tokGetFStringMode() Tok {
	return s.tokGetFStringModeImpl()
}

// lookahead probes the byte stream for test (a null-terminated literal
// in C, a Go string here) followed by something that is not an
// identifier char. Rewinds whatever it consumed on either branch.
//
// CPython: Parser/lexer/lexer.c:282 lookahead
func (s *State) lookahead(test string) bool {
	consumed := make([]int, 0, len(test)+1)
	matched := true
	for i := 0; i < len(test); i++ {
		c := s.nextC()
		consumed = append(consumed, c)
		if c != int(test[i]) {
			matched = false
			break
		}
	}
	res := false
	if matched {
		c := s.nextC()
		consumed = append(consumed, c)
		res = !isPotentialIdentifierChar(c)
	}
	for i := len(consumed) - 1; i >= 0; i-- {
		s.backup(consumed[i])
	}
	return res
}

// verifyEndOfNumber inspects the byte that terminated a numeric literal
// and either accepts it (returning true) or records a SyntaxError. The
// `1and` / `1or` keyword-abutting branch in CPython emits a
// SyntaxWarning before accepting; gopy's tokenizer doesn't reach the
// warnings module yet, so the warning is currently dropped (the literal
// is still accepted, matching CPython's accept-with-warning outcome
// when warnings are at their default disposition). When the trailing
// byte starts a fresh identifier (`1foo`), we surface
// "invalid <kind> literal" the same way CPython does.
//
// CPython: Parser/lexer/lexer.c:305 verify_end_of_number
func (s *State) verifyEndOfNumber(c int, kind string) bool {
	if s.tokExtraTokens {
		return true
	}
	r := false
	switch c {
	case 'a':
		r = s.lookahead("nd")
	case 'e':
		r = s.lookahead("lse")
	case 'f':
		r = s.lookahead("or")
	case 'i':
		c2 := s.nextC()
		if c2 == 'f' || c2 == 'n' || c2 == 's' {
			r = true
		}
		s.backup(c2)
	case 'o':
		r = s.lookahead("r")
	case 'n':
		r = s.lookahead("ot")
	}
	if r {
		// Backup so the keyword runs through the lexer normally on the
		// next pass, matching tok_backup(tok, c) in CPython.
		s.backup(c)
		s.parserWarn("SyntaxWarning", "invalid %s literal", kind)
		// Re-consume the byte we just backed up so the caller's cursor
		// stays where CPython leaves it after the tok_nextc(tok) call
		// inside verify_end_of_number.
		s.nextC()
		return true
	}
	if c < 128 && isPotentialIdentifierChar(c) {
		s.backup(c)
		s.syntaxError("invalid %s literal", kind)
		return false
	}
	return true
}

// verifyIdentifier checks that the bytes between s.start and s.cur form
// a valid PEP 3131 identifier by running scanIdentifier against the
// XID_Start / XID_Continue tables composed in xid.go.
//
// CPython: Parser/lexer/lexer.c:364 verify_identifier
func (s *State) verifyIdentifier() bool {
	if s.tokExtraTokens {
		return true
	}
	bs := s.buf[s.start:s.cur]
	if _, _, ok := ValidateUTF8(bs); !ok {
		s.done = eDecode
		s.recordError("invalid character in identifier")
		return false
	}
	off, bad, ok := scanIdentifier(string(bs))
	if ok {
		return true
	}
	// Pin tok->cur to the bad rune so callers see the right span.
	s.cur = s.start + off + utf8RuneLen(bad)
	if isPrintable(bad) {
		s.syntaxError("invalid character '%c' (U+%04X)", bad, bad)
	} else {
		s.syntaxError("invalid non-printable character U+%04X", bad)
	}
	return false
}

// utf8RuneLen reports the UTF-8 byte length of r, falling back to 1
// for the replacement-character path so we never advance past the
// buffer.
func utf8RuneLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}

// isPrintable mirrors Py_UNICODE_ISPRINTABLE for the error-message
// branch in verify_identifier.
//
// CPython: Objects/unicodectype.c:269 _PyUnicode_IsPrintable
func isPrintable(r rune) bool { return unicode.IsPrint(r) || r == ' ' }

// maybeRaiseSyntaxErrorForStringPrefixes flags incompatible string
// prefix combos. Supported combos: rb / rf / rt in any order. All other
// pairs across u / b / f / t / r are rejected with a SyntaxError that
// names the conflict.
//
// CPython: Parser/lexer/lexer.c:455 maybe_raise_syntax_error_for_string_prefixes
func (s *State) maybeRaiseSyntaxErrorForStringPrefixes(sawB, sawR, sawU, sawF, sawT bool) bool {
	emit := func(p1, p2 string) {
		startCol := s.start + 1 - s.lineStart
		endCol := s.cur - s.lineStart
		s.syntaxErrorKnownRange(startCol, endCol,
			"'%s' and '%s' prefixes are incompatible", p1, p2)
	}
	switch {
	case sawU && sawB:
		emit("u", "b")
	case sawU && sawR:
		emit("u", "r")
	case sawU && sawF:
		emit("u", "f")
	case sawU && sawT:
		emit("u", "t")
	case sawB && sawF:
		emit("b", "f")
	case sawB && sawT:
		emit("b", "t")
	case sawF && sawT:
		emit("f", "t")
	default:
		return false
	}
	return true
}
