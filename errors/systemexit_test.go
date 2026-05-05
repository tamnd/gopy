package errors

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestHandleSystemExitIntCode pins the most common SystemExit
// shape: raise SystemExit(2) returns (2, true) and clears the slot.
func TestHandleSystemExitIntCode(t *testing.T) {
	ts := state.NewThread()
	args := objects.NewTuple([]objects.Object{objects.NewInt(2)})
	Set(ts, PyExc_SystemExit, args)

	code, ok := HandleSystemExit(ts)
	if !ok {
		t.Fatalf("HandleSystemExit returned handled=false")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if Occurred(ts) != nil {
		t.Errorf("exception slot must be cleared after handling")
	}
}

// TestHandleSystemExitNoneCode pins the bare-raise shape: raise
// SystemExit() with no args (or args[0] = None) exits with code 0.
func TestHandleSystemExitNoneCode(t *testing.T) {
	ts := state.NewThread()
	Set(ts, PyExc_SystemExit, nil)

	code, ok := HandleSystemExit(ts)
	if !ok {
		t.Fatalf("HandleSystemExit returned handled=false")
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
}

// TestHandleSystemExitStringCode pins the "exit with a message"
// shape: raise SystemExit("oops") falls through to the print path
// (handled=false) so the caller can render the message.
func TestHandleSystemExitStringCode(t *testing.T) {
	ts := state.NewThread()
	args := objects.NewTuple([]objects.Object{objects.NewStr("oops")})
	Set(ts, PyExc_SystemExit, args)

	_, ok := HandleSystemExit(ts)
	if ok {
		t.Fatalf("HandleSystemExit returned handled=true for string code; should defer to print")
	}
	if Occurred(ts) == nil {
		t.Errorf("exception slot must remain set when not handled")
	}
}

// TestHandleSystemExitOnNonSystemExit pins the no-op path: a plain
// ValueError must not be intercepted.
func TestHandleSystemExitOnNonSystemExit(t *testing.T) {
	ts := state.NewThread()
	SetString(ts, PyExc_ValueError, "boom")

	_, ok := HandleSystemExit(ts)
	if ok {
		t.Fatalf("ValueError should not be handled by HandleSystemExit")
	}
}

// TestPrintExSystemExitReturnsCode pins the lifecycle integration:
// PrintEx with SystemExit(7) returns 7 and writes nothing.
func TestPrintExSystemExitReturnsCode(t *testing.T) {
	ts := state.NewThread()
	args := objects.NewTuple([]objects.Object{objects.NewInt(7)})
	Set(ts, PyExc_SystemExit, args)

	var buf bytes.Buffer
	rc := PrintEx(ts, &buf)
	if rc != 7 {
		t.Errorf("rc = %d, want 7", rc)
	}
	if buf.Len() != 0 {
		t.Errorf("PrintEx must not write for int SystemExit, got %q", buf.String())
	}
}

// TestPrintExNormalExceptionRenders pins the print path: a plain
// exception is rendered to w and the function returns 1.
func TestPrintExNormalExceptionRenders(t *testing.T) {
	ts := state.NewThread()
	SetString(ts, PyExc_ValueError, "bad value")

	var buf bytes.Buffer
	rc := PrintEx(ts, &buf)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(buf.String(), "ValueError") || !strings.Contains(buf.String(), "bad value") {
		t.Errorf("output %q must mention ValueError and 'bad value'", buf.String())
	}
}

// TestPrintExNoExceptionReturnsZero pins the empty-slot path.
func TestPrintExNoExceptionReturnsZero(t *testing.T) {
	ts := state.NewThread()
	var buf bytes.Buffer
	if rc := PrintEx(ts, &buf); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}
