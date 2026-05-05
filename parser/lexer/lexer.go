// CPython: Parser/lexer/lexer.c.
//
// This file ports the regular-mode FSM. The f-string mode FSM lives in
// fstring.go. The normal-mode entry point is tokGetNormalMode at
// Parser/lexer/lexer.c:501; the helpers nextC / backup / lineCont track
// position and refill, and the ASCII char-class predicates mirror the
// is_potential_identifier_* macros at the top of lexer.c.

package lexer

import "github.com/tamnd/gopy/tokenize"

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
// CPython: Parser/lexer/lexer.c:501 tok_get_normal_mode
func (s *State) tokGetNormalMode() Tok {
	s.start = s.cur
	s.startCol = s.col

	if s.atbol {
		s.atbol = false
		if t, emit := s.indentNL(); emit {
			return t
		}
	}

	if s.pendin != 0 {
		if s.pendin < 0 {
			s.pendin++
			return s.tokenSetup(tokenize.DEDENT, s.cur, s.cur)
		}
		s.pendin--
		return s.tokenSetup(tokenize.INDENT, s.cur, s.cur)
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
			end := s.cur
			if c == '\n' {
				end = s.cur - 1
			}
			tok := s.tokenSetup(tokenize.COMMENT, commentStart, end)
			if c == '\n' {
				s.commentNewline = true
			}
			if c == eof {
				s.done = eEOF
			}
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
		if s.level > 0 {
			return s.tokenSetup(tokenize.NL, start, end)
		}
		return s.tokenSetup(tokenize.NEWLINE, start, end)
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
	c := s.peek()
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
			return s.tokenSetup(tokenize.ERRORTOKEN, s.cur, s.cur), true
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
		return s.tokenSetup(tokenize.ERRORTOKEN, s.cur, s.cur), true
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
	return s.tokenSetup(tokenize.NAME, s.start, s.cur)
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
			return s.tokenSetup(tokenize.NUMBER, s.start, s.cur)
		}
		if c == 'o' || c == 'O' {
			for isOctDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
			return s.tokenSetup(tokenize.NUMBER, s.start, s.cur)
		}
		if c == 'b' || c == 'B' {
			for isBinDigitOrUnderscore(s.peek()) {
				s.nextC()
			}
			return s.tokenSetup(tokenize.NUMBER, s.start, s.cur)
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
	s.backup(c)
	return s.tokenSetup(tokenize.NUMBER, s.start, s.cur)
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
			return s.tokenSetup(tokenize.STRING, s.start, s.cur)
		}
	}
	for {
		c := s.nextC()
		switch c {
		case eof:
			s.done = eEOFS
			s.recordError("unterminated string literal")
			return s.tokenSetup(tokenize.ERRORTOKEN, s.start, s.cur)
		case '\\':
			if s.nextC() == eof {
				s.done = eEOFS
				s.recordError("unterminated string literal")
				return s.tokenSetup(tokenize.ERRORTOKEN, s.start, s.cur)
			}
			continue
		case '\n':
			if !triple {
				s.done = eEOLS
				s.recordError("unterminated string literal")
				return s.tokenSetup(tokenize.ERRORTOKEN, s.start, s.cur)
			}
			s.lineno++
			s.col = 0
		}
		if c == quote {
			if !triple {
				return s.tokenSetup(tokenize.STRING, s.start, s.cur)
			}
			if s.peek() == quote {
				s.nextC()
				if s.peek() == quote {
					s.nextC()
					return s.tokenSetup(tokenize.STRING, s.start, s.cur)
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
		if s.insideFString() {
			m := s.curMode()
			if c == '{' {
				m.curlyBracketDepth++
			}
		}
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
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
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	case '*', '/', '<', '>', '=', '!':
		if s.peek() == '=' {
			s.nextC()
		} else if (c == '*' || c == '/' || c == '<' || c == '>') && s.peek() == c {
			s.nextC()
			if s.peek() == '=' {
				s.nextC()
			}
		}
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	case '+', '%', '&', '|', '^', '@':
		if s.peek() == '=' {
			s.nextC()
		}
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	case '-':
		if s.peek() == '=' || s.peek() == '>' {
			s.nextC()
		}
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	case ':':
		if s.peek() == '=' {
			s.nextC()
		}
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	case '.':
		if s.peek() == '.' {
			s.nextC()
			if s.peek() == '.' {
				s.nextC()
			}
		} else if d := s.peek(); d >= '0' && d <= '9' {
			return s.scanNumber('.')
		}
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	case ',', ';', '~':
		return s.tokenSetup(tokenize.OP, s.start, s.cur)
	}
	s.done = eToken
	s.recordError("invalid character")
	return s.tokenSetup(tokenize.ERRORTOKEN, s.start, s.cur)
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
		s.recordError("unmatched closing bracket")
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
		s.recordError("closing bracket does not match opening")
	}
}

// endmarker emits the terminal ENDMARKER, also flushing any pending
// dedents back to indent level 0.
//
// CPython: Parser/lexer/lexer.c:1500 (EOF branch in tok_get_normal_mode)
func (s *State) endmarker() Tok {
	if s.indent > 0 {
		s.indent--
		return s.tokenSetup(tokenize.DEDENT, s.cur, s.cur)
	}
	s.done = eEOF
	return s.tokenSetup(tokenize.ENDMARKER, s.cur, s.cur)
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
	return s.typeCommentTokenSetup(tokenize.TYPE_COMMENT, s.startCol, s.col, body, end), true
}

// tokGetFStringMode scans inside an f-string or t-string body. Stub:
// the FSM re-entry for interpolation expressions is the bulk of
// Parser/lexer/lexer.c:1393 and lands in fstring.go.
//
// CPython: Parser/lexer/lexer.c:1393 tok_get_fstring_mode
func (s *State) tokGetFStringMode() Tok {
	return s.tokGetFStringModeImpl()
}
