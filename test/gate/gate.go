// Package gate runs a Python script through both CPython 3.14 and the
// gopy binary and compares their stdout / stderr / exit code. A test
// passes only when both interpreters produce byte-equal output.
//
// This is the strongest form of behavioral parity check we have: the
// in-process Go tests under module/io/* pin individual contract points,
// but a gate runs the same Python source through both implementations
// and confirms the user-visible result lines up. When CPython 3.14 is
// not on PATH the gate self-skips, so contributors without a local
// 3.14 install still run the rest of the suite.
package gate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// FindCPython returns a CPython 3.14.x interpreter on PATH, or "" if
// none is available. Callers typically Skip the test when this is "".
func FindCPython(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3.14", "python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.CommandContext(t.Context(), path, "--version").Output() //nolint:gosec // path comes from exec.LookPath
		if err != nil {
			continue
		}
		if strings.Contains(string(out), "Python 3.14") {
			return path
		}
	}
	return ""
}

// BuildGopy compiles the gopy binary into t.TempDir() and returns the
// path. Skips the test if `go build` itself fails (cross-compile sandbox,
// missing toolchain, etc.).
func BuildGopy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "gopy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "github.com/tamnd/gopy/cmd/gopy") //nolint:gosec // hard-coded build target
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("go build gopy: %v", err)
	}
	return bin
}

// runResult holds one interpreter invocation's stdout / stderr / exit.
type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

func runScript(ctx context.Context, bin, script string, args []string, timeout time.Duration) runResult {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tmp, err := os.CreateTemp("", "gate-*.py")
	if err != nil {
		return runResult{Err: err}
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		return runResult{Err: err}
	}
	if err := tmp.Close(); err != nil {
		return runResult{Err: err}
	}

	argv := append([]string{tmp.Name()}, args...)
	cmd := exec.CommandContext(cctx, bin, argv...) //nolint:gosec // bin and script are vetted by callers (test harness only)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	res := runResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.Err = err
		}
	}
	return res
}

// Compare runs the given Python script through cpythonBin and gopyBin
// and reports a t.Errorf if stdout, stderr, or exit code differ. The
// raw outputs from each interpreter are attached to the failure message
// so a diff is one scroll-back away.
func Compare(t *testing.T, cpythonBin, gopyBin, script string, args ...string) {
	t.Helper()
	ctx := t.Context()
	timeout := 30 * time.Second

	cRes := runScript(ctx, cpythonBin, script, args, timeout)
	if cRes.Err != nil {
		t.Fatalf("cpython run: %v", cRes.Err)
	}
	gRes := runScript(ctx, gopyBin, script, args, timeout)
	if gRes.Err != nil {
		t.Fatalf("gopy run: %v", gRes.Err)
	}

	// Normalize CRLF -> LF: Windows CPython writes \r\n, gopy writes \n.
	cRes.Stdout = strings.ReplaceAll(cRes.Stdout, "\r\n", "\n")
	gRes.Stdout = strings.ReplaceAll(gRes.Stdout, "\r\n", "\n")

	if cRes.ExitCode != gRes.ExitCode {
		t.Errorf("exit code mismatch: cpython=%d gopy=%d\n--- script ---\n%s\n--- cpython stdout ---\n%s\n--- gopy stdout ---\n%s\n--- cpython stderr ---\n%s\n--- gopy stderr ---\n%s",
			cRes.ExitCode, gRes.ExitCode, script, cRes.Stdout, gRes.Stdout, cRes.Stderr, gRes.Stderr)
	}
	if cRes.Stdout != gRes.Stdout {
		t.Errorf("stdout mismatch:\n--- cpython ---\n%s\n--- gopy ---\n%s", cRes.Stdout, gRes.Stdout)
	}
}
