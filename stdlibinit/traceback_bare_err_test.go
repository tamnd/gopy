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

// TestTracebackBareGoErrorMultiFrame covers the path where the raising
// op returns a bare Go error (1/0 in long_arith.go) instead of going
// through pyerrors.Raise. handleException must synthesize and install
// the exception on the thread state at the bottom frame so every
// frame's attachFrameTraceback can hang a TB entry off of it.
func TestTracebackBareGoErrorMultiFrame(t *testing.T) {
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
    return 1/0
def outer():
    return inner()
try:
    outer()
except ZeroDivisionError as e:
    for line in traceback.format_exception(e):
        print(line, end="")
`
	if _, runErr := pythonrun.RunString(ts, src, "<test>", parser.ModeFile, g, nil); runErr != nil {
		t.Fatalf("run: %v\nout:\n%s", runErr, stdout.String())
	}
	out := stdout.String()
	wants := []string{
		"Traceback (most recent call last):",
		`File "<test>", line 8, in <module>`,
		`File "<test>", line 6, in outer`,
		`File "<test>", line 4, in inner`,
		"ZeroDivisionError:",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\nfull:\n%s", w, out)
		}
	}
}
