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

	// warnings collects SyntaxWarning-class diagnostics from
	// parserWarn. The lexer keeps these out of err so the parse can
	// continue; consumers (module/_tokenize, py compile path) drain
	// them via Warnings() and surface them through the warnings
	// module.
	//
	// CPython: Parser/tokenizer/helpers.c:153 _PyTokenizer_parser_warn
	warnings []SyntaxError

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
// Values are not the literal E_* numbers (gopy uses iota), but the
// set tracks errcode.h one-to-one so callers can switch on it.
//
// CPython: Include/errcode.h:22 E_OK..E_COLUMNOVERFLOW
type errCode int

const (
	eOK       errCode = iota
	eEOF              // CPython: Include/errcode.h:23 E_EOF
	eIntr             // CPython: Include/errcode.h:24 E_INTR
	eToken            // CPython: Include/errcode.h:25 E_TOKEN
	eSyntax           // CPython: Include/errcode.h:26 E_SYNTAX
	eNomem            // CPython: Include/errcode.h:27 E_NOMEM
	eToodeep          // CPython: Include/errcode.h:32 E_TOODEEP
	eDedent           // CPython: Include/errcode.h:33 E_DEDENT
	eTabSpace         // CPython: Include/errcode.h:30 E_TABSPACE
	eOverflow         // CPython: Include/errcode.h:31 E_OVERFLOW
	eDecode           // CPython: Include/errcode.h:34 E_DECODE
	eEOFS             // CPython: Include/errcode.h:35 E_EOFS
	eEOLS             // CPython: Include/errcode.h:36 E_EOLS
	eLineCont         // CPython: Include/errcode.h:37 E_LINECONT
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

// InsideFString reports whether the tokenizer is currently inside an
// f-string or t-string body. Mirrors INSIDE_FSTRING, which is the
// guard CPython's pegen helpers use before consulting the active
// tokenizer mode.
//
// CPython: Parser/lexer/lexer.h:14 INSIDE_FSTRING
func (s *State) InsideFString() bool {
	if s.tokModeStackIndex <= 0 {
		return false
	}
	return s.tokModeStack[s.tokModeStackIndex].kind == tokFStringMode
}

// CurrentFStringRaw reports the `raw` flag of the active f-string or
// t-string mode. Caller is expected to gate the read with
// InsideFString.
//
// CPython: Parser/lexer/state.h:48 tokenizer_mode.raw
func (s *State) CurrentFStringRaw() bool {
	return s.tokModeStack[s.tokModeStackIndex].raw
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
	// Category is "SyntaxWarning" for warnings recorded via
	// parserWarn; empty for hard errors recorded via recordError.
	// Downstream consumers route on this when surfacing through the
	// warnings module vs raising.
	Category string
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

// SourceLine returns the nth (1-based) line of the buffered source.
// Returns "" for out-of-range or for streaming inputs whose lines
// have already been consumed.
//
// CPython: Parser/tokenizer/helpers.c reads tok->buf to populate the
// SyntaxError text field at error time.
func (s *State) SourceLine(n int) string { return nthLine(s.buf, n) }

// Encoding returns the source encoding detected from a BOM or
// PEP 263 cookie, or "" when no cookie was seen.
func (s *State) Encoding() string { return s.encoding }

// SetFilename pins a name for error messages.
func (s *State) SetFilename(name string) { s.filename = name }

// Err returns the first SyntaxError recorded, or nil.
func (s *State) Err() *SyntaxError { return s.err }

// Level reports the nesting depth of the paren stack. Non-zero means
// the lexer has at least one unclosed `(`, `[`, or `{`. The pegen
// error driver consults this from outside the package when surfacing
// unclosed-paren diagnostics.
//
// CPython: Parser/lexer/state.h tok_state.level
func (s *State) Level() int { return s.level }

// ParenInfo returns the recorded line, column and bracket byte of the
// outermost unclosed paren. lvl is 1-based (1 == innermost open paren),
// matching `parenstack[level-1]` in CPython. Returns (0, 0, 0) when lvl
// is out of range.
//
// CPython: Parser/pegen_errors.c:60 raise_unclosed_parentheses_error
func (s *State) ParenInfo(lvl int) (line, col int, ch byte) {
	idx := lvl - 1
	if idx < 0 || idx >= s.level {
		return 0, 0, 0
	}
	return s.parenLineno[idx], s.parenCol[idx], s.parenStack[idx]
}

// EOFCharOffset returns the 1-based code-point offset of the buffer's
// end (inp) measured from the current line_start. Mirrors CPython's
// _PyPegen_raise_error fallback for tokens with col_offset == -1:
// col_offset = tok->cur - tok->line_start, then converted to a
// character count via _PyPegen_byte_offset_to_character_offset. Used to
// place the caret at the position past the trailing backslash when the
// parser surfaces "unexpected EOF while parsing".
//
// CPython: Parser/pegen_errors.c:255 col_offset = cur - line_start
// CPython: Parser/pegen_errors.c:380 byte_offset_to_character_offset
func (s *State) EOFCharOffset() int {
	end := s.inp
	if end > len(s.buf) {
		end = len(s.buf)
	}
	return s.charColBetween(s.lineStart, end)
}

// EOFLineText returns the raw source line from the current line_start
// up to inp, including any trailing newline. CPython's
// _PyPegen_raise_error_known_location populates SyntaxError.text this
// way for string-input EOF; the appended '\n' that translate_newlines
// stamps on a non-terminated source is preserved so .text round-trips
// the same way CPython renders it.
//
// CPython: Parser/pegen_errors.c:362 PyUnicode_DecodeUTF8(line_start, inp - line_start)
func (s *State) EOFLineText() string {
	end := s.inp
	if end > len(s.buf) {
		end = len(s.buf)
	}
	if s.lineStart < 0 || s.lineStart > end {
		return ""
	}
	return string(s.buf[s.lineStart:end])
}

// Lineno returns the lexer's current line number. Exposed so the
// parser-side error driver can stamp the right line on the
// unexpected-EOF SyntaxError when the trailing token's lineno is
// stale (e.g., a backslash continuation consumed the '\n' but the
// pending lineno bump never flushed because there is no next char).
func (s *State) Lineno() int { return s.lineno }

// Warnings returns the SyntaxWarning-class diagnostics recorded
// during tokenization. Order matches emission order.
//
// CPython: Parser/tokenizer/helpers.c:153 _PyTokenizer_parser_warn
func (s *State) Warnings() []SyntaxError { return s.warnings }

// WarnHook is the package-level drain that FlushWarnings calls. It is
// nil until a runtime package (typically module/_warnings) registers a
// real implementation in its init function. Keeping the hook here
// (rather than importing module/_warnings directly) is what lets
// parser/lexer stay a leaf package while still routing through the
// warnings filter at runtime.
//
// CPython does the routing inline in _PyTokenizer_parser_warn
// (helpers.c:152); gopy needs the indirection because parser/lexer
// must not pull in the runtime's heavy dependency graph.
var WarnHook func(filename string, warns []SyntaxError)

// FlushWarnings forwards every recorded SyntaxWarning to WarnHook so
// the warnings filter sees them. Callers should invoke this once
// tokenization is complete; the hook is a no-op when module/_warnings
// is not linked into the binary.
//
// CPython: Parser/tokenizer/helpers.c:152 _PyTokenizer_parser_warn
// (where the actual PyErr_WarnExplicitObject call happens).
func (s *State) FlushWarnings() {
	if WarnHook == nil || len(s.warnings) == 0 {
		return
	}
	WarnHook(s.filename, s.warnings)
}

// Done returns the lexer's terminal status as an exported int that
// matches CPython's E_* numbering from Include/errcode.h. Callers
// outside the package (notably module/_tokenize) need to switch on
// it to map to the right Python exception class.
//
// CPython: Include/errcode.h:22 E_OK..E_COLUMNOVERFLOW
func (s *State) Done() int { return int(s.done) }

// Done* constants mirror the gopy errCode enum for cross-package
// switches. They are not the literal E_* numbers from errcode.h
// (gopy uses iota), but they track the family one-to-one so callers
// can categorize tok->done without depending on the unexported enum.
const (
	DoneOK             = int(eOK)
	DoneEOF            = int(eEOF)
	DoneIntr           = int(eIntr)
	DoneToken          = int(eToken)
	DoneSyntax         = int(eSyntax)
	DoneNomem          = int(eNomem)
	DoneToodeep        = int(eToodeep)
	DoneDedent         = int(eDedent)
	DoneTabSpace       = int(eTabSpace)
	DoneOverflow       = int(eOverflow)
	DoneDecode         = int(eDecode)
	DoneEOFS           = int(eEOFS)
	DoneEOLS           = int(eEOLS)
	DoneLineCont       = int(eLineCont)
	DoneErrLine        = int(eErrLine)
	DoneBadVisibility  = int(eBadVisibility)
	DoneEncoding       = int(eEncoding)
	DoneColumnOverflow = int(eColumnOverflow)
)

// recordError pins the first error we hit. CPython overwrites; we
// preserve the first because PEG callers retry tokenization for
// diagnostics. The column reported here is the UTF-8 character
// count from the line start to the cursor, matching CPython's
// _syntaxerror_range which decodes [tok->line_start, tok->cur) and
// uses PyUnicode_GET_LENGTH for the column.
//
// CPython: Parser/tokenizer/helpers.c:11 _syntaxerror_range
func (s *State) recordError(msg string) {
	if s.err != nil {
		return
	}
	col := s.charColAt(s.cur)
	s.err = &SyntaxError{
		Pos:     Pos{Line: s.lineno, Col: col},
		EndPos:  Pos{Line: s.lineno, Col: col},
		Message: msg,
	}
}

// recordStringError pins an unterminated-string error at the opening
// quote, matching CPython which rewinds:
//
//	tok->cur = tok->start; tok->cur++;
//	tok->line_start = tok->multi_line_start;
//	tok->lineno = tok->first_lineno;
//
// before _PyTokenizer_syntaxerror. The col offset is then character
// count from the opening line's start to one past the opening quote,
// so a `"` at column 0 reports offset 1.
//
// CPython: Parser/lexer/lexer.c:1175 tok->cur = (char *)tok->start; tok->cur++
// CPython: Parser/lexer/lexer.c:1177 tok->line_start = tok->multi_line_start
// CPython: Parser/lexer/lexer.c:1179 tok->lineno = tok->first_lineno
func (s *State) recordStringError(msg string) {
	if s.err != nil {
		return
	}
	col := s.charColBetween(s.multiLineStart, s.start+1)
	s.err = &SyntaxError{
		Pos:     Pos{Line: s.firstLine, Col: col},
		EndPos:  Pos{Line: s.firstLine, Col: col},
		Message: msg,
	}
}

// recordErrorWithText is recordError plus a populated Text field. Used
// at the BOM/cookie boundary where the offending line is known but the
// FSM has not yet ingested it, so the default Pos -> source-buffer
// lookup the lexer normally does is unavailable.
//
// CPython: Parser/tokenizer/helpers.c:153 _PyTokenizer_parser_warn and
// the SyntaxError builders both copy the offending line into the
// PySyntaxErrorObject.text field when the source is in hand.
func (s *State) recordErrorWithText(msg, text string) {
	if s.err != nil {
		return
	}
	col := s.charColAt(s.cur)
	s.err = &SyntaxError{
		Pos:     Pos{Line: s.lineno, Col: col},
		EndPos:  Pos{Line: s.lineno, Col: col},
		Message: msg,
		Text:    text,
	}
}

// charColAt counts how many Unicode code points sit between
// s.lineStart and pos in the current source buffer. Used by the
// error builders that need CPython-compatible col offsets even when
// the offending line contains multi-byte UTF-8 sequences. Invalid
// UTF-8 sequences are counted as one code point each, matching the
// errors='replace' decode CPython uses in _syntaxerror_range.
//
// CPython: Parser/tokenizer/helpers.c:27 _syntaxerror_range
// (PyUnicode_DecodeUTF8 with "replace" + PyUnicode_GET_LENGTH)
func (s *State) charColAt(pos int) int {
	return s.charColBetween(s.lineStart, pos)
}

// charColBetween is charColAt with an explicit line-start offset. The
// unterminated-string path needs to count from multi_line_start (the
// line containing the opening quote), which is different from
// s.lineStart by the time the lexer reaches EOF.
func (s *State) charColBetween(from, pos int) int {
	if pos < from {
		return 0
	}
	if pos > len(s.buf) {
		pos = len(s.buf)
	}
	if from < 0 {
		from = 0
	}
	bs := s.buf[from:pos]
	chars := 0
	for i := 0; i < len(bs); {
		c := bs[i]
		switch {
		case c < 0x80:
			i++
		case c < 0xC0:
			// Lone continuation byte; "replace" decode emits one
			// U+FFFD per byte.
			i++
		case c < 0xE0:
			i += 2
		case c < 0xF0:
			i += 3
		default:
			i += 4
		}
		if i > len(bs) {
			i = len(bs)
		}
		chars++
	}
	return chars
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
// metadata pointer to NULL; Tok values are zero-initialized in Go so
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
