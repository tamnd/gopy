package stdlibinit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

func runChainScript(t *testing.T, src string, wants []string) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{Paths: []string{stdlibPath(t)}, Compiler: abcCompiler})
	t.Cleanup(func() { imp.SetPathFinder(prev) })
	var stdout bytes.Buffer
	g, _ := builtins.Init(&stdout)
	_ = g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__"))
	ts := state.NewThread()
	if _, runErr := pythonrun.RunString(ts, src, "<chain>", parser.ModeFile, g, nil); runErr != nil {
		t.Fatalf("run: %v\nout:\n%s", runErr, stdout.String())
	}
	out := stdout.String()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\nfull:\n%s", w, out)
		}
	}
}

// TestTracebackFormatExceptionCauseChain renders `raise X from Y`.
// __cause__ must be set on the outer exception, and format_exception
// must emit the "direct cause" separator between the two frames.
//
// CPython: Lib/traceback.py TracebackException.format
func TestTracebackFormatExceptionCauseChain(t *testing.T) {
	src := `
import traceback
try:
    try:
        raise ValueError("inner")
    except ValueError as e:
        raise RuntimeError("outer") from e
except RuntimeError as e:
    for line in traceback.format_exception(e):
        print(line, end="")
`
	runChainScript(t, src, []string{
		"ValueError: inner",
		"The above exception was the direct cause of the following exception:",
		"RuntimeError: outer",
	})
}

// TestTracebackFormatExceptionContextChain renders implicit __context__:
// raising inside an except clause without `from` attaches the original
// exception as __context__, and format_exception emits the "during
// handling" separator.
//
// CPython: Lib/traceback.py TracebackException.format
func TestTracebackFormatExceptionContextChain(t *testing.T) {
	src := `
import traceback
try:
    try:
        raise ValueError("inner")
    except ValueError:
        raise RuntimeError("outer")
except RuntimeError as e:
    for line in traceback.format_exception(e):
        print(line, end="")
`
	runChainScript(t, src, []string{
		"ValueError: inner",
		"During handling of the above exception, another exception occurred:",
		"RuntimeError: outer",
	})
}
