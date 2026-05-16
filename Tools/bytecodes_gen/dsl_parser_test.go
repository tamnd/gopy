package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseString(t *testing.T, src string) *Defs {
	t.Helper()
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	d, err := parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func TestParseInstSimple(t *testing.T) {
	src := `inst(NOP, (--)) {
}`
	d := parseString(t, src)
	if len(d.Insts) != 1 {
		t.Fatalf("got %d insts, want 1", len(d.Insts))
	}
	in := d.Insts[0]
	if in.Kind != "inst" || in.Name != "NOP" {
		t.Errorf("got %s %s, want inst NOP", in.Kind, in.Name)
	}
	if len(in.Inputs) != 0 || len(in.Outputs) != 0 {
		t.Errorf("NOP should have empty stack effect, got %d in / %d out", len(in.Inputs), len(in.Outputs))
	}
}

func TestParseInstStack(t *testing.T) {
	src := `inst(BINARY_OP, (left, right -- res)) {
    res = left + right;
}`
	d := parseString(t, src)
	in := d.Insts[0]
	if len(in.Inputs) != 2 || in.Inputs[0].Stack.Name != "left" || in.Inputs[1].Stack.Name != "right" {
		t.Fatalf("inputs: %+v", in.Inputs)
	}
	if len(in.Outputs) != 1 || in.Outputs[0].Stack.Name != "res" {
		t.Fatalf("outputs: %+v", in.Outputs)
	}
	if len(in.Body) == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestParseInstAnnotations(t *testing.T) {
	src := `pure inst(LOAD_CONST, (-- value)) {
    value = consts[oparg];
}`
	d := parseString(t, src)
	in := d.Insts[0]
	if len(in.Annotations) != 1 || in.Annotations[0] != "pure" {
		t.Errorf("annotations: %v", in.Annotations)
	}
}

func TestParseInstReplicate(t *testing.T) {
	src := `replicate(8) inst(STORE_FAST, (value --)) {}`
	d := parseString(t, src)
	in := d.Insts[0]
	if len(in.Annotations) != 1 || in.Annotations[0] != "replicate(8)" {
		t.Errorf("annotations: %v", in.Annotations)
	}
}

func TestParseOpDef(t *testing.T) {
	src := `op(_SPECIALIZE_BINARY_OP, (counter/1, lhs, rhs -- lhs, rhs)) {}`
	d := parseString(t, src)
	if len(d.Insts) != 1 || d.Insts[0].Kind != "op" {
		t.Fatalf("expected one op, got %+v", d.Insts)
	}
	in := d.Insts[0]
	if in.Inputs[0].Cache == nil || in.Inputs[0].Cache.Name != "counter" || in.Inputs[0].Cache.Size != 1 {
		t.Errorf("first input should be cache counter/1, got %+v", in.Inputs[0])
	}
}

func TestParseInstTypedAndSized(t *testing.T) {
	src := `inst(EX, (vals[oparg] -- result: PyObject *)) {}`
	d := parseString(t, src)
	in := d.Insts[0]
	if in.Inputs[0].Stack.Name != "vals" || in.Inputs[0].Stack.Size != "oparg" {
		t.Errorf("input: %+v", in.Inputs[0].Stack)
	}
	if in.Outputs[0].Stack.Name != "result" || in.Outputs[0].Stack.Type != "PyObject *" {
		t.Errorf("output: %+v", in.Outputs[0].Stack)
	}
}

func TestParseMacro(t *testing.T) {
	src := `macro(BINARY_OP) = unused/1 + _SPECIALIZE_BINARY_OP + _BINARY_OP;`
	d := parseString(t, src)
	if len(d.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(d.Macros))
	}
	m := d.Macros[0]
	if m.Name != "BINARY_OP" {
		t.Errorf("name: %q", m.Name)
	}
	if len(m.UOps) != 3 {
		t.Fatalf("uops: %+v", m.UOps)
	}
	if m.UOps[0].Cache == nil || m.UOps[0].Cache.Name != "unused" || m.UOps[0].Cache.Size != 1 {
		t.Errorf("uops[0]: %+v", m.UOps[0])
	}
	if m.UOps[1].Name != "_SPECIALIZE_BINARY_OP" {
		t.Errorf("uops[1]: %+v", m.UOps[1])
	}
}

func TestParseFamily(t *testing.T) {
	src := `family(BINARY_OP, INLINE_CACHE_ENTRIES_BINARY_OP) = { BINARY_OP_ADD_INT, BINARY_OP_ADD_FLOAT };`
	d := parseString(t, src)
	if len(d.Famils) != 1 {
		t.Fatalf("expected 1 family, got %d", len(d.Famils))
	}
	f := d.Famils[0]
	if f.Name != "BINARY_OP" || f.Size != "INLINE_CACHE_ENTRIES_BINARY_OP" {
		t.Errorf("family head: %+v", f)
	}
	if len(f.Members) != 2 || f.Members[0] != "BINARY_OP_ADD_INT" {
		t.Errorf("members: %v", f.Members)
	}
}

func TestParsePseudo(t *testing.T) {
	src := `pseudo(JUMP, (-- )) = { JUMP_FORWARD, JUMP_BACKWARD };`
	d := parseString(t, src)
	if len(d.Pseudos) != 1 {
		t.Fatalf("expected 1 pseudo, got %d", len(d.Pseudos))
	}
	p := d.Pseudos[0]
	if p.Name != "JUMP" || len(p.Targets) != 2 || p.AsSequence {
		t.Errorf("pseudo: %+v", p)
	}
}

func TestParsePseudoSequence(t *testing.T) {
	src := `pseudo(LOAD_CLOSURE, (-- unused), (HAS_ARG)) = [LOAD_FAST];`
	d := parseString(t, src)
	p := d.Pseudos[0]
	if !p.AsSequence {
		t.Errorf("expected AsSequence")
	}
	if len(p.Flags) != 1 || p.Flags[0] != "HAS_ARG" {
		t.Errorf("flags: %v", p.Flags)
	}
	if len(p.Targets) != 1 || p.Targets[0] != "LOAD_FAST" {
		t.Errorf("targets: %v", p.Targets)
	}
}

func TestParseLabel(t *testing.T) {
	src := `label(error) {
    goto exit;
}
spilled label(exit_unwind) {
    return NULL;
}`
	d := parseString(t, src)
	if len(d.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(d.Labels))
	}
	if d.Labels[0].Name != "error" || d.Labels[0].Spilled {
		t.Errorf("label 0: %+v", d.Labels[0])
	}
	if d.Labels[1].Name != "exit_unwind" || !d.Labels[1].Spilled {
		t.Errorf("label 1: %+v", d.Labels[1])
	}
}

func TestParseOrder(t *testing.T) {
	src := `inst(A, (--)) {} macro(B) = A; family(C, 0) = { A };`
	d := parseString(t, src)
	if len(d.Order) != 3 {
		t.Fatalf("order: %v", d.Order)
	}
	if _, ok := d.Order[0].(*InstDef); !ok {
		t.Errorf("order[0] not InstDef: %T", d.Order[0])
	}
	if _, ok := d.Order[1].(*MacroDef); !ok {
		t.Errorf("order[1] not MacroDef: %T", d.Order[1])
	}
	if _, ok := d.Order[2].(*FamilyDef); !ok {
		t.Errorf("order[2] not FamilyDef: %T", d.Order[2])
	}
}

func TestParseBytecodesC(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		t.Skip("CPYTHON env not set")
	}
	data, err := os.ReadFile(filepath.Join(cpython, "Python", "bytecodes.c"))
	if err != nil {
		t.Skipf("read failed: %v", err)
	}
	d, err := ParseBytecodes(string(data))
	if err != nil {
		t.Fatalf("ParseBytecodes: %v", err)
	}
	var insts, ops int
	for _, in := range d.Insts {
		switch in.Kind {
		case "inst":
			insts++
		case "op":
			ops++
		}
	}
	if insts < 50 {
		t.Errorf("expected >= 50 inst defs, got %d", insts)
	}
	if ops < 100 {
		t.Errorf("expected >= 100 op defs, got %d", ops)
	}
	if len(d.Macros) < 50 {
		t.Errorf("expected >= 50 macros, got %d", len(d.Macros))
	}
	if len(d.Famils) < 5 {
		t.Errorf("expected >= 5 families, got %d", len(d.Famils))
	}
	t.Logf("parsed: %d inst, %d op, %d macro, %d family, %d pseudo, %d label",
		insts, ops, len(d.Macros), len(d.Famils), len(d.Pseudos), len(d.Labels))
	// Sanity: every inst/op must have a non-empty name.
	for _, in := range d.Insts {
		if strings.TrimSpace(in.Name) == "" {
			t.Errorf("inst with empty name at line %d", in.Line)
		}
	}
}
