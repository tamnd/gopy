// Package _tokenize ports CPython's Python/Python-tokenize.c. It
// exposes a single type, TokenizerIter, that drives the C lexer (here:
// the Go port under parser/lexer) and yields 5-tuples of the shape
// (type, str, (start_lineno, col), (end_lineno, col), line). The
// vendored Lib/tokenize.py drives this iterator directly via
// _generate_tokens_from_c_tokenizer.
//
// Function map (Python-tokenize.c -> gopy):
//
//	tokenizeriterobject               -> tokenizerIter
//	get_tokenize_state                -> (obsolete; gopy uses package state)
//	tokenizeriter_new_impl            -> tokenizerIterNew
//	_tokenizer_error                  -> tokenizerError
//	_get_current_line                 -> inlined in tokenizerIterNext (lineAt + lastLine cache)
//	_get_col_offsets                  -> inlined in tokenizerIterNext (byteToCharCol)
//	tokenizeriter_next                -> tokenizerIterNext
//	tokenizeriter_dealloc             -> (Go GC, no-op)
//	tokenizemodule_exec               -> buildModule
//	tokenizemodule_traverse/clear/free-> (Go GC, no-op)
//	PyInit__tokenize                  -> init() AppendInittab
//
// CPython: Python/Python-tokenize.c

package _tokenize

import (
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	parsererrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/token"
)

func init() {
	_ = imp.AppendInittab("_tokenize", buildModule)
}

// tokenizerIter is the Python TokenizerIter instance. Mirrors the
// tokenizeriterobject struct in CPython.
//
// CPython: Python/Python-tokenize.c:32 tokenizeriterobject
type tokenizerIter struct {
	objects.Header

	tok         *lexer.State
	done        bool
	extraTokens bool
	encoding    string

	lastLineno    int
	lastEndLineno int
	lastLine      objects.Object // *Unicode cache for the current line
	byteColDiff   int

	// warnIdx tracks how many of tok.Warnings() have already been
	// drained through WarnHook. Drives the per-token drain in
	// tokenizerIterNext so SyntaxWarnings surface to the warnings
	// filter as they happen, the way CPython's helpers.c:152
	// _PyTokenizer_parser_warn does inline.
	warnIdx int

	// linesByOneBased holds the source split into lines, indexed
	// 1..N. linesByOneBased[0] is an empty placeholder so lineno-1
	// indexing matches CPython's 1-based line numbers.
	linesByOneBased []string

	// lineEndCRLF[row]==true when the corresponding raw readline
	// returned a '\r\n' (rather than '\n' or no terminator). CPython's
	// TokenizerIter constructs the tokenizer with preserve_crlf=1, so
	// tok->start[0] reads back as '\r' for those NEWLINE tokens.
	// gopy folds CRLF -> LF in TranslateNewlines before the lexer sees
	// the buffer, so we keep the original ending out-of-band and
	// consult it when emitting the NEWLINE string.
	//
	// CPython: Python/Python-tokenize.c:318 tokenizeriter_next NEWLINE arm
	lineEndCRLF []bool

	// implicitNewline records that drainReadline appended a synthetic
	// '\n' because the source didn't end with one. The trailing
	// NEWLINE token whose end offset lands on that byte must report
	// string='' (CPython: Python/Python-tokenize.c:281 tokenizeriter_next
	// NEWLINE branch checks tok->implicit_newline).
	implicitNewline bool
	implicitEndOff  int
}

// Type returns the tokenizer iterator's type.
func (t *tokenizerIter) Type() *objects.Type { return tokenizerIterType }

// tokenizerIterType is the public Python type _tokenize.TokenizerIter.
//
// CPython: Python/Python-tokenize.c:371 tokenizeriter_spec
var tokenizerIterType = newTokenizerIterType()

func newTokenizerIterType() *objects.Type {
	t := objects.NewType("TokenizerIter", []*objects.Type{objects.ObjectType()})
	t.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	t.IterNext = tokenizerIterNext
	t.TpNew = tokenizerIterNew
	objects.AddIterSlotWrappers(t)
	return t
}

// tokenizerIterNew is the tp_new slot.
//
// CPython: Python/Python-tokenize.c:55 tokenizeriter_new_impl
func tokenizerIterNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: TokenizerIter() takes exactly one positional argument (%d given)", len(args))
	}
	readline := args[0]

	extraTokens := false
	encoding := ""
	for k, v := range kwargs {
		switch k {
		case "extra_tokens":
			extraTokens = objects.IsTrue(v)
		case "encoding":
			if v == objects.None() {
				encoding = ""
			} else if s, ok := v.(*objects.Unicode); ok {
				encoding = s.Value()
			} else {
				return nil, fmt.Errorf("TypeError: encoding must be str or None")
			}
		default:
			return nil, fmt.Errorf("TypeError: TokenizerIter() got an unexpected keyword argument %q", k)
		}
	}

	source, lines, crlf, implicit, err := drainReadline(readline, encoding)
	if err != nil {
		return nil, err
	}

	st := lexer.FromBytes(source, lexer.ModeFile)
	st.SetFilename("<string>")
	if extraTokens {
		st.SetExtraTokens(true)
	}

	it := &tokenizerIter{
		tok:             st,
		extraTokens:     extraTokens,
		encoding:        encoding,
		linesByOneBased: lines,
		lineEndCRLF:     crlf,
		implicitNewline: implicit,
		implicitEndOff:  len(source) - 1,
	}
	return it, nil
}

// drainReadline pulls every line out of the readline callable up
// front and returns the concatenated bytes plus a 1-based line index.
// CPython does this incrementally via tok->underflow; gopy collapses
// it because the lexer is not yet wired for streaming refill and the
// gate tests all hand fixed-size inputs.
//
// encoding mirrors tok->encoding in CPython's readline_tokenizer.c:
// when empty, readline must return str (the default
// _generate_tokens_from_c_tokenizer path); when set, readline must
// return bytes that the named encoding decodes.
//
// CPython: Parser/tokenizer/readline_tokenizer.c:10 tok_readline_string
func drainReadline(readline objects.Object, encoding string) ([]byte, []string, []bool, bool, error) {
	var buf []byte
	var lines []string
	var crlf []bool
	lines = append(lines, "") // 1-based padding
	crlf = append(crlf, false)
	for {
		res, err := objects.Call(readline, objects.NewTuple(nil), nil)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return nil, nil, nil, false, err
		}
		var line []byte
		switch v := res.(type) {
		case *objects.Bytes:
			if encoding == "" {
				return nil, nil, nil, false, fmt.Errorf("TypeError: readline() returned a non-string object")
			}
			decoded, _, derr := codecs.Decode(v.Bytes(), encoding, "replace")
			if derr != nil {
				return nil, nil, nil, false, derr
			}
			line = []byte(decoded)
		case *objects.ByteArray:
			if encoding == "" {
				return nil, nil, nil, false, fmt.Errorf("TypeError: readline() returned a non-string object")
			}
			decoded, _, derr := codecs.Decode(v.Bytes(), encoding, "replace")
			if derr != nil {
				return nil, nil, nil, false, derr
			}
			line = []byte(decoded)
		case *objects.Unicode:
			if encoding != "" {
				return nil, nil, nil, false, fmt.Errorf("TypeError: readline() returned a non-bytes object")
			}
			line = []byte(v.Value())
		default:
			if res == objects.None() {
				break
			}
			return nil, nil, nil, false, fmt.Errorf("TypeError: readline returned %s, expected bytes/str", res.Type().Name)
		}
		if len(line) == 0 {
			break
		}
		buf = append(buf, line...)
		// Preserve the raw line, including its trailing '\n' or '\r\n'
		// when one was present. CPython's _get_current_line constructs
		// the `line` field of each token tuple from tok->buf to tok->inp
		// (Python/Python-tokenize.c:288), which keeps the trailing
		// newline byte; the implicit-newline arm strips one byte off
		// the end. Mirror both by keeping the full line here and then
		// trimming the synthesized '\n' from the last entry below if
		// the source did not end with one.
		hadCRLF := false
		if n := len(line); n >= 2 && line[n-2] == '\r' && line[n-1] == '\n' {
			hadCRLF = true
		}
		lines = append(lines, string(line))
		crlf = append(crlf, hadCRLF)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	// CPython's tokenizer requires the buffer end with '\n'; mark
	// implicit when we synthesized one so the wrapper can echo ''
	// rather than '\n' for the trailing NEWLINE. Empty source is the
	// exception: CPython tok_underflow_string never appends a newline
	// when there is no source at all, so it emits ENDMARKER only.
	//
	// CPython: Parser/tokenizer/string_tokenizer.c:55 tok_underflow_string
	implicit := len(buf) > 0 && buf[len(buf)-1] != '\n'
	if implicit {
		buf = append(buf, '\n')
	}
	return buf, lines, crlf, implicit, nil
}

// tokenizerIterNext is the tp_iternext slot. It produces a 5-tuple
// per token until ENDMARKER is reached.
//
// CPython: Python/Python-tokenize.c:241 tokenizeriter_next
func tokenizerIterNext(o objects.Object) (objects.Object, error) {
	it, ok := o.(*tokenizerIter)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected TokenizerIter")
	}
	if it.done {
		return nil, objects.ErrStopIteration
	}

	tok := it.tok.Get()
	kind := tok.Kind

	// Drain any SyntaxWarnings the lexer stashed while producing this
	// token. CPython issues them inline from helpers.c:152
	// _PyTokenizer_parser_warn; gopy routes through lexer.WarnHook
	// (set by module/_warnings.init) so the warnings filter sees them
	// between iterator steps.
	if all := it.tok.Warnings(); len(all) > it.warnIdx {
		if lexer.WarnHook != nil {
			if err := lexer.WarnHook(it.tok.Filename(), all[it.warnIdx:]); err != nil {
				return nil, err
			}
		}
		it.warnIdx = len(all)
	}

	if kind == token.ERRORTOKEN {
		return nil, tokenizerError(it.tok, it.linesByOneBased)
	}

	str := string(tok.Bytes)

	// CPython treats DEDENT-at-EOF as a trailing token alongside
	// ENDMARKER so the extra_tokens reshape (lineno+1, 0) reaches both.
	//
	// CPython: Python/Python-tokenize.c:277 tokenizeriter_next
	isTrailing := kind == token.ENDMARKER || (kind == token.DEDENT && it.tok.Done() == lexer.DoneEOF)
	if kind == token.ENDMARKER {
		it.done = true
	}

	startLine := tok.Start.Line
	startCol := tok.Start.Col
	endLine := tok.End.Line
	endCol := tok.End.Col

	// CPython preserves col_offset = -1 for tokens that came in with
	// NULL pointers (notably INDENT / DEDENT outside extra_tokens
	// mode). Only convert byte offsets to character offsets when the
	// col is real; leave -1 sentinels alone.
	//
	// CPython: Python/Python-tokenize.c:204 _get_col_offsets
	if startCol >= 0 {
		startCol = byteToCharCol(it.lineAt(startLine), startCol)
	}
	if endCol >= 0 {
		if startLine == endLine && startCol >= 0 {
			endCol = startCol + utf8.RuneCountInString(string(tok.Bytes))
		} else {
			endCol = byteToCharCol(it.lineAt(endLine), endCol)
		}
	}

	var lineStr objects.Object = objects.NewStr("")
	if !(it.extraTokens && isTrailing) {
		// Use the cached line (with a CPython-style "\n appended"
		// shape) when the lineno matches the last token's lineno.
		line := it.lineAt(startLine)
		if startLine != it.lastLineno {
			it.lastLine = objects.NewStr(line)
		}
		lineStr = it.lastLine
		it.lastLineno = startLine
		it.lastEndLineno = endLine
	}

	if it.extraTokens {
		if isTrailing {
			startLine = startLine + 1
			endLine = startLine
			startCol = 0
			endCol = 0
		}
		// CPython collapses every operator/punctuation token in the
		// extra_tokens stream to the umbrella OP type, matching the
		// shape Lib/tokenize.py expects from the legacy pure-Python
		// tokenizer.
		if kind > token.DEDENT && kind < token.OP {
			kind = token.OP
		}
		if kind == token.NEWLINE {
			// CPython: Python/Python-tokenize.c:316 tokenizeriter_next
			// Implicit NEWLINE (source did not end with a real '\n') has
			// string=''. Explicit NEWLINE preserves the original line
			// terminator: '\r\n' for CRLF, '\n' otherwise.
			if it.isImplicitNewlineLine(startLine) {
				str = ""
			} else if it.lineHasCRLF(startLine) {
				str = "\r\n"
				endCol++
			} else {
				str = "\n"
			}
			endCol++
		} else if kind == token.NL {
			// NL tokens are emitted inside parens or for blank/comment
			// lines. CPython doesn't rewrite the str for NL, but the
			// extra_tokens path collapses the implicit-newline NL to ''
			// (Python/Python-tokenize.c:327).
			if it.isImplicitNewlineLine(startLine) {
				str = ""
			} else if it.lineHasCRLF(startLine) {
				str = "\r\n"
				endCol++
			}
		}
	}

	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(kind)),
		objects.NewStr(str),
		objects.NewTuple([]objects.Object{
			objects.NewInt(int64(startLine)),
			objects.NewInt(int64(startCol)),
		}),
		objects.NewTuple([]objects.Object{
			objects.NewInt(int64(endLine)),
			objects.NewInt(int64(endCol)),
		}),
		lineStr,
	}), nil
}

// lineAt returns the source line for 1-based lineno, or "" if out of
// range.
func (it *tokenizerIter) lineAt(lineno int) string {
	if lineno <= 0 || lineno >= len(it.linesByOneBased) {
		return ""
	}
	return it.linesByOneBased[lineno]
}

// lineHasCRLF reports whether the raw readline for 1-based lineno
// ended with '\r\n'. Used by the NEWLINE / NL branches in
// tokenizerIterNext to reproduce the str = '\r\n' that CPython picks
// when tok->start[0] == '\r'.
func (it *tokenizerIter) lineHasCRLF(lineno int) bool {
	if lineno <= 0 || lineno >= len(it.lineEndCRLF) {
		return false
	}
	return it.lineEndCRLF[lineno]
}

// isImplicitNewlineLine reports whether the token at lineno sits on
// the synthesized trailing '\n' line. CPython tracks this per-line
// via tok->implicit_newline (Parser/tokenizer/readline_tokenizer.c:90
// sets it to 1 only on the last underflow when that line lacked a
// terminator). gopy collapses every readline up front, so the
// equivalent is "the last entry in linesByOneBased and the source had
// no terminator at the time we synthesized one".
func (it *tokenizerIter) isImplicitNewlineLine(lineno int) bool {
	if !it.implicitNewline {
		return false
	}
	return lineno == len(it.linesByOneBased)-1
}

// trimLineTerminator strips a trailing '\r\n' or '\n' or '\r' from s.
// SyntaxError.text holds the offending source line without its
// terminator (CPython: Python/Python-tokenize.c:140 sets size -= 1
// before decoding the line), so error builders trim before stamping
// the field.
func trimLineTerminator(s string) string {
	n := len(s)
	if n >= 2 && s[n-2] == '\r' && s[n-1] == '\n' {
		return s[:n-2]
	}
	if n >= 1 && (s[n-1] == '\n' || s[n-1] == '\r') {
		return s[:n-1]
	}
	return s
}

// byteToCharCol converts a byte column offset into a character (rune)
// column offset within line. Mirrors
// _PyPegen_byte_offset_to_character_offset_line.
//
// CPython: Parser/pegen.c byte_offset_to_character_offset
func byteToCharCol(line string, byteCol int) int {
	if byteCol <= 0 {
		return 0
	}
	if byteCol > len(line) {
		byteCol = len(line)
	}
	return utf8.RuneCountInString(line[:byteCol])
}

// tokenizerError lifts a lexer error code into the matching Python
// exception. Mirrors CPython's _tokenizer_error switch case-for-case;
// dispatches on tok->done (lexer.State.Done()) so the categories
// stay aligned even when the recorded message text shifts.
//
// The result is a structured *parsererrors.SyntaxError so the eval
// unwind path can populate the SyntaxError instance with filename,
// lineno, offset, and text. test_tokenize.py asserts these attributes
// on the exceptions tokenize.tokenize raises, so a plain Go
// fmt.Errorf would surface as `e.lineno is None` (the canonical
// SyntaxError descriptor falls back to None when no info is attached).
//
// CPython: Python/Python-tokenize.c:87 _tokenizer_error
func tokenizerError(st *lexer.State, lines []string) error {
	se := st.Err()
	storedMsg := ""
	pos := parsererrors.Pos{}
	if se != nil {
		storedMsg = se.Message
		pos.Lineno = se.Pos.Line
		pos.ColOff = se.Pos.Col
	}

	kind := parsererrors.KindSyntax
	msg := ""
	useLineEndOffset := false
	switch st.Done() {
	case lexer.DoneToken:
		msg = "invalid token"
	case lexer.DoneEOF:
		// CPython attaches lineno/col via PyErr_SyntaxLocationObject
		// and returns immediately. We do the same through the
		// structured SyntaxError carrier.
		msg = "unexpected EOF in multi-line statement"
	case lexer.DoneDedent:
		kind = parsererrors.KindIndentation
		msg = "unindent does not match any outer indentation level"
		useLineEndOffset = true
	case lexer.DoneIntr:
		return fmt.Errorf("KeyboardInterrupt")
	case lexer.DoneNomem:
		return fmt.Errorf("MemoryError")
	case lexer.DoneTabSpace:
		kind = parsererrors.KindTab
		msg = "inconsistent use of tabs and spaces in indentation"
		useLineEndOffset = true
	case lexer.DoneToodeep:
		kind = parsererrors.KindIndentation
		msg = "too many levels of indentation"
		useLineEndOffset = true
	case lexer.DoneLineCont:
		msg = "unexpected character after line continuation character"
	default:
		if storedMsg != "" {
			msg = storedMsg
		} else {
			msg = "unknown tokenization error"
		}
	}

	// CPython overrides the canonical message with the lexer's stored
	// text on the SyntaxError path when it carries more detail (e.g.
	// "invalid character ... (U+...)"). Preserve that behaviour without
	// shadowing the dedicated TabError / IndentationError text.
	if kind == parsererrors.KindSyntax && storedMsg != "" && msg != storedMsg {
		msg = storedMsg
	}

	// CPython's _tokenizer_error decodes error_line from tok->buf with
	// size = tok->inp - tok->buf - 1 (Python/Python-tokenize.c:140);
	// that strips the trailing newline so the text on the SyntaxError
	// is the bare source line. The offset is then computed against
	// tok->inp - tok->buf (the full line size including the newline),
	// which for the indent-family errors lands one past the trailing
	// newline so offset = len(text) + 1 in character units.
	//
	// CPython: Python/Python-tokenize.c:140 _tokenizer_error
	text := ""
	if pos.Lineno > 0 && pos.Lineno < len(lines) {
		text = lines[pos.Lineno]
	}
	text = trimLineTerminator(text)
	if useLineEndOffset {
		pos.ColOff = utf8.RuneCountInString(text) + 1
	} else if pos.ColOff > 0 {
		// SyntaxError.offset is 1-based; the lexer records s.col as a
		// 0-based byte position. Convert by adding one (the bytes are
		// ASCII for every error path the lexer currently records).
		pos.ColOff = pos.ColOff + 1
	}

	return &parsererrors.SyntaxError{
		Kind:     kind,
		Pos:      pos,
		Filename: st.Filename(),
		Message:  msg,
		Text:     text,
	}
}

// buildModule materializes the _tokenize module dict.
//
// CPython: Python/Python-tokenize.c:378 tokenizemodule_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_tokenize")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("TokenizerIter"), tokenizerIterType); err != nil {
		return nil, err
	}
	return m, nil
}
