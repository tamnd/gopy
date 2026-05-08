// Tests for tier2_generator.go: dispatch + stubs end-to-end against
// the real Python/bytecodes.c, plus a per-replacer check for the
// Tier-2 EXIT_IF jump-target shape and a ported-detection round-trip.
//
// CPython: Tools/cases_generator/tier2_generator.py
package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

func TestGenerateTier2Dispatch_RealBytecodes(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/bytecodes.c"
	src, err := readBytecodesSection(path)
	if err != nil {
		t.Skipf("upstream not available: %v", err)
	}
	a := parseAndAnalyze(t, src, path)

	var out bytes.Buffer
	if err := GenerateTier2Dispatch(a, "", &out); err != nil {
		t.Fatalf("GenerateTier2Dispatch: %v", err)
	}
	got := out.String()

	wantDispatchHits := []string{
		"func (s *Tier2State) executeUop(inst *UOPInstruction) Tier2Status",
		"switch inst.Opcode()",
		"case UopLoadFast:\n\t\treturn s.uopLoadFast(inst)",
		"case UopExitTrace:\n\t\treturn s.uopExitTrace(inst)",
		"case UopCheckValidity:\n\t\treturn s.uopCheckValidity(inst)",
		"return StatusDeopt",
	}
	for _, want := range wantDispatchHits {
		if !strings.Contains(got, want) {
			t.Errorf("dispatch missing fragment %q\n--- output ---\n%s", want, got)
		}
	}

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "uops_dispatch_gen.go", got, parser.ParseComments); err != nil {
		t.Fatalf("dispatch is not parseable Go: %v\n--- output ---\n%s", err, got)
	}
}

func TestGenerateTier2Stubs_PortedDetectionFiltersHits(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/bytecodes.c"
	src, err := readBytecodesSection(path)
	if err != nil {
		t.Skipf("upstream not available: %v", err)
	}
	a := parseAndAnalyze(t, src, path)

	// First emit with no ported set: every viable uop is stubbed.
	var allStubs bytes.Buffer
	if err := GenerateTier2Stubs(a, nil, "", &allStubs); err != nil {
		t.Fatalf("GenerateTier2Stubs(nil): %v", err)
	}
	allText := allStubs.String()
	if !strings.Contains(allText, "func (s *Tier2State) uopLoadFast(") {
		t.Fatal("expected uopLoadFast stub when ported set is empty")
	}
	if !strings.Contains(allText, "func (s *Tier2State) uopCheckValidity(") {
		t.Fatal("expected uopCheckValidity stub when ported set is empty")
	}

	// Then emit with uopLoadFast marked ported: that one stub drops out.
	ported := map[string]struct{}{"uopLoadFast": {}}
	var partial bytes.Buffer
	if err := GenerateTier2Stubs(a, ported, "", &partial); err != nil {
		t.Fatalf("GenerateTier2Stubs(ported): %v", err)
	}
	partText := partial.String()
	if strings.Contains(partText, "func (s *Tier2State) uopLoadFast(") {
		t.Error("expected uopLoadFast stub to be dropped when ported")
	}
	if !strings.Contains(partText, "func (s *Tier2State) uopCheckValidity(") {
		t.Error("expected uopCheckValidity stub to remain when not ported")
	}

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "uops_stubs_gen.go", partText, parser.ParseComments); err != nil {
		t.Fatalf("stubs file is not parseable Go: %v", err)
	}
}

// TestDetectPortedUops_FlatDir round-trips the AST scanner over a
// throwaway temp dir holding two Go files: one with a hand-ported
// method on *Tier2State, and one *_gen.go file that the scanner must
// ignore.
func TestDetectPortedUops_FlatDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "uops_impl.go"), `package optimizer

type Tier2State struct{}
type UOPInstruction struct{}
type Tier2Status int

func (s *Tier2State) uopLoadFast(_ *UOPInstruction) Tier2Status { return 0 }
func (s *Tier2State) uopNop(_ *UOPInstruction) Tier2Status { return 0 }
func (s *Tier2State) helper() {}
`)
	// Generated file: must be ignored.
	mustWrite(t, filepath.Join(dir, "uops_stubs_gen.go"), `package optimizer

func (s *Tier2State) uopExitTrace(_ *UOPInstruction) Tier2Status { return 0 }
`)

	got, err := DetectPortedUops(dir)
	if err != nil {
		t.Fatalf("DetectPortedUops: %v", err)
	}
	if _, ok := got["uopLoadFast"]; !ok {
		t.Errorf("expected uopLoadFast in ported set, got %v", got)
	}
	if _, ok := got["uopNop"]; !ok {
		t.Errorf("expected uopNop in ported set, got %v", got)
	}
	if _, ok := got["uopExitTrace"]; ok {
		t.Errorf("uopExitTrace came from a *_gen.go file and must not be detected")
	}
	if _, ok := got["helper"]; ok {
		t.Errorf("helper does not start with uop and must not be detected")
	}
}

// TestTier2Emitter_ExitIfReplacer feeds a synthetic uop body with an
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

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
