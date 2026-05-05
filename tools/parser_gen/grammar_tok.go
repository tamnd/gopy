// Metagrammar tokenizer. Reads python.gram and emits a stream of
// tokens shaped like CPython's tokenize module. The parser in
// grammar_parser.go consumes this stream.
//
// The metagrammar is Python-flavored: indentation matters, '#' is
// comment, '\n' is NEWLINE, but nested in '(' '[' '{' a NEWLINE is
// suppressed. Strings are 'foo' / "foo"; triple-quoted '''...''' /
// """...""" survive across lines for @trailer-style metas.
//
// CPython: Tools/peg_generator/pegen/tokenizer.py + the standard
// library's tokenize module as referenced by grammar_parser.py.

package main

import (
	"fmt"
	"unicode"
)

type gtokKind int

const (
	gtNAME gtokKind = iota + 1
	gtSTRING
	gtNUMBER
	gtOP
	gtNEWLINE
	gtINDENT
	gtDEDENT
	gtENDMARKER
)

type gtok struct {
	Kind gtokKind
	Text string
	Line int
	Col  int
}

func (t gtok) String() string {
	return fmt.Sprintf("%d:%q@%d:%d", t.Kind, t.Text, t.Line, t.Col)
}

type gtokenizer struct {
	src     []byte
	off     int
	line    int
	col     int
	indents []int
	parens  int
	atLine  bool // true at start of a logical line, before INDENT/DEDENT decided
	pending []gtok
	done    bool
}

func newGTokenizer(src []byte) *gtokenizer {
	return &gtokenizer{src: src, line: 1, col: 0, indents: []int{0}, atLine: true}
}

// All drains the tokenizer into a slice. The metagrammar is small
// enough (~1500 lines for python.gram) that holding the full token
// stream is cheaper than wiring lazy fill into the parser.
func (g *gtokenizer) All() ([]gtok, error) {
	var out []gtok
	for {
		t, err := g.next()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		if t.Kind == gtENDMARKER {
			return out, nil
		}
	}
}

func (g *gtokenizer) next() (gtok, error) {
	if t, ok := g.popPending(); ok {
		return t, nil
	}
	if g.done {
		return gtok{Kind: gtENDMARKER, Line: g.line, Col: g.col}, nil
	}
	if g.atLine && g.parens == 0 {
		if err := g.handleIndent(); err != nil {
			return gtok{}, err
		}
		g.atLine = false
		if t, ok := g.popPending(); ok {
			return t, nil
		}
	}
	return g.scanOne()
}

func (g *gtokenizer) popPending() (gtok, bool) {
	if len(g.pending) == 0 {
		return gtok{}, false
	}
	t := g.pending[0]
	g.pending = g.pending[1:]
	return t, true
}

func (g *gtokenizer) scanOne() (gtok, error) {
	for {
		if g.off >= len(g.src) {
			return g.finish(), nil
		}
		c := g.src[g.off]
		if g.skipFiller(c) {
			continue
		}
		if c == '\n' {
			g.off++
			line, col := g.line, g.col
			g.line++
			g.col = 0
			if g.parens > 0 {
				continue
			}
			g.atLine = true
			return gtok{Kind: gtNEWLINE, Text: "\n", Line: line, Col: col}, nil
		}
		if c == '\'' || c == '"' {
			return g.readString()
		}
		if isIdentStart(rune(c)) {
			return g.readName(), nil
		}
		if c >= '0' && c <= '9' {
			return g.readNumber(), nil
		}
		return g.readOp(), nil
	}
}

// skipFiller handles whitespace, comments, and line continuations.
// Returns true if it consumed something and the caller should loop.
func (g *gtokenizer) skipFiller(c byte) bool {
	if c == ' ' || c == '\t' {
		g.off++
		g.col++
		return true
	}
	if c == '#' {
		for g.off < len(g.src) && g.src[g.off] != '\n' {
			g.off++
		}
		return true
	}
	if c == '\\' && g.off+1 < len(g.src) && g.src[g.off+1] == '\n' {
		g.off += 2
		g.line++
		g.col = 0
		return true
	}
	return false
}

func (g *gtokenizer) finish() gtok {
	for len(g.indents) > 1 {
		g.indents = g.indents[:len(g.indents)-1]
		g.pending = append(g.pending, gtok{Kind: gtDEDENT, Line: g.line, Col: 0})
	}
	g.pending = append(g.pending, gtok{Kind: gtENDMARKER, Line: g.line, Col: 0})
	g.done = true
	t := g.pending[0]
	g.pending = g.pending[1:]
	return t
}

func (g *gtokenizer) handleIndent() error {
	// Skip blank-only and comment-only lines.
	for {
		i := g.off
		col := 0
		for i < len(g.src) && (g.src[i] == ' ' || g.src[i] == '\t') {
			if g.src[i] == '\t' {
				col += 8 - (col % 8)
			} else {
				col++
			}
			i++
		}
		if i >= len(g.src) {
			g.off = i
			return nil
		}
		if g.src[i] == '\n' {
			g.off = i + 1
			g.line++
			g.col = 0
			continue
		}
		if g.src[i] == '#' {
			for i < len(g.src) && g.src[i] != '\n' {
				i++
			}
			g.off = i
			continue
		}
		// Real line; emit indent/dedent vs g.indents[top].
		g.off = i
		g.col = col
		top := g.indents[len(g.indents)-1]
		if col > top {
			g.indents = append(g.indents, col)
			g.pending = append(g.pending, gtok{Kind: gtINDENT, Line: g.line, Col: col})
		} else {
			for col < g.indents[len(g.indents)-1] {
				g.indents = g.indents[:len(g.indents)-1]
				g.pending = append(g.pending, gtok{Kind: gtDEDENT, Line: g.line, Col: col})
			}
			if col != g.indents[len(g.indents)-1] {
				return fmt.Errorf("metagrammar: inconsistent indent at line %d", g.line)
			}
		}
		return nil
	}
}

func (g *gtokenizer) readString() (gtok, error) {
	startLine, startCol, startOff := g.line, g.col, g.off
	q := g.src[g.off]
	triple := false
	if g.off+2 < len(g.src) && g.src[g.off+1] == q && g.src[g.off+2] == q {
		triple = true
		g.off += 3
		g.col += 3
	} else {
		g.off++
		g.col++
	}
	for g.off < len(g.src) {
		c := g.src[g.off]
		if c == '\\' && g.off+1 < len(g.src) {
			g.off += 2
			g.col += 2
			continue
		}
		if triple {
			if c == q && g.off+2 < len(g.src) && g.src[g.off+1] == q && g.src[g.off+2] == q {
				g.off += 3
				g.col += 3
				return gtok{Kind: gtSTRING, Text: string(g.src[startOff:g.off]), Line: startLine, Col: startCol}, nil
			}
			if c == '\n' {
				g.line++
				g.col = 0
				g.off++
				continue
			}
			g.off++
			g.col++
			continue
		}
		if c == q {
			g.off++
			g.col++
			return gtok{Kind: gtSTRING, Text: string(g.src[startOff:g.off]), Line: startLine, Col: startCol}, nil
		}
		if c == '\n' {
			return gtok{}, fmt.Errorf("metagrammar: unterminated string at line %d", startLine)
		}
		g.off++
		g.col++
	}
	return gtok{}, fmt.Errorf("metagrammar: EOF inside string at line %d", startLine)
}

func (g *gtokenizer) readName() gtok {
	startLine, startCol, startOff := g.line, g.col, g.off
	for g.off < len(g.src) && isIdentCont(rune(g.src[g.off])) {
		g.off++
		g.col++
	}
	return gtok{Kind: gtNAME, Text: string(g.src[startOff:g.off]), Line: startLine, Col: startCol}
}

func (g *gtokenizer) readNumber() gtok {
	startLine, startCol, startOff := g.line, g.col, g.off
	for g.off < len(g.src) && (g.src[g.off] >= '0' && g.src[g.off] <= '9') {
		g.off++
		g.col++
	}
	return gtok{Kind: gtNUMBER, Text: string(g.src[startOff:g.off]), Line: startLine, Col: startCol}
}

// readOp reads an operator. Action and annotation bodies between
// '{...}' and '[...]' are not pre-tokenised here; the parser's
// targetAtoms scanner reads raw bytes for those.
func (g *gtokenizer) readOp() gtok {
	startLine, startCol := g.line, g.col
	c := g.src[g.off]
	text := string(c)
	g.off++
	g.col++
	switch c {
	case '(', '[', '{':
		g.parens++
	case ')', ']', '}':
		if g.parens > 0 {
			g.parens--
		}
	}
	return gtok{Kind: gtOP, Text: text, Line: startLine, Col: startCol}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentCont(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
