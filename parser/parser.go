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
	p := pegen.New(st, pegenStartRule(mode), 0)
	node, err := pegen.Dispatch(p, pegenStartRule(mode))
	if errors.Is(err, pegen.ErrParserNotImplemented) {
		// Real SyntaxError beats the not-implemented sentinel:
		// CPython surfaces the pinned error at the farthest token
		// (Parser/pegen.c:1136 _PyPegen_run_parser).
		if e := p.PinnedError(); e != nil {
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
