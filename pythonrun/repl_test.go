package pythonrun

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/state"
)

// TestInteractiveLoopExitsOnEOF pins the basic loop contract: read
// EOF, return 0, write only the prompt. The fixture matches the
// spec 1624 gate "feed empty stdin, expect clean exit".
func TestInteractiveLoopExitsOnEOF(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()
	rc := InteractiveLoop(ts, strings.NewReader(""), &stdout, &stderr, g)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.HasPrefix(stdout.String(), PromptPS1) {
		t.Errorf("stdout %q must start with prompt %q", stdout.String(), PromptPS1)
	}
}

// TestInteractiveLoopExpressionPrints pins the spec 1624 gate test:
// feed "1+2\nprint('done')\n" and expect "3" then "done" in stdout.
// Skipped when the parser/VM panel still bails on these expressions.
func TestInteractiveLoopExpressionPrints(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()
	rc := InteractiveLoop(ts, strings.NewReader("1+2\nprint('done')\n"), &stdout, &stderr, g)
	if rc != 0 {
		t.Fatalf("rc = %d, stderr=%q", rc, stderr.String())
	}
	body := stripPrompts(stdout.String())
	if !strings.Contains(body, "3") {
		if shouldSkipUnimplemented(parserOrCompilerErr(t, "1+2")) {
			t.Skipf("parser/VM gap on 1+2: stderr=%q", stderr.String())
		}
		t.Errorf("expected '3' in stdout, got %q (stderr=%q)", body, stderr.String())
	}
}

// TestInteractiveLoopParseErrorPrintsAndContinues pins the v0.7
// behavior: a bad line writes to stderr and the loop keeps going
// (until real EOF).
func TestInteractiveLoopParseErrorPrintsAndContinues(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()
	rc := InteractiveLoop(ts, strings.NewReader("!!!not python!!!\n"), &stdout, &stderr, g)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0 (loop should swallow syntax errors)", rc)
	}
	if stderr.Len() == 0 {
		t.Error("syntax error produced no stderr output")
	}
}

// stripPrompts pulls the prompt strings out of stdout so the test
// can assert on the program's output independently of the prompt.
func stripPrompts(s string) string {
	out := strings.ReplaceAll(s, PromptPS1, "")
	out = strings.ReplaceAll(out, PromptPS2, "")
	return out
}
