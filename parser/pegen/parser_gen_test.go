// Round-trip and shape pins for the generated parser_gen.go.
// Re-running tools/parser_gen against the same python.gram must
// produce a byte-identical file; the rule-name table must agree
// with the const ids; and ErrParserNotImplemented must surface
// from Dispatch until the per-rule emitter ships.

package pegen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// repoRoot walks up from this test file until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("repoRoot: go.mod not found")
	return ""
}

func TestGeneratedRuleNamesIndex(t *testing.T) {
	if len(GeneratedRuleNames) == 0 {
		t.Fatal("GeneratedRuleNames is empty; regenerate parser_gen.go")
	}
	if GeneratedRuleNames[Rule_file] != "file" {
		t.Errorf("Rule_file -> %q, want \"file\"", GeneratedRuleNames[Rule_file])
	}
}

func TestDispatchReturnsSentinel(t *testing.T) {
	_, err := Dispatch(nil, StartFile)
	if !errors.Is(err, ErrParserNotImplemented) {
		t.Errorf("Dispatch err = %v, want ErrParserNotImplemented", err)
	}
}

func TestParserGenRoundtrip(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		cpython = filepath.Join(os.Getenv("HOME"), "github", "python", "cpython")
	}
	if _, err := os.Stat(filepath.Join(cpython, "Grammar", "python.gram")); err != nil {
		t.Skipf("CPYTHON checkout not available at %s; set $CPYTHON to enable", cpython)
	}
	root := repoRoot(t)
	current, err := os.ReadFile(filepath.Join(root, "parser", "pegen", "parser_gen.go"))
	if err != nil {
		t.Fatalf("read existing parser_gen.go: %v", err)
	}
	tmp, err := os.CreateTemp("", "parser_gen-*.go")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./tools/parser_gen", "-cpython="+cpython, "-out="+tmp.Name())
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("regenerate: %v\n%s", err, out)
	}
	regen, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatalf("read regenerated: %v", err)
	}
	if sum(current) != sum(regen) {
		t.Errorf("parser_gen.go drift: stored=%s regenerated=%s; rerun `go run ./tools/parser_gen`",
			sum(current), sum(regen))
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}
