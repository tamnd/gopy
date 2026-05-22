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

// ParseString parses src under the given mode and returns the
// AST root. Filename is used for SyntaxError text.
//
// CPython: Parser/peg_api.c:8 _PyParser_ASTFromString
func ParseString(src, filename string, mode Mode) (ast.Mod, error) {
	st := lexer.FromString(src, lexerMode(mode))
	st.SetFilename(filename)
	return runParse(st, mode)
}

// ParseBytes is the bytes-input form: src is run through
// lexer.FromBytes, which honors the PEP 263 coding cookie before
// handing decoded source to the tokenizer. compile() with a bytes
// argument routes through here.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl (bytes branch
// via _Py_SourceAsString)
func ParseBytes(src []byte, filename string, mode Mode) (ast.Mod, error) {
	st := lexer.FromBytes(src, lexerMode(mode))
	st.SetFilename(filename)
	return runParse(st, mode)
}

// Parse reads from r. Useful for tokenize.tokenize() over file
// input where buffered IO is preferable.
//
// CPython: Parser/peg_api.c:31 _PyParser_ASTFromFile
func Parse(r io.Reader, filename string, mode Mode) (ast.Mod, error) {
	st := lexer.FromReader(r, lexerMode(mode))
	st.SetFilename(filename)
	return runParse(st, mode)
}

func runParse(st *lexer.State, mode Mode) (ast.Mod, error) {
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
	p := pegen.New(st, pegenStartRule(mode), 0)
	node, err := pegen.Dispatch(p, pegenStartRule(mode))
	// CPython emits SyntaxWarnings inline from helpers.c:152
	// _PyTokenizer_parser_warn. gopy stashes them on State.warnings
	// to keep parser/lexer leaf, then drains them here via a package
	// hook that module/_warnings registers at init time.
	st.FlushWarnings()
	if errors.Is(err, pegen.ErrParserNotImplemented) {
		// Real SyntaxError beats the not-implemented sentinel:
		// CPython surfaces the pinned error at the farthest token
		// (Parser/pegen.c:1136 _PyPegen_run_parser).
		if e := p.PinnedError(); e != nil {
			return nil, e
		}
		// No rule called RaiseSyntaxError, but the parse still
		// failed; pin a generic SyntaxError at the farthest token
		// the parser reached so callers like compile() can populate
		// lineno / offset / filename / text on the exception.
		// CPython: Parser/pegen.c:1136 _PyPegen_run_parser uses
		// farthest_pos to pick the caret when no rule raised.
		if t := p.FarthestToken(); t != nil {
			text := ""
			if ts := p.Tokenizer(); ts != nil {
				text = ts.SourceLine(t.Lineno)
			}
			return nil, &perrors.SyntaxError{
				Kind: perrors.KindSyntax,
				Pos: perrors.Pos{
					Lineno:  t.Lineno,
					ColOff:  t.ColOff,
					EndLine: t.EndLine,
					EndCol:  t.EndCol,
				},
				Filename: st.Filename(),
				Message:  "invalid syntax",
				Text:     text,
			}
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
