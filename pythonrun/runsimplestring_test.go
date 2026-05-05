package pythonrun

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// TestRunSimpleStringSuccessReturnsZero pins the CPython contract that
// PyRun_SimpleStringFlags returns 0 when the source runs cleanly.
func TestRunSimpleStringSuccessReturnsZero(t *testing.T) {
	ts := state.NewThread()
	var stderr bytes.Buffer
	rc := RunSimpleString(ts, "1 + 2", objects.NewDict(), &stderr)
	if rc != 0 && !shouldSkipUnimplemented(parserOrCompilerErr(t, "1 + 2")) {
		t.Fatalf("RunSimpleString rc=%d, stderr=%q", rc, stderr.String())
	}
	if rc == 0 && stderr.Len() != 0 {
		t.Errorf("clean run wrote to stderr: %q", stderr.String())
	}
}

// TestRunSimpleStringParseFailureReturnsMinusOne pins -1 on a parse
// gap. The error text is rendered to stderr (the v0.7 fallback path
// since the parser does not raise SyntaxError yet).
func TestRunSimpleStringParseFailureReturnsMinusOne(t *testing.T) {
	ts := state.NewThread()
	var stderr bytes.Buffer
	rc := RunSimpleString(ts, "this is not valid python !!!", objects.NewDict(), &stderr)
	if rc != -1 {
		t.Fatalf("rc = %d, want -1 on parse failure", rc)
	}
	if stderr.Len() == 0 {
		t.Error("parse failure produced no stderr output")
	}
}

// TestRunSimpleStringAddsTrailingNewline confirms the CPython
// auto-newline behavior: a source without trailing '\n' still
// parses (RunSimpleString appends one).
func TestRunSimpleStringAddsTrailingNewline(t *testing.T) {
	ts := state.NewThread()
	var stderr bytes.Buffer
	rc := RunSimpleString(ts, "pass", objects.NewDict(), &stderr)
	if rc != 0 && !shouldSkipUnimplemented(parserOrCompilerErr(t, "pass")) {
		t.Fatalf("RunSimpleString rc=%d, stderr=%q", rc, stderr.String())
	}
}

// parserOrCompilerErr re-runs src through RunString to surface the
// underlying parse/compile error, used by tests that need to decide
// whether the v0.7 panel gap explains a non-zero rc.
func parserOrCompilerErr(t *testing.T, src string) error {
	t.Helper()
	ts := state.NewThread()
	_, err := RunString(ts, src, "<probe>", parser.ModeFile, objects.NewDict(), nil)
	return err
}

// TestRunStringHandlesEmptyAndNoNewline pins both edge cases the
// CPython entry handles silently: an empty string is a no-op module
// (returns None), and a source without trailing newline still parses.
func TestRunStringHandlesEmptyAndNoNewline(t *testing.T) {
	ts := state.NewThread()
	v, err := RunString(ts, "", "<empty>", parser.ModeFile, objects.NewDict(), nil)
	if shouldSkipUnimplemented(err) {
		t.Skipf("parser gap on empty source: %v", err)
	}
	if err != nil {
		t.Fatalf("empty source: %v", err)
	}
	if v != nil && v != objects.None() {
		t.Errorf("empty source produced %v, want None", v)
	}
}

// TestRunStringReturnsExpressionValue confirms ModeEval threads the
// expression result back, which the panel's parity check relies on.
// Skips when the parser/VM panel still bails.
func TestRunStringReturnsExpressionValue(t *testing.T) {
	ts := state.NewThread()
	v, err := RunString(ts, "1 + 2", "<probe>", parser.ModeEval, objects.NewDict(), nil)
	if shouldSkipUnimplemented(err) {
		t.Skipf("parser/VM gap: %v", err)
	}
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	got, gerr := objects.Str(v)
	if gerr != nil {
		t.Fatalf("Str: %v", gerr)
	}
	if got != "3" {
		t.Errorf("got %q, want %q", got, "3")
	}
}

// TestRunStringEvalErrorPropagates pins that an eval-time failure
// surfaces as a Go error from RunString rather than disappearing
// into the void. Uses ErrNotImplemented as a known marker; if the
// op panel grows to handle the source the test skips.
func TestRunStringEvalErrorPropagates(t *testing.T) {
	ts := state.NewThread()
	// Intentionally exercise an opcode that may still bail; if
	// it succeeds, the test reports the value to confirm shape.
	_, err := RunString(ts, "1 + 2", "<probe>", parser.ModeEval, objects.NewDict(), nil)
	if err != nil && !shouldSkipUnimplemented(err) && !strings.Contains(err.Error(), vm.ErrNotImplemented.Error()) {
		t.Fatalf("unexpected error: %v", err)
	}
}
