package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	stdout, cleanup := captureFile(t)
	defer cleanup()

	rc := run([]string{"--version"}, stdout, os.Stderr)
	if rc != 0 {
		t.Fatalf("run --version returned %d, want 0", rc)
	}

	out := readFile(t, stdout)
	if !strings.HasPrefix(out, "gopy ") {
		t.Fatalf("version output %q must start with %q", out, "gopy ")
	}
}

func TestRunDefault(t *testing.T) {
	stdout, cleanup := captureFile(t)
	defer cleanup()

	rc := run(nil, stdout, os.Stderr)
	if rc != 0 {
		t.Fatalf("run with no args returned %d, want 0", rc)
	}
	out := readFile(t, stdout)
	if !strings.Contains(out, "gopy ") {
		t.Fatalf("default output %q must contain %q", out, "gopy ")
	}
}

func TestRunCopyright(t *testing.T) {
	stdout, cleanup := captureFile(t)
	defer cleanup()

	rc := run([]string{"--copyright"}, stdout, os.Stderr)
	if rc != 0 {
		t.Fatalf("run --copyright returned %d, want 0", rc)
	}
	out := readFile(t, stdout)
	if !strings.Contains(out, "gopy Authors") {
		t.Fatalf("copyright output %q must mention the gopy Authors", out)
	}
}

func TestRunDashCSmoke(t *testing.T) {
	// The -c flag is the v0.6 smoke harness. The parser/VM panel is
	// incomplete, so we accept either a clean exit (0) or a non-zero
	// from the parser/eval bail; the contract here is just that the
	// flag wires through to the pipeline without panicking.
	stdout, cleanupOut := captureFile(t)
	defer cleanupOut()
	stderr, cleanupErr := captureFile(t)
	defer cleanupErr()

	rc := run([]string{"-c", "1 + 2"}, stdout, stderr)
	out := readFile(t, stdout)
	errOut := readFile(t, stderr)
	if rc != 0 && !strings.Contains(errOut, "parse:") && !strings.Contains(errOut, "eval:") && !strings.Contains(errOut, "compile:") {
		t.Fatalf("rc=%d stdout=%q stderr=%q: expected pipeline error or success", rc, out, errOut)
	}
}

func TestRunUnknownFlag(t *testing.T) {
	stdout, cleanupOut := captureFile(t)
	defer cleanupOut()
	stderr, cleanupErr := captureFile(t)
	defer cleanupErr()

	rc := run([]string{"--no-such-flag"}, stdout, stderr)
	if rc == 0 {
		t.Fatalf("run with unknown flag returned 0, want non-zero")
	}
}

func captureFile(t *testing.T) (file *os.File, cleanup func()) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gopy-*.out")
	if err != nil {
		t.Fatal(err)
	}
	return f, func() { _ = f.Close() }
}

func readFile(t *testing.T, f *os.File) string {
	t.Helper()
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
