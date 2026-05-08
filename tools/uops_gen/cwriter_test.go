package main

import (
	"bytes"
	"strings"
	"testing"
)

// emitTokens runs every Token in src through CWriter and returns the
// rendered output. Mirrors the pattern cwriter.py tests use: tokenize,
// then walk the token stream into a writer.
func emitTokens(t *testing.T, src string) string {
	t.Helper()
	tokens, err := Tokenize(src, "test.c", 1)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	for _, tok := range tokens {
		w.Emit(tok)
	}
	return buf.String()
}

func TestCWriterPreservesSpacing(t *testing.T) {
	src := "int x = 1;"
	got := emitTokens(t, src)
	if !strings.Contains(got, "int x = 1;") {
		t.Errorf("output should preserve spacing of source, got %q", got)
	}
}

func TestCWriterIndentsBraceBlock(t *testing.T) {
	src := "if (x) {\n    y = 1;\n}\n"
	got := emitTokens(t, src)
	if !strings.Contains(got, "y = 1;") {
		t.Errorf("output missing inner statement, got %q", got)
	}
}

func TestCWriterEmitStrTracksIndent(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.EmitStr("if (cond) {\n")
	w.EmitStr("body();\n")
	w.EmitStr("}\n")
	got := buf.String()
	if !strings.Contains(got, "    body();") {
		t.Errorf("brace block must indent body 4 spaces, got %q", got)
	}
}

func TestCWriterStartLineForcesNewline(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.EmitStr("x")
	w.StartLine()
	w.EmitStr("y\n")
	got := buf.String()
	if got != "x\ny\n" {
		t.Errorf("StartLine must force a newline before next emit, got %q", got)
	}
}

func TestCWriterSpillEmitsSetStackPointer(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.EmitSpill()
	w.Emit("foo();\n")
	got := buf.String()
	if !strings.Contains(got, "_PyFrame_SetStackPointer(frame, stack_pointer);") {
		t.Errorf("spill must flush a SetStackPointer call, got %q", got)
	}
}

func TestCWriterReloadEmitsGetStackPointer(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.EmitReload()
	w.Emit("foo();\n")
	got := buf.String()
	if !strings.Contains(got, "stack_pointer = _PyFrame_GetStackPointer(frame);") {
		t.Errorf("reload must flush a GetStackPointer call, got %q", got)
	}
}

func TestCWriterSpillCancelsReload(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.EmitReload()
	w.EmitSpill()
	w.Emit("foo();\n")
	got := buf.String()
	if strings.Contains(got, "_PyFrame_") {
		t.Errorf("spill after reload must cancel both, got %q", got)
	}
}

func TestCWriterReloadCancelsSpill(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.EmitSpill()
	w.EmitReload()
	w.Emit("foo();\n")
	got := buf.String()
	if strings.Contains(got, "_PyFrame_") {
		t.Errorf("reload after spill must cancel both, got %q", got)
	}
}

func TestNullCWriterDropsOutput(t *testing.T) {
	w := NullCWriter()
	w.EmitStr("anything\n")
	w.Emit("more\n")
	// No assertion beyond not panicking; io.Discard swallows everything.
}

func TestCWriterHeaderGuard(t *testing.T) {
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	w.HeaderGuardOpen("FOO_H")
	w.EmitStr("body\n")
	w.HeaderGuardClose("FOO_H")
	got := buf.String()
	for _, want := range []string{
		"#ifndef FOO_H",
		"#define FOO_H",
		`extern "C" {`,
		"#endif /* !FOO_H */",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("header guard missing %q, got %q", want, got)
		}
	}
}

func TestCWriterIsLabel(t *testing.T) {
	cases := []struct {
		txt  string
		want bool
	}{
		{"foo:", true},
		{"// comment:", false},
		{"x;", false},
		{"label_1:", true},
	}
	for _, tc := range cases {
		if got := isLabel(tc.txt); got != tc.want {
			t.Errorf("isLabel(%q) = %v, want %v", tc.txt, got, tc.want)
		}
	}
}

func TestSplitLinesKeepEnds(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a\nb\n", []string{"a\n", "b\n"}},
		{"a\nb", []string{"a\n", "b"}},
		{"only", []string{"only"}},
	}
	for _, tc := range cases {
		got := splitLinesKeepEnds(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitLinesKeepEnds(%q) len = %d, want %d (%v vs %v)", tc.in, len(got), len(tc.want), got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitLinesKeepEnds(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
