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

func captureFile(t *testing.T) (*os.File, func()) {
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
