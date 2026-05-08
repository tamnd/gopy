package regrtest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildGopy compiles the gopy binary into the test's temp dir and
// returns its path. Skips the test if `go build` itself is unhappy
// (e.g. cross-compile sandbox).
func buildGopy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gopy")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/tamnd/gopy/cmd/gopy")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("go build gopy: %v", err)
	}
	return bin
}

func TestRunnerRunPasses(t *testing.T) {
	bin := buildGopy(t)
	corpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(corpus, "test_smoke.py"), []byte(`print("ok")`+"\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	r := &Runner{Binary: bin, Corpus: corpus, Timeout: 30 * time.Second}
	res := r.Run(context.Background(), Entry{Name: "test_smoke", Status: StatusReady})
	if res.Outcome != OutcomePass {
		t.Fatalf("Outcome = %s (stderr=%q), want pass", res.Outcome, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "ok") {
		t.Errorf("stdout = %q, want contains ok", res.Stdout)
	}
}

func TestRunnerRunFails(t *testing.T) {
	bin := buildGopy(t)
	corpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(corpus, "test_boom.py"), []byte(`raise SystemExit(2)`+"\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	r := &Runner{Binary: bin, Corpus: corpus, Timeout: 30 * time.Second}
	res := r.Run(context.Background(), Entry{Name: "test_boom", Status: StatusReady})
	if res.Outcome != OutcomeFail {
		t.Fatalf("Outcome = %s, want fail", res.Outcome)
	}
	if res.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want non-zero")
	}
}

func TestRunnerSkipDeferred(t *testing.T) {
	r := &Runner{Binary: "gopy-not-needed", Corpus: "/does-not-matter"}
	res := r.Run(context.Background(), Entry{Name: "test_x", Status: StatusDeferred})
	if res.Outcome != OutcomeSkip {
		t.Fatalf("Outcome = %s, want skip", res.Outcome)
	}
}

func TestRunnerSkipOutOfScope(t *testing.T) {
	r := &Runner{Binary: "gopy-not-needed", Corpus: "/does-not-matter"}
	res := r.Run(context.Background(), Entry{Name: "test_capi/", Status: StatusOutOfScope})
	if res.Outcome != OutcomeSkip {
		t.Fatalf("Outcome = %s, want skip", res.Outcome)
	}
}

func TestRunnerMissingFile(t *testing.T) {
	bin := buildGopy(t)
	corpus := t.TempDir()
	r := &Runner{Binary: bin, Corpus: corpus, Timeout: 5 * time.Second}
	res := r.Run(context.Background(), Entry{Name: "test_absent", Status: StatusReady})
	if res.Outcome != OutcomeMissing {
		t.Fatalf("Outcome = %s, want missing", res.Outcome)
	}
}

func TestRunnerPackageEntryUnsupported(t *testing.T) {
	bin := buildGopy(t)
	corpus := t.TempDir()
	if err := os.Mkdir(filepath.Join(corpus, "test_pkg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	r := &Runner{Binary: bin, Corpus: corpus, Timeout: 5 * time.Second}
	res := r.Run(context.Background(), Entry{Name: "test_pkg/", Status: StatusReady})
	if res.Outcome != OutcomeError {
		t.Fatalf("Outcome = %s, want error (package entries not yet wired)", res.Outcome)
	}
}

func TestRunnerTimeout(t *testing.T) {
	bin := buildGopy(t)
	corpus := t.TempDir()
	src := `
i = 0
while i < 10**12:
    i = i + 1
`
	if err := os.WriteFile(filepath.Join(corpus, "test_spin.py"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := &Runner{Binary: bin, Corpus: corpus, Timeout: 200 * time.Millisecond}
	res := r.Run(context.Background(), Entry{Name: "test_spin", Status: StatusReady})
	if res.Outcome != OutcomeTimeout {
		t.Fatalf("Outcome = %s, want timeout", res.Outcome)
	}
}

func TestSummaryAndRunAll(t *testing.T) {
	m := &Manifest{Entries: []Entry{
		{Name: "test_a", Status: StatusDeferred},
		{Name: "test_b/", Status: StatusOutOfScope},
		{Name: "test_c", Status: StatusReady},
	}}
	corpus := t.TempDir()
	r := &Runner{Binary: "anything", Corpus: corpus, Manifest: m, Timeout: time.Second}
	results := r.RunAll(context.Background())
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	sum := Summary(results)
	if sum[OutcomeSkip] != 2 {
		t.Errorf("skip = %d, want 2", sum[OutcomeSkip])
	}
	if sum[OutcomeMissing] != 1 {
		t.Errorf("missing = %d, want 1", sum[OutcomeMissing])
	}
}

func TestCountFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"test_a.py", "test_b.py", "helper.py", "README"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	n, err := CountFiles(dir)
	if err != nil {
		t.Fatalf("CountFiles: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
}
