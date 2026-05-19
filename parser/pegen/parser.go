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
	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/build"
	perrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/token"
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
	Type     token.Type
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
		tok:            tok,
		startRule:      start,
		flags:          flags,
		featureVersion: build.PythonMinorVersion,
	}
}

// fillToken pulls one more token from the lexer into the buffer and
// returns its index. Returns -1 on lexer EOF.
//
// Generic OP tokens get upgraded to the specific operator type their
// bytes name (PyToken_OneChar / TwoChars / ThreeChars in CPython).
// The grammar matches operator tokens by exact type (EQUAL, COMMA,
// PLUSEQUAL, ...), so a flat OP would never match.
//
// CPython: Parser/pegen.c:62 _PyPegen_fill_token
// CPython: Parser/lexer/lexer.c PyToken_OneChar / TwoChars / ThreeChars
func (p *Parser) fillToken() int {
	if p.errorIndicator {
		return -1
	}
	tk := p.tok.Get()
	kind := tk.Kind
	if kind == token.OP {
		if exact, ok := opTokenType[string(tk.Bytes)]; ok {
			kind = exact
		}
	}
	t := &Token{
		Type:     kind,
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

// opTokenType maps the source bytes of an operator (or punctuation)
// to the specific token.Type CPython's tokenizer would assign.
// The lexer flattens everything into OP; the grammar reads exact
// types, so the parser has to upgrade on intake.
//
// CPython: Parser/lexer/lexer.c PyToken_OneChar / TwoChars / ThreeChars
var opTokenType = map[string]token.Type{
	"(":   token.LPAR,
	")":   token.RPAR,
	"[":   token.LSQB,
	"]":   token.RSQB,
	"{":   token.LBRACE,
	"}":   token.RBRACE,
	",":   token.COMMA,
	":":   token.COLON,
	";":   token.SEMI,
	".":   token.DOT,
	"+":   token.PLUS,
	"-":   token.MINUS,
	"*":   token.STAR,
	"/":   token.SLASH,
	"|":   token.VBAR,
	"&":   token.AMPER,
	"<":   token.LESS,
	">":   token.GREATER,
	"=":   token.EQUAL,
	"%":   token.PERCENT,
	"~":   token.TILDE,
	"^":   token.CIRCUMFLEX,
	"@":   token.AT,
	"!":   token.EXCLAMATION,
	"==":  token.EQEQUAL,
	"!=":  token.NOTEQUAL,
	"<=":  token.LESSEQUAL,
	">=":  token.GREATEREQUAL,
	"<<":  token.LEFTSHIFT,
	">>":  token.RIGHTSHIFT,
	"**":  token.DOUBLESTAR,
	"+=":  token.PLUSEQUAL,
	"-=":  token.MINEQUAL,
	"*=":  token.STAREQUAL,
	"/=":  token.SLASHEQUAL,
	"%=":  token.PERCENTEQUAL,
	"&=":  token.AMPEREQUAL,
	"|=":  token.VBAREQUAL,
	"^=":  token.CIRCUMFLEXEQUAL,
	"<<=": token.LEFTSHIFTEQUAL,
	">>=": token.RIGHTSHIFTEQUAL,
	"**=": token.DOUBLESTAREQUAL,
	"//":  token.DOUBLESLASH,
	"//=": token.DOUBLESLASHEQUAL,
	"@=":  token.ATEQUAL,
	"->":  token.RARROW,
	"...": token.ELLIPSIS,
	":=":  token.COLONEQUAL,
}

// Mark returns the current parse position. Pair with Reset.
//
// CPython: Parser/pegen.c:43 _PyPegen_seek
func (p *Parser) Mark() int { return p.mark }

// Span returns the source location covering tokens [startMark, mark).
// The end mark is the parser's current position; the last consumed
// token is p.tokens[mark-1]. Returns ast.NoPos when startMark is
// out of range or the run is empty.
//
// The end-position search walks backward past ENDMARKER / NEWLINE /
// INDENT / DEDENT so the AST node ends at the last token that carries
// source content. Without this, compound statements (function_def,
// class_def, for/while/if/with/try) inherit the position of the
// trailing DEDENT — the line of the next top-level statement, with
// col_offset == -1 — which then leaks into co_linetable.
//
// CPython: Parser/pegen.c:488 _PyPegen_get_last_nonnwhitespace_token
// + Tools/peg_generator/pegen/c_generator.py EXTRA macro.
func (p *Parser) Span(startMark int) ast.Pos {
	if startMark < 0 || startMark >= len(p.tokens) {
		return ast.NoPos
	}
	end := p.mark - 1
	if end < startMark {
		end = startMark
	}
	if end >= len(p.tokens) {
		end = len(p.tokens) - 1
	}
	for end > startMark {
		t := p.tokens[end]
		if t.Type != token.ENDMARKER && (t.Type < token.NEWLINE || t.Type > token.DEDENT) {
			break
		}
		end--
	}
	st := p.tokens[startMark]
	et := p.tokens[end]
	return ast.Pos{
		Lineno:       st.Lineno,
		ColOffset:    st.ColOff,
		EndLineno:    et.EndLine,
		EndColOffset: et.EndCol,
	}
}

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
// A NAME token whose bytes are a hard keyword is refused: CPython's
// lexer assigns those a per-keyword token type so the generic NAME
// alts never see them. gopy's lexer emits NAME for every identifier,
// so the discrimination has to happen at the parser layer instead.
// ExpectName / ExpectSoftKeyword bypass this filter; they match by
// bytes and need to accept keyword tokens.
//
// CPython: Parser/pegen.c:296 _PyPegen_expect_token
func (p *Parser) Expect(kind token.Type) *Token {
	t := p.Peek()
	if t == nil || t.Type != kind {
		return nil
	}
	if kind == token.NAME && hardKeywordSet[string(t.Bytes)] {
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
	if t == nil || t.Type != token.NAME || string(t.Bytes) != s {
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
