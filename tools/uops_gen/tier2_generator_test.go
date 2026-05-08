// Tests for tier2_generator.go: end-to-end pipeline against the real
// Python/bytecodes.c, plus a per-replacer check for the Tier-2 EXIT_IF
// jump-target shape.
//
// CPython: Tools/cases_generator/tier2_generator.py
package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func parseAndAnalyze(t *testing.T, src, path string) *Analysis {
	t.Helper()
	p, err := NewParser(src, path)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var forest []Node
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if n == nil {
			break
		}
		forest = append(forest, n)
	}
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	return a
}

func TestGenerateTier2_RealBytecodes(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/bytecodes.c"
	src, err := readBytecodesSection(path)
	if err != nil {
		t.Skipf("upstream not available: %v", err)
	}
	a := parseAndAnalyze(t, src, path)

	var out bytes.Buffer
	if err := GenerateTier2(a, "", &out); err != nil {
		t.Fatalf("GenerateTier2: %v", err)
	}
	got := out.String()

	// Representative uops we expect to see as comment blocks plus
	// handler stubs.
	wantUops := []string{
		"_LOAD_FAST",
		"_RESUME_CHECK",
		"_CHECK_VALIDITY",
		"_BINARY_OP_ADD_INT",
		"_GUARD_TOS_INT",
		"_GUARD_NOS_INT",
		"_EXIT_TRACE",
	}
	for _, name := range wantUops {
		caseMarker := "case " + name + ":"
		stubMarker := "func uop" + name + "()"
		if !strings.Contains(got, caseMarker) {
			t.Errorf("missing case marker for %s", name)
		}
		if !strings.Contains(got, stubMarker) {
			t.Errorf("missing handler stub for %s", name)
		}
	}

	// Output must be parseable Go.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "uops_cases_gen.go", got, parser.ParseComments); err != nil {
		// Print first ~20 lines to aid debugging without flooding.
		lines := strings.SplitN(got, "\n", 50)
		preview := strings.Join(lines[:min(40, len(lines))], "\n")
		t.Fatalf("generated file is not parseable Go: %v\n--- preview ---\n%s", err, preview)
	}

	// _LOAD_FAST's body should mention frame->localsplus inside its
	// comment block. We grep within the slice that starts at the case
	// marker and ends at the next handler stub.
	loadIdx := strings.Index(got, "// uop _LOAD_FAST body:")
	if loadIdx < 0 {
		t.Fatal("missing _LOAD_FAST comment block header")
	}
	loadEnd := strings.Index(got[loadIdx:], "func uop_LOAD_FAST()")
	if loadEnd < 0 {
		t.Fatal("missing _LOAD_FAST handler stub after comment block")
	}
	loadBlock := got[loadIdx : loadIdx+loadEnd]
	// _LOAD_FAST's body should reference GETLOCAL(oparg) (the macro
	// that expands to frame->localsplus[oparg] in the C side).
	if !strings.Contains(loadBlock, "GETLOCAL(oparg)") {
		t.Errorf("_LOAD_FAST body missing GETLOCAL(oparg) reference; got:\n%s", loadBlock)
	}
}

// TestTier2Emitter_ErrorIfReplacer feeds a synthetic uop body with an
// EXIT_IF and asserts that the Tier-2 replacer emits the
// JUMP_TO_JUMP_TARGET tail rather than the Tier-1 JUMP_TO_PREDICTED.
func TestTier2Emitter_ExitIfReplacer(t *testing.T) {
	const dsl = `op(_T2_EXIT_IF_TEST, (--)) {
    EXIT_IF(true);
}
`
	a := parseAndAnalyze(t, dsl, "<synthetic>")
	uop := a.Uops["_T2_EXIT_IF_TEST"]
	if uop == nil {
		t.Fatal("synthetic uop missing")
	}

	var buf bytes.Buffer
	cw := NewCWriter(&buf, 2, false)
	em := NewTier2Emitter(cw, a.Labels)
	storage, err := StorageForUop(NewStack(), uop, cw, true)
	if err != nil {
		t.Fatalf("StorageForUop: %v", err)
	}
	if _, _, err := em.EmitTokens(uop, storage, nil, false); err != nil {
		t.Fatalf("EmitTokens: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "JUMP_TO_JUMP_TARGET") {
		t.Errorf("expected JUMP_TO_JUMP_TARGET in Tier-2 EXIT_IF rewrite; got:\n%s", got)
	}
	if strings.Contains(got, "JUMP_TO_PREDICTED") {
		t.Errorf("Tier-2 EXIT_IF must not emit JUMP_TO_PREDICTED; got:\n%s", got)
	}
}
