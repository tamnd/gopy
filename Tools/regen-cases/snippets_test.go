package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnippetParity is the spec 1714 Phase 1 gate. For each
// directory under Tools/cases_generator/testdata/snippets/, it
// runs input.py with the generator dir on PYTHONPATH and asserts
// stdout equals want.txt. Adding a snippet means dropping a new
// directory next to the existing ones; no test-side change.
func TestSnippetParity(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	genDir := filepath.Join(root, "Tools", "cases_generator")
	snipsDir := filepath.Join(genDir, "testdata", "snippets")

	entries, err := os.ReadDir(snipsDir)
	if err != nil {
		t.Fatalf("read snippets dir: %v", err)
	}

	python := defaultPython()

	var ran int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dir := filepath.Join(snipsDir, name)
		input := filepath.Join(dir, "input.py")
		wantPath := filepath.Join(dir, "want.txt")
		if _, err := os.Stat(input); err != nil {
			continue
		}
		want, err := os.ReadFile(wantPath)
		if err != nil {
			t.Errorf("%s: want.txt missing: %v", name, err)
			continue
		}
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(python, input)
			cmd.Env = append(os.Environ(),
				"CASES_GENERATOR_DIR="+genDir,
				"PYTHONDONTWRITEBYTECODE=1",
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("input.py failed: %v\nstderr:\n%s", err, stderr.String())
			}
			got := stdout.Bytes()
			if !bytes.Equal(got, want) {
				t.Fatalf("output mismatch\n--- got ---\n%s\n--- want ---\n%s",
					indent(string(got)), indent(string(want)))
			}
		})
		ran++
	}
	if ran == 0 {
		t.Fatal("no snippets exercised")
	}
	if ran < 20 {
		t.Logf("only %d snippets exercised; spec 1714 Phase 1 target is 30 by Phase 1.4", ran)
	}
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
