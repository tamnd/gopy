package builtins

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestInputReadsLine(t *testing.T) {
	in := strings.NewReader("hello\n")
	var out bytes.Buffer
	fn := Input(in, &out)
	res, err := fn(nil, nil)
	if err != nil {
		t.Fatalf("input(): %v", err)
	}
	got, _ := objects.Str(res)
	if got != "hello" {
		t.Fatalf("input() = %q, want %q", got, "hello")
	}
	if out.Len() != 0 {
		t.Fatalf("no prompt should produce no output; got %q", out.String())
	}
}

func TestInputWritesPrompt(t *testing.T) {
	in := strings.NewReader("ok\n")
	var out bytes.Buffer
	fn := Input(in, &out)
	res, err := fn([]objects.Object{objects.NewStr("> ")}, nil)
	if err != nil {
		t.Fatalf("input('> '): %v", err)
	}
	if out.String() != "> " {
		t.Fatalf("prompt = %q, want %q", out.String(), "> ")
	}
	if got, _ := objects.Str(res); got != "ok" {
		t.Fatalf("input = %q, want ok", got)
	}
}

func TestInputStripsCRLF(t *testing.T) {
	in := strings.NewReader("hi\r\n")
	fn := Input(in, nil)
	res, err := fn(nil, nil)
	if err != nil {
		t.Fatalf("input: %v", err)
	}
	if got, _ := objects.Str(res); got != "hi" {
		t.Fatalf("got %q, want hi", got)
	}
}

func TestInputEOFRaises(t *testing.T) {
	in := strings.NewReader("")
	fn := Input(in, nil)
	if _, err := fn(nil, nil); err == nil {
		t.Fatal("input() on empty stdin: want EOFError, got nil")
	}
}

func TestInputAcceptsNonStrPrompt(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer
	fn := Input(in, &out)
	if _, err := fn([]objects.Object{objects.NewInt(42)}, nil); err != nil {
		t.Fatalf("input(42): %v", err)
	}
	if out.String() != "42" {
		t.Fatalf("prompt for int = %q, want 42", out.String())
	}
}

func TestInputNonePromptSkipsWrite(t *testing.T) {
	in := strings.NewReader("ok\n")
	var out bytes.Buffer
	fn := Input(in, &out)
	if _, err := fn([]objects.Object{objects.None()}, nil); err != nil {
		t.Fatalf("input(None): %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("None prompt wrote %q", out.String())
	}
}

func TestInputRejectsKwargs(t *testing.T) {
	fn := Input(strings.NewReader(""), nil)
	_, err := fn(nil, map[string]objects.Object{"foo": objects.NewInt(1)})
	if err == nil {
		t.Fatal("input(foo=1): want TypeError, got nil")
	}
}

func TestInputRejectsTooManyArgs(t *testing.T) {
	fn := Input(strings.NewReader(""), nil)
	_, err := fn([]objects.Object{objects.NewStr("a"), objects.NewStr("b")}, nil)
	if err == nil {
		t.Fatal("input('a','b'): want TypeError, got nil")
	}
}
