// Package lexer ports cpython/Parser/lexer/ and cpython/Parser/tokenizer/
// to Go. The lexer turns source bytes into tokens with positions; the
// driver layer feeds it from strings, byte slices, files, or readline
// callbacks.
//
// Tokens emitted here use kinds from the tokenize package (which are
// pinned to Include/internal/pycore_token.h). The pegen runtime in
// parser/pegen consumes these tokens.
//
// CPython: Parser/lexer/state.h, Parser/lexer/state.c
package lexer

import "github.com/tamnd/gopy/token"

const (
	maxIndent       = 100
	maxLevel        = 200
	maxFstringLevel = 150
	maxExprNesting  = 3

	tabSize    = 8
	altTabSize = 1
)

// Mode is the tokenizer top-level mode. Mirrors the PyCompile_Mode used
// by the upstream tokenizer entry points.
//
// CPython: Parser/lexer/state.h:14 start mode constants
type Mode int

// Mode constants. ModeFile is the default for `python script.py`,
// ModeSingle drives the REPL, ModeEval handles `eval(...)`,
// ModeFunctionType backs `inspect.signature` style annotation parsing,
// and ModeFString is reserved for direct f-string parsing.
const (
	ModeFile Mode = iota
	ModeSingle
	ModeEval
	ModeFunctionType
	ModeFString
)

// modeKind picks regular vs fstring scanner for a single
// tokenizer-mode-stack entry.
//
// CPython: Parser/lexer/state.h:36 tokenizer_mode_kind_t
type modeKind int

const (
	tokRegularMode modeKind = iota
	tokFStringMode
)

// stringKind distinguishes f-string vs t-string contexts on the
// tokenizer mode stack.
//
// CPython: Parser/lexer/state.h:41 string_kind_t
type stringKind int

const (
	kindFString stringKind = iota
	kindTString
)

// decodingState tracks PEP 263 source-encoding detection progress.
//
// CPython: Parser/lexer/state.h:15 decoding_state
type decodingState int

const (
	decodeInit decodingState = iota
	decodeSeekCoding
	decodeNormal
)

// interactiveUnderflow controls REPL refill behavior.
//
// CPython: Parser/lexer/state.h:21 interactive_underflow_t
type interactiveUnderflow int

const (
	iunderflowNormal interactiveUnderflow = iota
	iunderflowStop
)

// Pos is a token start/end coordinate. Both fields are 1-based for
// line and 0-based for col, matching CPython's lineno / col_offset
// convention.
type Pos struct {
	Line int
	Col  int
}

// Tok is the lexer's emitted token. Distinct from tokenize.Token (the
// Python-facing surface in 1665) which adds the Bytes/Line strings.
//
// CPython: Parser/lexer/state.h:29 struct token
type Tok struct {
	Kind        token.Type
	Bytes       []byte
	Start       Pos
	End         Pos
	Level       int
	StartOffset int
	EndOffset   int
	// Metadata holds f-string/t-string interpolation expression
	// text captured during scanning. nil for ordinary tokens.
	Metadata []byte
}

// tokenizerMode is one entry on the tokenizer mode stack. Each
// interpolated f-string or t-string pushes one of these so the
// scanner knows the quote style and brace depth to balance.
//
// CPython: Parser/lexer/state.h:48 tokenizer_mode
type tokenizerMode struct {
	kind                       modeKind
	curlyBracketDepth          int
	curlyBracketExprStartDepth int

	quote     byte
	quoteSize int
	raw       bool

	start            int // offset into the source buffer
	multiLineStart   int
	firstLine        int
	startOffset      int
	multiStartOffset int

	lastExprSize   int
	lastExprEnd    int
	lastExprBuffer []byte
	inDebug        bool
	inFormatSpec   bool

	stringKind stringKind
}

// State is the tokenizer's per-call state. One State drives one
// tokenization pass.
//
// CPython: Parser/lexer/state.h:74 struct tok_state
type State struct {
	// Input buffer. We keep cur, inp, end as offsets into buf so
	// growing the buffer (file refill) does not invalidate them,
	// avoiding the pointer-arithmetic style of the C source.
	buf []byte
	cur int
	inp int
	end int

	start int // offset of start of current token; -1 if none

	done errCode
	err  *SyntaxError

	mode     Mode
	tabSize  int
	indent   int
	indstack [maxIndent]int
	altstack [maxIndent]int
	atbol    bool
	pendin   int // >0 indents pending, <0 dedents pending
	lineno   int
	// pendingLineno defers the post-'\n' line bump until the next
	// non-EOF byte is actually consumed. CPython's tok_underflow_*
	// callbacks call ADVANCE_LINENO when they successfully fetch the
	// next line; gopy preloads the buffer, so we mimic the timing by
	// bumping in nextC instead.
	pendingLineno int
	firstLine     int

	startCol int
	col      int

	level       int
	parenStack  [maxLevel]byte
	parenLineno [maxLevel]int
	parenCol    [maxLevel]int

	filename string

	decode    decodingState
	encoding  string
	contLine  bool
	lineStart int

	multiLineStart int

	// readline drives interactive / file mode. Returns one line
	// (including any newline) or io.EOF.
	readline func() ([]byte, error)

	typeComments bool

	interactiveUnderflow interactiveUnderflow

	reportWarnings bool

	tokModeStack      [maxFstringLevel]tokenizerMode
	tokModeStackIndex int

	tokExtraTokens  bool
	commentNewline  bool
	implicitNewline bool

	// blankline tracks "this line had no real tokens": indent loop
	// landed on '#', '\n', or EOF. Mirrors the local `blankline` in
	// CPython tok_get_normal_mode (Parser/lexer/lexer.c:504). Used by
	// the '\n' branch to skip blank/comment-only lines instead of
	// emitting NEWLINE, matching the C `goto nextline`.
	blankline bool

	// underflow refills buf when cur == inp. nil for in-memory
	// drivers that load the whole source up front.
	underflow func(*State) bool
}

// errCode is the lexer's done state. Mirrors errcode.h's E_* family.
//
// CPython: Parser/lexer/state.h:113 (and Include/internal/pycore_pyerrors.h E_OK family)
type errCode int

const (
	eOK errCode = iota
	eEOF
	eIntr
	eToken
	eSyntax
	eIndent
	eDedent
	eTabSpace
	eOverflow
	eDecode
	eEOFS
	eEOLS
	eErrLine
	eBadVisibility
	eEncoding
	eColumnOverflow
)

// newState allocates and initializes a fresh State with CPython's
// default field values.
//
// CPython: Parser/lexer/state.c:13 _PyTokenizer_tok_new
func newState() *State {
	s := &State{
		done:                 eOK,
		tabSize:              tabSize,
		atbol:                true,
		startCol:             -1,
		col:                  -1,
		decode:               decodeInit,
		interactiveUnderflow: iunderflowNormal,
		reportWarnings:       true,
		start:                -1,
	}
	// indstack[0] and altstack[0] start at zero; Go zero-initializes.
	s.tokModeStack[0] = tokenizerMode{kind: tokRegularMode, lastExprEnd: -1}
	return s
}

// curMode is TOK_GET_MODE in the C source: the active tokenizer-mode
// stack entry.
//
// CPython: Parser/lexer/lexer.c:26 TOK_GET_MODE
func (s *State) curMode() *tokenizerMode {
	return &s.tokModeStack[s.tokModeStackIndex]
}

// pushMode is TOK_NEXT_MODE: enter a nested f-string or t-string
// scanning context.
//
// CPython: Parser/lexer/lexer.c:31 TOK_NEXT_MODE
func (s *State) pushMode() *tokenizerMode {
	s.tokModeStackIndex++
	s.tokModeStack[s.tokModeStackIndex] = tokenizerMode{lastExprEnd: -1}
	return &s.tokModeStack[s.tokModeStackIndex]
}

// popMode leaves the current f-string/t-string context.
//
// CPython: Parser/lexer/lexer.c:1088 implicit pop in tok_get_normal_mode
func (s *State) popMode() {
	if s.tokModeStackIndex > 0 {
		s.tokModeStackIndex--
	}
}

// insideFString reports whether we are scanning inside an f-string or
// t-string body (versus the outer Python source).
//
// CPython: Parser/lexer/state.h:10 INSIDE_FSTRING
func (s *State) insideFString() bool {
	return s.tokModeStackIndex > 0
}

// insideFStringExpr reports whether we are scanning a {expr} block
// inside an f-string or t-string.
//
// CPython: Parser/lexer/state.h:11 INSIDE_FSTRING_EXPR
func (s *State) insideFStringExpr() bool {
	return s.curMode().curlyBracketExprStartDepth >= 0
}

// SyntaxError is the lexer's error type. The pegen runtime lifts this
// into the parser-level *SyntaxError when needed.
//
// CPython: Parser/pegen_errors.c:184 _PyPegen_raise_error_known_location
type SyntaxError struct {
	Pos     Pos
	EndPos  Pos
	Message string
	Text    string
}

// Error renders the lexer error in CPython's "<msg>" form. The full
// "File ..., line N" envelope is added by the pegen layer.
func (e *SyntaxError) Error() string {
	return e.Message
}

// SetExtraTokens enables COMMENT, NL, and ENCODING token emission.
// Mirrors tokenize.tokenize()'s extra_tokens flag.
//
// CPython: Parser/lexer/state.h:133 tok_extra_tokens
func (s *State) SetExtraTokens(v bool) { s.tokExtraTokens = v }

// SetTypeComments enables type-comment emission (`# type: ...`).
//
// CPython: Parser/lexer/state.h:122 type_comments
func (s *State) SetTypeComments(v bool) { s.typeComments = v }

// Filename returns the configured filename. Used by error formatters.
func (s *State) Filename() string { return s.filename }

// Encoding returns the source encoding detected from a BOM or
// PEP 263 cookie, or "" when no cookie was seen.
func (s *State) Encoding() string { return s.encoding }

// SetFilename pins a name for error messages.
func (s *State) SetFilename(name string) { s.filename = name }

// Err returns the first SyntaxError recorded, or nil.
func (s *State) Err() *SyntaxError { return s.err }

// recordError pins the first error we hit. CPython overwrites; we
// preserve the first because PEG callers retry tokenization for
// diagnostics.
func (s *State) recordError(msg string) {
	if s.err != nil {
		return
	}
	s.err = &SyntaxError{
		Pos:     Pos{Line: s.lineno, Col: s.col},
		EndPos:  Pos{Line: s.lineno, Col: s.col},
		Message: msg,
	}
}

// freeFStringExpressions clears the per-mode last_expr_buffer slots.
// CPython has to free them by hand because PyMem_Malloc owns the
// memory; in gopy the GC reclaims the slice once we drop the
// reference, so the body just nils the fields out so a debugger sees
// a clean state.
//
// CPython: Parser/lexer/state.c:25 free_fstring_expressions
func (s *State) freeFStringExpressions() {
	for i := s.tokModeStackIndex; i >= 0; i-- {
		m := &s.tokModeStack[i]
		m.lastExprBuffer = nil
		m.lastExprSize = 0
		m.lastExprEnd = -1
		m.inFormatSpec = false
	}
}

// Free releases the tokenizer state. In CPython this hand-frees
// encoding / buf / input / interactive_src_start / fstring history;
// gopy uses the Go GC so the body just clears slices to break
// reference cycles and calls freeFStringExpressions for parity.
//
// CPython: Parser/lexer/state.c:43 _PyTokenizer_Free
func (s *State) Free() {
	s.encoding = ""
	s.buf = nil
	s.filename = ""
	s.freeFStringExpressions()
}

// TokenInit zeroes a Tok value. CPython's _PyToken_Init sets the
// metadata pointer to NULL; Tok values are zero-initialised in Go so
// this is purely a citation anchor for parity.
//
// CPython: Parser/lexer/state.c:67 _PyToken_Init
func TokenInit(t *Tok) {
	if t != nil {
		*t = Tok{}
	}
}

// TokenFree releases a Tok. CPython drops the metadata reference;
// gopy lets the GC handle it. Kept as a citation anchor.
//
// CPython: Parser/lexer/state.c:63 _PyToken_Free
func TokenFree(_ *Tok) {}
