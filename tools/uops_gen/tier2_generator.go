// Tier-2 case-table generator: rewrites each Tier-2 viable uop body through the shared Emitter and emits a Go file with per-uop comment blocks plus handler stubs.
//
// CPython: Tools/cases_generator/tier2_generator.py
package main

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Tier2Emitter overrides the Tier-1 default replacers for DEOPT_IF /
// EXIT_IF / oparg with the Tier-2 jump-target shape.
//
// CPython: Tools/cases_generator/tier2_generator.py:61-133 Tier2Emitter
type Tier2Emitter struct {
	*Emitter
}

// NewTier2Emitter wires the shared Emitter then overrides the three
// replacers that differ for Tier-2.
//
// CPython: Tools/cases_generator/tier2_generator.py:63-65 Tier2Emitter.__init__
func NewTier2Emitter(out *CWriter, labels map[string]*Label) *Tier2Emitter {
	base := NewEmitter(out, labels)
	t := &Tier2Emitter{Emitter: base}
	base.SetReplacer("DEOPT_IF", t.deoptIf)
	base.SetReplacer("EXIT_IF", t.exitIf)
	base.SetReplacer("oparg", t.oparg)
	return t
}

// deoptIfExitIf is the shared body for DEOPT_IF / EXIT_IF in Tier-2.
//
// CPython: Tools/cases_generator/tier2_generator.py:73-112 Tier2Emitter.deopt_if / exit_if
func (t *Tier2Emitter) deoptIfExitIf(
	tkn Token, it *TokenIterator,
) (bool, error) {
	t.Out.EmitAt("if ", tkn)
	lparen, ok := it.Next()
	if !ok || lparen.Kind != TokLParen {
		return false, analysisError("Expected '(' after DEOPT_IF/EXIT_IF", tkn)
	}
	t.Emit(lparen)
	first, hasFirst := it.Peek()
	if _, err := emitTo(t.Out, it, TokRParen); err != nil {
		return false, err
	}
	if _, ok := it.Next(); !ok { // semicolon
		return false, analysisError("Expected ';' after DEOPT_IF/EXIT_IF", tkn)
	}
	t.Emit(") {\n")
	t.Emit("UOP_STAT_INC(uopcode, miss);\n")
	t.Emit("JUMP_TO_JUMP_TARGET();\n")
	t.Emit("}\n")
	return !alwaysTrue(first, hasFirst), nil
}

// deoptIf is the Tier-2 DEOPT_IF replacer.
//
// CPython: Tools/cases_generator/tier2_generator.py:73-92 Tier2Emitter.deopt_if
func (t *Tier2Emitter) deoptIf(
	tkn Token, it *TokenIterator, _ CodeSection, _ *Storage, _ *Instruction,
) (bool, error) {
	return t.deoptIfExitIf(tkn, it)
}

// exitIf is the Tier-2 EXIT_IF replacer (identical body to deoptIf).
//
// CPython: Tools/cases_generator/tier2_generator.py:94-112 Tier2Emitter.exit_if
func (t *Tier2Emitter) exitIf(
	tkn Token, it *TokenIterator, _ CodeSection, _ *Storage, _ *Instruction,
) (bool, error) {
	return t.deoptIfExitIf(tkn, it)
}

// oparg substitutes the literal 0 or 1 when the uop name ends in _0 or
// _1 and the token is followed by `& 1`. Otherwise it emits the bare
// identifier.
//
// CPython: Tools/cases_generator/tier2_generator.py:114-133 Tier2Emitter.oparg
func (t *Tier2Emitter) oparg(
	tkn Token, it *TokenIterator, uop CodeSection, _ *Storage, _ *Instruction,
) (bool, error) {
	u, ok := uop.(*Uop)
	if !ok || (!strings.HasSuffix(u.Name, "_0") && !strings.HasSuffix(u.Name, "_1")) {
		t.Emit(tkn)
		return true, nil
	}
	amp, ok := it.Peek()
	if !ok || amp.Text != "&" {
		t.Emit(tkn)
		return true, nil
	}
	if _, ok := it.Next(); !ok { // consume &
		t.Emit(tkn)
		return true, nil
	}
	one, ok := it.Next()
	if !ok || one.Text != "1" {
		// Replay best-effort if the lookahead does not match.
		t.Emit(tkn)
		t.Emit(amp)
		if ok {
			t.Emit(one)
		}
		return true, nil
	}
	t.Out.EmitAt(string(u.Name[len(u.Name)-1]), tkn)
	return true, nil
}

// declareTier2Variable emits a single C declaration for a stack item.
//
// CPython: Tools/cases_generator/tier2_generator.py:36-44 declare_variable
func declareTier2Variable(v *StackItem, seen map[string]struct{}, out *CWriter) {
	if !v.Used {
		return
	}
	if _, ok := seen[v.Name]; ok {
		return
	}
	seen[v.Name] = struct{}{}
	typ, _ := typeAndNull(v)
	space := ""
	if n := len(typ); n > 0 {
		last := typ[n-1]
		if (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') || (last >= '0' && last <= '9') || last == '_' {
			space = " "
		}
	}
	out.Emit(fmt.Sprintf("%s%s%s;\n", typ, space, v.Name))
}

// declareTier2Variables emits the declarations for all live stack
// variables of a uop.
//
// CPython: Tools/cases_generator/tier2_generator.py:47-58 declare_variables
func declareTier2Variables(uop *Uop, out *CWriter) {
	stack := NewStack()
	null := NullCWriter()
	for i := len(uop.Stack.Inputs) - 1; i >= 0; i-- {
		_, _ = stack.Pop(uop.Stack.Inputs[i], null)
	}
	for _, v := range uop.Stack.Outputs {
		stack.Push(LocalUndefined(v))
	}
	seen := map[string]struct{}{"unused": {}}
	for i := len(uop.Stack.Inputs) - 1; i >= 0; i-- {
		declareTier2Variable(uop.Stack.Inputs[i], seen, out)
	}
	for _, v := range uop.Stack.Outputs {
		declareTier2Variable(v, seen, out)
	}
}

// writeTier2Uop runs the Emitter over a single uop's body.
//
// CPython: Tools/cases_generator/tier2_generator.py:136-161 write_uop
func writeTier2Uop(uop *Uop, emitter *Tier2Emitter, stack *Stack) (*Stack, error) {
	emitter.Out.StartLine()
	switch {
	case uop.Properties.Oparg:
		emitter.Emit("oparg = CURRENT_OPARG();\n")
		// const_oparg < 0 is an invariant in upstream when Oparg is set.
	case uop.Properties.ConstOparg >= 0:
		emitter.Emit(fmt.Sprintf("oparg = %d;\n", uop.Properties.ConstOparg))
		emitter.Emit("assert(oparg == CURRENT_OPARG());\n")
	}
	storage, err := StorageForUop(stack, uop, emitter.Out, true)
	if err != nil {
		return nil, err
	}
	idx := 0
	for _, cache := range uop.Caches {
		if cache.Name == "unused" {
			continue
		}
		var typ, cast string
		if cache.Size == 4 {
			typ = "PyObject *"
			cast = "PyObject *"
		} else {
			typ = fmt.Sprintf("uint%d_t ", cache.Size*16)
			cast = fmt.Sprintf("uint%d_t", cache.Size*16)
		}
		emitter.Emit(fmt.Sprintf("%s%s = (%s)CURRENT_OPERAND%d();\n", typ, cache.Name, cast, idx))
		idx++
	}
	_, storage, err = emitter.EmitTokens(uop, storage, nil, false)
	if err != nil {
		return nil, err
	}
	if err := storage.Flush(emitter.Out); err != nil {
		return nil, err
	}
	return storage.Stack, nil
}

// tier2Skips is the upstream SKIPS tuple of names that never appear in
// the Tier-2 case table.
//
// CPython: Tools/cases_generator/tier2_generator.py:163 SKIPS
var tier2Skips = map[string]struct{}{"_EXTENDED_ARG": {}}

// orderedUopNames returns analysis.Uops keys in sorted order to keep
// the generated file deterministic.
func orderedUopNames(a *Analysis) []string {
	names := make([]string, 0, len(a.Uops))
	for k := range a.Uops {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// commentPrefix turns a captured C-flavored body into a Go // comment
// block, one line at a time, so the generated file remains valid Go.
func commentPrefix(body string) string {
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		l = strings.TrimRight(l, "\r ")
		if l == "" {
			lines[i] = "//"
		} else {
			lines[i] = "// " + l
		}
	}
	// Drop trailing empty comment if the body ended with a newline.
	for len(lines) > 0 && lines[len(lines)-1] == "//" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n") + "\n"
}

// GenerateTier2 writes the gopy-flavored Tier-2 case table into out.
//
// The output file is parseable Go (package optimizer, one stub func per
// uop, body comment block per uop). The C-flavored rewritten body is
// preserved verbatim inside the comment block so the hand-written
// optimizer/uops.go interpreter loop can audit it.
//
// CPython: Tools/cases_generator/tier2_generator.py:166-202 generate_tier2
func GenerateTier2(analysis *Analysis, projectRoot string, out *bytes.Buffer) error {
	// File header, parity with the upstream banner but using //.
	if err := writeHeader(
		"tools/uops_gen/tier2_generator.go",
		[]string{"Python/bytecodes.c"},
		out, "//", projectRoot,
	); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "// Code generated by tools/uops_gen; DO NOT EDIT.\n\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "package optimizer\n\n"); err != nil {
		return err
	}

	for _, name := range orderedUopNames(analysis) {
		uop := analysis.Uops[name]
		if _, skip := tier2Skips[name]; skip {
			continue
		}
		if uop.Properties.Tier == 1 {
			continue
		}
		if uop.IsSuper() {
			continue
		}
		if reason := uop.WhyNotViable(); reason != "" {
			fmt.Fprintf(out, "// %s is not a viable micro-op for tier 2 because it %s\n\n", uop.Name, reason)
			continue
		}

		// Capture the rewritten body to a side buffer so we can wrap
		// the C tokens in // comments before emitting them into the Go
		// file.
		var body bytes.Buffer
		writer := NewCWriter(&body, 2, false)
		emitter := NewTier2Emitter(writer, analysis.Labels)
		writer.Emit(fmt.Sprintf("case %s: {\n", uop.Name))
		declareTier2Variables(uop, writer)
		stack := NewStack()
		if _, err := writeTier2Uop(uop, emitter, stack); err != nil {
			return fmt.Errorf("write_uop %s: %w", uop.Name, err)
		}
		writer.StartLine()
		if !uop.Properties.AlwaysExits {
			writer.Emit("break;\n")
		}
		writer.StartLine()
		writer.Emit("}\n\n")

		fmt.Fprintf(out, "// uop %s body:\n", uop.Name)
		if _, err := io.WriteString(out, commentPrefix(body.String())); err != nil {
			return err
		}
		fmt.Fprintf(out, "func uop%s() int { return 0 }\n\n", uop.Name)
	}
	return nil
}
