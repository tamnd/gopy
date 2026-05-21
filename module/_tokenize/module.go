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

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
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

	// linesByOneBased holds the source split into lines, indexed
	// 1..N. linesByOneBased[0] is an empty placeholder so lineno-1
	// indexing matches CPython's 1-based line numbers.
	linesByOneBased []string
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

	source, lines, err := drainReadline(readline, encoding)
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
func drainReadline(readline objects.Object, encoding string) ([]byte, []string, error) {
	var buf []byte
	var lines []string
	lines = append(lines, "") // 1-based padding
	for {
		res, err := objects.Call(readline, objects.NewTuple(nil), nil)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return nil, nil, err
		}
		var line []byte
		switch v := res.(type) {
		case *objects.Bytes:
			if encoding == "" {
				return nil, nil, fmt.Errorf("TypeError: readline() returned a non-string object")
			}
			line = append([]byte(nil), v.Bytes()...)
		case *objects.ByteArray:
			if encoding == "" {
				return nil, nil, fmt.Errorf("TypeError: readline() returned a non-string object")
			}
			line = append([]byte(nil), v.Bytes()...)
		case *objects.Unicode:
			if encoding != "" {
				return nil, nil, fmt.Errorf("TypeError: readline() returned a non-bytes object")
			}
			line = []byte(v.Value())
		default:
			if res == objects.None() {
				break
			}
			return nil, nil, fmt.Errorf("TypeError: readline returned %s, expected bytes/str", res.Type().Name)
		}
		if len(line) == 0 {
			break
		}
		buf = append(buf, line...)
		// Track the per-line view (without the trailing newline) so we
		// can emit the `line` field of each token tuple.
		end := len(line)
		if end > 0 && line[end-1] == '\n' {
			end--
			if end > 0 && line[end-1] == '\r' {
				end--
			}
		}
		lines = append(lines, string(line[:end]))
		if errors.Is(err, io.EOF) {
			break
		}
	}
	// CPython's tokenizer requires the buffer end with '\n'.
	if len(buf) == 0 || buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}
	return buf, lines, nil
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

	if kind == token.ERRORTOKEN {
		return nil, tokenizerError(it.tok)
	}

	str := string(tok.Bytes)

	isTrailing := false
	if kind == token.ENDMARKER {
		isTrailing = true
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
			if str == "" {
				str = "\n"
			}
			endCol++
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
// CPython: Python/Python-tokenize.c:87 _tokenizer_error
func tokenizerError(st *lexer.State) error {
	se := st.Err()
	storedMsg := ""
	if se != nil {
		storedMsg = se.Message
	}

	errClass := "SyntaxError"
	msg := ""
	switch st.Done() {
	case lexer.DoneToken:
		msg = "invalid token"
	case lexer.DoneEOF:
		// CPython attaches lineno/col via PyErr_SyntaxLocationObject
		// and returns immediately. gopy reports the canonical text.
		return fmt.Errorf("SyntaxError: unexpected EOF in multi-line statement")
	case lexer.DoneDedent:
		errClass = "IndentationError"
		msg = "unindent does not match any outer indentation level"
	case lexer.DoneIntr:
		return fmt.Errorf("KeyboardInterrupt")
	case lexer.DoneNomem:
		return fmt.Errorf("MemoryError")
	case lexer.DoneTabSpace:
		errClass = "TabError"
		msg = "inconsistent use of tabs and spaces in indentation"
	case lexer.DoneToodeep:
		errClass = "IndentationError"
		msg = "too many levels of indentation"
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
	if errClass == "SyntaxError" && storedMsg != "" && msg != storedMsg {
		msg = storedMsg
	}

	return fmt.Errorf("%s: %s", errClass, msg)
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
