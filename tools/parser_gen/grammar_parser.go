// Recursive-descent parser for the metagrammar. Mirrors the rules
// in cpython/Tools/peg_generator/pegen/grammar_parser.py one-to-one.
// The metagrammar is fixed and small; we hand-write the parser
// rather than self-bootstrapping.
//
// CPython: Tools/peg_generator/pegen/grammar_parser.py

package main

import (
	"fmt"
	"strings"
)

type gparser struct {
	toks []gtok
	pos  int
}

// ParseGrammar tokenises src and runs the metagrammar parser. The
// returned Grammar contains every rule in declaration order. Errors
// pinpoint the line and column of the first unexpected token.
func ParseGrammar(src []byte) (*Grammar, error) {
	toks, err := newGTokenizer(src).All()
	if err != nil {
		return nil, err
	}
	gp := &gparser{toks: toks}
	g, err := gp.grammar()
	if err != nil {
		return nil, err
	}
	gp.skipNewlines()
	if t := gp.peek(); t.Kind != gtENDMARKER {
		return nil, gp.errAt(t, "expected ENDMARKER, got %q", t.Text)
	}
	return g, nil
}

func (p *gparser) peek() gtok        { return p.toks[p.pos] }
func (p *gparser) peekAt(n int) gtok { return p.toks[p.pos+n] }
func (p *gparser) advance() gtok     { t := p.toks[p.pos]; p.pos++; return t }
func (p *gparser) save() int         { return p.pos }
func (p *gparser) restore(m int)     { p.pos = m }
func (p *gparser) skipNewlines() {
	for p.peek().Kind == gtNEWLINE {
		p.pos++
	}
}

func (p *gparser) errAt(t gtok, format string, args ...any) error {
	return fmt.Errorf("metagrammar: %s at line %d col %d", fmt.Sprintf(format, args...), t.Line, t.Col)
}

func (p *gparser) tryOp(op string) bool {
	t := p.peek()
	if t.Kind == gtOP && t.Text == op {
		p.pos++
		return true
	}
	return false
}

func (p *gparser) tryName(name string) bool {
	t := p.peek()
	if t.Kind == gtNAME && t.Text == name {
		p.pos++
		return true
	}
	return false
}

// grammar: metas? rule+
func (p *gparser) grammar() (*Grammar, error) {
	g := &Grammar{}
	for p.peek().Kind == gtOP && p.peek().Text == "@" {
		m, err := p.meta()
		if err != nil {
			return nil, err
		}
		g.Metas = append(g.Metas, m)
	}
	for {
		p.skipNewlines()
		if p.peek().Kind != gtNAME {
			break
		}
		r, err := p.rule()
		if err != nil {
			return nil, err
		}
		g.Rules = append(g.Rules, r)
	}
	return g, nil
}

// meta: '@' NAME (NAME | STRING)? NEWLINE
func (p *gparser) meta() (Meta, error) {
	if !p.tryOp("@") {
		return Meta{}, p.errAt(p.peek(), "expected @")
	}
	t := p.advance()
	if t.Kind != gtNAME {
		return Meta{}, p.errAt(t, "expected NAME after @")
	}
	m := Meta{Name: t.Text}
	t2 := p.peek()
	switch t2.Kind {
	case gtNEWLINE:
		p.pos++
		return m, nil
	case gtNAME:
		p.pos++
		m.Value = t2.Text
	case gtSTRING:
		p.pos++
		m.Value = t2.Text
	default:
		return Meta{}, p.errAt(t2, "expected NAME, STRING, or NEWLINE after @%s", m.Name)
	}
	if p.peek().Kind != gtNEWLINE {
		return Meta{}, p.errAt(p.peek(), "expected NEWLINE after @%s value", m.Name)
	}
	p.pos++
	return m, nil
}

// rule: NAME ('[' targetAtoms ']')? ('(' 'memo' ')')? ':' (alts NEWLINE INDENT moreAlts DEDENT | NEWLINE INDENT moreAlts DEDENT | alts NEWLINE)
func (p *gparser) rule() (*Rule, error) {
	t := p.advance()
	if t.Kind != gtNAME {
		return nil, p.errAt(t, "expected rule name")
	}
	r := &Rule{Name: t.Text}
	if p.tryOp("[") {
		body, err := p.targetAtoms("]")
		if err != nil {
			return nil, err
		}
		r.Type = body
	}
	if p.tryOp("(") {
		if !p.tryName("memo") {
			return nil, p.errAt(p.peek(), "expected 'memo' in (memo)")
		}
		if !p.tryOp(")") {
			return nil, p.errAt(p.peek(), "expected ')' after memo")
		}
		r.Memo = true
	}
	if !p.tryOp(":") {
		return nil, p.errAt(p.peek(), "expected ':' after rule name %q", r.Name)
	}
	// Three forms: inline alts then NEWLINE; bare NEWLINE then INDENT block;
	// or alts then NEWLINE then INDENT extension.
	if p.peek().Kind == gtNEWLINE {
		p.pos++
		if p.peek().Kind != gtINDENT {
			return nil, p.errAt(p.peek(), "expected INDENT after rule %q header", r.Name)
		}
		p.pos++
		alts, err := p.moreAlts()
		if err != nil {
			return nil, err
		}
		r.RHS = alts
		if p.peek().Kind != gtDEDENT {
			return nil, p.errAt(p.peek(), "expected DEDENT closing rule %q", r.Name)
		}
		p.pos++
		return r, nil
	}
	alts, err := p.alts()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != gtNEWLINE {
		return nil, p.errAt(p.peek(), "expected NEWLINE after rule %q alts", r.Name)
	}
	p.pos++
	if p.peek().Kind == gtINDENT {
		p.pos++
		extra, err := p.moreAlts()
		if err != nil {
			return nil, err
		}
		alts.Alts = append(alts.Alts, extra.Alts...)
		if p.peek().Kind != gtDEDENT {
			return nil, p.errAt(p.peek(), "expected DEDENT closing rule %q", r.Name)
		}
		p.pos++
	}
	r.RHS = alts
	return r, nil
}

// moreAlts: ('|' alts NEWLINE)+
func (p *gparser) moreAlts() (*RHS, error) {
	rhs := &RHS{}
	for p.tryOp("|") {
		alts, err := p.alts()
		if err != nil {
			return nil, err
		}
		rhs.Alts = append(rhs.Alts, alts.Alts...)
		if p.peek().Kind != gtNEWLINE {
			return nil, p.errAt(p.peek(), "expected NEWLINE after | alts")
		}
		p.pos++
	}
	if len(rhs.Alts) == 0 {
		return nil, p.errAt(p.peek(), "expected at least one '| alts NEWLINE'")
	}
	return rhs, nil
}

// alts: alt ('|' alt)*
func (p *gparser) alts() (*RHS, error) {
	rhs := &RHS{}
	a, err := p.alt()
	if err != nil {
		return nil, err
	}
	rhs.Alts = append(rhs.Alts, a)
	// Only consume bars that introduce another inline alt, not the
	// leading '|' of the next moreAlts line.
	for p.peek().Kind != gtNEWLINE && p.tryOp("|") {
		a, err := p.alt()
		if err != nil {
			return nil, err
		}
		rhs.Alts = append(rhs.Alts, a)
	}
	return rhs, nil
}

// alt: items ('$')? action?
func (p *gparser) alt() (*Alt, error) {
	a := &Alt{Icut: -1}
	items, err := p.items(a)
	if err != nil {
		return nil, err
	}
	a.Items = items
	if p.tryOp("$") {
		a.Items = append(a.Items, &NamedItem{Item: &NameLeaf{Value: "ENDMARKER"}})
	}
	if p.peek().Kind == gtOP && p.peek().Text == "{" {
		p.pos++ // consume the '{'
		body, err := p.targetAtoms("}")
		if err != nil {
			return nil, err
		}
		a.Action = body
	}
	return a, nil
}

// items: namedItem+
func (p *gparser) items(a *Alt) ([]*NamedItem, error) {
	var out []*NamedItem
	for {
		t := p.peek()
		if t.Kind == gtNEWLINE || t.Kind == gtENDMARKER {
			break
		}
		if t.Kind == gtOP {
			switch t.Text {
			case "|", "$", "{", ")", "]", "}":
				return out, nil
			}
		}
		ni, err := p.namedItem()
		if err != nil {
			return nil, err
		}
		// Cut markers turn into a Cut item; record icut for the alt.
		if _, ok := ni.Item.(*Cut); ok && a.Icut == -1 {
			a.Icut = len(out)
		}
		out = append(out, ni)
	}
	if len(out) == 0 {
		return nil, p.errAt(p.peek(), "expected at least one item")
	}
	return out, nil
}

// namedItem: NAME annotation? '=' item | item | forced | lookahead
func (p *gparser) namedItem() (*NamedItem, error) {
	mark := p.save()
	if p.peek().Kind == gtNAME {
		// Try NAME [annot] '=' item.
		nm := p.advance()
		var ann string
		if p.tryOp("[") {
			body, err := p.targetAtoms("]")
			if err != nil {
				return nil, err
			}
			ann = body
		}
		if p.tryOp("=") {
			it, err := p.item()
			if err != nil {
				return nil, err
			}
			return &NamedItem{Name: nm.Text, Item: it, Type: ann}, nil
		}
		// Roll back; that NAME starts a plain item.
		p.restore(mark)
	}
	// forced: && atom
	if p.peek().Kind == gtOP && p.peek().Text == "&" && p.peekAt(1).Kind == gtOP && p.peekAt(1).Text == "&" {
		p.pos += 2
		atom, err := p.atom()
		if err != nil {
			return nil, err
		}
		return &NamedItem{Item: &Forced{Node: atom}}, nil
	}
	// lookahead: & atom | ! atom | ~
	if p.peek().Kind == gtOP {
		switch p.peek().Text {
		case "&":
			p.pos++
			atom, err := p.atom()
			if err != nil {
				return nil, err
			}
			return &NamedItem{Item: &PositiveLookahead{Node: atom}}, nil
		case "!":
			p.pos++
			atom, err := p.atom()
			if err != nil {
				return nil, err
			}
			return &NamedItem{Item: &NegativeLookahead{Node: atom}}, nil
		case "~":
			p.pos++
			return &NamedItem{Item: &Cut{}}, nil
		}
	}
	it, err := p.item()
	if err != nil {
		return nil, err
	}
	return &NamedItem{Item: it}, nil
}

// item: '[' alts ']' | atom '?' | atom '*' | atom '+' | atom '.' atom '+' | atom
func (p *gparser) item() (Item, error) {
	if p.tryOp("[") {
		alts, err := p.alts()
		if err != nil {
			return nil, err
		}
		if !p.tryOp("]") {
			return nil, p.errAt(p.peek(), "expected ']' to close optional")
		}
		return &Opt{Item: alts}, nil
	}
	atom, err := p.atom()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind == gtOP {
		switch p.peek().Text {
		case "?":
			p.pos++
			return &Opt{Item: atom}, nil
		case "*":
			p.pos++
			return &Repeat0{Item: atom}, nil
		case "+":
			p.pos++
			return &Repeat1{Item: atom}, nil
		case ".":
			// gather: sep '.' node '+'
			save := p.save()
			p.pos++
			node, nerr := p.atom()
			if nerr == nil && p.tryOp("+") {
				return &Gather{Sep: atom, Node: node}, nil
			}
			p.restore(save)
		}
	}
	return atom, nil
}

// atom: '(' alts ')' | NAME | STRING
func (p *gparser) atom() (Item, error) {
	t := p.peek()
	switch {
	case t.Kind == gtOP && t.Text == "(":
		p.pos++
		alts, err := p.alts()
		if err != nil {
			return nil, err
		}
		if !p.tryOp(")") {
			return nil, p.errAt(p.peek(), "expected ')'")
		}
		return &Group{RHS: alts}, nil
	case t.Kind == gtNAME:
		p.pos++
		return &NameLeaf{Value: t.Text}, nil
	case t.Kind == gtSTRING:
		p.pos++
		return &StringLeaf{Value: t.Text}, nil
	}
	return nil, p.errAt(t, "expected atom, got %q", t.Text)
}

// targetAtoms reads tokens until the matching closer ('}' or ']')
// is seen at depth zero, joining their text with single spaces. The
// opener has already been consumed by the caller.
//
// CPython: Tools/peg_generator/pegen/grammar_parser.py target_atoms
func (p *gparser) targetAtoms(closer string) (string, error) {
	var parts []string
	depth := 0
	for {
		t := p.peek()
		if t.Kind == gtENDMARKER {
			return "", p.errAt(t, "unterminated %s block", closer)
		}
		if t.Kind == gtOP && depth == 0 && t.Text == closer {
			p.pos++
			return strings.Join(parts, " "), nil
		}
		if t.Kind == gtOP {
			switch t.Text {
			case "{", "[", "(":
				depth++
			case "}", "]", ")":
				if depth > 0 {
					depth--
				}
			}
		}
		// NEWLINE / INDENT / DEDENT inside an action are noise; skip.
		if t.Kind == gtNEWLINE || t.Kind == gtINDENT || t.Kind == gtDEDENT {
			p.pos++
			continue
		}
		parts = append(parts, t.Text)
		p.pos++
	}
}
