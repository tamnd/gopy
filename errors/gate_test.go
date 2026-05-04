// Package errors gate test exercises the v0.3 release gate from
// notes/Spec/1600/1611_gopy_errors.md: raise, catch, chain, and format
// a traceback through the Go API.
package errors_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/traceback"
)

func TestGateRaiseSetStringOccurred(t *testing.T) {
	ts := state.NewThread()
	if errors.Occurred(ts) != nil {
		t.Fatal("fresh thread should have no exception")
	}
	errors.SetString(ts, errors.PyExc_ValueError, "boom")
	exc := errors.Occurred(ts)
	if exc == nil {
		t.Fatal("expected ValueError after SetString")
	}
	if exc.TypeName() != "ValueError" {
		t.Fatalf("type: got %q want ValueError", exc.TypeName())
	}
	if exc.Message() != "boom" {
		t.Fatalf("message: got %q want boom", exc.Message())
	}
	if !errors.Match(exc, errors.PyExc_Exception) {
		t.Fatal("ValueError must match Exception")
	}
}

func TestGateClearAndFetch(t *testing.T) {
	ts := state.NewThread()
	errors.SetString(ts, errors.PyExc_KeyError, "k")
	typ, val, _ := errors.Fetch(ts)
	if typ == nil || val == nil {
		t.Fatal("Fetch returned nil")
	}
	if errors.Occurred(ts) != nil {
		t.Fatal("Fetch must clear the slot")
	}
	errors.Restore(ts, typ, val, nil)
	if errors.Occurred(ts) == nil {
		t.Fatal("Restore must reinstate the exception")
	}
	errors.Clear(ts)
	if errors.Occurred(ts) != nil {
		t.Fatal("Clear must drop the exception")
	}
}

func TestGateChainContextCause(t *testing.T) {
	ts := state.NewThread()
	errors.SetString(ts, errors.PyExc_ValueError, "first")
	first := errors.Occurred(ts)
	errors.SetString(ts, errors.PyExc_RuntimeError, "second")
	second := errors.Occurred(ts)
	if second.Context != first {
		t.Fatal("Raise must thread the previous exception into Context")
	}

	cause := errors.New(errors.PyExc_KeyError, objects.NewTuple([]objects.Object{objects.NewStr("k")}))
	chained := errors.New(errors.PyExc_ValueError, objects.NewTuple([]objects.Object{objects.NewStr("wrap")}))
	errors.Clear(ts)
	errors.RaiseFrom(ts, chained, cause)
	got := errors.Occurred(ts)
	if got != chained || got.Cause != cause || !got.Suppress {
		t.Fatal("RaiseFrom must set Cause and Suppress")
	}
}

func TestGateAttachTracebackAndFormat(t *testing.T) {
	ts := state.NewThread()
	errors.SetString(ts, errors.PyExc_ValueError, "boom")
	errors.AttachTraceback(ts, traceback.Entry{File: "a.py", Line: 1, Name: "f"})
	errors.AttachTraceback(ts, traceback.Entry{File: "b.py", Line: 2, Name: "g"})

	out := errors.FormatException(errors.Occurred(ts))
	for _, want := range []string{
		"Traceback (most recent call last):",
		`File "a.py", line 1, in f`,
		`File "b.py", line 2, in g`,
		"ValueError: boom",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("format missing %q\n---\n%s", want, out)
		}
	}
}

func TestGatePrintWritesAndClears(t *testing.T) {
	ts := state.NewThread()
	errors.SetString(ts, errors.PyExc_ValueError, "boom")
	var buf bytes.Buffer
	errors.Print(ts, &buf)
	if !strings.Contains(buf.String(), "ValueError: boom") {
		t.Fatalf("Print output missing payload: %q", buf.String())
	}
	if errors.Occurred(ts) != nil {
		t.Fatal("Print must clear the slot")
	}
}

func TestGateKeyErrorReprFormat(t *testing.T) {
	exc := errors.New(errors.PyExc_KeyError, objects.NewTuple([]objects.Object{objects.NewStr("missing")}))
	out := errors.FormatException(exc)
	if !strings.Contains(out, "KeyError: 'missing'") {
		t.Fatalf("KeyError must repr its key: %q", out)
	}
}

func TestGateSuggestAttr(t *testing.T) {
	got := errors.SuggestAttr("appedn", []string{"append", "extend", "pop"})
	if got != "append" {
		t.Fatalf("suggestion: got %q want append", got)
	}
	if errors.SuggestAttr("zzz", []string{"append"}) != "" {
		t.Fatal("far-away names should yield no suggestion")
	}
}

func TestGateMatchHierarchy(t *testing.T) {
	exc := errors.New(errors.PyExc_KeyError, nil)
	if !errors.Match(exc, errors.PyExc_LookupError) {
		t.Fatal("KeyError must match LookupError")
	}
	if !errors.Match(exc, errors.PyExc_BaseException) {
		t.Fatal("KeyError must match BaseException")
	}
	if errors.Match(exc, errors.PyExc_ValueError) {
		t.Fatal("KeyError must NOT match ValueError")
	}
}
