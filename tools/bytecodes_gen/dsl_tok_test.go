package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenizeBasic(t *testing.T) {
	src := `inst(NOP, (--)) {
}
`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	want := []tokKind{
		tokInst, tokLParen, tokIdentifier, tokComma, tokLParen, tokMinusMinus, tokRParen, tokRParen, tokLBrace, tokRBrace,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("tok %d: kind %s, want %s (%q)", i, toks[i].Kind, k, toks[i].Text)
		}
	}
}

func TestTokenizeStackEffect(t *testing.T) {
	src := `inst(BINARY_OP, (left, right -- res)) {}`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	// Expect: inst ( BINARY_OP , ( left , right -- res ) ) { }
	want := []string{"inst", "(", "BINARY_OP", ",", "(", "left", ",", "right", "--", "res", ")", ")", "{", "}"}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].Text != w {
			t.Errorf("tok %d: text %q, want %q", i, toks[i].Text, w)
		}
	}
}

func TestTokenizeAnnotation(t *testing.T) {
	src := `pure inst(LOAD_CONST, (-- value)) {}`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	if toks[0].Kind != tokAnnotation || toks[0].Text != "pure" {
		t.Errorf("first token: %v, want ANNOTATION(pure)", toks[0])
	}
	if toks[1].Kind != tokInst {
		t.Errorf("second token: %v, want INST", toks[1])
	}
}

func TestTokenizeStrings(t *testing.T) {
	src := `STAT_INC("BINARY_OP", hit);`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	if toks[2].Kind != tokString || toks[2].Text != `"BINARY_OP"` {
		t.Errorf("string token: %v, want STRING(\"BINARY_OP\")", toks[2])
	}
}

func TestTokenizeNumbers(t *testing.T) {
	for _, src := range []string{"42", "0x1F", "0755", "3.14", "1e10", "0xffffu"} {
		toks, err := tokenize(src)
		if err != nil {
			t.Fatalf("tokenize %q: %v", src, err)
		}
		if len(toks) != 1 || toks[0].Kind != tokNumber || toks[0].Text != src {
			t.Errorf("number %q: got %v, want one NUMBER token", src, toks)
		}
	}
}

func TestTokenizeComments(t *testing.T) {
	src := "// line\nx /* block */ y"
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	// Comments are dropped from the main stream.
	want := []string{"x", "y"}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].Text != w {
			t.Errorf("tok %d: %q, want %q", i, toks[i].Text, w)
		}
	}
	// Line numbers must advance past the dropped block comment.
	if toks[1].Line != 2 {
		t.Errorf("y should be on line 2, got line %d", toks[1].Line)
	}
}

func TestTokenizeArrowAndOps(t *testing.T) {
	src := `a->b; x++; y <<= 1;`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	want := []tokKind{
		tokIdentifier, tokArrow, tokIdentifier, tokSemi,
		tokIdentifier, tokPlusPlus, tokSemi,
		tokIdentifier, tokLShiftEq, tokNumber, tokSemi,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("tok %d: %s, want %s (%q)", i, toks[i].Kind, k, toks[i].Text)
		}
	}
}

func TestTokenizeBytecodesC(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		t.Skip("CPYTHON env not set")
	}
	path := filepath.Join(cpython, "Python", "bytecodes.c")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read %s: %v", path, err)
	}
	toks, err := tokenize(string(data))
	if err != nil {
		t.Fatalf("tokenize bytecodes.c: %v", err)
	}
	if len(toks) < 1000 {
		t.Errorf("expected > 1000 tokens from bytecodes.c, got %d", len(toks))
	}
	// Sanity check: bytecodes.c must contain inst, op, macro, family.
	have := map[tokKind]int{}
	for _, tk := range toks {
		have[tk.Kind]++
	}
	for _, k := range []tokKind{tokInst, tokOp, tokMacro} {
		if have[k] == 0 {
			t.Errorf("bytecodes.c had zero %s tokens", k)
		}
	}
	t.Logf("tokenized %d tokens; %d inst, %d op, %d macro", len(toks), have[tokInst], have[tokOp], have[tokMacro])
}

func TestTokenizeNoInvalid(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		t.Skip("CPYTHON env not set")
	}
	data, err := os.ReadFile(filepath.Join(cpython, "Python", "bytecodes.c"))
	if err != nil {
		t.Skipf("read failed: %v", err)
	}
	toks, err := tokenize(string(data))
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	for _, tk := range toks {
		if tk.Kind == tokInvalid {
			t.Errorf("invalid token at %d:%d %q", tk.Line, tk.Col, tk.Text)
		}
	}
}

func TestTokenizeSplit(t *testing.T) {
	// Make sure the tokenizer recognises the `--` stack-effect separator
	// distinctly from `-- ` C decrement, and from `->`.
	src := `(a -- b)`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	got := []string{}
	for _, tk := range toks {
		got = append(got, tk.Text)
	}
	want := []string{"(", "a", "--", "b", ")"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}
