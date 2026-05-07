// Coverage for the myreadline package: stdio fallback, custom reader
// install, EOF / interrupt signaling, prompt writing, re-entrancy.

package myreadline

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStdioReadlineReturnsLine(t *testing.T) {
	out := &bytes.Buffer{}
	got, err := StdioReadline(strings.NewReader("hello\nworld\n"), out, "ps1> ")
	if err != nil {
		t.Fatalf("StdioReadline: %v", err)
	}
	if got != "hello\n" {
		t.Fatalf("got %q, want %q", got, "hello\n")
	}
	if out.String() != "ps1> " {
		t.Fatalf("prompt = %q, want ps1> ", out.String())
	}
}

func TestStdioReadlineEOFReturnsBlank(t *testing.T) {
	got, err := StdioReadline(strings.NewReader(""), &bytes.Buffer{}, "")
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestStdioReadlineEOFAfterPartialLine(t *testing.T) {
	got, err := StdioReadline(strings.NewReader("partial"), &bytes.Buffer{}, "")
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if got != "partial" {
		t.Fatalf("got %q, want partial", got)
	}
}

func TestReadlineDispatchesToInstalledReader(t *testing.T) {
	prev := CurrentReader()
	t.Cleanup(func() { SetReader(prev) })

	called := false
	SetReader(func(_ io.Reader, _ io.Writer, prompt string) (string, error) {
		called = true
		if prompt != "ps2: " {
			t.Fatalf("custom reader saw prompt %q", prompt)
		}
		return "from-custom\n", nil
	})

	got, err := Readline(strings.NewReader("ignored"), &bytes.Buffer{}, "ps2: ")
	if err != nil {
		t.Fatalf("Readline: %v", err)
	}
	if !called {
		t.Fatalf("installed reader was not invoked")
	}
	if got != "from-custom\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReadlineFallsBackWhenReaderNil(t *testing.T) {
	prev := CurrentReader()
	t.Cleanup(func() { SetReader(prev) })
	SetReader(nil)

	got, err := Readline(strings.NewReader("plain\n"), &bytes.Buffer{}, "")
	if err != nil {
		t.Fatalf("Readline: %v", err)
	}
	if got != "plain\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReadlineRefusesReentry(t *testing.T) {
	prev := CurrentReader()
	t.Cleanup(func() { SetReader(prev) })

	var inner error
	SetReader(func(stdin io.Reader, stdout io.Writer, _ string) (string, error) {
		_, inner = Readline(stdin, stdout, "")
		return "outer\n", nil
	})

	out, err := Readline(strings.NewReader(""), &bytes.Buffer{}, "")
	if err != nil {
		t.Fatalf("outer Readline: %v", err)
	}
	if out != "outer\n" {
		t.Fatalf("outer got %q", out)
	}
	if inner == nil || !strings.Contains(inner.Error(), "re-enter readline") {
		t.Fatalf("inner err = %v, want re-enter RuntimeError", inner)
	}
}

func TestReadlineNilStdin(t *testing.T) {
	prev := CurrentReader()
	t.Cleanup(func() { SetReader(prev) })
	SetReader(nil)

	_, err := Readline(nil, &bytes.Buffer{}, "")
	if err == nil || !strings.Contains(err.Error(), "lost sys.stdin") {
		t.Fatalf("err = %v, want lost-stdin RuntimeError", err)
	}
}

func TestReadlineInterruptIsDistinctFromEOF(t *testing.T) {
	prev := CurrentReader()
	t.Cleanup(func() { SetReader(prev) })
	SetReader(func(_ io.Reader, _ io.Writer, _ string) (string, error) {
		return "", ErrInterrupt
	})

	_, err := Readline(strings.NewReader(""), &bytes.Buffer{}, "")
	if !errors.Is(err, ErrInterrupt) {
		t.Fatalf("err = %v, want ErrInterrupt", err)
	}
}
