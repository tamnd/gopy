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

// TestTracebackFormatExceptionMultiFrame is the gate for spec 1702
// traceback. Builds a four-frame call chain (<module> -> top -> middle
// -> inner) that raises ValueError and asserts that traceback.format_exception
// returns the full chain with the expected co_name labels.
func TestTracebackFormatExceptionMultiFrame(t *testing.T) {
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{Paths: []string{stdlibPath(t)}, Compiler: abcCompiler})
	t.Cleanup(func() { imp.SetPathFinder(prev) })
	var stdout bytes.Buffer
	g, _ := builtins.Init(&stdout)
	_ = g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__"))
	ts := state.NewThread()
	src := `
import traceback
def inner():
    raise ValueError("boom")
def middle():
    inner()
def top():
    middle()
try:
    top()
except ValueError as e:
    for line in traceback.format_exception(e):
        print(line, end="")
`
	if _, runErr := pythonrun.RunString(ts, src, "<test>", parser.ModeFile, g, nil); runErr != nil {
		t.Fatalf("run: %v\nout:\n%s", runErr, stdout.String())
	}
	out := stdout.String()
	wants := []string{
		"Traceback (most recent call last):",
		`File "<test>", line 10, in <module>`,
		`File "<test>", line 8, in top`,
		`File "<test>", line 6, in middle`,
		`File "<test>", line 4, in inner`,
		"ValueError: boom",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\nfull:\n%s", w, out)
		}
	}
}
