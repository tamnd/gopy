// C-expression action translator. Each grammar alt has a C action
// body like `_PyAST_Module(a, NULL, p->arena)`; the emitter passes
// it through translateAction to produce a Go expression that builds
// the AST node or calls a pegen helper.
//
// The translator is intentionally narrow: it understands the
// constructor calls (_PyAST_Foo), pegen helpers (_PyPegen_foo),
// the location macro EXTRA, the CHECK and RAISE_ macros, and the
// pointer-deref operator `->`. Anything else falls back to
// placeholderMatched and a TODO comment so the file still compiles.
//
// CPython: Tools/peg_generator/pegen/c_generator.py action handling
// CPython: Parser/pegen.h CHECK_*, RAISE_*, EXTRA macros

package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// translateAction returns (goExpr, ok). ok is false when the action
// hit a pattern the translator does not yet handle; the emitter
// falls back to placeholderMatched in that case. bound names the
// in-scope locals from the alt; any other identifier (other than a
// handful of constants) causes a bail-out so we never emit a
// reference to an undefined symbol.
func translateAction(body string, bound map[string]bool) (string, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "placeholderMatched", true
	}
	t := &ctok{src: body}
	tokens, err := t.all()
	if err != nil {
		return "", false
	}
	tr := &cTranslator{toks: tokens, bound: bound}
	expr, ok := tr.parseExpr()
	if !ok || tr.pos != len(tr.toks) {
		return "", false
	}
	return expr, true
}

// ctok is a small C-flavored tokenizer. It produces identifiers,
// numbers, strings, and operator tokens. The translator consumes
// these to emit Go source.
type ctok struct {
	src string
	pos int
}

type ctokTok struct {
	kind string // "id", "num", "str", "op"
	text string
}

func (t *ctok) all() ([]ctokTok, error) {
	var out []ctokTok
	for t.pos < len(t.src) {
		tok, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, tok)
		}
	}
	return out, nil
}

func (t *ctok) next() (ctokTok, bool, error) {
	c := t.src[t.pos]
	switch {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		t.pos++
		return ctokTok{}, false, nil
	case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		start := t.pos
		for t.pos < len(t.src) && isIdentByte(t.src[t.pos]) {
			t.pos++
		}
		return ctokTok{kind: "id", text: t.src[start:t.pos]}, true, nil
	case c >= '0' && c <= '9':
		start := t.pos
		for t.pos < len(t.src) && (isIdentByte(t.src[t.pos]) || t.src[t.pos] == '.') {
			t.pos++
		}
		return ctokTok{kind: "num", text: t.src[start:t.pos]}, true, nil
	case c == '"' || c == '\'':
		tok, err := t.readString(c)
		return tok, err == nil, err
	default:
		return t.readOp(), true, nil
	}
}

func (t *ctok) readString(quote byte) (ctokTok, error) {
	start := t.pos
	t.pos++
	for t.pos < len(t.src) {
		c := t.src[t.pos]
		if c == '\\' && t.pos+1 < len(t.src) {
			t.pos += 2
			continue
		}
		if c == quote {
			t.pos++
			return ctokTok{kind: "str", text: t.src[start:t.pos]}, nil
		}
		t.pos++
	}
	return ctokTok{}, fmt.Errorf("unterminated string in C action")
}

func (t *ctok) readOp() ctokTok {
	for _, op := range []string{"->", "==", "!=", "<=", ">=", "&&", "||", "<<", ">>"} {
		if strings.HasPrefix(t.src[t.pos:], op) {
			t.pos += len(op)
			return ctokTok{kind: "op", text: op}
		}
	}
	c := t.src[t.pos]
	t.pos++
	return ctokTok{kind: "op", text: string(c)}
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// cTranslator walks the C token stream and emits Go.
type cTranslator struct {
	toks  []ctokTok
	pos   int
	bound map[string]bool
}

func (tr *cTranslator) peek() ctokTok {
	if tr.pos >= len(tr.toks) {
		return ctokTok{}
	}
	return tr.toks[tr.pos]
}

func (tr *cTranslator) advance() ctokTok {
	t := tr.toks[tr.pos]
	tr.pos++
	return t
}

// parseExpr handles the top-level expression of an action body.
// Most actions are a single call; some are a simple identifier or
// a parenthesised conditional. We accept what we can and reject
// what we cannot.
func (tr *cTranslator) parseExpr() (string, bool) {
	return tr.parsePrimary()
}

func (tr *cTranslator) parsePrimary() (string, bool) {
	if tr.pos >= len(tr.toks) {
		return "", false
	}
	t := tr.peek()
	switch t.kind {
	case "id":
		return tr.parseIDExpr()
	case "num":
		tr.advance()
		return t.text, true
	case "str":
		tr.advance()
		return goString(t.text), true
	case "op":
		if t.text == "(" {
			tr.advance()
			inner, ok := tr.parseExpr()
			if !ok {
				return "", false
			}
			if tr.peek().text != ")" {
				return "", false
			}
			tr.advance()
			return "(" + inner + ")", true
		}
		if t.text == "&" || t.text == "*" || t.text == "-" || t.text == "!" {
			tr.advance()
			rest, ok := tr.parsePrimary()
			if !ok {
				return "", false
			}
			if t.text == "&" {
				// C address-of has no Go counterpart; drop it.
				return rest, true
			}
			if t.text == "*" {
				// Pointer deref; drop, Go uses values directly.
				return rest, true
			}
			return t.text + rest, true
		}
	}
	return "", false
}

// parseIDExpr handles identifiers that may be plain refs, calls, or
// chains of `->` / `.` accesses.
func (tr *cTranslator) parseIDExpr() (string, bool) {
	t := tr.advance()
	name := t.text
	switch name {
	case "NULL":
		return "nil", true
	case "p":
		// The parser pointer is always in scope inside generated bodies.
		return "p", true
	case "EXTRA":
		// EXTRA expands to start/end location args. We surface a
		// sentinel constant that helpers translate into actual
		// location args; callers strip it from arg lists.
		return "EXTRA", true
	case "true", "false":
		return name, true
	}
	// Function call?
	if tr.peek().text == "(" {
		return tr.parseCall(name)
	}
	// Member access chains drop us into typed-field territory. Bound
	// vars in the generated closures are interface{}, so a field
	// access cannot compile. Bail out and let the alt fall back to
	// placeholderMatched until the runtime types are firmed up.
	if op := tr.peek(); op.text == "->" || op.text == "." {
		return "", false
	}
	// Only accept identifiers that are actually in scope. Anything
	// else (AST enum constants like Store, Add, etc., free helpers,
	// stray macros) would not compile.
	if tr.bound == nil || !tr.bound[name] {
		return "", false
	}
	return goIdent(name), true
}

// parseCall translates a C call. fname is the function name we just
// consumed; "(" is the next token.
func (tr *cTranslator) parseCall(fname string) (string, bool) {
	tr.advance() // consume "("
	var args []string
	if tr.peek().text != ")" {
		for {
			a, ok := tr.parseExpr()
			if !ok {
				return "", false
			}
			args = append(args, a)
			if tr.peek().text == "," {
				tr.advance()
				continue
			}
			break
		}
	}
	if tr.peek().text != ")" {
		return "", false
	}
	tr.advance()
	return translateCall(fname, args)
}

// translateCall maps a C function name + arg list onto a Go
// expression. Returns ok=false for patterns we cannot handle yet.
func translateCall(fname string, args []string) (string, bool) {
	// CHECK macros are pass-through wrappers in C; the value is the
	// last arg (the expression), the leading args are type tags or
	// version gates that the C parser uses for diagnostics.
	switch fname {
	case "CHECK", "CHECK_NULL_ALLOWED":
		if len(args) >= 2 {
			return args[len(args)-1], true
		}
	case "CHECK_VERSION":
		if len(args) >= 4 {
			return args[len(args)-1], true
		}
	case "RAISE_SYNTAX_ERROR", "RAISE_SYNTAX_ERROR_KNOWN_LOCATION",
		"RAISE_SYNTAX_ERROR_KNOWN_RANGE", "RAISE_SYNTAX_ERROR_ON_NEXT_TOKEN",
		"RAISE_SYNTAX_ERROR_STARTING_FROM", "RAISE_SYNTAX_ERROR_INVALID_TARGET",
		"RAISE_INDENTATION_ERROR", "RAISE_ERROR_KNOWN_LOCATION":
		return "raiseAction(p, " + strconv.Quote(fname) + ", " + joinArgsForRaise(args) + ")", true
	}
	if strings.HasPrefix(fname, "_PyAST_") {
		ctor := fname[len("_PyAST_"):]
		return "actionAst" + capName(ctor) + helperArgList(stripExtra(args)), true
	}
	if strings.HasPrefix(fname, "_PyPegen_") {
		helper := fname[len("_PyPegen_"):]
		return "actionPgen" + capName(helper) + helperArgList(stripExtra(args)), true
	}
	return "", false
}

// stripExtra removes the EXTRA sentinel from an argument list.
// Helper functions take p as their first parameter and pull the
// location fields off the parser internally.
func stripExtra(args []string) []string {
	out := args[:0]
	for _, a := range args {
		if strings.TrimSpace(a) == "EXTRA" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func helperArgList(args []string) string {
	if len(args) == 0 {
		return "(p)"
	}
	return "(p, " + strings.Join(args, ", ") + ")"
}

func joinArgsForRaise(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args, ", ")
}

func goIdent(s string) string {
	return s
}

// capName turns a C-style snake_case identifier into Go CamelCase.
// `make_module` becomes `MakeModule`, `seq_flatten` becomes
// `SeqFlatten`. Leading underscore is dropped.
func capName(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		if unicode.IsLower(r[0]) {
			r[0] = unicode.ToUpper(r[0])
		}
		b.WriteString(string(r))
	}
	return b.String()
}

// goString rewraps a C-style string literal as a Go raw string.
// CPython actions occasionally use C escapes; the simple cases pass
// through fine because both languages accept the same `\n`, `\t`,
// `\\`, `\"` sequences inside double quotes.
func goString(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return "\"" + s[1:len(s)-1] + "\""
	}
	return s
}
