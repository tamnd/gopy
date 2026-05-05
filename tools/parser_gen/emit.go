// Per-rule emitter. Walks each Rule and writes a Go parse function
// to parser_gen.go. Mirrors the structure of CPython's
// Tools/peg_generator/pegen/c_generator.py but emits Go that calls
// into parser/pegen.Parser helpers.
//
// M3 scope: non-left-recursive rules emit a working backtracking
// body. Left-recursive rules emit a stub returning nil; M4 lands the
// seed-and-grow loop. Actions are not yet translated; each alt
// returns a slice of its named-item locals as a placeholder so the
// match/no-match boundary is correct even though the result tree is
// shaped wrong. M6 lands the real action translator.
//
// CPython: Tools/peg_generator/pegen/c_generator.py
// CPython: Tools/peg_generator/pegen/parser_generator.py

package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// exactTokenTypes maps an operator literal to its tokenize.Type
// constant name. Mirrors token.EXACT_TOKEN_TYPES from CPython.
//
// CPython: Lib/token.py EXACT_TOKEN_TYPES
var exactTokenTypes = map[string]string{
	"!": "EXCLAMATION", "!=": "NOTEQUAL",
	"%": "PERCENT", "%=": "PERCENTEQUAL",
	"&": "AMPER", "&=": "AMPEREQUAL",
	"(": "LPAR", ")": "RPAR",
	"*": "STAR", "**": "DOUBLESTAR", "**=": "DOUBLESTAREQUAL", "*=": "STAREQUAL",
	"+": "PLUS", "+=": "PLUSEQUAL",
	",": "COMMA",
	"-": "MINUS", "-=": "MINEQUAL", "->": "RARROW",
	".": "DOT", "...": "ELLIPSIS",
	"/": "SLASH", "//": "DOUBLESLASH", "//=": "DOUBLESLASHEQUAL", "/=": "SLASHEQUAL",
	":": "COLON", ":=": "COLONEQUAL",
	";": "SEMI",
	"<": "LESS", "<<": "LEFTSHIFT", "<<=": "LEFTSHIFTEQUAL", "<=": "LESSEQUAL",
	"=": "EQUAL", "==": "EQEQUAL",
	">": "GREATER", ">=": "GREATEREQUAL", ">>": "RIGHTSHIFT", ">>=": "RIGHTSHIFTEQUAL",
	"@": "AT", "@=": "ATEQUAL",
	"[": "LSQB", "]": "RSQB",
	"^": "CIRCUMFLEX", "^=": "CIRCUMFLEXEQUAL",
	"{": "LBRACE", "}": "RBRACE",
	"|": "VBAR", "|=": "VBAREQUAL",
	"~": "TILDE",
}

// builtinTokens is the set of NameLeaf values that resolve to a
// raw token kind rather than to another rule.
var builtinTokens = map[string]string{
	"NAME":           "NAME",
	"NUMBER":         "NUMBER",
	"STRING":         "STRING",
	"OP":             "OP",
	"NEWLINE":        "NEWLINE",
	"INDENT":         "INDENT",
	"DEDENT":         "DEDENT",
	"ENDMARKER":      "ENDMARKER",
	"FSTRING_START":  "FSTRING_START",
	"FSTRING_MIDDLE": "FSTRING_MIDDLE",
	"FSTRING_END":    "FSTRING_END",
	"TSTRING_START":  "TSTRING_START",
	"TSTRING_MIDDLE": "TSTRING_MIDDLE",
	"TSTRING_END":    "TSTRING_END",
	"TYPE_COMMENT":   "TYPE_COMMENT",
	"SOFT_KEYWORD":   "SOFT_KEYWORD",
}

// emitter holds the in-progress generator state.
type emitter struct {
	g            *Grammar
	rules        map[string]*Rule
	artificial   []*Rule // synthesized helpers, populated during body emission
	artCache     map[string]string
	keywords     map[string]int // keyword string → assigned id
	softKeywords map[string]bool
	buf          *strings.Builder
	bodies       *strings.Builder // buffered rule bodies
	gramHash     string           // sha256 of the source python.gram, if known
}

// Emit returns Go source for parser_gen.go. The grammar must have
// been parsed already; nullability and left-recursion analysis are
// run inside.
func Emit(g *Grammar) string { return EmitWithSource(g, nil) }

// EmitWithSource is Emit with the original python.gram bytes. The
// emitter records a SHA256 of the source in the preamble so a
// drift check can flag stale generated output.
func EmitWithSource(g *Grammar, gram []byte) string {
	e := &emitter{
		g:            g,
		rules:        map[string]*Rule{},
		artCache:     map[string]string{},
		keywords:     map[string]int{},
		softKeywords: map[string]bool{},
		buf:          &strings.Builder{},
		bodies:       &strings.Builder{},
		gramHash:     hashGrammar(gram),
	}
	for _, r := range g.Rules {
		e.rules[r.Name] = r
	}
	computeLeftRecursive(g)
	e.collectKeywords()
	// Emit bodies first into a side buffer; this populates artificial.
	for _, r := range g.Rules {
		e.writeRule(r)
	}
	for i := 0; i < len(e.artificial); i++ {
		e.writeRule(e.artificial[i])
	}
	e.writePreamble()
	e.writeRuleConstants()
	e.writeRuleNameTable()
	e.writeKeywordTables()
	e.buf.WriteString(e.bodies.String())
	e.writeActionHelperStubs()
	e.writeDispatch()
	return e.buf.String()
}

func (e *emitter) printf(format string, args ...any) {
	fmt.Fprintf(e.buf, format, args...)
}

func (e *emitter) collectKeywords() {
	var visitItem func(Item)
	visitItem = func(it Item) {
		switch x := it.(type) {
		case *StringLeaf:
			s, soft := unquoteLeaf(x.Value)
			if soft {
				e.softKeywords[s] = true
			} else if isKeywordLiteral(s) {
				if _, ok := e.keywords[s]; !ok {
					e.keywords[s] = 500 + len(e.keywords)
				}
			}
		case *Group:
			for _, a := range x.RHS.Alts {
				for _, ni := range a.Items {
					visitItem(ni.Item)
				}
			}
		case *Opt:
			visitItem(x.Item)
		case *Repeat0:
			visitItem(x.Item)
		case *Repeat1:
			visitItem(x.Item)
		case *Gather:
			visitItem(x.Sep)
			visitItem(x.Node)
		case *Forced:
			visitItem(x.Node)
		case *PositiveLookahead:
			visitItem(x.Node)
		case *NegativeLookahead:
			visitItem(x.Node)
		}
	}
	for _, r := range e.g.Rules {
		for _, a := range r.RHS.Alts {
			for _, ni := range a.Items {
				visitItem(ni.Item)
			}
		}
	}
}

// unquoteLeaf strips the surrounding quotes from a StringLeaf
// literal. Returns the inner text and whether the literal was
// double-quoted (soft keyword).
func unquoteLeaf(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], true
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1], false
	}
	return s, false
}

// isKeywordLiteral reports whether s looks like a Python identifier
// rather than an operator. The PEG metagrammar allows single-quoted
// strings for both, so the emitter discriminates by content.
func isKeywordLiteral(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func (e *emitter) writePreamble() {
	e.buf.WriteString("// Code generated by tools/parser_gen from cpython/Grammar/python.gram. DO NOT EDIT.\n")
	e.buf.WriteString("//\n// CPython: Grammar/python.gram\n")
	if e.gramHash != "" {
		e.printf("// gram-sha256: %s\n", e.gramHash)
	}
	e.buf.WriteString("\npackage pegen\n\n")
	e.buf.WriteString("import (\n")
	e.buf.WriteString("\t\"errors\"\n\n")
	e.buf.WriteString("\t\"github.com/tamnd/gopy/ast\"\n")
	e.buf.WriteString("\t\"github.com/tamnd/gopy/tokenize\"\n")
	e.buf.WriteString(")\n\n")
	e.buf.WriteString("// ErrParserNotImplemented is returned by Dispatch while the\n")
	e.buf.WriteString("// per-rule emitter still produces placeholder action results.\n")
	e.buf.WriteString("// The real AST surface lands with the action translator (M6).\n")
	e.buf.WriteString("var ErrParserNotImplemented = errors.New(\"pegen: generated rule bodies not yet emitted\")\n\n")
	e.buf.WriteString("var _ = tokenize.NAME // keep import alive when no rule uses it.\n")
	e.buf.WriteString("var _ = ast.Add       // keep import alive when no rule uses it.\n\n")
}

func (e *emitter) writeRuleConstants() {
	e.buf.WriteString("// Generated rule ids. Dense small ints in declaration\n")
	e.buf.WriteString("// order, then artificial helper rules. Stable across\n")
	e.buf.WriteString("// regenerations as long as python.gram does not insert,\n")
	e.buf.WriteString("// reorder, or rephrase rules in ways that change the\n")
	e.buf.WriteString("// artificial-rule deduplication keys.\n")
	e.buf.WriteString("const (\n")
	idx := 0
	for _, r := range e.g.Rules {
		e.printf("\tRule_%s = %d\n", r.Name, idx)
		idx++
	}
	for _, r := range e.artificial {
		e.printf("\tRule_%s = %d\n", r.Name, idx)
		idx++
	}
	e.buf.WriteString(")\n\n")
}

func (e *emitter) writeRuleNameTable() {
	e.buf.WriteString("// GeneratedRuleNames is the parallel name table.\n")
	e.buf.WriteString("var GeneratedRuleNames = []string{\n")
	for _, r := range e.g.Rules {
		e.printf("\t%q,\n", r.Name)
	}
	for _, r := range e.artificial {
		e.printf("\t%q,\n", r.Name)
	}
	e.buf.WriteString("}\n\n")
}

func (e *emitter) writeKeywordTables() {
	kws := make([]string, 0, len(e.keywords))
	for k := range e.keywords {
		kws = append(kws, k)
	}
	sort.Strings(kws)
	e.buf.WriteString("// HardKeywords are the single-quoted literals from python.gram.\n")
	e.buf.WriteString("var HardKeywords = []string{\n")
	for _, k := range kws {
		e.printf("\t%q,\n", k)
	}
	e.buf.WriteString("}\n\n")
	soft := make([]string, 0, len(e.softKeywords))
	for k := range e.softKeywords {
		soft = append(soft, k)
	}
	sort.Strings(soft)
	e.buf.WriteString("// SoftKeywords are the double-quoted literals from python.gram.\n")
	e.buf.WriteString("var SoftKeywords = []string{\n")
	for _, k := range soft {
		e.printf("\t%q,\n", k)
	}
	e.buf.WriteString("}\n\n")
}

// writeRule emits one parse function for r. Leaders of a
// left-recursive cycle get a seed-and-grow wrapper plus a separate
// `_raw` body. Non-leader members of a cycle get a non-memoized
// body (CPython @logger). Plain rules get the M3 body. Rules whose
// name starts with "invalid_" are skipped during the first parse
// pass; M5 wires the second-pass invocation.
func (e *emitter) writeRule(r *Rule) {
	out := e.bodies
	saved := e.buf
	e.buf = out
	defer func() { e.buf = saved }()
	switch r.Type {
	case "loop_marker":
		e.printf("// parseRule_%s parses %s.\nfunc parseRule_%s(p *Parser) any {\n", r.Name, r.Name, r.Name)
		e.writeLoopBody(r, false)
		return
	case "loop1_marker":
		e.printf("// parseRule_%s parses %s.\nfunc parseRule_%s(p *Parser) any {\n", r.Name, r.Name, r.Name)
		e.writeLoopBody(r, true)
		return
	case "gather_marker":
		e.printf("// parseRule_%s parses %s.\nfunc parseRule_%s(p *Parser) any {\n", r.Name, r.Name, r.Name)
		e.writeGatherBody(r)
		return
	}
	if r.LeftRecursive && r.Leader {
		e.writeLeaderWrapper(r)
		e.writeRuleBody(r, "_raw", false)
		return
	}
	if r.LeftRecursive {
		// Non-leader cycle member: no memoization.
		e.writeRuleBody(r, "", false)
		return
	}
	e.writeRuleBody(r, "", true)
}

// writeRuleBody emits the alt-by-alt body. suffix is appended to the
// function name (so leaders use "_raw"). memoize controls whether the
// body wraps itself in IsMemoized/InsertMemo. Invalid rules
// (name starting with "invalid_") gate themselves on
// p.CallInvalid(); they only run on the second parse pass.
//
// CPython: Tools/peg_generator/pegen/c_generator.py call_invalid_rules
func (e *emitter) writeRuleBody(r *Rule, suffix string, memoize bool) {
	out := e.buf
	e.printf("// parseRule_%s%s parses %s.\n", r.Name, suffix, r.Name)
	e.printf("func parseRule_%s%s(p *Parser) any {\n", r.Name, suffix)
	out.WriteString("\tif p.ErrorIndicator() { return nil }\n")
	if isInvalidRule(r.Name) {
		out.WriteString("\tif !p.CallInvalid() { return nil }\n")
	}
	if memoize {
		e.printf("\tif v, ok := p.IsMemoized(Rule_%s); ok { return v }\n", r.Name)
	}
	out.WriteString("\tmark := p.Mark()\n")
	out.WriteString("\t_ = mark\n")
	for ai, alt := range r.RHS.Alts {
		out.WriteString("\t// alt ")
		out.WriteString(strconv.Itoa(ai))
		out.WriteString("\n")
		e.writeAltBlock(r, alt)
	}
	if memoize {
		e.printf("\tp.InsertMemo(mark, Rule_%s, nil)\n", r.Name)
	}
	out.WriteString("\treturn nil\n}\n\n")
}

// writeLeaderWrapper emits the seed-and-grow wrapper for a left-
// recursive leader. Mirrors the @memoize_left_rec decorator.
//
// CPython: Tools/peg_generator/pegen/parser.py memoize_left_rec
func (e *emitter) writeLeaderWrapper(r *Rule) {
	out := e.buf
	e.printf("// parseRule_%s seeds and grows %s as a left-recursive rule.\n", r.Name, r.Name)
	e.printf("func parseRule_%s(p *Parser) any {\n", r.Name)
	out.WriteString("\tif p.ErrorIndicator() { return nil }\n")
	out.WriteString("\tmark := p.Mark()\n")
	e.printf("\tif v, ok := p.IsMemoized(Rule_%s); ok { return v }\n", r.Name)
	out.WriteString("\tlastMark := mark\n")
	out.WriteString("\tvar lastResult any\n")
	e.printf("\tp.InsertMemo(mark, Rule_%s, nil)\n", r.Name)
	out.WriteString("\tfor {\n")
	out.WriteString("\t\tp.Reset(mark)\n")
	e.printf("\t\tresult := parseRule_%s_raw(p)\n", r.Name)
	out.WriteString("\t\tend := p.Mark()\n")
	out.WriteString("\t\tif end <= lastMark { break }\n")
	out.WriteString("\t\tlastResult = result\n")
	out.WriteString("\t\tlastMark = end\n")
	e.printf("\t\tp.UpdateMemo(mark, Rule_%s, result)\n", r.Name)
	out.WriteString("\t}\n")
	out.WriteString("\tp.Reset(lastMark)\n")
	out.WriteString("\treturn lastResult\n}\n\n")
}

// writeLoopBody emits a Repeat0/Repeat1 helper body.
func (e *emitter) writeLoopBody(r *Rule, requireOne bool) {
	out := e.buf
	out.WriteString("\tif p.ErrorIndicator() { return nil }\n")
	out.WriteString("\tmark := p.Mark()\n")
	out.WriteString("\tvar children []any\n")
	inner := r.RHS.Alts[0].Items[0].Item
	spec := e.callItemRaw(inner)
	out.WriteString("\tfor {\n")
	switch spec.shape {
	case shapeBlocking:
		e.printf("\t\tv := %s\n", spec.expr)
		out.WriteString("\t\tif v == nil { break }\n")
		out.WriteString("\t\tchildren = append(children, v)\n")
	case shapeAlways:
		e.printf("\t\tv := %s\n", spec.expr)
		out.WriteString("\t\tchildren = append(children, v)\n")
	case shapeBoolean:
		e.printf("\t\tif !%s { break }\n", spec.expr)
	default:
		out.WriteString("\t\tbreak\n")
	}
	out.WriteString("\t\tmark = p.Mark()\n")
	out.WriteString("\t}\n")
	out.WriteString("\tp.Reset(mark)\n")
	if requireOne {
		out.WriteString("\tif len(children) == 0 { return nil }\n")
	}
	out.WriteString("\treturn children\n}\n\n")
}

// writeGatherBody emits a `sep.node+` helper body.
func (e *emitter) writeGatherBody(r *Rule) {
	out := e.buf
	out.WriteString("\tif p.ErrorIndicator() { return nil }\n")
	out.WriteString("\tmark := p.Mark()\n")
	sepSpec := e.callItemRaw(r.RHS.Alts[0].Items[0].Item)
	nodeSpec := e.callItemRaw(r.RHS.Alts[0].Items[1].Item)
	e.printf("\tfirst := %s\n", nodeSpec.expr)
	out.WriteString("\tif first == nil { p.Reset(mark); return nil }\n")
	out.WriteString("\tchildren := []any{first}\n")
	out.WriteString("\tfor {\n")
	out.WriteString("\t\tsmark := p.Mark()\n")
	e.printf("\t\tif s := %s; s == nil { p.Reset(smark); break } else { _ = s }\n", sepSpec.expr)
	e.printf("\t\tn := %s\n", nodeSpec.expr)
	out.WriteString("\t\tif n == nil { p.Reset(smark); break }\n")
	out.WriteString("\t\tchildren = append(children, n)\n")
	out.WriteString("\t}\n")
	out.WriteString("\treturn children\n}\n\n")
}

// writeAltBlock emits one alt as: an inner closure that returns nil
// on miss and the placeholder action value on match. On match the
// outer body memoizes and returns; on miss it Reset()s and falls
// through to the next alt. If the alt contains a Cut, a captured
// `cut` flag is set when crossed; on miss the rule short-circuits
// to a nil return (no further alts tried).
//
// CPython: Tools/peg_generator/pegen/c_generator.py visit_Alt cut path
func (e *emitter) writeAltBlock(r *Rule, a *Alt) {
	out := e.buf
	hasCut := a.Icut >= 0
	out.WriteString("\t{\n")
	if hasCut {
		out.WriteString("\t\tcut := false\n")
	}
	out.WriteString("\t\tif v := func() any {\n")
	names := e.writeAltItems(a, hasCut)
	e.writeAltReturn(a, names)
	out.WriteString("\t\t}(); v != nil {\n")
	if e.canMemoize(r) {
		e.printf("\t\t\tp.InsertMemo(mark, Rule_%s, v)\n", r.Name)
	}
	out.WriteString("\t\t\treturn v\n\t\t}\n")
	out.WriteString("\t\tp.Reset(mark)\n")
	if hasCut {
		out.WriteString("\t\tif cut {\n")
		if e.canMemoize(r) {
			e.printf("\t\t\tp.InsertMemo(mark, Rule_%s, nil)\n", r.Name)
		}
		out.WriteString("\t\t\treturn nil\n\t\t}\n")
	}
	out.WriteString("\t}\n")
}

// writeAltItems emits the per-item bindings inside an alt closure
// and returns the list of bound names available to the action.
func (e *emitter) writeAltItems(a *Alt, hasCut bool) []string {
	out := e.buf
	names := []string{}
	dedup := map[string]int{}
	for _, ni := range a.Items {
		spec := e.callItem(ni)
		varName := spec.varName
		if ni.Name != "" {
			varName = ni.Name
		}
		if varName == "" {
			varName = "_skip"
		} else {
			n := dedup[varName]
			dedup[varName]++
			if n > 0 {
				varName = fmt.Sprintf("%s_%d", varName, n)
			}
		}
		switch spec.shape {
		case shapeBlocking:
			if varName == "_skip" {
				e.printf("\t\t\tif _v := %s; _v == nil { return nil }\n", spec.expr)
			} else {
				e.printf("\t\t\t%s := %s\n", varName, spec.expr)
				e.printf("\t\t\tif %s == nil { return nil }\n", varName)
				e.printf("\t\t\t_ = %s\n", varName)
				names = append(names, varName)
			}
		case shapeAlways:
			e.printf("\t\t\t%s := %s\n", varName, spec.expr)
			e.printf("\t\t\t_ = %s\n", varName)
			if varName != "_skip" {
				names = append(names, varName)
			}
		case shapeBoolean:
			e.printf("\t\t\tif !%s { return nil }\n", spec.expr)
		case shapeNoop:
			if hasCut {
				out.WriteString("\t\t\tcut = true\n")
			}
		}
	}
	return names
}

// writeAltReturn emits the closure's return statement: a translated
// action expression when possible, otherwise the bound names as a
// []any (or placeholderMatched if nothing was bound).
func (e *emitter) writeAltReturn(a *Alt, names []string) {
	out := e.buf
	bound := map[string]bool{}
	for _, n := range names {
		bound[n] = true
	}
	if expr, ok := translateAction(a.Action, bound); ok && a.Action != "" {
		e.printf("\t\t\treturn %s\n", expr)
		return
	}
	if len(names) == 0 {
		out.WriteString("\t\t\treturn placeholderMatched\n")
		return
	}
	out.WriteString("\t\t\treturn []any{")
	for i, n := range names {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(n)
	}
	out.WriteString("}\n")
}

// canMemoize reports whether the rule's body should write memo
// entries. Leader wrappers manage their own memo; non-leader cycle
// members never memoize.
func (e *emitter) canMemoize(r *Rule) bool {
	return !r.LeftRecursive
}

type callShape int

const (
	shapeBlocking callShape = iota // expr returns any; nil ⇒ alt fails
	shapeAlways                    // expr returns any; the alt continues regardless
	shapeBoolean                   // expr is a Go bool; false ⇒ alt fails
	shapeNoop                      // emit nothing (cut)
)

type callSpec struct {
	varName string
	expr    string
	shape   callShape
}

// callItem produces the Go expression that parses ni.Item.
func (e *emitter) callItem(ni *NamedItem) callSpec {
	return e.callItemRaw(ni.Item)
}

func (e *emitter) callItemRaw(it Item) callSpec {
	switch x := it.(type) {
	case *NameLeaf:
		return e.callNameLeaf(x)
	case *StringLeaf:
		return e.callStringLeaf(x)
	case *Group:
		return e.callGroup(x)
	case *Opt:
		inner := e.callItemRaw(x.Item)
		return callSpec{varName: "opt", expr: inner.expr, shape: shapeAlways}
	case *Repeat0:
		name := e.artificialFromRepeat(x.Item, false)
		return callSpec{varName: "rep", expr: "parseRule_" + name + "(p)", shape: shapeAlways}
	case *Repeat1:
		name := e.artificialFromRepeat(x.Item, true)
		return callSpec{varName: "rep", expr: "parseRule_" + name + "(p)", shape: shapeBlocking}
	case *Gather:
		name := e.artificialFromGather(x)
		return callSpec{varName: "gat", expr: "parseRule_" + name + "(p)", shape: shapeBlocking}
	case *Forced:
		return e.callForced(x)
	case *PositiveLookahead:
		return e.lookaheadCall(x.Node, true)
	case *NegativeLookahead:
		return e.lookaheadCall(x.Node, false)
	case *Cut:
		return callSpec{shape: shapeNoop}
	case *RHS:
		if len(x.Alts) == 1 && len(x.Alts[0].Items) == 1 {
			return e.callItemRaw(x.Alts[0].Items[0].Item)
		}
		name := e.artificialFromRHS(x, "rhs")
		return callSpec{varName: "g", expr: "parseRule_" + name + "(p)", shape: shapeBlocking}
	}
	return callSpec{varName: "_unk", expr: "nil", shape: shapeBlocking}
}

func (e *emitter) callNameLeaf(x *NameLeaf) callSpec {
	// Mirror CPython's c_generator BASE_NODETYPES special-case: NAME,
	// NUMBER, and STRING leaves resolve to typed expr_ty / Token *
	// helpers rather than to the generic ExpectToken path. Without
	// this, atom rules like `atom: NAME` return the raw token and
	// downstream rules see a *Token where they expect ast.Expr.
	//
	// CPython: Tools/peg_generator/pegen/c_generator.py:157 visit_NameLeaf
	switch x.Value {
	case "NAME":
		return callSpec{varName: "name_var", expr: "nameToken(p)", shape: shapeBlocking}
	case "NUMBER":
		return callSpec{varName: "number_var", expr: "numberToken(p)", shape: shapeBlocking}
	case "STRING":
		return callSpec{varName: "string_var", expr: "stringToken(p)", shape: shapeBlocking}
	}
	if tok, ok := builtinTokens[x.Value]; ok {
		return callSpec{varName: lower(x.Value), expr: "p.ExpectToken(tokenize." + tok + ")", shape: shapeBlocking}
	}
	return callSpec{varName: x.Value, expr: "parseRule_" + x.Value + "(p)", shape: shapeBlocking}
}

func (e *emitter) callStringLeaf(x *StringLeaf) callSpec {
	s, soft := unquoteLeaf(x.Value)
	if soft {
		return callSpec{varName: "kw", expr: "p.ExpectSoftKeyword(" + strconv.Quote(s) + ")", shape: shapeBlocking}
	}
	if tok, ok := exactTokenTypes[s]; ok {
		return callSpec{varName: "op", expr: "p.ExpectToken(tokenize." + tok + ")", shape: shapeBlocking}
	}
	return callSpec{varName: "kw", expr: "p.ExpectName(" + strconv.Quote(s) + ")", shape: shapeBlocking}
}

func (e *emitter) callGroup(x *Group) callSpec {
	if len(x.RHS.Alts) == 1 && len(x.RHS.Alts[0].Items) == 1 {
		return e.callItemRaw(x.RHS.Alts[0].Items[0].Item)
	}
	name := e.artificialFromRHS(x.RHS, "group")
	return callSpec{varName: "g", expr: "parseRule_" + name + "(p)", shape: shapeBlocking}
}

func (e *emitter) callForced(x *Forced) callSpec {
	if sl, ok := x.Node.(*StringLeaf); ok {
		s, soft := unquoteLeaf(sl.Value)
		if !soft {
			if tok, okt := exactTokenTypes[s]; okt {
				return callSpec{varName: "f", expr: "p.ExpectForced(tokenize." + tok + ", " + strconv.Quote(s) + ")", shape: shapeBlocking}
			}
		}
	}
	inner := e.callItemRaw(x.Node)
	return callSpec{varName: "f", expr: inner.expr, shape: shapeBlocking}
}

func (e *emitter) lookaheadCall(node Item, positive bool) callSpec {
	inner := e.callItemRaw(node)
	pol := "true"
	if !positive {
		pol = "false"
	}
	body := "func(p *Parser) any { return " + inner.expr + " }"
	return callSpec{expr: "p.Lookahead(" + pol + ", " + body + ")", shape: shapeBoolean}
}

// artificialFromRepeat synthesizes a helper rule that consumes the
// inner item zero or more (Repeat0) or one or more (Repeat1) times.
func (e *emitter) artificialFromRepeat(inner Item, isOnePlus bool) string {
	prefix := "_loop0"
	if isOnePlus {
		prefix = "_loop1"
	}
	key := prefix + "::" + itemKey(inner)
	if n, ok := e.artCache[key]; ok {
		return n
	}
	name := fmt.Sprintf("%s_%d", prefix, e.nextArtIdx())
	e.artCache[key] = name
	r := &Rule{
		Name: name,
		RHS: &RHS{Alts: []*Alt{{
			Items: []*NamedItem{{Item: inner}},
			Icut:  -1,
		}}},
	}
	r.Memo = false
	e.artificial = append(e.artificial, r)
	// Override the body emitter for loops via a meta-marker: we
	// actually do not emit r the normal way; we synthesize the body
	// in writeRule by detecting the _loop prefix.
	r.Type = "loop_marker"
	if isOnePlus {
		r.Type = "loop1_marker"
	}
	return name
}

// artificialFromGather synthesizes a helper rule for a gather
// `sep.node+`. The body matches a node, then any number of
// `sep node` pairs.
func (e *emitter) artificialFromGather(g *Gather) string {
	key := "_gather::" + itemKey(g.Sep) + "::" + itemKey(g.Node)
	if n, ok := e.artCache[key]; ok {
		return n
	}
	name := fmt.Sprintf("_gather_%d", e.nextArtIdx())
	e.artCache[key] = name
	r := &Rule{
		Name: name,
		Type: "gather_marker",
		RHS: &RHS{Alts: []*Alt{{
			Items: []*NamedItem{
				{Item: g.Sep, Name: "_sep"},
				{Item: g.Node, Name: "_node"},
			},
			Icut: -1,
		}}},
	}
	e.artificial = append(e.artificial, r)
	return name
}

// artificialFromRHS synthesizes a helper rule that wraps a sub-RHS
// (used when the call site needs a multi-alt or multi-item group).
func (e *emitter) artificialFromRHS(rhs *RHS, prefix string) string {
	key := prefix + "::" + rhsKey(rhs)
	if n, ok := e.artCache[key]; ok {
		return n
	}
	name := fmt.Sprintf("_%s_%d", prefix, e.nextArtIdx())
	e.artCache[key] = name
	r := &Rule{Name: name, RHS: rhs}
	e.artificial = append(e.artificial, r)
	return name
}

func (e *emitter) nextArtIdx() int { return len(e.artificial) + 1 }

// rhsKey and itemKey are dedup keys for artificial rules. They must
// be stable across runs so the generated file is byte-identical.
func rhsKey(rhs *RHS) string {
	parts := make([]string, len(rhs.Alts))
	for i, a := range rhs.Alts {
		ips := make([]string, len(a.Items))
		for j, ni := range a.Items {
			tag := ""
			if ni.Name != "" {
				tag = ni.Name + "="
			}
			ips[j] = tag + itemKey(ni.Item)
		}
		parts[i] = strings.Join(ips, " ")
		if a.Action != "" {
			parts[i] += " {" + a.Action + "}"
		}
	}
	return strings.Join(parts, " | ")
}

func itemKey(it Item) string {
	switch x := it.(type) {
	case *NameLeaf:
		return x.Value
	case *StringLeaf:
		return x.Value
	case *Group:
		return "(" + rhsKey(x.RHS) + ")"
	case *Opt:
		return itemKey(x.Item) + "?"
	case *Repeat0:
		return itemKey(x.Item) + "*"
	case *Repeat1:
		return itemKey(x.Item) + "+"
	case *Gather:
		return itemKey(x.Sep) + "." + itemKey(x.Node) + "+"
	case *Forced:
		return "&&" + itemKey(x.Node)
	case *PositiveLookahead:
		return "&" + itemKey(x.Node)
	case *NegativeLookahead:
		return "!" + itemKey(x.Node)
	case *Cut:
		return "~"
	case *RHS:
		return rhsKey(x)
	}
	return "?"
}

// writeActionHelperStubs scans the already-emitted rule bodies for
// references to action helpers (actionAst*, actionPgen*, raiseAction)
// and emits panic-stubs so the generated file compiles. Real
// implementations land alongside the AST surface in M7.
func (e *emitter) writeActionHelperStubs() {
	body := e.bodies.String()
	re := regexp.MustCompile(`\b(actionAst[A-Za-z0-9_]+|actionPgen[A-Za-z0-9_]+|raiseAction)\b`)
	// Names that have hand-written implementations elsewhere in the
	// pegen package. Each entry is a place where the typed AST
	// surface meets the untyped any boundary.
	excluded := map[string]bool{
		"actionPgenMakeModule":             true,
		"actionAstPass":                    true,
		"actionAstBreak":                   true,
		"actionAstContinue":                true,
		"actionAstReturn":                  true,
		"actionAstRaise":                   true,
		"actionAstAssert":                  true,
		"actionAstDelete":                  true,
		"actionAstExpr":                    true,
		"actionAstYield":                   true,
		"actionAstYieldFrom":               true,
		"actionAstImport":                  true,
		"actionAstImportFrom":              true,
		"actionAstIf":                      true,
		"actionAstWhile":                   true,
		"actionAstIfExp":                   true,
		"actionAstTry":                     true,
		"actionAstExceptHandler":           true,
		"actionAstSlice":                   true,
		"actionAstSet":                     true,
		"actionAstListComp":                true,
		"actionAstSetComp":                 true,
		"actionAstGeneratorExp":            true,
		"actionAstMatchAs":                 true,
		"actionAstMatchClass":              true,
		"actionAstMatchMapping":            true,
		"actionAstMatchSequence":           true,
		"actionAstMatchStar":               true,
		"actionAstMatchValue":              true,
		"actionPgenSeqCountDots":           true,
		"actionPgenSingletonSeq":           true,
		"actionPgenSeqInsertInFront":       true,
		"actionPgenJoinNamesWithDot":       true,
		"actionPgenJoinSequences":          true,
		"actionPgenGetExprName":            true,
		"actionPgenInteractiveExit":        true,
		"actionPgenKeyValuePair":           true,
		"actionPgenKeyPatternPair":         true,
		"actionPgenKeywordOrStarred":       true,
		"actionPgenNameDefaultPair":        true,
		"actionPgenConstantFromToken":      true,
		"actionPgenConstantFromString":     true,
		"actionPgenDecodedConstantFromToken": true,
		"actionPgenEnsureImaginary":        true,
		"actionPgenEnsureReal":             true,
		"actionPgenFormattedValue":         true,
		"actionPgenInterpolation":          true,
		"actionPgenConcatenateStrings":     true,
		"actionPgenConcatenateTstrings":    true,
		"actionPgenCheckFstringConversion": true,
		"actionPgenAddTypeCommentToArg":    true,
		"actionPgenArgumentsParsingError":  true,
		"actionPgenClassDefDecorators":     true,
		"actionPgenFunctionDefDecorators":  true,
		"actionPgenCollectCallSeqs":        true,
		"actionPgenMakeArguments":          true,
		"actionPgenNonparenGenexpInCall":   true,
		"actionPgenStarEtc":                true,
		"actionAstAssign":                  true,
		"actionAstAugAssign":               true,
		"actionAstAnnAssign":               true,
		"actionAstBinOp":                   true,
		"actionAstBoolOp":                  true,
		"actionAstUnaryOp":                 true,
		"actionAstCompare":                 true,
		"actionAstNamedExpr":               true,
		"actionAstAwait":                   true,
		"actionAstName":                    true,
		"actionAstAttribute":               true,
		"actionAstSubscript":               true,
		"actionAstStarred":                 true,
		"actionAstTuple":                   true,
		"actionAstList":                    true,
		"actionAstDict":                    true,
		"actionAstCall":                    true,
		"actionAstConstant":                true,
		"actionAstFor":                     true,
		"actionAstAsyncFor":                true,
		"actionAstWith":                    true,
		"actionAstAsyncWith":               true,
		"actionAstMatchSingleton":          true,
		"actionPgenAugoperator":            true,
		"actionPgenCmpopExprPair":          true,
		"actionPgenSetExprContext":         true,
		"actionPgenSeqFlatten":             true,
		"actionPgenSeqAppendToEnd":         true,
		"actionPgenRegisterStmts":          true,
		"actionPgenSlashWithDefault":       true,
		"actionPgenSetupFullFormatSpec":    true,
		"actionPgenJoinedStr":              true,
		"actionAstFunctionDef":             true,
		"actionAstAsyncFunctionDef":        true,
		"actionAstComprehension":           true,
		"actionAstExpression":              true,
		"actionAstInteractive":             true,
		"actionAstFunctionType":            true,
		"actionPgenEmptyArguments":         true,
		"actionAstArg":                     true,
	}
	seen := map[string]bool{}
	for _, m := range re.FindAllString(body, -1) {
		if excluded[m] {
			continue
		}
		seen[m] = true
	}
	if len(seen) == 0 {
		return
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	e.buf.WriteString("// Action helper stubs. The action translator emits calls into\n")
	e.buf.WriteString("// these names; real implementations land with the AST surface.\n")
	for _, n := range names {
		if n == "raiseAction" {
			e.buf.WriteString("func raiseAction(p *Parser, kind string, args ...any) any {\n")
			e.buf.WriteString("\t_ = p\n\t_ = kind\n\t_ = args\n")
			e.buf.WriteString("\tp.SetErrorIndicator(true)\n")
			e.buf.WriteString("\treturn nil\n}\n\n")
			continue
		}
		e.printf("func %s(p *Parser, args ...any) any { _ = p; _ = args; return placeholderMatched }\n", n)
	}
	e.buf.WriteString("\n")
}

// writeDispatch emits the entry-point dispatcher and the
// placeholder helpers the generated rule bodies reference.
func (e *emitter) writeDispatch() {
	e.buf.WriteString("// placeholderMatched is a non-nil sentinel returned by alts\n")
	e.buf.WriteString("// whose items all bind to skip-vars. M6 replaces it with\n")
	e.buf.WriteString("// the translated action result.\n")
	e.buf.WriteString("var placeholderMatched = struct{}{}\n\n")

	e.buf.WriteString("// Dispatch picks the entry-point rule for m and runs it.\n")
	e.buf.WriteString("// Mirrors CPython's _PyPegen_run_parser: try once with\n")
	e.buf.WriteString("// call_invalid_rules off, retry once with it on if the\n")
	e.buf.WriteString("// first pass missed, then surface the rule result. If\n")
	e.buf.WriteString("// the second pass also misses, ErrParserNotImplemented\n")
	e.buf.WriteString("// rides through so callers can fall back to the fixture\n")
	e.buf.WriteString("// path until the action surface fully types up.\n")
	e.buf.WriteString("//\n")
	e.buf.WriteString("// CPython: Parser/pegen.c:946 _PyPegen_run_parser\n")
	e.buf.WriteString("func Dispatch(p *Parser, m StartRule) (any, error) {\n")
	e.buf.WriteString("\tif p == nil { return nil, ErrParserNotImplemented }\n")
	e.buf.WriteString("\tvar result any\n")
	e.buf.WriteString("\tswitch m {\n")
	e.buf.WriteString("\tcase StartFile:\n\t\tresult = parseRule_file(p)\n")
	e.buf.WriteString("\tcase StartSingle:\n\t\tresult = parseRule_interactive(p)\n")
	e.buf.WriteString("\tcase StartEval:\n\t\tresult = parseRule_eval(p)\n")
	e.buf.WriteString("\tcase StartFunctionType:\n\t\tresult = parseRule_func_type(p)\n")
	e.buf.WriteString("\tdefault:\n\t\treturn nil, ErrParserNotImplemented\n")
	e.buf.WriteString("\t}\n")
	e.buf.WriteString("\tif result == nil {\n")
	e.buf.WriteString("\t\tp.SetCallInvalid(true)\n")
	e.buf.WriteString("\t\tp.Reset(0)\n")
	e.buf.WriteString("\t\tswitch m {\n")
	e.buf.WriteString("\t\tcase StartFile:\n\t\t\tresult = parseRule_file(p)\n")
	e.buf.WriteString("\t\tcase StartSingle:\n\t\t\tresult = parseRule_interactive(p)\n")
	e.buf.WriteString("\t\tcase StartEval:\n\t\t\tresult = parseRule_eval(p)\n")
	e.buf.WriteString("\t\tcase StartFunctionType:\n\t\t\tresult = parseRule_func_type(p)\n")
	e.buf.WriteString("\t\t}\n")
	e.buf.WriteString("\t\tif result == nil { return nil, ErrParserNotImplemented }\n")
	e.buf.WriteString("\t}\n")
	e.buf.WriteString("\treturn result, nil\n")
	e.buf.WriteString("}\n")
}

func lower(s string) string { return strings.ToLower(s) }

func isInvalidRule(name string) bool { return strings.HasPrefix(name, "invalid_") }
