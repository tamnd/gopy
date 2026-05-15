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
	}
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
			if s.pendin < 0 {
				s.pendin++
				return s.tokenSetup(token.DEDENT, s.cur, s.cur)
			}
			s.pendin--
			return s.tokenSetup(token.INDENT, s.cur, s.cur)
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
			s.lineno++
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
			start := s.start
			end := s.cur
			s.atbol = true
			s.lineno++
			s.col = 0
			s.lineStart = s.cur
			// CPython: Parser/lexer/lexer.c:805. Blank line or in-paren
			// continuation drops the newline entirely (`goto nextline`)
			// unless extra-tokens mode wants the NL. Otherwise emit
			// NEWLINE.
			if s.blankline || s.level > 0 {
				if s.tokExtraTokens {
					if s.commentNewline {
						s.commentNewline = false
					}
					return s.tokenSetup(token.NL, start, end)
				}
				continue
			}
			// CPython: Parser/lexer/lexer.c:819. A comment-only line in
			// extra-tokens mode emitted COMMENT first, then this branch
			// flushes the trailing NL.
			if s.commentNewline && s.tokExtraTokens {
				s.commentNewline = false
				return s.tokenSetup(token.NL, start, end)
			}
			return s.tokenSetup(token.NEWLINE, start, end)
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
// CPython: Parser/lexer/lexer.c:501 tok_get_normal_mode (atbol branch)
func (s *State) indentNL() (Tok, bool) {
	col := 0
	altcol := 0
	for {
		c := s.nextC()
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
		default:
			s.backup(c)
			goto done
		}
	}
done:

	// Blank line, comment-only line, or in-paren continuation: do not
	// adjust indent stack.
	//
	// CPython: Parser/lexer/lexer.c:550 sets blankline=1 when the
	// indent loop lands on '#', '\n', or '\r'. The '\n' branch later
	// uses blankline to drop the newline via `goto nextline` so blank
	// and comment-only lines don't reach the parser as NEWLINE tokens.
	c := s.peek()
	if c == '#' || c == '\n' || c == eof {
		s.blankline = true
	}
	if c == '#' || c == '\n' || c == eof || s.level > 0 {
		return Tok{}, false
	}

	if col == s.indstack[s.indent] {
		// Same level.
		return Tok{}, false
	}
	if col > s.indstack[s.indent] {
		if s.indent+1 >= maxIndent {
			s.done = eIndent
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

// scanName scans an identifier starting at the byte already consumed
// into c. ASCII-only for now; non-ASCII bytes are accepted but the
// PEP 3131 normalization pass lives in helpers.go.
//
// CPython: Parser/lexer/lexer.c:743 (identifier branch in tok_get_normal_mode)
func (s *State) scanName(_ int) Tok {
	var c int
	for {
		c = s.nextC()
		if !isPotentialIdentifierChar(c) {
			break
		}
	}
	// String-prefix detection: f", t", rf", rt", fr", tr", b", u", r".
	if c == '"' || c == '\'' {
		if _, _, ok := s.detectStringPrefix(s.start, s.cur-1); ok {
			return s.startFString(s.start, s.cur-1, c)
		}
		// Plain b/u/r prefix: rewind the quote and let scanString do it.
		s.backup(c)
		c = s.nextC()
		return s.scanString(c)
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
			return s.tokenSetup(token.NUMBER, s.start, s.cur)
		}
		if c == 'o' || c == 'O' {
			for isOctDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
			return s.tokenSetup(token.NUMBER, s.start, s.cur)
		}
		if c == 'b' || c == 'B' {
			for isBinDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
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
			if s.nextC() == eof {
				s.done = eEOFS
				s.recordError("unterminated string literal")
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			continue
		case '\n':
			if !triple {
				s.done = eEOLS
				s.recordError("unterminated string literal")
				return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
			}
			s.lineno++
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
		return s.tokenSetup(oneCharOp(c), s.start, s.cur)
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
		return s.tokenSetup(oneCharOp(c), s.start, s.cur)
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
		return s.tokenSetup(classifyOp(c, c2, c3), s.start, s.cur)
	case '+', '%', '&', '|', '^', '@':
		c2 := 0
		if s.peek() == '=' {
			c2 = s.peek()
			s.nextC()
		}
		return s.tokenSetup(classifyOp(c, c2, 0), s.start, s.cur)
	case '-':
		c2 := 0
		if s.peek() == '=' || s.peek() == '>' {
			c2 = s.peek()
			s.nextC()
		}
		return s.tokenSetup(classifyOp(c, c2, 0), s.start, s.cur)
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
		return s.tokenSetup(classifyOp(c, c2, 0), s.start, s.cur)
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
				return s.tokenSetup(threeCharOp('.', '.', '.'), s.start, s.cur)
			}
			s.backup('.')
		} else if d := s.peek(); d >= '0' && d <= '9' {
			return s.scanNumber('.')
		}
		return s.tokenSetup(oneCharOp(c), s.start, s.cur)
	case ',', ';', '~':
		return s.tokenSetup(oneCharOp(c), s.start, s.cur)
	}
	s.done = eToken
	s.recordError("invalid character")
	return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
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
// dedents back to indent level 0.
//
// CPython: Parser/lexer/lexer.c:1500 (EOF branch in tok_get_normal_mode)
func (s *State) endmarker() Tok {
	if s.indent > 0 {
		s.indent--
		return s.tokenSetup(token.DEDENT, s.cur, s.cur)
	}
	s.done = eEOF
	return s.tokenSetup(token.ENDMARKER, s.cur, s.cur)
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
		// Trailing keyword (`1and`, `1or`, ...): accept the literal.
		// CPython emits a SyntaxWarning, gopy's tokenizer doesn't reach
		// the warnings module yet so we record nothing.
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
// a valid PEP 3131 identifier. CPython runs _PyUnicode_ScanIdentifier
// against the Unicode XID_Start / XID_Continue tables. gopy doesn't yet
// vendor those tables (objects.IsXIDStartRune is itself approximated
// via unicode.IsLetter, which can disagree on a handful of codepoints
// at Unicode-version boundaries). Until the unicodedata XID tables
// land, this routine only enforces UTF-8 validity. That's permissive:
// it accepts a few identifiers CPython would reject. The function is
// wired into scanName so the entry point is in place; tightening it
// is one swap away once the tables exist. Gap tracked under #612.
//
// CPython: Parser/lexer/lexer.c:364 verify_identifier
func (s *State) verifyIdentifier() bool {
	if s.tokExtraTokens {
		return true
	}
	bs := s.buf[s.start:s.cur]
	asciiOnly := true
	for _, b := range bs {
		if b >= 0x80 {
			asciiOnly = false
			break
		}
	}
	if asciiOnly {
		return true
	}
	if _, _, ok := ValidateUTF8(bs); !ok {
		s.done = eDecode
		s.recordError("invalid character in identifier")
		return false
	}
	return true
}

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
