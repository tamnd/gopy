// DSL parser for Python/bytecodes.c. Produces a typed AST of inst /
// op / macro / family / pseudo / label definitions. Statement-level
// parsing inside instruction bodies is deferred: bodies survive as a
// raw token slice the action translator consumes in B6.
//
// CPython: Tools/cases_generator/parsing.py:333 Parser

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Defs is the parsed bytecodes.c top-level. The order field
// preserves source order so the emitter can keep output stable.
type Defs struct {
	Order   []Def
	Insts   []*InstDef
	Macros  []*MacroDef
	Famils  []*FamilyDef
	Pseudos []*PseudoDef
	Labels  []*LabelDef
}

// Def is the closed sum of top-level declarations.
type Def interface{ defKind() string }

// StackEffect mirrors parsing.StackEffect: a named stack slot with
// optional :type annotation and optional [size] for variadic slots.
type StackEffect struct {
	Name string
	Type string // optional, may end in " *"
	Size string // optional; mutually exclusive with Type
}

// CacheEffect mirrors parsing.CacheEffect: an inline cache slot of
// `size` code units.
type CacheEffect struct {
	Name string
	Size int
}

// Input is one element of an instruction's input signature.
// Either StackEffect or CacheEffect.
type Input struct {
	Stack *StackEffect
	Cache *CacheEffect
}

// Output is one element of an instruction's output signature.
type Output struct {
	Stack *StackEffect
}

// InstDef is a real instruction or fragment: `inst(NAME, (in -- out)) { body }`
// or `op(NAME, (in -- out)) { body }`. Kind discriminates the two.
type InstDef struct {
	Annotations []string
	Kind        string // "inst" or "op"
	Name        string
	Inputs      []Input
	Outputs     []Output
	Body        []dslTok // raw body tokens (incl. braces? no: between braces)
	Line        int
}

func (*InstDef) defKind() string { return "inst" }

// UOp is one element of a `macro(NAME) = OP1 + OP2 + cache/N;` rhs.
// Either an opcode name or a cache effect.
type UOp struct {
	Name  string
	Cache *CacheEffect
}

// MacroDef is `macro(NAME) = uop + uop + ...;`.
type MacroDef struct {
	Name string
	UOps []UOp
	Line int
}

func (*MacroDef) defKind() string { return "macro" }

// FamilyDef is `family(NAME, size) = { MEMBER, MEMBER, ... };`.
type FamilyDef struct {
	Name    string
	Size    string
	Members []string
	Line    int
}

func (*FamilyDef) defKind() string { return "family" }

// PseudoDef is `pseudo(NAME, (in -- out), (flags)) = { TARGETS };`.
type PseudoDef struct {
	Name       string
	Inputs     []Input
	Outputs    []Output
	Flags      []string
	Targets    []string
	AsSequence bool
	Line       int
}

func (*PseudoDef) defKind() string { return "pseudo" }

// LabelDef is `label(NAME) { body }`, optionally `spilled label(...)`.
type LabelDef struct {
	Name    string
	Spilled bool
	Body    []dslTok
	Line    int
}

func (*LabelDef) defKind() string { return "label" }

type parser struct {
	toks []dslTok
	pos  int
}

func parse(toks []dslTok) (*Defs, error) {
	p := &parser{toks: toks}
	d := &Defs{}
	for p.pos < len(p.toks) {
		def, err := p.definition()
		if err != nil {
			return d, err
		}
		if def == nil {
			break
		}
		d.Order = append(d.Order, def)
		switch v := def.(type) {
		case *InstDef:
			d.Insts = append(d.Insts, v)
		case *MacroDef:
			d.Macros = append(d.Macros, v)
		case *FamilyDef:
			d.Famils = append(d.Famils, v)
		case *PseudoDef:
			d.Pseudos = append(d.Pseudos, v)
		case *LabelDef:
			d.Labels = append(d.Labels, v)
		}
	}
	return d, nil
}

// ParseBytecodes is the public entry: tokenize, slice the
// `// BEGIN BYTECODES //` ... `// END BYTECODES //` window, parse.
func ParseBytecodes(src string) (*Defs, error) {
	body, err := bytecodesWindow(src)
	if err != nil {
		return nil, err
	}
	toks, err := tokenize(body)
	if err != nil {
		return nil, err
	}
	return parse(toks)
}

// bytecodesWindow extracts the body between `// BEGIN BYTECODES //`
// and `// END BYTECODES //`. Mirrors parsing.py's __main__ block.
func bytecodesWindow(src string) (string, error) {
	const beginMark = "\n// BEGIN BYTECODES //\n"
	const endMark = "\n// END BYTECODES //\n"
	bi := strings.Index(src, beginMark)
	if bi < 0 {
		return "", fmt.Errorf("missing %q marker", beginMark)
	}
	bi += len(beginMark)
	ei := strings.Index(src[bi:], endMark)
	if ei < 0 {
		return "", fmt.Errorf("missing %q marker", endMark)
	}
	return src[bi : bi+ei], nil
}

func (p *parser) peek() *dslTok {
	for p.pos < len(p.toks) && p.toks[p.pos].Kind == tokNewline {
		p.pos++
	}
	if p.pos >= len(p.toks) {
		return nil
	}
	return &p.toks[p.pos]
}

func (p *parser) next() {
	if p.peek() != nil {
		p.pos++
	}
}

func (p *parser) save() int       { return p.pos }
func (p *parser) restore(pos int) { p.pos = pos }
func (p *parser) eof() bool       { return p.pos >= len(p.toks) }
func (p *parser) accept(k tokKind) *dslTok {
	if t := p.peek(); t != nil && t.Kind == k {
		p.pos++
		return t
	}
	return nil
}

func (p *parser) acceptIdentText(text string) *dslTok {
	if t := p.peek(); t != nil && t.Kind == tokIdentifier && t.Text == text {
		p.pos++
		return t
	}
	return nil
}

func (p *parser) require(k tokKind) (*dslTok, error) {
	if t := p.accept(k); t != nil {
		return t, nil
	}
	got := p.peek()
	if got == nil {
		return nil, fmt.Errorf("expected %s, got EOF", k)
	}
	return nil, fmt.Errorf("line %d col %d: expected %s, got %s (%q)", got.Line, got.Col, k, got.Kind, got.Text)
}

func (p *parser) definition() (Def, error) {
	if p.eof() {
		return nil, nil
	}
	if m, err := p.tryMacroDef(); m != nil || err != nil {
		return m, err
	}
	if f, err := p.tryFamilyDef(); f != nil || err != nil {
		return f, err
	}
	if ps, err := p.tryPseudoDef(); ps != nil || err != nil {
		return ps, err
	}
	if l, err := p.tryLabelDef(); l != nil || err != nil {
		return l, err
	}
	if i, err := p.tryInstDef(); i != nil || err != nil {
		return i, err
	}
	// Anything else at top level is unexpected.
	t := p.peek()
	if t == nil {
		return nil, nil
	}
	return nil, fmt.Errorf("line %d col %d: unexpected %s (%q) at top level", t.Line, t.Col, t.Kind, t.Text)
}

func (p *parser) tryInstDef() (*InstDef, error) {
	saved := p.save()
	annotations := []string{}
	for {
		if a := p.accept(tokAnnotation); a != nil {
			text := a.Text
			if text == "replicate" {
				if _, err := p.require(tokLParen); err != nil {
					return nil, err
				}
				num, err := p.require(tokNumber)
				if err != nil {
					return nil, err
				}
				if _, err := p.require(tokRParen); err != nil {
					return nil, err
				}
				text = fmt.Sprintf("replicate(%s)", num.Text)
			}
			annotations = append(annotations, text)
			continue
		}
		break
	}
	var kind string
	t := p.peek()
	switch {
	case t == nil:
		p.restore(saved)
		return nil, nil
	case t.Kind == tokInst:
		kind = "inst"
		p.next()
	case t.Kind == tokOp:
		kind = "op"
		p.next()
	default:
		p.restore(saved)
		return nil, nil
	}
	startLine := t.Line
	if _, err := p.require(tokLParen); err != nil {
		return nil, err
	}
	nameT, err := p.require(tokIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := p.require(tokComma); err != nil {
		return nil, err
	}
	inputs, outputs, err := p.ioEffect()
	if err != nil {
		return nil, err
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, err
	}
	body, err := p.collectBraceBody()
	if err != nil {
		return nil, err
	}
	return &InstDef{
		Annotations: annotations,
		Kind:        kind,
		Name:        nameT.Text,
		Inputs:      inputs,
		Outputs:     outputs,
		Body:        body,
		Line:        startLine,
	}, nil
}

func (p *parser) ioEffect() ([]Input, []Output, error) {
	if _, err := p.require(tokLParen); err != nil {
		return nil, nil, err
	}
	inputs, err := p.parseInputs()
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.require(tokMinusMinus); err != nil {
		return nil, nil, err
	}
	outputs, err := p.parseOutputs()
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, nil, err
	}
	return inputs, outputs, nil
}

func (p *parser) parseInputs() ([]Input, error) {
	var inputs []Input
	for {
		t := p.peek()
		if t == nil || t.Kind == tokMinusMinus {
			return inputs, nil
		}
		in, err := p.parseInput()
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, in)
		if p.accept(tokComma) == nil {
			return inputs, nil
		}
	}
}

func (p *parser) parseInput() (Input, error) {
	// Either IDENT '/' [SIGN] NUMBER (cache) or IDENT [':' TYPE [*]] [ '[' EXPR ']' ] (stack)
	saved := p.save()
	idT, err := p.require(tokIdentifier)
	if err != nil {
		return Input{}, err
	}
	if p.accept(tokDivide) != nil {
		// cache effect
		sign := 1
		if p.accept(tokMinus) != nil {
			sign = -1
		}
		num, err := p.require(tokNumber)
		if err != nil {
			return Input{}, err
		}
		n, perr := strconv.Atoi(num.Text)
		if perr != nil {
			return Input{}, fmt.Errorf("line %d: cache size %q not an integer", num.Line, num.Text)
		}
		return Input{Cache: &CacheEffect{Name: idT.Text, Size: sign * n}}, nil
	}
	// stack effect
	p.restore(saved)
	se, err := p.parseStackEffect()
	if err != nil {
		return Input{}, err
	}
	return Input{Stack: se}, nil
}

func (p *parser) parseOutputs() ([]Output, error) {
	var outputs []Output
	for {
		t := p.peek()
		if t == nil || t.Kind == tokRParen {
			return outputs, nil
		}
		se, err := p.parseStackEffect()
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, Output{Stack: se})
		if p.accept(tokComma) == nil {
			return outputs, nil
		}
	}
}

func (p *parser) parseStackEffect() (*StackEffect, error) {
	idT, err := p.require(tokIdentifier)
	if err != nil {
		return nil, err
	}
	se := &StackEffect{Name: idT.Text}
	if p.accept(tokColon) != nil {
		typeT, err := p.require(tokIdentifier)
		if err != nil {
			return nil, err
		}
		se.Type = strings.TrimSpace(typeT.Text)
		if p.accept(tokTimes) != nil {
			se.Type += " *"
		}
	}
	if p.accept(tokLBracket) != nil {
		if se.Type != "" {
			return nil, fmt.Errorf("line %d: stack-effect %q has both type and size", idT.Line, idT.Text)
		}
		expr, err := p.collectExprUntil(tokRBracket)
		if err != nil {
			return nil, err
		}
		if _, err := p.require(tokRBracket); err != nil {
			return nil, err
		}
		se.Size = strings.TrimSpace(tokensText(expr))
	}
	return se, nil
}

// collectExprUntil collects tokens up to (but not consuming) the
// terminator, balancing nested () and [].
func (p *parser) collectExprUntil(term tokKind) ([]dslTok, error) {
	var out []dslTok
	level := 0
	for {
		t := p.peek()
		if t == nil {
			return nil, fmt.Errorf("unexpected EOF in expression")
		}
		if level == 0 && t.Kind == term {
			return out, nil
		}
		switch t.Kind {
		case tokLParen, tokLBracket:
			level++
		case tokRParen, tokRBracket:
			level--
			if level < 0 {
				return out, nil
			}
		}
		out = append(out, *t)
		p.next()
	}
}

// collectBraceBody collects tokens between matching { and }, returning
// the inner tokens (braces consumed but not included).
func (p *parser) collectBraceBody() ([]dslTok, error) {
	if _, err := p.require(tokLBrace); err != nil {
		return nil, err
	}
	var body []dslTok
	level := 1
	for level > 0 {
		t := p.peek()
		if t == nil {
			return nil, fmt.Errorf("unexpected EOF in body")
		}
		switch t.Kind {
		case tokLBrace:
			level++
		case tokRBrace:
			level--
			if level == 0 {
				p.next()
				return body, nil
			}
		}
		body = append(body, *t)
		p.next()
	}
	return body, nil
}

func (p *parser) tryMacroDef() (*MacroDef, error) {
	if p.peek() == nil || p.peek().Kind != tokMacro {
		return nil, nil
	}
	saved := p.save()
	startLine := p.peek().Line
	p.next() // 'macro'
	if _, err := p.require(tokLParen); err != nil {
		return nil, err
	}
	nameT, err := p.require(tokIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, err
	}
	if _, err := p.require(tokEquals); err != nil {
		return nil, err
	}
	uops, err := p.parseUOps()
	if err != nil {
		return nil, err
	}
	if _, err := p.require(tokSemi); err != nil {
		return nil, err
	}
	_ = saved
	return &MacroDef{Name: nameT.Text, UOps: uops, Line: startLine}, nil
}

func (p *parser) parseUOps() ([]UOp, error) {
	var out []UOp
	for {
		idT, err := p.require(tokIdentifier)
		if err != nil {
			return nil, err
		}
		if p.accept(tokDivide) != nil {
			sign := 1
			if p.accept(tokMinus) != nil {
				sign = -1
			}
			num, err := p.require(tokNumber)
			if err != nil {
				return nil, err
			}
			n, perr := strconv.Atoi(num.Text)
			if perr != nil {
				return nil, fmt.Errorf("cache size %q not int", num.Text)
			}
			out = append(out, UOp{Cache: &CacheEffect{Name: idT.Text, Size: sign * n}})
		} else {
			out = append(out, UOp{Name: idT.Text})
		}
		if p.accept(tokPlus) == nil {
			return out, nil
		}
	}
}

func (p *parser) tryFamilyDef() (*FamilyDef, error) {
	saved := p.save()
	idT := p.acceptIdentText("family")
	if idT == nil {
		return nil, nil
	}
	startLine := idT.Line
	if _, err := p.require(tokLParen); err != nil {
		return nil, err
	}
	nameT, err := p.require(tokIdentifier)
	if err != nil {
		return nil, err
	}
	size := ""
	if p.accept(tokComma) != nil {
		if t := p.accept(tokIdentifier); t != nil {
			size = t.Text
		} else if t := p.accept(tokNumber); t != nil {
			size = t.Text
		} else {
			return nil, fmt.Errorf("family %s: expected size identifier or number", nameT.Text)
		}
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, err
	}
	if _, err := p.require(tokEquals); err != nil {
		return nil, err
	}
	if _, err := p.require(tokLBrace); err != nil {
		return nil, err
	}
	members := p.parseMemberList(tokRBrace)
	if _, err := p.require(tokRBrace); err != nil {
		return nil, err
	}
	if _, err := p.require(tokSemi); err != nil {
		return nil, err
	}
	_ = saved
	return &FamilyDef{Name: nameT.Text, Size: size, Members: members, Line: startLine}, nil
}

func (p *parser) parseMemberList(_ tokKind) []string {
	var members []string
	idT := p.accept(tokIdentifier)
	if idT == nil {
		return members
	}
	members = append(members, idT.Text)
	for p.accept(tokComma) != nil {
		t := p.accept(tokIdentifier)
		if t == nil {
			return members // trailing comma
		}
		members = append(members, t.Text)
	}
	return members
}

func (p *parser) tryPseudoDef() (*PseudoDef, error) {
	saved := p.save()
	idT := p.acceptIdentText("pseudo")
	if idT == nil {
		return nil, nil
	}
	startLine := idT.Line
	if _, err := p.require(tokLParen); err != nil {
		return nil, err
	}
	nameT, err := p.require(tokIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := p.require(tokComma); err != nil {
		return nil, err
	}
	inputs, outputs, err := p.ioEffect()
	if err != nil {
		return nil, err
	}
	flags := []string{}
	if p.accept(tokComma) != nil {
		flags, err = p.parseFlags()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, err
	}
	if _, err := p.require(tokEquals); err != nil {
		return nil, err
	}
	asSequence := false
	closing := tokRBrace
	switch {
	case p.accept(tokLBrace) != nil:
	case p.accept(tokLBracket) != nil:
		asSequence = true
		closing = tokRBracket
	default:
		return nil, fmt.Errorf("pseudo %s: expected { or [", nameT.Text)
	}
	targets := p.parseMemberList(closing)
	if _, err := p.require(closing); err != nil {
		return nil, err
	}
	if _, err := p.require(tokSemi); err != nil {
		return nil, err
	}
	_ = saved
	return &PseudoDef{
		Name: nameT.Text, Inputs: inputs, Outputs: outputs,
		Flags: flags, Targets: targets, AsSequence: asSequence, Line: startLine,
	}, nil
}

func (p *parser) parseFlags() ([]string, error) {
	if p.accept(tokLParen) == nil {
		return nil, nil
	}
	var flags []string
	if t := p.accept(tokIdentifier); t != nil {
		flags = append(flags, t.Text)
		for p.accept(tokComma) != nil {
			t := p.accept(tokIdentifier)
			if t == nil {
				break
			}
			flags = append(flags, t.Text)
		}
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, err
	}
	return flags, nil
}

func (p *parser) tryLabelDef() (*LabelDef, error) {
	saved := p.save()
	spilled := p.accept(tokSpilled) != nil
	if p.accept(tokLabel) == nil {
		p.restore(saved)
		return nil, nil
	}
	if _, err := p.require(tokLParen); err != nil {
		return nil, err
	}
	nameT, err := p.require(tokIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := p.require(tokRParen); err != nil {
		return nil, err
	}
	body, err := p.collectBraceBody()
	if err != nil {
		return nil, err
	}
	return &LabelDef{Name: nameT.Text, Spilled: spilled, Body: body, Line: nameT.Line}, nil
}

func tokensText(toks []dslTok) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text)
	}
	return b.String()
}
