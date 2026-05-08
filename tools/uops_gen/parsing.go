// Parser for bytecodes.inst. Builds an AST of inst / op / macro /
// family / pseudo / label declarations from a Tokenize stream.
//
// CPython: Tools/cases_generator/parsing.py
package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Context records the half-open token range a parsed Node spans plus
// the owning Parser, mirroring parsing.py's Context NamedTuple.
//
// CPython: Tools/cases_generator/parsing.py:32-39 Context
type Context struct {
	Begin int
	End   int
	Owner *Parser
}

// Node is the common base for every AST node. Holds optional Context
// after the contextual() wrapper succeeds.
//
// CPython: Tools/cases_generator/parsing.py:42-69 Node
type Node interface {
	setContext(Context)
	GetContext() *Context
}

// node is the embedded base struct providing context plumbing for
// every concrete Node implementation.
type node struct {
	Context *Context
}

func (n *node) setContext(c Context) { n.Context = &c }
func (n *node) GetContext() *Context { return n.Context }
func (n *node) Tokens() []Token {
	if n.Context == nil {
		return nil
	}
	return n.Context.Owner.Tokens[n.Context.Begin:n.Context.End]
}

func (n *node) Text() string {
	if n.Context == nil {
		return ""
	}
	return ToText(n.Context.Owner.Tokens[n.Context.Begin:n.Context.End], 0)
}

func (n *node) FirstToken() Token {
	return n.Context.Owner.Tokens[n.Context.Begin]
}

// Stmt is a parsed statement inside an inst / op block.
//
// CPython: Tools/cases_generator/parsing.py:75-90 Stmt
type Stmt interface {
	StmtTokens() []Token
}

// IfStmt models `if (cond) { body } [else ...]`.
//
// CPython: Tools/cases_generator/parsing.py:93-125 IfStmt
type IfStmt struct {
	If        Token
	Condition []Token
	Body      Stmt
	Else      *Token
	ElseBody  Stmt
}

func (s *IfStmt) StmtTokens() []Token {
	out := []Token{s.If}
	out = append(out, s.Condition...)
	out = append(out, s.Body.StmtTokens()...)
	if s.Else != nil {
		out = append(out, *s.Else)
	}
	if s.ElseBody != nil {
		out = append(out, s.ElseBody.StmtTokens()...)
	}
	return out
}

// ForStmt models `for (header) { body }`.
//
// CPython: Tools/cases_generator/parsing.py:128-147 ForStmt
type ForStmt struct {
	For    Token
	Header []Token
	Body   Stmt
}

func (s *ForStmt) StmtTokens() []Token {
	out := []Token{s.For}
	out = append(out, s.Header...)
	out = append(out, s.Body.StmtTokens()...)
	return out
}

// WhileStmt models `while (cond) { body }`.
//
// CPython: Tools/cases_generator/parsing.py:150-169 WhileStmt
type WhileStmt struct {
	While     Token
	Condition []Token
	Body      Stmt
}

func (s *WhileStmt) StmtTokens() []Token {
	out := []Token{s.While}
	out = append(out, s.Condition...)
	out = append(out, s.Body.StmtTokens()...)
	return out
}

// MacroIfStmt models `#if cond ... [#else ...] #endif`.
//
// CPython: Tools/cases_generator/parsing.py:172-203 MacroIfStmt
type MacroIfStmt struct {
	Condition Token
	Body      []Stmt
	Else      *Token
	ElseBody  []Stmt
	Endif     Token
}

func (s *MacroIfStmt) StmtTokens() []Token {
	out := []Token{s.Condition}
	for _, st := range s.Body {
		out = append(out, st.StmtTokens()...)
	}
	for _, st := range s.ElseBody {
		out = append(out, st.StmtTokens()...)
	}
	return out
}

// BlockStmt models a brace-delimited compound.
//
// CPython: Tools/cases_generator/parsing.py:206-228 BlockStmt
type BlockStmt struct {
	Open  Token
	Body  []Stmt
	Close Token
}

func (s *BlockStmt) StmtTokens() []Token {
	out := []Token{s.Open}
	for _, st := range s.Body {
		out = append(out, st.StmtTokens()...)
	}
	out = append(out, s.Close)
	return out
}

// SimpleStmt models any statement consumed up to a semicolon.
//
// CPython: Tools/cases_generator/parsing.py:231-245 SimpleStmt
type SimpleStmt struct {
	Contents []Token
}

func (s *SimpleStmt) StmtTokens() []Token { return s.Contents }

// StackEffect names a single stack slot, optionally typed (`name:type`)
// or sized (`name[size]`). Either type or size is set, not both.
//
// CPython: Tools/cases_generator/parsing.py:247-258 StackEffect
type StackEffect struct {
	node
	Name string
	Type string
	Size string
}

// Expression is a token-list consumed inside `[...]` or `(...)`.
//
// CPython: Tools/cases_generator/parsing.py:261-263 Expression
type Expression struct {
	node
	Size string
}

// CacheEffect declares an inline cache slot of `size` code units.
//
// CPython: Tools/cases_generator/parsing.py:266-269 CacheEffect
type CacheEffect struct {
	node
	Name string
	Size int
}

// OpName is a lone identifier referenced inside a macro definition.
//
// CPython: Tools/cases_generator/parsing.py:272-274 OpName
type OpName struct {
	node
	Name string
}

// InputEffect / OutputEffect / UOp are sum types in Python; in Go we
// expose them via a pair of small interfaces with empty marker methods
// the relevant concrete types implement.
type InputEffect interface {
	Node
	isInputEffect()
}

type OutputEffect interface {
	Node
	isOutputEffect()
}

type UOp interface {
	Node
	isUOp()
}

func (*StackEffect) isInputEffect()  {}
func (*StackEffect) isOutputEffect() {}
func (*CacheEffect) isInputEffect()  {}
func (*OpName) isUOp()               {}
func (*CacheEffect) isUOp()          {}

// InstHeader is the declaration up through the `)` before the body.
//
// CPython: Tools/cases_generator/parsing.py:282-289 InstHeader
type InstHeader struct {
	node
	Annotations []string
	Kind        string // "inst" | "op"
	Name        string
	Inputs      []InputEffect
	Outputs     []OutputEffect
}

// InstDef is a parsed `inst(NAME, (...))` or `op(NAME, (...))` block.
//
// CPython: Tools/cases_generator/parsing.py:291-298 InstDef
type InstDef struct {
	node
	Annotations []string
	Kind        string
	Name        string
	Inputs      []InputEffect
	Outputs     []OutputEffect
	Block       *BlockStmt
}

// Macro is `macro(NAME) = uop1 + uop2 + ...;`.
//
// CPython: Tools/cases_generator/parsing.py:301-304 Macro
type Macro struct {
	node
	Name string
	UOps []UOp
}

// Family is `family(NAME, size) = { members };`.
//
// CPython: Tools/cases_generator/parsing.py:307-311 Family
type Family struct {
	node
	Name    string
	Size    string
	Members []string
}

// Pseudo is `pseudo(NAME, (...), [flags]) = { targets };` (or [...]).
//
// CPython: Tools/cases_generator/parsing.py:314-321 Pseudo
type Pseudo struct {
	node
	Name       string
	Inputs     []InputEffect
	Outputs    []OutputEffect
	Flags      []string
	Targets    []string
	AsSequence bool
}

// LabelDef is `[spilled] label(NAME) { body }`.
//
// CPython: Tools/cases_generator/parsing.py:323-327 LabelDef
type LabelDef struct {
	node
	Name    string
	Spilled bool
	Block   *BlockStmt
}

// Parser embeds PLexer and adds the grammar methods.
//
// CPython: Tools/cases_generator/parsing.py:333 Parser
type Parser struct {
	*PLexer
}

// NewParser tokenizes src and returns a Parser ready to call
// Definition() in a loop until it yields nil.
func NewParser(src, filename string) (*Parser, error) {
	pl, err := NewPLexer(src, filename)
	if err != nil {
		return nil, err
	}
	return &Parser{PLexer: pl}, nil
}

// Definition parses one top-level declaration, returning nil at EOF.
//
// CPython: Tools/cases_generator/parsing.py:334-346 definition
func (p *Parser) Definition() (Node, error) {
	begin := p.GetPos()
	if m, err := p.macroDef(); err != nil {
		return nil, err
	} else if m != nil {
		return m, nil
	}
	if f, err := p.familyDef(); err != nil {
		return nil, err
	} else if f != nil {
		return f, nil
	}
	if ps, err := p.pseudoDef(); err != nil {
		return nil, err
	} else if ps != nil {
		return ps, nil
	}
	if i, err := p.instDef(); err != nil {
		return nil, err
	} else if i != nil {
		return i, nil
	}
	if l, err := p.labelDef(); err != nil {
		return nil, err
	} else if l != nil {
		return l, nil
	}
	p.SetPos(begin)
	return nil, nil
}

// labelDef parses `[spilled] label(NAME) { ... }`.
//
// CPython: Tools/cases_generator/parsing.py:348-359 label_def
func (p *Parser) labelDef() (*LabelDef, error) {
	begin := p.GetPos()
	spilled := false
	if _, ok := p.Expect(TokSpilled); ok {
		spilled = true
	}
	if _, ok := p.Expect(TokLabel); ok {
		if _, ok := p.Expect(TokLParen); ok {
			if tkn, ok := p.Expect(TokIdentifier); ok {
				if _, ok := p.Expect(TokRParen); ok {
					blk, err := p.block()
					if err != nil {
						return nil, err
					}
					ld := &LabelDef{Name: tkn.Text, Spilled: spilled, Block: blk}
					end := p.GetPos()
					ld.setContext(Context{Begin: begin, End: end, Owner: p})
					return ld, nil
				}
			}
		}
	}
	p.SetPos(begin)
	return nil, nil
}

// instDef parses an inst() or op() declaration plus body.
//
// CPython: Tools/cases_generator/parsing.py:361-373 inst_def
func (p *Parser) instDef() (*InstDef, error) {
	begin := p.GetPos()
	hdr, err := p.instHeader()
	if err != nil {
		return nil, err
	}
	if hdr == nil {
		p.SetPos(begin)
		return nil, nil
	}
	blk, err := p.block()
	if err != nil {
		return nil, err
	}
	id := &InstDef{
		Annotations: hdr.Annotations,
		Kind:        hdr.Kind,
		Name:        hdr.Name,
		Inputs:      hdr.Inputs,
		Outputs:     hdr.Outputs,
		Block:       blk,
	}
	end := p.GetPos()
	id.setContext(Context{Begin: begin, End: end, Owner: p})
	return id, nil
}

// instHeader parses `annotation* (inst|op) ( NAME , (inputs -- outputs) )`
// and peeks at LBRACE so the caller knows a body follows.
//
// CPython: Tools/cases_generator/parsing.py:375-400 inst_header
func (p *Parser) instHeader() (*InstHeader, error) {
	begin := p.GetPos()
	var annotations []string
	for {
		anno, ok := p.Expect(TokAnnotation)
		if !ok {
			break
		}
		if anno.Text == "replicate" {
			if _, err := p.Require(TokLParen); err != nil {
				return nil, err
			}
			times, err := p.Require(TokNumber)
			if err != nil {
				return nil, err
			}
			if _, err := p.Require(TokRParen); err != nil {
				return nil, err
			}
			annotations = append(annotations, fmt.Sprintf("replicate(%s)", times.Text))
		} else {
			annotations = append(annotations, anno.Text)
		}
	}
	tkn, ok := p.Expect(TokInst)
	if !ok {
		tkn, ok = p.Expect(TokOp)
	}
	if !ok {
		p.SetPos(begin)
		return nil, nil
	}
	kind := tkn.Text
	if _, ok := p.Expect(TokLParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	nameTok, ok := p.Expect(TokIdentifier)
	if !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokComma); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	inp, outp, err := p.ioEffect()
	if err != nil {
		return nil, err
	}
	if _, ok := p.Expect(TokRParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	peek, ok := p.Peek(false)
	if !ok || peek.Kind != TokLBrace {
		p.SetPos(begin)
		return nil, nil
	}
	hdr := &InstHeader{
		Annotations: annotations, Kind: kind, Name: nameTok.Text,
		Inputs: inp, Outputs: outp,
	}
	end := p.GetPos()
	hdr.setContext(Context{Begin: begin, End: end, Owner: p})
	return hdr, nil
}

// ioEffect parses `( inputs -- outputs )`.
//
// CPython: Tools/cases_generator/parsing.py:402-410 io_effect
func (p *Parser) ioEffect() ([]InputEffect, []OutputEffect, error) {
	if _, ok := p.Expect(TokLParen); ok {
		inp, err := p.inputs()
		if err != nil {
			return nil, nil, err
		}
		if _, ok := p.Expect(TokMinusMinus); ok {
			outp, err := p.outputs()
			if err != nil {
				return nil, nil, err
			}
			if _, ok := p.Expect(TokRParen); ok {
				if inp == nil {
					inp = []InputEffect{}
				}
				if outp == nil {
					outp = []OutputEffect{}
				}
				return inp, outp, nil
			}
		}
	}
	return nil, nil, p.MakeSyntaxError("Expected stack effect", Token{}, false)
}

// inputs parses `input (',' input)*` or returns nil.
//
// CPython: Tools/cases_generator/parsing.py:412-424 inputs
func (p *Parser) inputs() ([]InputEffect, error) {
	here := p.GetPos()
	inp, err := p.input()
	if err != nil {
		return nil, err
	}
	if inp == nil {
		p.SetPos(here)
		return nil, nil
	}
	near := p.GetPos()
	if _, ok := p.Expect(TokComma); ok {
		rest, err := p.inputs()
		if err != nil {
			return nil, err
		}
		if rest != nil {
			return append([]InputEffect{inp}, rest...), nil
		}
	}
	p.SetPos(near)
	return []InputEffect{inp}, nil
}

// input parses one cache_effect or stack_effect.
//
// CPython: Tools/cases_generator/parsing.py:426-428 input
func (p *Parser) input() (InputEffect, error) {
	begin := p.GetPos()
	ce, err := p.cacheEffect()
	if err != nil {
		return nil, err
	}
	if ce != nil {
		return ce, nil
	}
	se, err := p.stackEffect()
	if err != nil {
		return nil, err
	}
	if se != nil {
		return se, nil
	}
	p.SetPos(begin)
	return nil, nil
}

// outputs parses `output (',' output)*` or returns nil.
//
// CPython: Tools/cases_generator/parsing.py:430-441 outputs
func (p *Parser) outputs() ([]OutputEffect, error) {
	here := p.GetPos()
	o, err := p.output()
	if err != nil {
		return nil, err
	}
	if o == nil {
		p.SetPos(here)
		return nil, nil
	}
	near := p.GetPos()
	if _, ok := p.Expect(TokComma); ok {
		rest, err := p.outputs()
		if err != nil {
			return nil, err
		}
		if rest != nil {
			return append([]OutputEffect{o}, rest...), nil
		}
	}
	p.SetPos(near)
	return []OutputEffect{o}, nil
}

// output parses a single stack_effect.
//
// CPython: Tools/cases_generator/parsing.py:443-445 output
func (p *Parser) output() (OutputEffect, error) {
	se, err := p.stackEffect()
	if err != nil {
		return nil, err
	}
	if se == nil {
		return nil, nil
	}
	return se, nil
}

// cacheEffect parses `IDENT '/' NUMBER`.
//
// CPython: Tools/cases_generator/parsing.py:447-459 cache_effect
func (p *Parser) cacheEffect() (*CacheEffect, error) {
	begin := p.GetPos()
	tkn, ok := p.Expect(TokIdentifier)
	if !ok {
		return nil, nil
	}
	if _, ok := p.Expect(TokDivide); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	num, err := p.Require(TokNumber)
	if err != nil {
		return nil, err
	}
	size, perr := strconv.Atoi(num.Text)
	if perr != nil {
		return nil, p.MakeSyntaxError(fmt.Sprintf("Expected integer, got %q", num.Text), num, true)
	}
	ce := &CacheEffect{Name: tkn.Text, Size: size}
	end := p.GetPos()
	ce.setContext(Context{Begin: begin, End: end, Owner: p})
	return ce, nil
}

// stackEffect parses `IDENT [':' IDENT [TIMES]] | IDENT '[' expr ']'`.
//
// CPython: Tools/cases_generator/parsing.py:461-480 stack_effect
func (p *Parser) stackEffect() (*StackEffect, error) {
	begin := p.GetPos()
	tkn, ok := p.Expect(TokIdentifier)
	if !ok {
		return nil, nil
	}
	typeText := ""
	if _, ok := p.Expect(TokColon); ok {
		ident, err := p.Require(TokIdentifier)
		if err != nil {
			return nil, err
		}
		typeText = strings.TrimSpace(ident.Text)
		if _, ok := p.Expect(TokTimes); ok {
			typeText += " *"
		}
	}
	sizeText := ""
	if _, ok := p.Expect(TokLBracket); ok {
		if typeText != "" {
			return nil, p.MakeSyntaxError("Unexpected [", Token{}, false)
		}
		expr := p.expression()
		if expr == nil {
			return nil, p.MakeSyntaxError("Expected expression", Token{}, false)
		}
		if _, err := p.Require(TokRBracket); err != nil {
			return nil, err
		}
		sizeText = strings.TrimSpace(expr.Text())
	}
	se := &StackEffect{Name: tkn.Text, Type: typeText, Size: sizeText}
	end := p.GetPos()
	se.setContext(Context{Begin: begin, End: end, Owner: p})
	return se, nil
}

// expression consumes tokens up to the matching ] or ).
//
// CPython: Tools/cases_generator/parsing.py:482-497 expression
func (p *Parser) expression() *Expression {
	begin := p.GetPos()
	var tokens []Token
	level := 1
	for {
		tkn, ok := p.Peek(false)
		if !ok {
			break
		}
		if tkn.Kind == TokLBracket || tkn.Kind == TokLParen {
			level++
		} else if tkn.Kind == TokRBracket || tkn.Kind == TokRParen {
			level--
			if level == 0 {
				break
			}
		}
		tokens = append(tokens, tkn)
		p.Next(false)
	}
	if len(tokens) == 0 {
		p.SetPos(begin)
		return nil
	}
	e := &Expression{Size: strings.TrimSpace(ToText(tokens, 0))}
	end := p.GetPos()
	e.setContext(Context{Begin: begin, End: end, Owner: p})
	return e
}

// macroDef parses `macro(NAME) = uops;`.
//
// CPython: Tools/cases_generator/parsing.py:513-524 macro_def
func (p *Parser) macroDef() (*Macro, error) {
	begin := p.GetPos()
	if _, ok := p.Expect(TokMacro); !ok {
		return nil, nil
	}
	if _, ok := p.Expect(TokLParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	tkn, ok := p.Expect(TokIdentifier)
	if !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokRParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokEquals); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	uops, err := p.uops()
	if err != nil {
		return nil, err
	}
	if uops == nil {
		p.SetPos(begin)
		return nil, nil
	}
	if _, err := p.Require(TokSemi); err != nil {
		return nil, err
	}
	m := &Macro{Name: tkn.Text, UOps: uops}
	end := p.GetPos()
	m.setContext(Context{Begin: begin, End: end, Owner: p})
	return m, nil
}

// uops parses `uop ('+' uop)*`.
//
// CPython: Tools/cases_generator/parsing.py:526-537 uops
func (p *Parser) uops() ([]UOp, error) {
	first, err := p.uop()
	if err != nil {
		return nil, err
	}
	if first == nil {
		return nil, nil
	}
	out := []UOp{first}
	for {
		if _, ok := p.Expect(TokPlus); !ok {
			break
		}
		next, err := p.uop()
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, p.MakeSyntaxError("Expected op name or cache effect", Token{}, false)
		}
		out = append(out, next)
	}
	return out, nil
}

// uop parses `IDENT` or `IDENT '/' [-]NUMBER`.
//
// CPython: Tools/cases_generator/parsing.py:539-558 uop
func (p *Parser) uop() (UOp, error) {
	begin := p.GetPos()
	tkn, ok := p.Expect(TokIdentifier)
	if !ok {
		return nil, nil
	}
	if _, ok := p.Expect(TokDivide); ok {
		sign := 1
		if _, ok := p.Expect(TokMinus); ok {
			sign = -1
		}
		num, ok := p.Expect(TokNumber)
		if !ok {
			return nil, p.MakeSyntaxError("Expected integer", Token{}, false)
		}
		size, perr := strconv.Atoi(num.Text)
		if perr != nil {
			return nil, p.MakeSyntaxError(fmt.Sprintf("Expected integer, got %q", num.Text), num, true)
		}
		ce := &CacheEffect{Name: tkn.Text, Size: sign * size}
		end := p.GetPos()
		ce.setContext(Context{Begin: begin, End: end, Owner: p})
		return ce, nil
	}
	on := &OpName{Name: tkn.Text}
	end := p.GetPos()
	on.setContext(Context{Begin: begin, End: end, Owner: p})
	return on, nil
}

// familyDef parses `family(NAME, size) = { members };`.
//
// CPython: Tools/cases_generator/parsing.py:560-581 family_def
func (p *Parser) familyDef() (*Family, error) {
	begin := p.GetPos()
	tkn, ok := p.Expect(TokIdentifier)
	if !ok || tkn.Text != "family" {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokLParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	nameTok, ok := p.Expect(TokIdentifier)
	if !ok {
		p.SetPos(begin)
		return nil, nil
	}
	var sizeText string
	if _, ok := p.Expect(TokComma); ok {
		s, ok := p.Expect(TokIdentifier)
		if !ok {
			s, ok = p.Expect(TokNumber)
			if !ok {
				return nil, p.MakeSyntaxError("Expected identifier or number", Token{}, false)
			}
		}
		sizeText = s.Text
	}
	if _, ok := p.Expect(TokRParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokEquals); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokLBrace); !ok {
		return nil, p.MakeSyntaxError("Expected {", Token{}, false)
	}
	members, err := p.members(false)
	if err != nil {
		return nil, err
	}
	if members == nil {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokRBrace); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokSemi); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	f := &Family{Name: nameTok.Text, Size: sizeText, Members: members}
	end := p.GetPos()
	f.setContext(Context{Begin: begin, End: end, Owner: p})
	return f, nil
}

// flags parses an optional `(IDENT, IDENT, ...)` group used by pseudo.
//
// CPython: Tools/cases_generator/parsing.py:583-597 flags
func (p *Parser) flags() ([]string, error) {
	here := p.GetPos()
	if _, ok := p.Expect(TokLParen); !ok {
		p.SetPos(here)
		return nil, nil
	}
	tkn, ok := p.Expect(TokIdentifier)
	if !ok {
		p.SetPos(here)
		return nil, nil
	}
	flags := []string{tkn.Text}
	for {
		if _, ok := p.Expect(TokComma); !ok {
			break
		}
		t, ok := p.Expect(TokIdentifier)
		if !ok {
			break
		}
		flags = append(flags, t.Text)
	}
	if _, ok := p.Expect(TokRParen); !ok {
		return nil, p.MakeSyntaxError("Expected comma or right paren", Token{}, false)
	}
	return flags, nil
}

// pseudoDef parses `pseudo(NAME, (...), [flags]) = { ... } | [...];`.
//
// CPython: Tools/cases_generator/parsing.py:599-626 pseudo_def
func (p *Parser) pseudoDef() (*Pseudo, error) {
	begin := p.GetPos()
	tkn, ok := p.Expect(TokIdentifier)
	if !ok || tkn.Text != "pseudo" {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokLParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	nameTok, ok := p.Expect(TokIdentifier)
	if !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokComma); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	inp, outp, err := p.ioEffect()
	if err != nil {
		return nil, err
	}
	var flags []string
	if _, ok := p.Expect(TokComma); ok {
		flags, err = p.flags()
		if err != nil {
			return nil, err
		}
	}
	if _, ok := p.Expect(TokRParen); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokEquals); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	var asSequence bool
	var closing string
	if _, ok := p.Expect(TokLBrace); ok {
		closing = TokRBrace
	} else if _, ok := p.Expect(TokLBracket); ok {
		asSequence = true
		closing = TokRBracket
	} else {
		return nil, p.MakeSyntaxError("Expected { or [", Token{}, false)
	}
	members, err := p.members(true)
	if err != nil {
		return nil, err
	}
	if members == nil {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(closing); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	if _, ok := p.Expect(TokSemi); !ok {
		p.SetPos(begin)
		return nil, nil
	}
	ps := &Pseudo{
		Name: nameTok.Text, Inputs: inp, Outputs: outp,
		Flags: flags, Targets: members, AsSequence: asSequence,
	}
	end := p.GetPos()
	ps.setContext(Context{Begin: begin, End: end, Owner: p})
	return ps, nil
}

// members parses `IDENT (',' IDENT)*` and validates the closer.
//
// CPython: Tools/cases_generator/parsing.py:628-644 members
func (p *Parser) members(allowSequence bool) ([]string, error) {
	here := p.GetPos()
	tkn, ok := p.Expect(TokIdentifier)
	if !ok {
		p.SetPos(here)
		return nil, nil
	}
	out := []string{tkn.Text}
	for {
		if _, ok := p.Expect(TokComma); !ok {
			break
		}
		t, ok := p.Expect(TokIdentifier)
		if !ok {
			break
		}
		out = append(out, t.Text)
	}
	peek, ok := p.Peek(false)
	want := []string{TokRBrace}
	if allowSequence {
		want = append(want, TokRBracket)
	}
	if !ok || !slices.Contains(want, peek.Kind) {
		msg := "Expected comma or right paren"
		if allowSequence {
			msg = "Expected comma or right paren/bracket"
		}
		return nil, p.MakeSyntaxError(msg, Token{}, false)
	}
	return out, nil
}

// block parses `{ stmt* }` and returns a BlockStmt.
//
// CPython: Tools/cases_generator/parsing.py:646-651 block
func (p *Parser) block() (*BlockStmt, error) {
	open, err := p.Require(TokLBrace)
	if err != nil {
		return nil, err
	}
	var stmts []Stmt
	for {
		closeTok, ok := p.Expect(TokRBrace)
		if ok {
			return &BlockStmt{Open: open, Body: stmts, Close: closeTok}, nil
		}
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
	}
}

// stmt dispatches on the next token to the right statement parser.
//
// CPython: Tools/cases_generator/parsing.py:653-677 stmt
func (p *Parser) stmt() (Stmt, error) {
	if tkn, ok := p.Expect(TokIf); ok {
		return p.ifStmt(tkn)
	}
	if _, ok := p.Expect(TokLBrace); ok {
		p.Backup()
		return p.block()
	}
	if tkn, ok := p.Expect(TokFor); ok {
		return p.forStmt(tkn)
	}
	if tkn, ok := p.Expect(TokWhile); ok {
		return p.whileStmt(tkn)
	}
	if tkn, ok := p.Expect(TokCMacroIf); ok {
		return p.macroIf(tkn)
	}
	if _, ok := p.Expect(TokCMacroElse); ok {
		return nil, p.MakeSyntaxError("Unexpected #else", Token{}, false)
	}
	if _, ok := p.Expect(TokCMacroEndif); ok {
		return nil, p.MakeSyntaxError("Unexpected #endif", Token{}, false)
	}
	if tkn, ok := p.Expect(TokCMacroOther); ok {
		return &SimpleStmt{Contents: []Token{tkn}}, nil
	}
	if _, ok := p.Expect(TokSwitch); ok {
		return nil, p.MakeSyntaxError(
			"switch statements are not supported due to their complex flow control. Sorry.",
			Token{}, false)
	}
	tokens, err := p.ConsumeTo(TokSemi)
	if err != nil {
		return nil, err
	}
	return &SimpleStmt{Contents: tokens}, nil
}

// ifStmt parses `if (cond) body [else ...]`.
//
// CPython: Tools/cases_generator/parsing.py:679-690 if_stmt
func (p *Parser) ifStmt(ifTok Token) (*IfStmt, error) {
	lparen, err := p.Require(TokLParen)
	if err != nil {
		return nil, err
	}
	rest, err := p.ConsumeTo(TokRParen)
	if err != nil {
		return nil, err
	}
	condition := append([]Token{lparen}, rest...)
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	var elseTok *Token
	var elseBody Stmt
	if e, ok := p.Expect(TokElse); ok {
		t := e
		elseTok = &t
		if inner, ok := p.Expect(TokIf); ok {
			elseBody, err = p.ifStmt(inner)
			if err != nil {
				return nil, err
			}
		} else {
			elseBody, err = p.block()
			if err != nil {
				return nil, err
			}
		}
	}
	return &IfStmt{If: ifTok, Condition: condition, Body: body, Else: elseTok, ElseBody: elseBody}, nil
}

// macroIf parses `#if cond ... [#else ...] #endif`.
//
// CPython: Tools/cases_generator/parsing.py:692-707 macro_if
func (p *Parser) macroIf(cond Token) (*MacroIfStmt, error) {
	var (
		elseTok  *Token
		body     []Stmt
		elseBody []Stmt
		inElse   bool
	)
	for {
		if tkn, ok := p.Expect(TokCMacroEndif); ok {
			return &MacroIfStmt{Condition: cond, Body: body, Else: elseTok, ElseBody: elseBody, Endif: tkn}, nil
		}
		if tkn, ok := p.Expect(TokCMacroElse); ok {
			if inElse {
				return nil, p.MakeSyntaxError("Multiple #else", Token{}, false)
			}
			t := tkn
			elseTok = &t
			elseBody = []Stmt{}
			inElse = true
			continue
		}
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		if inElse {
			elseBody = append(elseBody, s)
		} else {
			body = append(body, s)
		}
	}
}

// forStmt parses `for (header) body`.
//
// CPython: Tools/cases_generator/parsing.py:709-713 for_stmt
func (p *Parser) forStmt(forTok Token) (*ForStmt, error) {
	lparen, err := p.Require(TokLParen)
	if err != nil {
		return nil, err
	}
	rest, err := p.ConsumeTo(TokRParen)
	if err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	return &ForStmt{For: forTok, Header: append([]Token{lparen}, rest...), Body: body}, nil
}

// whileStmt parses `while (cond) body`.
//
// CPython: Tools/cases_generator/parsing.py:715-719 while_stmt
func (p *Parser) whileStmt(whileTok Token) (*WhileStmt, error) {
	lparen, err := p.Require(TokLParen)
	if err != nil {
		return nil, err
	}
	rest, err := p.ConsumeTo(TokRParen)
	if err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{While: whileTok, Condition: append([]Token{lparen}, rest...), Body: body}, nil
}
