// CPython: Parser/lexer/lexer.c f-string and t-string scanning. Two
// halves: the prefix branch in tok_get_normal_mode (around
// Parser/lexer/lexer.c:1051) recognises the f"/t"/rf"/rt"/fr"/tr" lead-in
// and pushes a tokenizer_mode entry, then the body scanner at
// Parser/lexer/lexer.c:1393 emits FSTRING_START / FSTRING_MIDDLE /
// FSTRING_END (or the TSTRING variants) and re-enters regular mode for
// the {expr} block.

package lexer

import "github.com/tamnd/gopy/tokenize"

// detectStringPrefix returns the (rawFlag, kind, ok) triple for a name
// already scanned at start..s.cur followed by a quote at s.cur. Empty
// prefix returns false.
//
// CPython: Parser/lexer/lexer.c:457 maybe_raise_syntax_error_for_string_prefixes
func (s *State) detectStringPrefix(start, end int) (raw bool, kind stringKind, isFTString bool) {
	if end-start > 2 {
		return false, kindFString, false
	}
	sawF, sawT, sawR := false, false, false
	for i := start; i < end; i++ {
		switch s.buf[i] {
		case 'f', 'F':
			sawF = true
		case 't', 'T':
			sawT = true
		case 'r', 'R':
			sawR = true
		case 'b', 'B', 'u', 'U':
			// Plain string prefix; not f/t.
		default:
			return false, kindFString, false
		}
	}
	if !sawF && !sawT {
		return false, kindFString, false
	}
	if sawT {
		return sawR, kindTString, true
	}
	return sawR, kindFString, true
}

// startFString is invoked by scanName when the identifier is a string
// prefix and the next byte is a quote. It pushes a tokenizer_mode and
// returns FSTRING_START or TSTRING_START.
//
// CPython: Parser/lexer/lexer.c:1051 f_string_quote
func (s *State) startFString(prefixStart, prefixEnd int, quote int) Tok {
	raw, kind, ok := s.detectStringPrefix(prefixStart, prefixEnd)
	if !ok {
		return s.syntaxError("invalid string prefix")
	}
	if s.tokModeStackIndex+1 >= maxFstringLevel {
		return s.syntaxError("too many nested f-strings or t-strings")
	}

	quoteSize := 1
	c1 := s.nextC()
	if c1 == quote {
		c2 := s.nextC()
		if c2 == quote {
			quoteSize = 3
		} else {
			s.backup(c2)
			s.backup(c1)
		}
	} else {
		s.backup(c1)
	}

	s.firstLine = s.lineno
	s.multiLineStart = s.lineStart

	m := s.pushMode()
	m.kind = tokFStringMode
	m.quote = byte(quote)
	m.quoteSize = quoteSize
	m.raw = raw
	m.start = prefixStart
	m.multiLineStart = s.lineStart
	m.firstLine = s.lineno
	m.startOffset = -1
	m.multiStartOffset = -1
	m.lastExprBuffer = nil
	m.lastExprSize = 0
	m.lastExprEnd = -1
	m.inFormatSpec = false
	m.inDebug = false
	m.stringKind = kind
	m.curlyBracketDepth = 0
	m.curlyBracketExprStartDepth = -1

	t := s.tokenSetup(tokenize.FSTRING_START, prefixStart, s.cur)
	if kind == kindTString {
		t.Kind = tokenize.TSTRING_START
	}
	return t
}

// tokGetFStringMode replaces the stub in lexer.go. Scans inside the
// f-string or t-string body, emitting FSTRING_MIDDLE / FSTRING_END (or
// the TSTRING variants) and re-entering regular mode at each {expr}.
//
// CPython: Parser/lexer/lexer.c:1393 tok_get_fstring_mode
func (s *State) tokGetFStringModeImpl() Tok {
	m := s.curMode()
	s.start = s.cur
	s.startCol = s.col
	s.firstLine = s.lineno

	startChar := s.nextC()
	if startChar == '{' {
		peek1 := s.nextC()
		s.backup(peek1)
		s.backup(startChar)
		if peek1 != '{' {
			m.curlyBracketExprStartDepth++
			if m.curlyBracketExprStartDepth >= maxExprNesting {
				return s.syntaxError("f-string: expressions nested too deeply")
			}
			m.kind = tokRegularMode
			return s.tokGetNormalMode()
		}
	} else {
		s.backup(startChar)
	}

	// Check for closing quote(s).
	for i := 0; i < m.quoteSize; i++ {
		q := s.nextC()
		if q != int(m.quote) {
			s.backup(q)
			return s.fstringMiddle(m)
		}
	}
	end := s.tokenSetup(s.fstringEndKind(m), s.start, s.cur)
	s.popMode()
	return end
}

// fstringEndKind returns FSTRING_END or TSTRING_END for the active
// string kind.
//
// CPython: Parser/lexer/lexer.c:42 FTSTRING_END
func (s *State) fstringEndKind(m *tokenizerMode) tokenize.Type {
	if m.stringKind == kindTString {
		return tokenize.TSTRING_END
	}
	return tokenize.FSTRING_END
}

// fstringMiddleKind is the FSTRING_MIDDLE / TSTRING_MIDDLE selector.
//
// CPython: Parser/lexer/lexer.c:41 FTSTRING_MIDDLE
func (s *State) fstringMiddleKind(m *tokenizerMode) tokenize.Type {
	if m.stringKind == kindTString {
		return tokenize.TSTRING_MIDDLE
	}
	return tokenize.FSTRING_MIDDLE
}

// fstringMiddle scans literal text up to the next { or } or closing
// quote, emitting an FSTRING_MIDDLE / TSTRING_MIDDLE token.
//
// CPython: Parser/lexer/lexer.c:1446 f_string_middle label
func (s *State) fstringMiddle(m *tokenizerMode) Tok {
	endQuoteSize := 0
	for endQuoteSize != m.quoteSize {
		c := s.nextC()
		if c == eof || (m.quoteSize == 1 && c == '\n') {
			return s.syntaxError("unterminated %c-string", s.fstringPrefixChar(m))
		}
		if c == int(m.quote) {
			endQuoteSize++
			continue
		}
		endQuoteSize = 0

		if c == '{' {
			peek := s.nextC()
			if peek != '{' {
				s.backup(peek)
				s.backup(c)
				m.curlyBracketExprStartDepth++
				if m.curlyBracketExprStartDepth >= maxExprNesting {
					return s.syntaxError("f-string: expressions nested too deeply")
				}
				m.kind = tokRegularMode
				m.inFormatSpec = false
				return s.tokenSetup(s.fstringMiddleKind(m), s.start, s.cur)
			}
			// {{ literal: emit through to one open brace as middle.
			return s.tokenSetup(s.fstringMiddleKind(m), s.start, s.cur-1)
		}
		if c == '}' {
			peek := s.nextC()
			if peek == '}' && m.curlyBracketDepth == 0 {
				return s.tokenSetup(s.fstringMiddleKind(m), s.start, s.cur-1)
			}
			s.backup(peek)
			s.backup(c)
			m.kind = tokRegularMode
			m.inFormatSpec = false
			return s.tokenSetup(s.fstringMiddleKind(m), s.start, s.cur)
		}
		if c == '\\' {
			peek := s.nextC()
			if peek == '\r' {
				peek = s.nextC()
			}
			if peek == '{' || peek == '}' {
				s.backup(peek)
				continue
			}
			// Skip the escaped character. Named escapes \N{...} fall
			// through to the regular middle scanning.
		}
	}
	// Hit the closing quotes during literal scan: back them up so the
	// next call emits FSTRING_END.
	for i := 0; i < m.quoteSize; i++ {
		s.cur--
		s.col--
	}
	return s.tokenSetup(s.fstringMiddleKind(m), s.start, s.cur)
}

// fstringPrefixChar returns 'f' or 't' for the active mode. Used in
// error messages.
//
// CPython: Parser/lexer/lexer.c:43 TOK_GET_STRING_PREFIX
func (s *State) fstringPrefixChar(m *tokenizerMode) byte {
	if m.stringKind == kindTString {
		return 't'
	}
	return 'f'
}
