package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// readBytecodesSection mirrors the if __name__ == "__main__" block in
// parsing.py: slice the file between // BEGIN BYTECODES // and
// // END BYTECODES // so the parser only sees the DSL body.
func readBytecodesSection(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimRight(l, "\r") {
		case "// BEGIN BYTECODES //":
			begin = i
		case "// END BYTECODES //":
			end = i
		}
	}
	if begin < 0 || end < 0 || end <= begin {
		return "", fmt.Errorf("BEGIN/END markers not found in %s", path)
	}
	return strings.Join(lines[begin+1:end], "\n"), nil
}

// parseAll runs Definition() to EOF and reports anything unconsumed.
func parseAll(t *testing.T, src string) []Node {
	t.Helper()
	p, err := NewParser(src, "<test>")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var out []Node
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if n == nil {
			break
		}
		out = append(out, n)
	}
	if !p.EOF() {
		tok, _ := p.Peek(false)
		t.Fatalf("unconsumed input at %d:%d: %q", tok.Begin.Line, tok.Begin.Col, tok.Text)
	}
	return out
}

func TestParse_InstSimple(t *testing.T) {
	src := `inst(NOP, (--)) { }`
	nodes := parseAll(t, src)
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	id, ok := nodes[0].(*InstDef)
	if !ok {
		t.Fatalf("node 0 = %T, want *InstDef", nodes[0])
	}
	if id.Name != "NOP" || id.Kind != "inst" {
		t.Errorf("got name=%q kind=%q", id.Name, id.Kind)
	}
	if len(id.Inputs) != 0 || len(id.Outputs) != 0 {
		t.Errorf("got inputs=%d outputs=%d, want 0/0", len(id.Inputs), len(id.Outputs))
	}
}

func TestParse_InstWithStackEffect(t *testing.T) {
	src := `inst(POP_TOP, (value -- )) { Py_DECREF(value); }`
	nodes := parseAll(t, src)
	id := nodes[0].(*InstDef)
	if len(id.Inputs) != 1 {
		t.Fatalf("got %d inputs, want 1", len(id.Inputs))
	}
	se := id.Inputs[0].(*StackEffect)
	if se.Name != "value" {
		t.Errorf("input name = %q, want value", se.Name)
	}
}

func TestParse_InstWithTypedAndSized(t *testing.T) {
	src := `op(LOAD, (idx: PyObject -- arr[idx])) { }`
	nodes := parseAll(t, src)
	id := nodes[0].(*InstDef)
	if id.Inputs[0].(*StackEffect).Type != "PyObject" {
		t.Errorf("input type = %q, want PyObject", id.Inputs[0].(*StackEffect).Type)
	}
	if id.Outputs[0].(*StackEffect).Size != "idx" {
		t.Errorf("output size = %q, want idx", id.Outputs[0].(*StackEffect).Size)
	}
}

func TestParse_CacheEffectInInputs(t *testing.T) {
	src := `inst(LOAD_GLOBAL, (idx/2 -- res)) { }`
	nodes := parseAll(t, src)
	id := nodes[0].(*InstDef)
	ce, ok := id.Inputs[0].(*CacheEffect)
	if !ok {
		t.Fatalf("input 0 = %T, want *CacheEffect", id.Inputs[0])
	}
	if ce.Size != 2 {
		t.Errorf("cache size = %d, want 2", ce.Size)
	}
}

func TestParse_AnnotationsAndReplicate(t *testing.T) {
	src := `pure replicate(2) inst(FOO, (--)) { }`
	nodes := parseAll(t, src)
	id := nodes[0].(*InstDef)
	if len(id.Annotations) != 2 || id.Annotations[0] != "pure" || id.Annotations[1] != "replicate(2)" {
		t.Errorf("annotations = %v, want [pure replicate(2)]", id.Annotations)
	}
}

func TestParse_Macro(t *testing.T) {
	src := `macro(BINARY) = OP_A + OP_B + cache/1;`
	nodes := parseAll(t, src)
	m := nodes[0].(*Macro)
	if m.Name != "BINARY" {
		t.Errorf("name = %q, want BINARY", m.Name)
	}
	if len(m.UOps) != 3 {
		t.Fatalf("got %d uops, want 3", len(m.UOps))
	}
	if _, ok := m.UOps[2].(*CacheEffect); !ok {
		t.Errorf("uop 2 = %T, want *CacheEffect", m.UOps[2])
	}
}

func TestParse_Family(t *testing.T) {
	src := `family(LOAD_FAST, INLINE_CACHE_ENTRIES) = { LOAD_FAST_CHECK, LOAD_FAST_AND_CLEAR };`
	nodes := parseAll(t, src)
	f := nodes[0].(*Family)
	if f.Name != "LOAD_FAST" || f.Size != "INLINE_CACHE_ENTRIES" {
		t.Errorf("got name=%q size=%q", f.Name, f.Size)
	}
	if len(f.Members) != 2 {
		t.Errorf("members = %v, want len 2", f.Members)
	}
}

func TestParse_PseudoBraceAndBracket(t *testing.T) {
	src := `pseudo(JUMP, (--), (HAS_JUMP)) = { JUMP_FORWARD, JUMP_BACKWARD };`
	nodes := parseAll(t, src)
	ps := nodes[0].(*Pseudo)
	if ps.AsSequence {
		t.Error("brace form should not be sequence")
	}
	if len(ps.Flags) != 1 || ps.Flags[0] != "HAS_JUMP" {
		t.Errorf("flags = %v", ps.Flags)
	}

	src2 := `pseudo(SEQ, (--)) = [ A, B ];`
	nodes = parseAll(t, src2)
	ps = nodes[0].(*Pseudo)
	if !ps.AsSequence {
		t.Error("bracket form should be sequence")
	}
}

func TestParse_LabelDef(t *testing.T) {
	src := `spilled label(error) { goto handler; }`
	nodes := parseAll(t, src)
	ld := nodes[0].(*LabelDef)
	if !ld.Spilled || ld.Name != "error" {
		t.Errorf("got name=%q spilled=%v", ld.Name, ld.Spilled)
	}
}

func TestParse_BlockWithControlFlow(t *testing.T) {
	src := `inst(BIG, (--)) {
		if (cond) { foo(); } else { bar(); }
		for (int i = 0; i < n; i++) { step(); }
		while (cond) { tick(); }
	}`
	nodes := parseAll(t, src)
	id := nodes[0].(*InstDef)
	if len(id.Block.Body) != 3 {
		t.Fatalf("body has %d stmts, want 3", len(id.Block.Body))
	}
	if _, ok := id.Block.Body[0].(*IfStmt); !ok {
		t.Errorf("body[0] = %T, want *IfStmt", id.Block.Body[0])
	}
	if _, ok := id.Block.Body[1].(*ForStmt); !ok {
		t.Errorf("body[1] = %T, want *ForStmt", id.Block.Body[1])
	}
	if _, ok := id.Block.Body[2].(*WhileStmt); !ok {
		t.Errorf("body[2] = %T, want *WhileStmt", id.Block.Body[2])
	}
}

func TestParse_MacroIfInBody(t *testing.T) {
	src := `inst(GUARD, (--)) {
		#if Py_DEBUG
		dump();
		#else
		quiet();
		#endif
	}`
	nodes := parseAll(t, src)
	id := nodes[0].(*InstDef)
	mi, ok := id.Block.Body[0].(*MacroIfStmt)
	if !ok {
		t.Fatalf("body[0] = %T, want *MacroIfStmt", id.Block.Body[0])
	}
	if len(mi.Body) != 1 || len(mi.ElseBody) != 1 {
		t.Errorf("got body=%d else=%d, want 1/1", len(mi.Body), len(mi.ElseBody))
	}
}

func TestParse_RejectsSwitch(t *testing.T) {
	p, _ := NewParser(`inst(BAD, (--)) { switch (x) { case 1: break; } }`, "<test>")
	_, err := p.Definition()
	if err == nil || !strings.Contains(err.Error(), "switch") {
		t.Errorf("err = %v, want switch rejection", err)
	}
}

func TestParse_RealBytecodesC_DoesNotErr(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/bytecodes.c"
	src, err := readBytecodesSection(path)
	if err != nil {
		t.Skipf("upstream not available: %v", err)
	}
	p, err := NewParser(src, path)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	count := 0
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if n == nil {
			break
		}
		count++
	}
	if count < 100 {
		t.Errorf("only parsed %d definitions, expected many more", count)
	}
}
