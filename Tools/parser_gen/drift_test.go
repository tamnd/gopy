// Pins the drift check: the parser_gen.go shipped under
// parser/pegen/ must match the grammar at $CPYTHON/Grammar/
// python.gram. CI fails loudly when they diverge so a CPython
// rebase that forgets to rerun the generator gets caught.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckGrammarDrift(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		cpython = filepath.Join(os.Getenv("HOME"), "github", "python", "cpython")
	}
	gram := filepath.Join(cpython, "Grammar", "python.gram")
	if _, err := os.Stat(gram); err != nil {
		t.Skipf("python.gram unavailable: %v", err)
	}
	// Walk up from the test binary's working dir to the repo root.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repo := filepath.Clean(filepath.Join(wd, "..", ".."))
	gen := filepath.Join(repo, "parser", "pegen", "parser_gen.go")
	if _, err := os.Stat(gen); err != nil {
		t.Skipf("parser_gen.go unavailable: %v", err)
	}
	if err := CheckGrammarDrift(gram, gen); err != nil {
		t.Fatalf("drift detected: %v", err)
	}
}

func TestRecordedGrammarHashMissing(t *testing.T) {
	if got := recordedGrammarHash([]byte("// no header here\npackage x\n")); got != "" {
		t.Errorf("recordedGrammarHash on plain file = %q, want empty", got)
	}
}

func TestRecordedGrammarHashRoundtrip(t *testing.T) {
	src := []byte("// blah\n// gram-sha256: deadbeef1234\n\npackage x\n")
	if got := recordedGrammarHash(src); got != "deadbeef1234" {
		t.Errorf("recordedGrammarHash = %q, want deadbeef1234", got)
	}
}
