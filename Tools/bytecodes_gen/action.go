// Action body translator (B6). Walks the raw token slice that survived
// from the parser and tries to render a Go body for the dispatch arm.
//
// The translator is opportunistic, mirroring parser_gen's approach: it
// understands a fixed set of control macros and a small calling
// convention for `_Py*` helpers, and bails out to a panic-stub for
// anything it cannot type. As the typed object surface lands, more
// shapes flip from panic-stub to real translation without touching the
// generator's other stages.
//
// CPython: Tools/cases_generator/generators_common.py.

package main

import (
	"fmt"
	"strings"
)

// TranslateBody renders the action body for one analyzed instruction.
// Returns ok=false (with a non-nil note) when a shape is not yet
// supported, so the emitter can fall back to a panic-stub arm.
//
// terminates=true means the rendered body already emits its own
// dispatch return (e.g. JUMPBY); the emitter must not append the
// default `return e.advance()` tail in that case.
//
// The output is a Go statement block (no surrounding braces); the
// caller indents and wraps it.
func TranslateBody(body []dslTok, sig *SignatureAnalysis) (goSrc string, terminates, ok bool, note string) {
	// Outputs with a Sized variadic shape or an explicit :type
	// annotation need the typed helper layer; we only translate the
	// simple stack-ref outputs today.
	for _, o := range sig.Outputs {
		if o.Sized {
			return "", false, false, "sized output not yet handled by action translator"
		}
		if o.Type != "" {
			return "", false, false, fmt.Sprintf("typed output %q not yet handled by action translator", o.Type)
		}
	}
	t := &actionTranslator{
		sig:      sig,
		bound:    bindNames(sig),
		toks:     stripWhitespace(body),
		writer:   &strings.Builder{},
		assigned: map[string]bool{},
		locals:   map[string]bool{},
	}
	if err := t.run(); err != nil {
		return "", false, false, err.Error()
	}
	// Every output must have been assigned by the body; otherwise the
	// epilogue would push an undefined slot.
	for _, o := range sig.Outputs {
		if o.Name == "" || o.Name == "unused" {
			continue
		}
		if !t.assigned[o.Name] {
			return "", false, false, fmt.Sprintf("output %q never assigned", o.Name)
		}
	}
	return t.writer.String(), t.terminates, true, ""
}

type actionTranslator struct {
	sig        *SignatureAnalysis
	bound      map[string]string // name -> "in"/"out"
	toks       []dslTok
	pos        int
	writer     *strings.Builder
	terminates bool            // body emits its own dispatch return
	assigned   map[string]bool // output names that got assigned
	locals     map[string]bool // C-locals declared in the body
}

func (t *actionTranslator) run() error {
	if len(t.toks) == 0 {
		return nil
	}
	// Statement-level walker. We only recognize a handful of
	// statement shapes today; anything else trips the fallback.
	for t.pos < len(t.toks) {
		if err := t.translateStmt(); err != nil {
			return err
		}
	}
	return nil
}

func (t *actionTranslator) translateStmt() error {
	tk := t.toks[t.pos]
	switch tk.Text {
	case "DEOPT_IF":
		return t.translateControlIf("return e.deoptHere()")
	case "ERROR_IF":
		return t.translateErrorIf()
	case "EXIT_IF":
		return t.translateControlIf("return e.exitTrace()")
	case "DECREF_INPUTS":
		return t.translateNullary(fmt.Sprintf("e.decrefInputs(%d)", len(t.sig.Inputs)))
	case "INPUTS_DEAD":
		return t.translateNullary("// INPUTS_DEAD: no-op in refcount-only path")
	case "STAT_INC", "STAT_DEC":
		return t.skipParenthesised()
	case "DEAD":
		// DEAD(name) marks a name as no longer referenced after this
		// point. The refcount-only path has nothing to do; the stack
		// slot for the input has already been popped by the prologue.
		return t.skipParenthesised()
	case "SYNC_SP", "DISPATCH", "DISPATCH_GOTO", "DISPATCH_SAME_OPARG",
		"ADVANCE_ADAPTIVE_COUNTER", "PAUSE_ADAPTIVE_COUNTER":
		// Cache/dispatch macros that are no-ops for v0.6 (no
		// specializer, no computed-goto). The eval loop drives every
		// dispatch through the main switch, so DISPATCH() is implicit.
		return t.skipParenthesised()
	case "assert":
		// CPython sprinkles assert(invariant) through bytecodes; Go has
		// no compiled-out assert. The body's behavior holds regardless,
		// so consume `assert(...)` and any trailing `;` and continue.
		return t.skipParenthesised()
	case "JUMPBY":
		return t.translateJumpBy()
	case "PyStackRef_CLOSE", "PyStackRef_XCLOSE":
		// Stack-ref close: drop the runtime ref. CLOSE asserts non-null;
		// XCLOSE tolerates null. Both map to .Close() on the popped
		// local, since stackref.Ref's Close already null-checks.
		return t.translateUnaryCall(".Close()")
	case "GETLOCAL":
		// `GETLOCAL(oparg) = expr;` — CPython uses GETLOCAL as both
		// rvalue (handled in expression parsing) and lvalue (handled
		// here). The lvalue case translates to e.setLocal.
		return t.translateGetlocalAssign()
	case "_PyStackRef":
		// `_PyStackRef name = expr;` — C-local declaration. The Go
		// equivalent is `name := expr`; we track the name so later
		// statements that reference it (e.g. PyStackRef_CLOSE(name))
		// can find it.
		return t.translateLocalDecl()
	}
	// Output assignment: `name = expr ;` where name is a bound output.
	if isBareIdent(tk.Text) && t.bound[tk.Text] == "out" {
		return t.translateOutputAssign(tk.Text)
	}
	return fmt.Errorf("unrecognized token at action body start: %q", tk.Text)
}

// translateLocalDecl handles `_PyStackRef NAME = EXPR;`. The Go output
// is `NAME := <go-expr>`; the name is recorded as a declared C-local
// so unary-call helpers (e.g. PyStackRef_CLOSE(NAME)) can resolve it.
func (t *actionTranslator) translateLocalDecl() error {
	t.pos++ // _PyStackRef
	if t.pos >= len(t.toks) {
		return fmt.Errorf("expected name after _PyStackRef")
	}
	name := t.toks[t.pos].Text
	if !isBareIdent(name) {
		return fmt.Errorf("expected identifier after _PyStackRef, got %q", name)
	}
	t.pos++
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after _PyStackRef %s", name)
	}
	t.pos++
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	rhsExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("_PyStackRef %s rhs: %w", name, err)
	}
	t.locals[name] = true
	fmt.Fprintf(t.writer, "%s := %s\n", goLocalName(name), rhsExpr)
	return nil
}

// translateGetlocalAssign handles `GETLOCAL(<idx>) = <expr>;`. The
// LHS macro doubles as an lvalue in CPython; we route it through the
// e.setLocal helper which already handles closing the previous slot.
func (t *actionTranslator) translateGetlocalAssign() error {
	t.pos++ // GETLOCAL
	idxToks, err := t.takeParenTokens()
	if err != nil {
		return err
	}
	idxExpr, err := t.translateExprFromStrings(idxToks)
	if err != nil {
		return fmt.Errorf("GETLOCAL lvalue index: %w", err)
	}
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after GETLOCAL(...) lvalue")
	}
	t.pos++
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	rhsExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("GETLOCAL lvalue rhs: %w", err)
	}
	fmt.Fprintf(t.writer, "e.setLocal(int(%s), %s)\n", idxExpr, rhsExpr)
	return nil
}

// takeParenTokens is the token-slice analogue of takeParenthesised:
// returns the raw inner tokens (still as text) so they can be fed back
// into translateExpr without losing structure.
func (t *actionTranslator) takeParenTokens() ([]string, error) {
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "(" {
		return nil, fmt.Errorf("expected '(' at position %d", t.pos)
	}
	t.pos++
	depth := 1
	out := []string{}
	for t.pos < len(t.toks) && depth > 0 {
		tk := t.toks[t.pos]
		switch tk.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				t.pos++
				return out, nil
			}
		}
		out = append(out, tk.Text)
		t.pos++
	}
	return nil, fmt.Errorf("unterminated '(' at position %d", t.pos)
}

// translateExprFromStrings is the version of translateExpr that takes
// raw token strings (already split by the caller).
func (t *actionTranslator) translateExprFromStrings(toks []string) (string, error) {
	return t.translateExpr(toks)
}

// translateOutputAssign handles `name = expr ;` where name is a
// declared output of the instruction. The right-hand side is parsed
// from a small vocabulary of CPython expressions; unrecognized shapes
// bail so the emitter can fall back to the panic-stub.
func (t *actionTranslator) translateOutputAssign(name string) error {
	t.pos++ // name
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after output name %q", name)
	}
	t.pos++ // =
	// Collect the RHS up to the semicolon.
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	goExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("output assign %q: %w", name, err)
	}
	fmt.Fprintf(t.writer, "%s = %s\n", goLocalName(name), goExpr)
	t.assigned[name] = true
	return nil
}

// translateExpr renders a small CPython expression vocabulary into Go.
// The vocabulary grows as more opcode bodies migrate. The parser is a
// tiny recursive-descent walker over the token stream.
func (t *actionTranslator) translateExpr(toks []string) (string, error) {
	p := &exprParser{toks: toks, bound: t.bound, locals: t.locals}
	out, err := p.parse()
	if err != nil {
		return "", err
	}
	if p.pos != len(toks) {
		return "", fmt.Errorf("trailing tokens after expression: %q", strings.Join(toks[p.pos:], " "))
	}
	return out, nil
}

// exprParser walks a flat token slice and produces a Go expression.
// It only understands the CPython idioms that have been ported so
// far; unknown shapes bail with an error so the emitter falls back to
// a panic-stub.
type exprParser struct {
	toks   []string
	pos    int
	bound  map[string]string
	locals map[string]bool
}

func (p *exprParser) parse() (string, error) {
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (string, error) {
	if p.pos >= len(p.toks) {
		return "", fmt.Errorf("unexpected end of expression")
	}
	tk := p.toks[p.pos]
	p.pos++
	switch tk {
	case "PyStackRef_NULL":
		return "stackref.Null", nil
	case "PyStackRef_True":
		return "stackref.True", nil
	case "PyStackRef_False":
		return "stackref.False", nil
	case "PyStackRef_DUP", "PyStackRef_Borrow":
		// CPython distinguishes owned (DUP) from borrowed (Borrow) refs;
		// under Go's GC the distinction collapses, so both render the
		// same way.
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".Dup()", nil
	case "PyStackRef_IsNull":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".IsNull()", nil
	case "GETLOCAL":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("e.localAt(int(%s))", arg), nil
	}
	if tk == "oparg" {
		return "oparg", nil
	}
	if isBareIdent(tk) {
		if dir, ok := p.bound[tk]; ok && dir == "in" {
			return goLocalName(tk), nil
		}
		if p.locals[tk] {
			return goLocalName(tk), nil
		}
	}
	return "", fmt.Errorf("unexpected token %q in expression", tk)
}

// parseCallArg consumes ( <expr> ) and returns the rendered inner
// expression. Single-arg callees only for now; multi-arg can layer on
// when an opcode needs it.
func (p *exprParser) parseCallArg() (string, error) {
	if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
		return "", fmt.Errorf("expected '(' after function name")
	}
	p.pos++
	inner, err := p.parsePrimary()
	if err != nil {
		return "", err
	}
	if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
		return "", fmt.Errorf("expected ')' to close call")
	}
	p.pos++
	return inner, nil
}

// translateJumpBy emits the dispatch return for `JUMPBY(<expr>);`.
// CPython's JUMPBY adjusts next_instr by oparg codeunits *relative to
// the instruction following the current one*, so our jumpBy helper
// adds 1 to land on the same place.
func (t *actionTranslator) translateJumpBy() error {
	t.pos++ // JUMPBY
	arg, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	arg = strings.TrimSpace(arg)
	if !isBareIdent(arg) || arg != "oparg" {
		return fmt.Errorf("JUMPBY arg %q is not the bare 'oparg' identifier", arg)
	}
	fmt.Fprintln(t.writer, "return e.jumpBy(int(oparg) + 1), nil, nil, false, nil")
	t.terminates = true
	return nil
}

// translateControlIf emits:
//
//	if <cond> { <action> }
//
// for `MACRO(cond);` shapes where the action is a fixed string.
func (t *actionTranslator) translateControlIf(action string) error {
	t.pos++ // macro name
	cond, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	fmt.Fprintf(t.writer, "if %s { %s }\n", cond, action)
	return nil
}

// translateErrorIf handles `ERROR_IF(cond);` by routing through the
// eval loop's error helper. CPython 3.14 dropped the label argument;
// the macro always jumps to the per-instruction `error` label, so we
// always emit the same `e.error("error")` return.
//
// CPython: Tools/cases_generator/generators_common.py error_if.
func (t *actionTranslator) translateErrorIf() error {
	t.pos++ // ERROR_IF
	cond, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	cond = strings.TrimSpace(cond)
	if cond == "true" {
		// Unconditional error path. CPython skips the `if` and emits
		// the jump straight; we mirror that.
		fmt.Fprintln(t.writer, `return 0, e.error("error")`)
		t.terminates = true
		return nil
	}
	fmt.Fprintf(t.writer, "if %s { return 0, e.error(\"error\") }\n", cond)
	return nil
}

// translateUnaryCall handles `MACRO(arg);` where arg is a single bound
// stack-slot name. Emits `arg<suffix>` (e.g. `.Close()`). Bails when
// the argument is anything more interesting than a bare identifier,
// since those need typed-helper translation that we do not have yet.
func (t *actionTranslator) translateUnaryCall(suffix string) error {
	t.pos++ // macro name
	arg, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	arg = strings.TrimSpace(arg)
	if !isBareIdent(arg) {
		return fmt.Errorf("unary call arg %q is not a bare identifier", arg)
	}
	if _, isBound := t.bound[arg]; !isBound {
		if !t.locals[arg] {
			return fmt.Errorf("unary call arg %q is not a bound stack slot or local", arg)
		}
	}
	fmt.Fprintf(t.writer, "%s%s\n", goLocalName(arg), suffix)
	return nil
}

// goLocalName mirrors bindName for the case where the slot has a real
// source name. Keyword slots get a `_v` suffix so the body references
// match the prologue's locals.
func goLocalName(name string) string {
	if goKeywords[name] {
		return name + "_v"
	}
	return name
}

// isBareIdent returns true if s is a single C identifier with no
// operators, casts, or member access.
func isBareIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			// always allowed
		case (r >= '0' && r <= '9') && i > 0:
			// allowed after first char
		default:
			return false
		}
	}
	return true
}

// translateNullary emits a single statement and consumes
// `MACRO();` from the token stream.
func (t *actionTranslator) translateNullary(stmt string) error {
	t.pos++
	if _, err := t.takeParenthesised(); err != nil {
		return err
	}
	t.acceptSemi()
	fmt.Fprintln(t.writer, stmt)
	return nil
}

// skipParenthesised drops a `MACRO(...)` plus optional `;`. Used for
// statistics macros that have no runtime effect.
func (t *actionTranslator) skipParenthesised() error {
	t.pos++
	if _, err := t.takeParenthesised(); err != nil {
		return err
	}
	t.acceptSemi()
	return nil
}

// takeParenthesised consumes `(...)` starting at t.pos and returns the
// raw text inside the parens (joined with single spaces).
func (t *actionTranslator) takeParenthesised() (string, error) {
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "(" {
		return "", fmt.Errorf("expected '(' at position %d", t.pos)
	}
	t.pos++
	depth := 1
	parts := []string{}
	for t.pos < len(t.toks) && depth > 0 {
		tk := t.toks[t.pos]
		switch tk.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				t.pos++
				return strings.Join(parts, " "), nil
			}
		}
		parts = append(parts, tk.Text)
		t.pos++
	}
	return "", fmt.Errorf("unterminated '(' at position %d", t.pos)
}

func (t *actionTranslator) acceptSemi() {
	if t.pos < len(t.toks) && t.toks[t.pos].Text == ";" {
		t.pos++
	}
}

// bindNames returns a name->direction map for the signature.
// Synthetic / unused slots don't appear.
func bindNames(sig *SignatureAnalysis) map[string]string {
	out := map[string]string{}
	for _, b := range sig.Inputs {
		if b.Name == "" || b.Name == "unused" {
			continue
		}
		out[b.Name] = "in"
	}
	for _, b := range sig.Outputs {
		if b.Name == "" || b.Name == "unused" {
			continue
		}
		out[b.Name] = "out"
	}
	return out
}

// stripWhitespace drops newline / comment tokens so the statement
// walker doesn't have to skip them at every step.
func stripWhitespace(in []dslTok) []dslTok {
	out := make([]dslTok, 0, len(in))
	for _, t := range in {
		switch t.Kind {
		case tokNewline, tokComment, tokCMacro:
			continue
		}
		out = append(out, t)
	}
	return out
}

// splitTopLevelComma splits "a, b" into ("a", "b", true). Commas
// inside nested parens or brackets are ignored.
func splitTopLevelComma(s string) (head, tail string, ok bool) {
	depth := 0
	for i, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return s[:i], s[i+1:], true
			}
		}
	}
	return s, "", false
}
