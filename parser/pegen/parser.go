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
	"fmt"

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
	// pinnedFromTokenizer is set when pinnedErr came from an
	// ERRORTOKEN dispatch (the lexer raised a structured error).
	// Parser-side recordError must not overwrite it, mirroring
	// CPython's _Pypegen_set_syntax_error which checks PyErr_Occurred
	// before refining the message in the second pass.
	//
	// CPython: Parser/pegen_errors.c:416 _Pypegen_set_syntax_error
	pinnedFromTokenizer bool
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
	if p.tok == nil {
		return -1
	}
	tk := p.tok.Get()
	kind := tk.Kind
	if kind == token.OP {
		if exact, ok := opTokenType[string(tk.Bytes)]; ok {
			kind = exact
		}
	}
	// Single-input mode: when the lexer hands us the first ENDMARKER
	// and the parser has already consumed at least one real token,
	// rewrite it to NEWLINE so `statement_newline: compound_stmt
	// NEWLINE` matches even though the source ended on a DEDENT (or
	// on a non-newline char like "x"). Mirrors the C fill_token arm
	// that also primes the lexer with a -indent pendin so the
	// follow-up calls flush DEDENTs before the real ENDMARKER.
	//
	// CPython: Parser/pegen.c:268 _PyPegen_fill_token single-input
	if p.startRule == StartSingle && kind == token.ENDMARKER && p.parsingStarted {
		kind = token.NEWLINE
		p.parsingStarted = false
		p.tok.ForceDedentsAtEOF()
	} else if kind != token.ENDMARKER {
		p.parsingStarted = true
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
	// CPython: Parser/pegen.c:218 _PyPegen_fill_token surfaces the
	// tokenizer's error code as a typed SyntaxError subclass when the
	// new token is ERRORTOKEN. gopy used to absorb the error and let
	// Dispatch return ErrParserNotImplemented, so deep-indent /
	// tab-space / unclosed-paren cases came back as plain SyntaxError
	// instead of IndentationError / TabError / pinned-location
	// SyntaxError.
	if kind == token.ERRORTOKEN {
		p.errorIndicator = true
		// Only pin when the lexer stored a specific error. A bare
		// ERRORTOKEN with no stored message (e.g. an unrecognised
		// character like `$` inside an f-string expression) must leave
		// pinnedErr nil so the grammar's error-recovery rules can fire
		// and produce the context-appropriate message ("f-string:
		// expecting '=', or '!', or ':', or '}'"). Callers that reach
		// the top of the grammar without a pinned error fall through to
		// SetSyntaxError which emits "invalid syntax".
		//
		// CPython: Parser/pegen.c:218 _PyPegen_fill_token
		if p.pinnedErr == nil && p.tok.Err() != nil {
			p.pinnedErr = tokenizerSyntaxError(p.tok, t)
			p.pinnedFromTokenizer = true
		} else if p.pinnedErr == nil && p.tok.Done() == lexer.DoneEOF && p.tok.Level() > 0 {
			// EOF with an unclosed paren: pin "'{' was never closed" at
			// fill time. CPython's _Pypegen_tokenizer_error handles the
			// E_EOF+level case by calling raise_unclosed_parentheses_error
			// immediately, before any grammar rule fires. This blocks the
			// second pass from overriding the message with a generic
			// "f-string: expecting ..." from invalid_* rules.
			//
			// CPython: Parser/pegen_errors.c:81 case E_EOF: tok->level → raise_unclosed_parentheses_error
			p.raiseUnclosedParenthesesError()
		}
		return -1
	}
	return idx
}

// tokenizerSyntaxError maps the lexer's Done() state to the matching
// SyntaxError subclass. The token carries the offending line/col so the
// builder can stamp filename / lineno / offset / text correctly.
//
// CPython: Parser/pegen_errors.c:69 _Pypegen_tokenizer_error
func tokenizerSyntaxError(st *lexer.State, tk *Token) *perrors.SyntaxError {
	stored := st.Err()
	pos := perrors.Pos{Lineno: tk.Lineno, ColOff: tk.ColOff, EndLine: tk.EndLine, EndCol: tk.EndCol}
	storedMsg := ""
	if stored != nil {
		storedMsg = stored.Message
		if stored.Pos.Line > 0 {
			pos.Lineno = stored.Pos.Line
			pos.ColOff = stored.Pos.Col
			// CPython's recordStringError pins both start and end at
			// the opening quote (Parser/lexer/lexer.c:1175); take the
			// stored EndPos when set, otherwise drop end_col to -1 so
			// the traceback renders a single caret instead of striping
			// a range across the rest of the source line.
			//
			// CPython: Parser/pegen_errors.c:123 RAISE_ERROR_KNOWN_LOCATION
			pos.EndLine = stored.EndPos.Line
			if stored.EndPos.Line == 0 {
				pos.EndLine = stored.Pos.Line
			}
			pos.EndCol = -1
		}
	}
	kind := perrors.KindSyntax
	msg := ""
	switch st.Done() {
	case lexer.DoneToken:
		// CPython maps E_TOKEN to "invalid syntax", not "invalid token".
		//
		// CPython: Parser/pegen_errors.c:79 E_TOKEN → "invalid syntax"
		msg = "invalid syntax"
	case lexer.DoneDedent:
		kind = perrors.KindIndentation
		msg = "unindent does not match any outer indentation level"
	case lexer.DoneTabSpace:
		kind = perrors.KindTab
		msg = "inconsistent use of tabs and spaces in indentation"
	case lexer.DoneToodeep:
		kind = perrors.KindIndentation
		msg = "too many levels of indentation"
	case lexer.DoneLineCont:
		msg = "unexpected character after line continuation character"
	case lexer.DoneColumnOverflow:
		kind = perrors.KindOverflow
		msg = "Parser column offset overflow - source line is too big"
	case lexer.DoneEOF:
		// Unterminated input: backslash-continuation at EOF, or
		// unclosed paren level. CPython's _Pypegen_tokenizer_error
		// raises "unexpected EOF while parsing" for the no-level case
		// and "'%c' was never closed" when a paren is still open.
		//
		// CPython: Parser/pegen_errors.c:84 E_EOF branch
		if st.Level() > 0 {
			line, col, ch := st.ParenInfo(st.Level())
			pos.Lineno = line
			pos.ColOff = col
			pos.EndLine = line
			pos.EndCol = -1
			msg = fmt.Sprintf("'%c' was never closed", ch)
		} else {
			// CPython's _PyPegen_raise_error sees the ERRORTOKEN's
			// col_offset == -1, recomputes it as (tok->cur -
			// tok->line_start), then runs the byte-to-character
			// conversion in raise_error_known_location so the caret
			// lands one column past the trailing backslash. text
			// comes from line_start..inp WITH the trailing '\n' the
			// translate_newlines pass appended.
			//
			// CPython: Parser/pegen_errors.c:248 col_offset == -1 branch
			// CPython: Parser/pegen_errors.c:362 error_line from line_start..inp
			pos.ColOff = st.EOFCharOffset()
			pos.EndLine = pos.Lineno
			pos.EndCol = -1
			msg = "unexpected EOF while parsing"
		}
	default:
		if storedMsg != "" {
			msg = storedMsg
		} else {
			msg = "invalid syntax"
		}
	}
	if kind == perrors.KindSyntax && storedMsg != "" && msg != storedMsg {
		msg = storedMsg
	}
	text := ""
	if stored != nil {
		text = stored.Text
	}
	if text == "" && st.Done() == lexer.DoneEOF && st.Level() == 0 {
		// CPython routes string-input EOF through the fallback in
		// _PyPegen_raise_error_known_location that decodes from
		// tok->line_start to tok->inp, so the trailing '\n' that
		// translate_newlines appended is preserved on SyntaxError.text.
		// The file-input path (ProgramDecodedTextObject) strips it; we
		// land on the string-input shape because gopy serves source from
		// the lexer buffer, not from a re-opened file.
		//
		// CPython: Parser/pegen_errors.c:362 PyUnicode_DecodeUTF8(line_start, inp - line_start, "replace")
		text = st.EOFLineText()
	}
	return &perrors.SyntaxError{
		Kind:     kind,
		Pos:      pos,
		Filename: st.Filename(),
		Message:  msg,
		Text:     text,
	}
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

// ResetForErrorPass clears the memo cache on every cached token and
// rewinds the mark so the second parse pass starts from scratch with
// invalid-rule diagnostics active. CPython does the same between its
// two passes; without it the first pass's nil-memo entries are reused
// and the second pass returns immediately without running the deeper
// invalid_* rules.
//
// CPython: Parser/pegen.c:879 reset_parser_state_for_error_pass
func (p *Parser) ResetForErrorPass() {
	for i := 0; i < p.fill; i++ {
		p.tokens[i].memo = nil
	}
	p.mark = 0
	p.callInvalid = true
	p.lastStmt = Location{}
	// Clear errorIndicator when no specific error is pinned so the
	// second pass can run invalid_* rules and produce context-aware
	// messages. This mirrors CPython's reset_parser_state_for_error_pass
	// which clears p->error_indicator when no Python exception is set.
	// If pinnedErr is already set (e.g. tokenizer-raised TabError), keep
	// errorIndicator true so the second pass short-circuits and the
	// original structured error is preserved.
	//
	// CPython: Parser/pegen.c:879 reset_parser_state_for_error_pass
	if p.pinnedErr == nil {
		p.errorIndicator = false
	}
}

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

// FarthestToken returns the token at the deepest position any rule
// reached during this parse. Used by the driver to synthesize a
// SyntaxError at the parser's farthest point when no rule pinned one.
//
// CPython: Parser/pegen.c:1136 _PyPegen_run_parser uses farthest_pos
// to pick the error caret when raise_error fired without a known
// location.
func (p *Parser) FarthestToken() *Token {
	idx := p.farthestPos
	if idx < 0 {
		idx = 0
	}
	if idx >= len(p.tokens) {
		idx = len(p.tokens) - 1
	}
	if idx < 0 {
		return nil
	}
	return p.tokens[idx]
}

// Tokenizer returns the lexer state driving this parser. The driver
// reads the source buffer through it to populate SyntaxError.text.
func (p *Parser) Tokenizer() *lexer.State { return p.tok }
