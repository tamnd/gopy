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
// The output is a Go statement block (no surrounding braces); the
// caller indents and wraps it.
func TranslateBody(body []dslTok, sig *SignatureAnalysis) (goSrc string, ok bool, note string) {
	if len(sig.Outputs) > 0 {
		// The current panel only understands control macros that do
		// not push. Anything that produces outputs needs the typed
		// helper layer that the action translator does not yet drive,
		// so bail and let the emitter use a panic-stub.
		return "", false, "outputs not yet handled by action translator"
	}
	t := &actionTranslator{
		sig:    sig,
		bound:  bindNames(sig),
		toks:   stripWhitespace(body),
		writer: &strings.Builder{},
	}
	if err := t.run(); err != nil {
		return "", false, err.Error()
	}
	return t.writer.String(), true, ""
}

type actionTranslator struct {
	sig    *SignatureAnalysis
	bound  map[string]string // name -> "in"/"out"
	toks   []dslTok
	pos    int
	writer *strings.Builder
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
	}
	return fmt.Errorf("unrecognized token at action body start: %q", tk.Text)
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

// translateErrorIf handles `ERROR_IF(cond, label);` by routing through
// the eval loop's labeled-error helper. The label is preserved as a
// string for the error message.
func (t *actionTranslator) translateErrorIf() error {
	t.pos++ // ERROR_IF
	args, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	// args: "cond, label". Split on the first comma at paren-depth 0.
	cond, label, ok := splitTopLevelComma(args)
	if !ok {
		return fmt.Errorf("ERROR_IF: missing label argument in %q", args)
	}
	fmt.Fprintf(t.writer, "if %s { return 0, e.error(%q) }\n",
		strings.TrimSpace(cond), strings.TrimSpace(label))
	return nil
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
