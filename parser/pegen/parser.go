// Package pegen ports the runtime half of cpython 3.14 Parser/pegen.c
// and pegen.h. The package owns the Parser struct, the token buffer
// and memo table, the mark/restore backtracking primitives, the
// keyword tables, and the error tracking surface that the generated
// parser table calls into.
//
// The generated parser table itself is not in this file. CPython
// regenerates Parser/parser.c from Grammar/python.gram via
// Tools/peg_generator/. The Go-targeted equivalent lives in
// tools/parser_gen/ and emits parser_gen.go in this package.
//
// CPython: Parser/pegen.h, Parser/pegen.c
package pegen

import (
	perrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/tokenize"
)

// Flag bits that mirror PyPARSE_*. The parser respects a subset; the
// rest are accepted for ABI parity.
//
// CPython: Parser/pegen.h:14 PyPARSE_* flags
const (
	FlagDontImplyDedent      = 0x0002
	FlagIgnoreCookie         = 0x0010
	FlagBarryAsBDFL          = 0x0020
	FlagTypeComments         = 0x0040
	FlagAllowIncompleteInput = 0x0100
)

// StartRule selects the entry point of the generated parser table.
// Mirrors the Py_*_input constants.
//
// CPython: Include/compile.h Py_file_input / Py_eval_input / Py_single_input
type StartRule int

// Entry-point rule numbers. The values match CPython's grammar
// non-terminal ids so generated parser tables can index directly.
const (
	StartFile         StartRule = 257
	StartSingle       StartRule = 256
	StartEval         StartRule = 258
	StartFunctionType StartRule = 345
	StartFString      StartRule = 800
)

// Token is the parser's view of a lexer token. It pins the metadata
// the action_helpers layer needs for error messages and AST building.
//
// CPython: Parser/pegen.h:39 Token
type Token struct {
	Type     tokenize.Type
	Bytes    []byte
	Level    int
	Lineno   int
	ColOff   int
	EndLine  int
	EndCol   int
	Metadata []byte
	memo     *memo
}

// memo is one cached parse result for (rule, token-index).
//
// CPython: Parser/pegen.h:32 Memo
type memo struct {
	rule int
	node any
	mark int
	next *memo
}

// KeywordToken pairs a keyword string with the NAME-derived token kind
// the generated parser table compares against.
//
// CPython: Parser/pegen.h:48 KeywordToken
type KeywordToken struct {
	Str  string
	Type int
}

// Parser is the runtime state. The generated table threads through
// this struct via mark/restore and the consume helpers.
//
// CPython: Parser/pegen.h:70 Parser
type Parser struct {
	tok            *lexer.State
	tokens         []*Token
	mark           int
	fill           int
	keywords       [][]KeywordToken
	softKeywords   []string
	startRule      StartRule
	errCode        *int
	parsingStarted bool
	startingLine   int
	startingCol    int
	errorIndicator bool
	flags          int
	featureVersion int
	level          int
	callInvalid    bool
	debug          bool
	knownErrToken  *Token
	lastStmt       Location
	farthestPos    int
	pinnedErr      *perrors.SyntaxError
}

// Location pins a (start, end) source span. The generated parser uses
// this to attach source positions to AST nodes.
//
// CPython: Parser/pegen.h:63 location
type Location struct {
	Lineno  int
	ColOff  int
	EndLine int
	EndCol  int
}

// New builds a Parser around an already-configured lexer State. The
// token buffer is filled lazily as the generated parser advances.
//
// CPython: Parser/pegen.c:1024 _PyPegen_Parser_New
func New(tok *lexer.State, start StartRule, flags int) *Parser {
	return &Parser{
		tok:       tok,
		startRule: start,
		flags:     flags,
	}
}

// fillToken pulls one more token from the lexer into the buffer and
// returns its index. Returns -1 on lexer EOF.
//
// CPython: Parser/pegen.c:62 _PyPegen_fill_token
func (p *Parser) fillToken() int {
	if p.errorIndicator {
		return -1
	}
	tk := p.tok.Get()
	t := &Token{
		Type:     tk.Kind,
		Bytes:    tk.Bytes,
		Level:    tk.Level,
		Lineno:   tk.Start.Line,
		ColOff:   tk.Start.Col,
		EndLine:  tk.End.Line,
		EndCol:   tk.End.Col,
		Metadata: tk.Metadata,
	}
	p.tokens = append(p.tokens, t)
	idx := p.fill
	p.fill++
	return idx
}

// Mark returns the current parse position. Pair with Reset.
//
// CPython: Parser/pegen.c:43 _PyPegen_seek
func (p *Parser) Mark() int { return p.mark }

// Reset rewinds the parse to a previously-taken Mark.
func (p *Parser) Reset(m int) { p.mark = m }

// Peek returns the token at the current mark, filling the buffer if
// needed. Returns nil at EOF.
//
// CPython: Parser/pegen.c:80 _PyPegen_get_token
func (p *Parser) Peek() *Token {
	for p.mark >= p.fill {
		if p.fillToken() < 0 {
			return nil
		}
	}
	if p.mark >= len(p.tokens) {
		return nil
	}
	return p.tokens[p.mark]
}

// Expect consumes the token at the mark if it matches kind, returning
// the token; otherwise returns nil and leaves mark unchanged.
//
// CPython: Parser/pegen.c:296 _PyPegen_expect_token
func (p *Parser) Expect(kind tokenize.Type) *Token {
	t := p.Peek()
	if t == nil || t.Type != kind {
		return nil
	}
	p.mark++
	return t
}

// ExpectName consumes a NAME token whose bytes match s.
//
// CPython: Parser/pegen.c:268 _PyPegen_expect_keyword
func (p *Parser) ExpectName(s string) *Token {
	t := p.Peek()
	if t == nil || t.Type != tokenize.NAME || string(t.Bytes) != s {
		return nil
	}
	p.mark++
	return t
}

// ErrorIndicator reports whether the parser has hit a fatal error.
func (p *Parser) ErrorIndicator() bool { return p.errorIndicator }

// SetErrorIndicator pins the error flag. The action_helpers layer
// flips this when it constructs a SyntaxError.
func (p *Parser) SetErrorIndicator(v bool) { p.errorIndicator = v }
