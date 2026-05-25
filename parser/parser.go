// Package parser is the top-level entry point: source bytes in,
// ast.Mod out. The package wires the lexer, the pegen runtime,
// and (eventually) the generated parser table into a single
// Parse / ParseString surface that mirrors CPython's
// _PyPegen_run_parser entry point.
//
// Until the generated parser table lands the entry points return
// a sentinel error; callers can program against the stable shape
// today and the body will fill in once parser_gen ships.
//
// CPython: Parser/peg_api.c
package parser

import (
	"errors"
	"fmt"
	"io"

	"github.com/tamnd/gopy/ast"
	perrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/parser/pegen"
)

// Mode picks the entry rule of the generated parser table.
type Mode int

// Parse modes mirror Py_*_input.
const (
	ModeFile Mode = iota
	ModeSingle
	ModeEval
)

// ErrParserNotImplemented is returned by Parse / ParseString
// while the generated parser table is offline. The lexer and
// error surfaces still work; downstream code should detect this
// sentinel and route through a fixture-driven path until the
// real parser lands.
var ErrParserNotImplemented = fmt.Errorf("parser: generated rule bodies not yet emitted")

// Python compile() flag constants that control parser behaviour.
// Mirrors Include/cpython/code.h PyCF_* values gopy reads.
//
// CPython: Include/cpython/code.h PyCF_ALLOW_INCOMPLETE_INPUT
const (
	// PyCFAllowIncompleteInput = 0x4000 matches CPython's
	// PyCF_ALLOW_INCOMPLETE_INPUT. When set, unclosed brackets at EOF
	// raise _IncompleteInputError rather than SyntaxError.
	PyCFAllowIncompleteInput = 0x4000
	// PyCFDontImplyDedent = 0x200 matches CPython's PyCF_DONT_IMPLY_DEDENT.
	PyCFDontImplyDedent = 0x0200
	// PyCFTypeComments = 0x1000 matches CPython's PyCF_TYPE_COMMENTS.
	// When set, the tokenizer emits TYPE_COMMENT tokens for `# type: ...`
	// comments and the parser validates them. Used by ast.parse(type_comments=True).
	//
	// CPython: Include/cpython/compile.h:19 PyCF_TYPE_COMMENTS
	PyCFTypeComments = 0x1000
)

// ParseString parses src under the given mode and returns the
// AST root. Filename is used for SyntaxError text.
//
// CPython: Parser/peg_api.c:8 _PyParser_ASTFromString
func ParseString(src, filename string, mode Mode) (ast.Mod, error) {
	return ParseStringFlags(src, filename, mode, 0)
}

// ParseStringFlags is ParseString with compile() Python-level flags
// forwarded into the pegen runtime.
//
// CPython: Parser/peg_api.c:8 _PyParser_ASTFromString
func ParseStringFlags(src, filename string, mode Mode, pyFlags int) (ast.Mod, error) {
	st := lexer.FromString(src, lexerMode(mode))
	st.SetFilename(filename)
	return runParse(st, mode, pegenFlags(pyFlags))
}

// ParseBytes is the bytes-input form: src is run through
// lexer.FromBytes, which honors the PEP 263 coding cookie before
// handing decoded source to the tokenizer. compile() with a bytes
// argument routes through here.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl (bytes branch
// via _Py_SourceAsString)
func ParseBytes(src []byte, filename string, mode Mode) (ast.Mod, error) {
	return ParseBytesFlags(src, filename, mode, 0)
}

// ParseBytesFlags is ParseBytes with compile() Python-level flags
// forwarded into the pegen runtime.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl (bytes branch via _Py_SourceAsString)
func ParseBytesFlags(src []byte, filename string, mode Mode, pyFlags int) (ast.Mod, error) {
	st := lexer.FromBytes(src, lexerMode(mode))
	st.SetFilename(filename)
	return runParse(st, mode, pegenFlags(pyFlags))
}

// Parse reads from r. Useful for tokenize.tokenize() over file
// input where buffered IO is preferable.
//
// CPython: Parser/peg_api.c:31 _PyParser_ASTFromFile
func Parse(r io.Reader, filename string, mode Mode) (ast.Mod, error) {
	st := lexer.FromReader(r, lexerMode(mode))
	st.SetFilename(filename)
	return runParse(st, mode, 0)
}

// pegenFlags maps Python-level PyCF_* compile flags to the pegen
// parser's PyPARSE_* internal flags.
//
// CPython: Parser/peg_api.c:73 compute_parser_flags
func pegenFlags(pyFlags int) int {
	var f int
	if pyFlags&PyCFAllowIncompleteInput != 0 {
		f |= pegen.FlagAllowIncompleteInput
	}
	// CPython: Parser/peg_api.c:795 PyCF_TYPE_COMMENTS → PyPARSE_TYPE_COMMENTS
	if pyFlags&PyCFTypeComments != 0 {
		f |= pegen.FlagTypeComments
	}
	return f
}

func runParse(st *lexer.State, mode Mode, flags int) (ast.Mod, error) {
	// Lexer-recorded errors (PEP 263 cookie / BOM conflicts, non-utf-8
	// source) trump anything pegen would surface: the parser cannot
	// progress past a broken decode. Lift the lexer's structured
	// record into the parser-level *perrors.SyntaxError so the VM
	// boundary can build a Python SyntaxError with lineno/offset/text
	// populated rather than synthesizing one from the message prefix.
	// CPython: Parser/peg_api.c:73 _PyParser_ASTFromString tok->done check
	if e := st.Err(); e != nil {
		// CPython's pegen runtime promotes the lexer's done state to
		// the matching IndentationError / TabError subclass before
		// raising. Parser/pegen_errors.c:69 _Pypegen_tokenizer_error
		// dispatches on E_DEDENT / E_TABSPACE / E_TOODEEP separately
		// from the generic E_SYNTAX path.
		kind := perrors.KindSyntax
		switch st.Done() {
		case lexer.DoneDedent, lexer.DoneToodeep:
			kind = perrors.KindIndentation
		case lexer.DoneTabSpace:
			kind = perrors.KindTab
		case lexer.DoneColumnOverflow:
			kind = perrors.KindOverflow
		}
		return nil, &perrors.SyntaxError{
			Kind: kind,
			Pos: perrors.Pos{
				Lineno:  e.Pos.Line,
				ColOff:  e.Pos.Col,
				EndLine: e.EndPos.Line,
				EndCol:  e.EndPos.Col,
			},
			Filename: st.Filename(),
			Message:  e.Message,
			Text:     e.Text,
		}
	}
	p := pegen.New(st, pegenStartRule(mode), flags)
	node, err := pegen.Dispatch(p, pegenStartRule(mode))
	// CPython emits SyntaxWarnings inline from helpers.c:152
	// _PyTokenizer_parser_warn. gopy stashes them on State.warnings
	// to keep parser/lexer leaf, then drains them here via a package
	// hook that module/_warnings registers at init time.
	if flushErr := st.FlushWarnings(); flushErr != nil {
		return nil, flushErr
	}
	if errors.Is(err, pegen.ErrIncompleteInput) {
		// First pass failed at end-of-source with PyCF_ALLOW_INCOMPLETE_INPUT
		// set. Raise _IncompleteInputError without running the second pass.
		// CPython: Parser/pegen.c:952 _PyPegen_run_parser _is_end_of_source
		return nil, &perrors.SyntaxError{
			Kind:     perrors.KindIncompleteInput,
			Filename: st.Filename(),
			Message:  "incomplete input",
		}
	}
	if errors.Is(err, pegen.ErrParserNotImplemented) {
		// CPython promotes the pinned SyntaxError at the farthest
		// token, and runs _Pypegen_set_syntax_error once after the
		// parser fails so the unclosed-paren / unexpected-EOF /
		// indent / tokenizer-drain refinements override the generic
		// "invalid syntax" caret.
		//
		// CPython: Parser/pegen.c:1136 _PyPegen_run_parser
		// CPython: Parser/pegen_errors.c:413 _Pypegen_set_syntax_error
		p.SetSyntaxError()
		if e := p.PinnedError(); e != nil {
			if e.Text == "" {
				if ts := p.Tokenizer(); ts != nil {
					e.Text = ts.SourceLine(e.Pos.Lineno)
				}
			}
			if e.Filename == "" {
				e.Filename = st.Filename()
			}
			// Populate metadata for traceback.py's _find_keyword_typos.
			// CPython: Parser/pegen.c:902 _PyPegen_set_syntax_error_metadata
			e.SourceText = st.Source()
			e.LastStmtLineno = p.LastStmtLineno()
			e.LastStmtColOff = p.LastStmtColOff()
			// When PyCF_ALLOW_INCOMPLETE_INPUT is set, promote unclosed
			// bracket errors to _IncompleteInputError so codeop's
			// _maybe_compile loop can distinguish "incomplete" from
			// "hard syntax error". The message check mirrors CPython's
			// _Pypegen_check_tokenizer E_EOF + tok->level path.
			//
			// CPython: Parser/peg_api.c:73 compute_parser_flags / PyPARSE_ALLOW_INCOMPLETE_INPUT
			if flags&pegen.FlagAllowIncompleteInput != 0 && p.IsUnclosedBracketError(e) {
				e.Kind = perrors.KindIncompleteInput
			}
			return nil, e
		}
		return nil, ErrParserNotImplemented
	}
	if err != nil {
		return nil, err
	}
	if mod, ok := node.(ast.Mod); ok {
		return mod, nil
	}
	return nil, ErrParserNotImplemented
}

func lexerMode(m Mode) lexer.Mode {
	switch m {
	case ModeSingle:
		return lexer.ModeSingle
	case ModeEval:
		return lexer.ModeEval
	default:
		return lexer.ModeFile
	}
}

func pegenStartRule(m Mode) pegen.StartRule {
	switch m {
	case ModeSingle:
		return pegen.StartSingle
	case ModeEval:
		return pegen.StartEval
	default:
		return pegen.StartFile
	}
}
