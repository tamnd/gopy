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
	return tr.parseTernary()
}

// parseTernary recognizes C's `cond ? yes : no` and translates it
// into a Go IIFE that returns the matching branch. Both branches are
// kept lazy by being inside the func body, so a CHECK or RAISE in
// the alternative side does not run when the condition picks the
// other side.
func (tr *cTranslator) parseTernary() (string, bool) {
	cond, ok := tr.parseInfix()
	if !ok {
		return "", false
	}
	if tr.peek().text != "?" {
		return cond, true
	}
	tr.advance()
	yes, ok := tr.parseInfix()
	if !ok {
		return "", false
	}
	if tr.peek().text != ":" {
		return "", false
	}
	tr.advance()
	no, ok := tr.parseTernary()
	if !ok {
		return "", false
	}
	return "func() any { if truthy(" + cond + ") { return " + yes + " }; return " + no + " }()", true
}

// parseInfix accepts a small set of C infix operators that show up
// in grammar action bodies. Most actions never need them; the few
// that do (asdl_seq_LEN(p) == 1) read a primary, an op, and another
// primary. We do not implement precedence beyond left-to-right.
func (tr *cTranslator) parseInfix() (string, bool) {
	left, ok := tr.parsePrimary()
	if !ok {
		return "", false
	}
	for {
		op := tr.peek().text
		switch op {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			tr.advance()
			right, ok := tr.parsePrimary()
			if !ok {
				return "", false
			}
			left = left + " " + op + " " + right
		default:
			return left, true
		}
	}
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
			// C type cast pattern: `(typename ...*)expr`. Skip the
			// cast and parse the suffix expression. Recognized when
			// the first token after `(` is an identifier whose name
			// looks like a C type and is followed by zero or more
			// `*` then `)`.
			if cast, ok := tr.tryParseCast(); ok {
				_ = cast
				return tr.parsePrimary()
			}
			inner, ok := tr.parseExpr()
			if !ok {
				return "", false
			}
			if tr.peek().text != ")" {
				return "", false
			}
			tr.advance()
			// A parenthesised expression may be followed by a member
			// access chain (`((expr_ty) b)->v.Call.args` etc). The
			// inner string already names the receiver in Go, so feed
			// it back through parseMemberAccess.
			if op := tr.peek(); op.text == "->" || op.text == "." {
				return tr.parseMemberAccess(inner)
			}
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

// astEnumConstants names the C asdl enum values that the grammar uses
// directly (Add, Sub, Eq, Load, Store, ...). The Go AST package
// re-exports them as package-level constants, so a bare identifier in
// the grammar maps to ast.Foo.
//
// CPython: Include/internal/pycore_ast.h asdl enum tags.
var astEnumConstants = map[string]string{
	"Load":  "ast.Load",
	"Store": "ast.Store",
	"Del":   "ast.Del",

	"And": "ast.And",
	"Or":  "ast.Or",

	"Add":      "ast.Add",
	"Sub":      "ast.Sub",
	"Mult":     "ast.Mult",
	"MatMult":  "ast.MatMult",
	"Div":      "ast.Div",
	"Mod":      "ast.ModOperator",
	"Pow":      "ast.Pow",
	"LShift":   "ast.LShift",
	"RShift":   "ast.RShift",
	"BitOr":    "ast.BitOr",
	"BitXor":   "ast.BitXor",
	"BitAnd":   "ast.BitAnd",
	"FloorDiv": "ast.FloorDiv",

	"Invert": "ast.Invert",
	"Not":    "ast.Not",
	"UAdd":   "ast.UAdd",
	"USub":   "ast.USub",

	"Eq":    "ast.Eq",
	"NotEq": "ast.NotEq",
	"Lt":    "ast.Lt",
	"LtE":   "ast.LtE",
	"Gt":    "ast.Gt",
	"GtE":   "ast.GtE",
	"Is":    "ast.Is",
	"IsNot": "ast.IsNot",
	"In":    "ast.In",
	"NotIn": "ast.NotIn",
}

// pyConstants names the C global singletons (Py_True, Py_None, ...)
// that grammar actions reach into when building Constant nodes. The
// Go side surfaces them through helper sentinels so the action layer
// can land them as ast.Constant values.
var pyConstants = map[string]string{
	"Py_True":     "pyTrueSentinel",
	"Py_False":    "pyFalseSentinel",
	"Py_None":     "pyNoneSentinel",
	"Py_Ellipsis": "pyEllipsisSentinel",
}

// cTypeNames lists the C / asdl type names the grammar uses inside
// cast expressions. The translator drops the cast: the Go side
// already handles values as interface{}.
var cTypeNames = map[string]bool{
	"asdl_stmt_seq":          true,
	"asdl_expr_seq":          true,
	"asdl_alias_seq":         true,
	"asdl_arg_seq":           true,
	"asdl_excepthandler_seq": true,
	"asdl_keyword_seq":       true,
	"asdl_match_case_seq":    true,
	"asdl_pattern_seq":       true,
	"asdl_type_param_seq":    true,
	"asdl_withitem_seq":      true,
	"asdl_comprehension_seq": true,
	"asdl_int_seq":           true,
	"asdl_seq":               true,
	"asdl_identifier_seq":    true,
	"expr_ty":                true,
	"stmt_ty":                true,
	"arguments_ty":           true,
	"arg_ty":                 true,
	"keyword_ty":             true,
	"comprehension_ty":       true,
	"alias_ty":               true,
	"withitem_ty":            true,
	"match_case_ty":          true,
	"pattern_ty":             true,
	"excepthandler_ty":       true,
	"AugOperator":            true,
	"CmpopExprPair":          true,
	"KeyValuePair":           true,
	"KeyPatternPair":         true,
	"KeywordOrStarred":       true,
	"NameDefaultPair":        true,
	"SlashWithDefault":       true,
	"StarEtc":                true,
	"PyObject":               true,
	"int":                    true,
	"void":                   true,
	"char":                   true,
	"const":                  true,
}

// tryParseCast checks whether the current cursor (just past an open
// paren) is a C type cast. If so, advances past the typename, the
// pointer stars, and the closing paren and returns ok=true. Leaves
// the cursor unchanged otherwise.
func (tr *cTranslator) tryParseCast() (string, bool) {
	saved := tr.pos
	if tr.peek().kind != "id" {
		return "", false
	}
	name := tr.peek().text
	if !cTypeNames[name] {
		return "", false
	}
	tr.advance()
	// Skip an optional `const` after the type — appears in some
	// grammar casts but does not affect semantics.
	if tr.peek().kind == "id" && tr.peek().text == "const" {
		tr.advance()
	}
	for tr.peek().text == "*" {
		tr.advance()
	}
	if tr.peek().text != ")" {
		tr.pos = saved
		return "", false
	}
	tr.advance() // consume ')'
	return name, true
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
		// Allow `p->arena` style selectors so action bodies that pass
		// the arena (which is later stripped) translate cleanly.
		if op := tr.peek(); op.text == "->" || op.text == "." {
			return tr.parseMemberAccess("p")
		}
		return "p", true
	case "EXTRA":
		// EXTRA expands to start/end location args. We surface a
		// sentinel constant that helpers translate into actual
		// location args; callers strip it from arg lists.
		return "EXTRA", true
	case "true", "false":
		return name, true
	}
	// Bare C type names show up inside CHECK(type_ty, expr) as a
	// type tag. translateCall drops the leading args, so any string
	// works; emit a typed-nil so the result still type-checks if a
	// later pattern slips through. Type tags often appear with one
	// or more `*` suffixes (`asdl_expr_seq*`, `expr_ty**`); consume
	// them here so the surrounding parseCall sees a clean `,` next.
	if cTypeNames[name] {
		for tr.peek().text == "*" {
			tr.advance()
		}
		return "(any)(nil)", true
	}
	if v, ok := astEnumConstants[name]; ok {
		// Bail if a member-access chain follows an enum; not
		// expected today and the Go form differs anyway.
		if op := tr.peek(); op.text == "->" || op.text == "." {
			return "", false
		}
		return v, true
	}
	if v, ok := pyConstants[name]; ok {
		return v, true
	}
	// Function call?
	if tr.peek().text == "(" {
		return tr.parseCall(name)
	}
	// Member access chains: bound names are interface{} so field
	// access cannot compile directly. We recognize three shapes:
	//
	//   <bound>->kind                 → AugOperator helper output, the
	//                                   bound name already carries the
	//                                   operator so drop the selector.
	//   <bound>->v.Name.id            → identifier text from a NAME
	//                                   token; map to nameIDOf(<bound>).
	//   <bound>->v.<Type>.<field>     → field on an expr_ty union; map
	//                                   to a typed accessor helper.
	if op := tr.peek(); op.text == "->" || op.text == "." {
		if tr.bound == nil || !tr.bound[name] {
			return "", false
		}
		expr, ok := tr.parseMemberAccess(goIdent(name))
		return expr, ok
	}
	// Only accept identifiers that are actually in scope. Anything
	// else (free helpers, stray macros) would not compile.
	if tr.bound == nil || !tr.bound[name] {
		return "", false
	}
	return goIdent(name), true
}

// parseMemberAccess walks a chain of `->` / `.` selectors that
// follows a bound name. The bound name itself was already consumed
// and `recv` carries the Go expression that names it. The cursor is
// at the first `->` or `.`.
//
// We recognize:
//
//	<recv>->kind             → recv (the operator/kind value flows
//	                            through the bound name unchanged)
//	<recv>->v.Name.id        → nameIDOf(recv)
//	<recv>->v.Call.args      → callArgsOf(recv)
//	<recv>->v.Call.keywords  → callKwOf(recv)
//
// Anything else fails the translation and the alt falls back to the
// raw arg-list shape.
func (tr *cTranslator) parseMemberAccess(recv string) (string, bool) {
	tr.advance() // consume the leading -> or .
	if tr.peek().kind != "id" {
		return "", false
	}
	first := tr.advance().text
	if first == "kind" {
		return recv, true
	}
	if first == "arena" {
		// `p->arena` is stripped by stripExtra; return a recognizable
		// form so the surrounding call-arg join sees an exact match.
		return recv + ".arena", true
	}
	// KeyValuePair / KeyPatternPair / NameDefaultPair top-level
	// fields. The Go encoding stores these as `[2]any{a, b}` so we
	// pull out the column directly.
	switch first {
	case "key":
		return "kvKey(" + recv + ")", true
	case "value":
		return "kvValue(" + recv + ")", true
	case "name":
		return "kvKey(" + recv + ")", true
	case "pattern":
		return "kvValue(" + recv + ")", true
	}
	if first != "v" {
		return "", false
	}
	if tr.peek().text != "." && tr.peek().text != "->" {
		return "", false
	}
	tr.advance()
	if tr.peek().kind != "id" {
		return "", false
	}
	typeName := tr.advance().text
	if tr.peek().text != "." && tr.peek().text != "->" {
		return "", false
	}
	tr.advance()
	if tr.peek().kind != "id" {
		return "", false
	}
	field := tr.advance().text
	switch typeName + "." + field {
	case "Name.id":
		return "nameIDOf(" + recv + ")", true
	case "Call.args":
		return "callArgsOf(" + recv + ")", true
	case "Call.keywords":
		return "callKwOf(" + recv + ")", true
	}
	return "", false
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
	case "NEW_TYPE_COMMENT":
		// Macro that wraps an optional TYPE_COMMENT token into a
		// PyObject* string. The action helpers accept the raw token
		// and decode internally, so just pass the token through.
		if len(args) == 2 {
			return args[1], true
		}
	case "INVALID_VERSION_CHECK":
		// Version-gate macro: emits the body when the runtime is at
		// least the named version. Pass-through to the body arg.
		if len(args) >= 3 {
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
// location fields off the parser internally. The CPython arena
// pointer (`p->arena`, translated as `p.arena`) is also stripped
// since helpers allocate using Go memory directly.
func stripExtra(args []string) []string {
	out := args[:0]
	for _, a := range args {
		t := strings.TrimSpace(a)
		if t == "EXTRA" || t == "p.arena" || t == "p->arena" {
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
