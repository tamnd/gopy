// PLexer is a positional wrapper over the Tokenize output: it owns the
// token slice and a cursor, and exposes the small parser-helper API the
// cases_generator parser uses (peek / expect / require / consume_to).
// COMMENT tokens are skipped by default; pass raw=true to see them.
//
// CPython: Tools/cases_generator/plexer.py PLexer
package main

import (
	"fmt"
	"strings"
)

// PLexer holds the lexed tokens for src plus a cursor.
//
// CPython: Tools/cases_generator/plexer.py:6-11 PLexer.__init__
type PLexer struct {
	Src      string
	Filename string
	Tokens   []Token
	Pos      int
}

// NewPLexer tokenizes src and returns a fresh PLexer at position 0.
//
// CPython: Tools/cases_generator/plexer.py:6-11 PLexer.__init__
func NewPLexer(src, filename string) (*PLexer, error) {
	toks, err := Tokenize(src, filename, 1)
	if err != nil {
		return nil, err
	}
	return &PLexer{Src: src, Filename: filename, Tokens: toks}, nil
}

// GetPos returns the current cursor.
//
// CPython: Tools/cases_generator/plexer.py:13-15 getpos
func (p *PLexer) GetPos() int { return p.Pos }

// EOF reports whether the cursor is past the last token.
//
// CPython: Tools/cases_generator/plexer.py:17-19 eof
func (p *PLexer) EOF() bool { return p.Pos >= len(p.Tokens) }

// SetPos rewinds or fast-forwards the cursor; pos must be in [0, len].
//
// CPython: Tools/cases_generator/plexer.py:21-24 setpos
func (p *PLexer) SetPos(pos int) {
	if pos < 0 || pos > len(p.Tokens) {
		panic(fmt.Sprintf("PLexer.SetPos: %d out of [0,%d]", pos, len(p.Tokens)))
	}
	p.Pos = pos
}

// Backup steps the cursor back by one. Panics at position 0.
//
// CPython: Tools/cases_generator/plexer.py:26-29 backup
func (p *PLexer) Backup() {
	if p.Pos == 0 {
		panic("PLexer.Backup: already at position 0")
	}
	p.Pos--
}

// Next returns the next token and advances. With raw=false COMMENT
// tokens are skipped. Returns (Token{}, false) at EOF.
//
// CPython: Tools/cases_generator/plexer.py:31-39 next
func (p *PLexer) Next(raw bool) (Token, bool) {
	for p.Pos < len(p.Tokens) {
		tok := p.Tokens[p.Pos]
		p.Pos++
		if raw || tok.Kind != TokComment {
			return tok, true
		}
	}
	return Token{}, false
}

// Peek returns the next token without advancing.
//
// CPython: Tools/cases_generator/plexer.py:41-45 peek
func (p *PLexer) Peek(raw bool) (Token, bool) {
	tok, ok := p.Next(raw)
	if ok {
		p.Backup()
	}
	return tok, ok
}

// Maybe peeks and returns the token if its kind matches; otherwise
// returns (Token{}, false) without advancing.
//
// CPython: Tools/cases_generator/plexer.py:47-52 maybe
func (p *PLexer) Maybe(kind string, raw bool) (Token, bool) {
	tok, ok := p.Peek(raw)
	if ok && tok.Kind == kind {
		return tok, true
	}
	return Token{}, false
}

// Expect advances and returns the next token if its kind matches; on
// mismatch the cursor is rewound and the call returns false.
//
// CPython: Tools/cases_generator/plexer.py:54-61 expect
func (p *PLexer) Expect(kind string) (Token, bool) {
	tok, ok := p.Next(false)
	if !ok {
		return Token{}, false
	}
	if tok.Kind == kind {
		return tok, true
	}
	p.Backup()
	return Token{}, false
}

// Require returns the next token; mismatched kind or EOF produces a
// SyntaxError.
//
// CPython: Tools/cases_generator/plexer.py:63-70 require
func (p *PLexer) Require(kind string) (Token, error) {
	tok, ok := p.Next(false)
	if ok && tok.Kind == kind {
		return tok, nil
	}
	var got string
	if ok {
		got = fmt.Sprintf("%q", tok.Text)
	} else {
		got = "None"
	}
	return Token{}, p.MakeSyntaxError(
		fmt.Sprintf("Expected %q but got %s", kind, got), tok, ok)
}

// ConsumeTo gathers raw tokens (comments included) up through the next
// occurrence of end at paren-depth zero.
//
// CPython: Tools/cases_generator/plexer.py:72-85 consume_to
func (p *PLexer) ConsumeTo(end string) ([]Token, error) {
	var res []Token
	parens := 0
	var last Token
	have := false
	for {
		tok, ok := p.Next(true)
		if !ok {
			return nil, p.MakeSyntaxError(
				fmt.Sprintf("Expected %q but reached EOF", end), last, have)
		}
		res = append(res, tok)
		last = tok
		have = true
		if tok.Kind == end && parens == 0 {
			return res, nil
		}
		if tok.Kind == TokLParen {
			parens++
		}
		if tok.Kind == TokRParen {
			parens--
		}
	}
}

// ExtractLine returns the 1-based source line lineno (or "" past EOF).
//
// CPython: Tools/cases_generator/plexer.py:87-92 extract_line
func (p *PLexer) ExtractLine(lineno int) string {
	lines := strings.Split(p.Src, "\n")
	if lineno < 1 || lineno > len(lines) {
		return ""
	}
	line := lines[lineno-1]
	return strings.TrimRight(line, "\r")
}

// MakeSyntaxError builds a SyntaxError positioned at tkn (or, when
// have=false, at the current peek / last token).
//
// CPython: Tools/cases_generator/plexer.py:94-101 make_syntax_error
func (p *PLexer) MakeSyntaxError(message string, tkn Token, have bool) *SyntaxError {
	if !have {
		if t, ok := p.Peek(false); ok {
			tkn, have = t, true
		} else if len(p.Tokens) > 0 {
			tkn, have = p.Tokens[len(p.Tokens)-1], true
		}
	}
	se := &SyntaxError{Message: message, Filename: p.Filename}
	if have {
		se.Line = tkn.Begin.Line
		se.Column = tkn.Begin.Col
		se.LineText = p.ExtractLine(tkn.Begin.Line)
	}
	return se
}
