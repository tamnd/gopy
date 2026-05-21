// CPython: Parser/lexer/lexer.c f-string and t-string scanning. Two
// halves: the prefix branch in tok_get_normal_mode (around
// Parser/lexer/lexer.c:1051) recognizes the f"/t"/rf"/rt"/fr"/tr" lead-in
// and pushes a tokenizer_mode entry, then the body scanner at
// Parser/lexer/lexer.c:1393 emits FSTRING_START / FSTRING_MIDDLE /
// FSTRING_END (or the TSTRING variants) and re-enters regular mode for
// the {expr} block.

package lexer

import "github.com/tamnd/gopy/token"

// detectStringPrefix returns the (rawFlag, kind, isFTString, ok)
// triple for a name already scanned at start..s.cur followed by a
// quote at s.cur. `ok` is false when the prefix combo is incompatible
// (s.err is populated and s.done is set to eSyntax) or when the
// identifier is not a string prefix at all.
//
// CPython: Parser/lexer/lexer.c:457 maybe_raise_syntax_error_for_string_prefixes
func (s *State) detectStringPrefix(start, end int) (raw bool, kind stringKind, isFTString, ok bool) {
	if end-start > 2 {
		return false, kindFString, false, true
	}
	sawF, sawT, sawR, sawB, sawU := false, false, false, false, false
	for i := start; i < end; i++ {
		switch s.buf[i] {
		case 'f', 'F':
			sawF = true
		case 't', 'T':
			sawT = true
		case 'r', 'R':
			sawR = true
		case 'b', 'B':
			sawB = true
		case 'u', 'U':
			sawU = true
		default:
			return false, kindFString, false, true
		}
	}
	// CPython: Parser/lexer/lexer.c:455 maybe_raise_syntax_error_for_string_prefixes
	if s.maybeRaiseSyntaxErrorForStringPrefixes(sawB, sawR, sawU, sawF, sawT) {
		return false, kindFString, false, false
	}
	if !sawF && !sawT {
		return false, kindFString, false, true
	}
	if sawT {
		return sawR, kindTString, true, true
	}
	return sawR, kindFString, true, true
}

// startFString is invoked by scanName when the identifier is a string
// prefix and the next byte is a quote. It pushes a tokenizer_mode and
// returns FSTRING_START or TSTRING_START.
//
// CPython: Parser/lexer/lexer.c:1051 f_string_quote
func (s *State) startFString(prefixStart, prefixEnd int, quote int) Tok {
	raw, kind, isFT, ok := s.detectStringPrefix(prefixStart, prefixEnd)
	if !ok {
		return s.tokenSetup(token.ERRORTOKEN, s.start, s.cur)
	}
	if !isFT {
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

	t := s.tokenSetup(token.FSTRING_START, prefixStart, s.cur)
	if kind == kindTString {
		t.Kind = token.TSTRING_START
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
func (s *State) fstringEndKind(m *tokenizerMode) token.Type {
	if m.stringKind == kindTString {
		return token.TSTRING_END
	}
	return token.FSTRING_END
}

// fstringMiddleKind is the FSTRING_MIDDLE / TSTRING_MIDDLE selector.
//
// CPython: Parser/lexer/lexer.c:41 FTSTRING_MIDDLE
func (s *State) fstringMiddleKind(m *tokenizerMode) token.Type {
	if m.stringKind == kindTString {
		return token.TSTRING_MIDDLE
	}
	return token.FSTRING_MIDDLE
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
			// CPython snapshots the expression source at the opening
			// `{` so set_ftstring_expr can later attach it as token
			// metadata for debug f-strings and t-strings.
			//
			// CPython: Parser/lexer/lexer.c:1525
			s.updateFtstringExpr('{')
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

// updateFtstringExpr records expression bytes for the active f-string
// (or t-string) so set_ftstring_expr can later attach them as token
// metadata. CPython uses C string operations on a null-terminated
// buffer; gopy uses offsets into s.buf, so strlen(tok->cur) becomes
// inp-cur and strlen(tok->start) becomes inp-start.
//
// CPython returns int (0/1) so PyMem allocation failures can bubble
// up. Go's append never fails, so the gopy port is void.
//
// CPython: Parser/lexer/lexer.c:227 _PyLexer_update_ftstring_expr
func (s *State) updateFtstringExpr(cur byte) {
	size := s.inp - s.cur
	m := s.curMode()

	switch cur {
	case 0:
		if m.lastExprBuffer == nil || m.lastExprEnd >= 0 {
			return
		}
		m.lastExprBuffer = append(m.lastExprBuffer, s.buf[s.cur:s.cur+size]...)
		m.lastExprSize += size
	case '{':
		m.lastExprBuffer = append(m.lastExprBuffer[:0], s.buf[s.cur:s.cur+size]...)
		m.lastExprSize = size
		m.lastExprEnd = -1
	case '}', '!':
		m.lastExprEnd = s.inp - s.start
	case ':':
		if m.lastExprEnd == -1 {
			m.lastExprEnd = s.inp - s.start
		}
	default:
		panic("updateFtstringExpr: unexpected character")
	}
}

// setFtstringExpr writes the buffered expression text into token's
// Metadata field. Mirrors CPython's set_ftstring_expr: skips when
// already populated, otherwise extracts last_expr_buffer truncated
// by last_expr_end, stripping comments for t-strings or debug-mode
// f-strings.
//
// CPython returns int (0=ok, 1=error) so PyUnicode_FromStringAndSize
// failures bubble up. gopy carries raw bytes through Metadata and the
// allocation cannot fail, so the gopy port is void.
//
// CPython: Parser/lexer/lexer.c:114 set_ftstring_expr
func (s *State) setFtstringExpr(tok *Tok, c byte) {
	if c != '}' && c != ':' && c != '!' {
		return
	}
	m := s.curMode()
	if (!m.inDebug && m.stringKind != kindTString) || tok.Metadata != nil {
		return
	}

	usable := m.lastExprSize - m.lastExprEnd
	if usable <= 0 || m.lastExprBuffer == nil {
		return
	}

	src := m.lastExprBuffer[:usable]

	hashDetected := false
	inString := false
	var quoteChar byte
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if ch == '\\' {
			i++
			continue
		}
		if ch == '"' || ch == '\'' {
			if !inString {
				inString = true
				quoteChar = ch
			} else if ch == quoteChar {
				inString = false
			}
			continue
		}
		if ch == '#' && !inString {
			hashDetected = true
			break
		}
	}

	if !hashDetected {
		if _, _, ok := ValidateUTF8(src); !ok {
			s.done = eDecode
			s.syntaxError("invalid character in f-string expression")
			return
		}
		tok.Metadata = append([]byte(nil), src...)
		return
	}

	out := make([]byte, 0, len(src))
	inString = false
	quoteChar = 0
	for i := 0; i < len(src); {
		ch := src[i]
		if ch == '\\' {
			if i+1 < len(src) {
				out = append(out, ch, src[i+1])
				i += 2
				continue
			}
			out = append(out, ch)
			i++
			continue
		}
		if ch == '"' || ch == '\'' {
			if !inString {
				inString = true
				quoteChar = ch
			} else if ch == quoteChar {
				inString = false
			}
			out = append(out, ch)
			i++
			continue
		}
		if ch == '#' && !inString {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
			continue
		}
		out = append(out, ch)
		i++
	}
	if _, _, ok := ValidateUTF8(out); !ok {
		s.done = eDecode
		s.syntaxError("invalid character in f-string expression")
		return
	}
	tok.Metadata = out
}
