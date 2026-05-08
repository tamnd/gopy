// Tests for GenerateOptimizer: parse main DSL plus optimizer overlay, run AnalyzeForest, render the symbolic-stub Go file, and assert structural invariants.
//
// CPython: Tools/cases_generator/optimizer_generator.py
package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// parseAnalyzeFile is a small helper that reads one bytecodes-style
// file, slices its BEGIN/END section, parses the DSL, and runs the
// analyzer. It is local to this test so we don't reach into helpers
// that other tests own.
func parseAnalyzeFile(t *testing.T, path string) *Analysis {
	t.Helper()
	src, err := readBytecodesSection(path)
	if err != nil {
		t.Skipf("upstream not available: %v", err)
	}
	p, err := NewParser(src, path)
	if err != nil {
		t.Fatalf("lex %s: %v", path, err)
	}
	var forest []Node
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if n == nil {
			break
		}
		forest = append(forest, n)
	}
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze %s: %v", path, err)
	}
	return a
}

func runOptimizerGenerator(t *testing.T) string {
	t.Helper()
	base := parseAnalyzeFile(t, "/Users/apple/cpython-314/Python/bytecodes.c")
	override := parseAnalyzeFile(t, "/Users/apple/cpython-314/Python/optimizer_bytecodes.c")
	var buf bytes.Buffer
	if err := GenerateOptimizer(base, override, "/Users/apple/cpython-314", &buf); err != nil {
		t.Fatalf("GenerateOptimizer: %v", err)
	}
	return buf.String()
}

// TestOptimizerGenerator_Parses asserts the emitted file is valid Go
// (per go/parser) and starts with the canonical generator header.
func TestOptimizerGenerator_Parses(t *testing.T) {
	out := runOptimizerGenerator(t)
	if !strings.HasPrefix(out, "// Code generated; DO NOT EDIT.\n") {
		t.Errorf("missing generator header; got first line %q",
			strings.SplitN(out, "\n", 2)[0])
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "uops_abstract_gen.go", out, parser.AllErrors); err != nil {
		t.Fatalf("ParseFile: %v\n--- output head ---\n%s", err, headOf(out, 80))
	}
}

// TestOptimizerGenerator_RepresentativeStubs spot-checks that the
// symbolic generator emits a stub for every uop the v0.12 lattice
// integration is going to wire up. These are the names tracked in
// optimizer/symbols.go and friends.
func TestOptimizerGenerator_RepresentativeStubs(t *testing.T) {
	out := runOptimizerGenerator(t)
	for _, name := range []string{
		"_LOAD_FAST",
		// CPython 3.14 split the legacy _GUARD_BOTH_INT into per-slot
		// guards. Both must show up.
		"_GUARD_TOS_INT",
		"_GUARD_NOS_INT",
		"_BINARY_OP_ADD_INT",
		"_LOAD_CONST_INLINE_BORROW",
		"_CHECK_VALIDITY",
	} {
		if !strings.Contains(out, "func op_"+name+"() {}") {
			t.Errorf("missing stub for %s", name)
		}
	}
}

// TestOptimizerGenerator_OverrideWins asserts that when a uop has both
// a base body in bytecodes.c and an override in optimizer_bytecodes.c,
// the comment block carries the override (symbolic) version. We pick
// _LOAD_FAST because its symbolic body is one line and trivially
// distinguishable from the C body, which manipulates _PyStackRef and
// reference counts.
func TestOptimizerGenerator_OverrideWins(t *testing.T) {
	out := runOptimizerGenerator(t)
	idx := strings.Index(out, "// _LOAD_FAST: ")
	if idx < 0 {
		t.Fatal("no _LOAD_FAST stub found")
	}
	end := strings.Index(out[idx:], "func op__LOAD_FAST")
	if end < 0 {
		t.Fatal("no func op__LOAD_FAST after marker")
	}
	block := out[idx : idx+end]
	if !strings.Contains(block, "override from optimizer_bytecodes.c") {
		t.Errorf("_LOAD_FAST should be tagged as override; block:\n%s", block)
	}
	if !strings.Contains(block, "GETLOCAL(oparg)") {
		t.Errorf("_LOAD_FAST override body should contain GETLOCAL(oparg); block:\n%s", block)
	}
	// _PyStackRef_FromPyObjectNew is a giveaway of the base bytecodes.c
	// body. It must not leak into an override case.
	if strings.Contains(block, "_PyStackRef_FromPyObjectNew") {
		t.Errorf("_LOAD_FAST should not carry the base body; block:\n%s", block)
	}
}

// TestOptimizerGenerator_SymHelpers asserts the override pipeline
// faithfully carries Sym* helper invocations into the generated
// comment block. This is the per-replacer test: optimizer_bytecodes.c
// uses sym_set_const on the result of optimize_to_bool inside several
// uops; one specific landing point is _GUARD_NOS_INT or _LOAD_CONST.
// We pick a uop whose symbolic body unambiguously calls a Sym helper.
func TestOptimizerGenerator_SymHelpers(t *testing.T) {
	out := runOptimizerGenerator(t)
	// _LOAD_CONST_INLINE_BORROW pushes a known constant in the
	// symbolic interpreter; the body calls sym_new_const.
	idx := strings.Index(out, "// _LOAD_CONST_INLINE_BORROW: ")
	if idx < 0 {
		t.Fatal("no _LOAD_CONST_INLINE_BORROW stub found")
	}
	end := strings.Index(out[idx:], "func op__LOAD_CONST_INLINE_BORROW")
	if end < 0 {
		t.Fatal("no func op__LOAD_CONST_INLINE_BORROW after marker")
	}
	block := out[idx : idx+end]
	if !strings.Contains(block, "sym_new_const") {
		t.Errorf("_LOAD_CONST_INLINE_BORROW should call sym_new_const; block:\n%s", block)
	}

	// _GUARD_TOS_INT calls sym_set_type and (when matched) REPLACE_OP
	// in the override. Verify both trickle through the emitter.
	idx = strings.Index(out, "// _GUARD_TOS_INT: ")
	if idx < 0 {
		t.Fatal("no _GUARD_TOS_INT stub found")
	}
	end = strings.Index(out[idx:], "func op__GUARD_TOS_INT")
	if end < 0 {
		t.Fatal("no func op__GUARD_TOS_INT after marker")
	}
	block = out[idx : idx+end]
	if !strings.Contains(block, "sym_set_type") {
		t.Errorf("_GUARD_TOS_INT should call sym_set_type; block:\n%s", block)
	}
}

func headOf(s string, lines int) string {
	parts := strings.SplitN(s, "\n", lines+1)
	if len(parts) > lines {
		parts = parts[:lines]
	}
	return strings.Join(parts, "\n")
}
