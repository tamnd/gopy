// Package tokenize is the Go port of cpython/Python/Python-tokenize.c.
// The C file is the Python-visible wrapper around the parser's lexer;
// it exposes the TokenizerIter class that `tokenize.tokenize()` in
// the stdlib delegates to.
//
// The token kind constants live in the sibling token package, mirroring
// CPython's split between Include/internal/pycore_token.h (consumed by
// the C tokenizer) and Lib/token.py (re-exported by Lib/tokenize.py).
//
// CPython: Python/Python-tokenize.c
package tokenize

import (
	"io"

	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/token"
)

// Pos is the (line, column) source position of a token boundary. Line
// is 1-based and Col is 0-based, matching CPython's tokenize.TokenInfo.
//
// CPython: Python/Python-tokenize.c tokenizeriter_next
type Pos struct {
	Line int
	Col  int
}

// Token is one record emitted by the iterator. Mirrors the 5-tuple
// (type, string, start, end, line) the C wrapper returns.
//
// CPython: Python/Python-tokenize.c tokenizeriter_next
type Token struct {
	Type  token.Type
	Value string
	Start Pos
	End   Pos
	Line  string
}

// Iter is the Go-side TokenizerIter equivalent. Next advances the
// underlying lexer state by one token; EOF is reported as io.EOF.
//
// CPython: Python/Python-tokenize.c tokenizeriterobject
type Iter struct {
	state *lexer.State
	done  bool
}

// New constructs an Iter over a source string. extraTokens enables
// the COMMENT / NL / ENCODING / NEWLINE-at-EOF tokens that the
// stdlib filters out by default.
//
// CPython: Python/Python-tokenize.c tokenizeriter_new (source path)
func New(src string, extraTokens bool) *Iter {
	st := lexer.FromString(src, lexer.ModeFile)
	st.SetExtraTokens(extraTokens)
	return &Iter{state: st}
}

// NewReadline constructs an Iter that pulls source lines from a
// readline-shaped callable, the same shape io.TextIO.readline has on
// the Python side. The callback returns one line of source (including
// any trailing newline) or io.EOF at end of stream.
//
// CPython: Python/Python-tokenize.c tokenizeriter_new (readline path)
func NewReadline(rl func() (string, error), extraTokens bool) *Iter {
	adapter := func() ([]byte, error) {
		s, err := rl()
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	st := lexer.FromReadline(adapter, lexer.ModeFile)
	st.SetExtraTokens(extraTokens)
	return &Iter{state: st}
}

// Next returns the next token. Returns io.EOF after the lexer's
// ENDMARKER has been delivered, matching the Python iterator
// protocol's StopIteration translation.
//
// CPython: Python/Python-tokenize.c tokenizeriter_next
func (it *Iter) Next() (Token, error) {
	if it.done {
		return Token{}, io.EOF
	}
	tok := it.state.Get()
	if tok.Kind == token.ERRORTOKEN {
		if e := it.state.Err(); e != nil {
			return Token{}, e
		}
	}
	out := Token{
		Type:  tok.Kind,
		Value: string(tok.Bytes),
		Start: Pos{Line: tok.Start.Line, Col: tok.Start.Col},
		End:   Pos{Line: tok.End.Line, Col: tok.End.Col},
	}
	if tok.Kind == token.ENDMARKER {
		it.done = true
	}
	return out, nil
}
