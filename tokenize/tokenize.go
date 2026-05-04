// Package tokenize is the Go port of cpython/Python/Python-tokenize.c.
// The C file is the Python-visible wrapper around the parser's lexer;
// it exposes the TokenizerIter class that `tokenize.tokenize()` in
// the stdlib delegates to.
//
// This file is a skeleton. The lexer state machine (indent tracking,
// f-string handling, encoding detection) lives in
// cpython/Parser/tokenizer/* and lands with the parser port. The
// public surface here is the stable Go-idiomatic shape every consumer
// can program against today; the implementation under Iter fills in
// once the lexer skeleton catches up.
//
// CPython: Python/Python-tokenize.c
package tokenize

import "io"

// Pos is the (line, column) source position of a token boundary. Both
// fields are 1-based, matching CPython's tokenize.TokenInfo.
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
	Type  Type
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
	// state holds the parser-side lexer once the parser port lands.
	// Until then Next returns io.EOF immediately.
	state any
}

// New constructs an Iter over a source string. extraTokens enables
// the COMMENT / NL / ENCODING / NEWLINE-at-EOF tokens that the
// stdlib filters out by default.
//
// CPython: Python/Python-tokenize.c tokenizeriter_new (source path)
func New(_ string, _ bool) *Iter {
	return &Iter{}
}

// NewReadline constructs an Iter that pulls source lines from a
// readline-shaped callable, the same shape io.TextIO.readline has on
// the Python side.
//
// CPython: Python/Python-tokenize.c tokenizeriter_new (readline path)
func NewReadline(_ func() (string, error), _ bool) *Iter {
	return &Iter{}
}

// Next returns the next token. The skeleton always reports io.EOF;
// the parser port fills in real tokens.
//
// CPython: Python/Python-tokenize.c tokenizeriter_next
func (it *Iter) Next() (Token, error) {
	return Token{}, io.EOF
}
