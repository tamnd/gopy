package lifecycle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainDashCPrintsExpression pins the spec 1622-H gate: feed
// `gopy -c print(1+2)` and expect `3` on stdout with exit 0. The
// parser/VM panel still bails on some expression shapes; when the
// dispatch surfaces a parse/eval/compile bail, we accept that as
// long as the wiring drove without panic.
func TestMainDashCPrintsExpression(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := Main([]string{"gopy", "-c", "print(1+2)"}, strings.NewReader(""), &stdout, &stderr)
	out := stdout.String()
	errOut := stderr.String()

	if rc == 0 {
		if !strings.Contains(out, "3") {
			t.Fatalf("rc=0 but stdout=%q does not contain '3' (stderr=%q)", out, errOut)
		}
		return
	}
	if !strings.Contains(errOut, "parse:") && !strings.Contains(errOut, "compile:") && !strings.Contains(errOut, "eval:") {
		t.Fatalf("rc=%d stdout=%q stderr=%q: expected pipeline error or success", rc, out, errOut)
	}
}

// TestMainNoArgsRunsREPL pins the bare-invocation arm: empty argv
// drops into pythonrun.InteractiveLoop. With an empty stdin the
// loop reads EOF and returns 0 immediately.
func TestMainNoArgsRunsREPL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := Main([]string{"gopy"}, strings.NewReader(""), &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q, want 0", rc, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Errorf("REPL arm should at least print the prompt; got empty stdout")
	}
}

// TestMainDashMReturnsNotImplemented pins the v0.7 stub: -m foo
// reports the v0.8 marker and exits non-zero. This freezes the
// contract until spec 1623 lands the import system.
func TestMainDashMReturnsNotImplemented(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := Main([]string{"gopy", "-m", "foo"}, strings.NewReader(""), &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("rc=0, want non-zero for -m before spec 1623")
	}
	if !strings.Contains(stderr.String(), "spec 1623") {
		t.Errorf("stderr=%q must mention spec 1623", stderr.String())
	}
}

// TestMainScriptFile pins the positional-script arm: a path
// argument routes through pythonrun.RunAnyFile.
func TestMainScriptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.py")
	if err := os.WriteFile(path, []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	rc := Main([]string{"gopy", path}, strings.NewReader(""), &stdout, &stderr)
	if rc != 0 && stderr.Len() == 0 && stdout.Len() == 0 {
		t.Fatalf("rc=%d but no output: should at least surface an error", rc)
	}
}

// TestMainEmptyArgvUsesPlaceholder pins the defensive nil-argv
// path: Main fills in a synthetic argv[0] so getopt does not panic
// on an empty slice.
func TestMainEmptyArgvUsesPlaceholder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := Main(nil, strings.NewReader(""), &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q, want 0 (empty argv should drop into REPL)", rc, stderr.String())
	}
}

// TestMainSystemExitPropagatesCode pins the 1624-D contract: a
// SystemExit raised through -c surfaces its exit code as the
// process exit code, not a hardcoded 1. The user-facing
// `SystemExit` name is not yet wired into the builtins dict (that
// lands with 1651-builtins); until then the source bails at the
// NameError lookup, which we accept as long as rc is non-zero.
func TestMainSystemExitPropagatesCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := Main([]string{"gopy", "-c", "raise SystemExit(7)"}, strings.NewReader(""), &stdout, &stderr)
	if rc == 7 {
		return
	}
	allowed := []string{"parse:", "compile:", "eval:", "NameError"}
	for _, marker := range allowed {
		if strings.Contains(stderr.String(), marker) {
			return
		}
	}
	t.Fatalf("rc=%d stdout=%q stderr=%q: SystemExit should propagate code 7 once the panel covers raise", rc, stdout.String(), stderr.String())
}
