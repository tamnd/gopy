// Gate test: import traceback and exercise format_exc() end-to-end.
//
// CPython: Lib/traceback.py
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

// TestTracebackFormatExc verifies that traceback.format_exc() runs
// without error inside an active except block and returns a string
// containing the exception class and message.
func TestTracebackFormatExc(t *testing.T) {
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"traceback", "linecache", "textwrap", "codeop", "keyword",
			"tokenize", "token", "io", "collections", "collections.abc",
			"argparse", "gettext", "operator", "enum", "types",
			"abc", "_py_abc", "_weakrefset", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	_ = g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__"))
	ts := state.NewThread()

	src := `
import traceback
try:
    raise ValueError('sentinel')
except:
    s = traceback.format_exc()
`
	if _, runErr := pythonrun.RunString(ts, src, "<test>", parser.ModeFile, g, nil); runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	sVal, getErr := g.GetItem(objects.NewStr("s"))
	if getErr != nil {
		t.Fatalf("no 's' in globals: %v", getErr)
	}
	result, _ := objects.Str(sVal)
	if !strings.Contains(result, "ValueError") || !strings.Contains(result, "sentinel") {
		t.Errorf("format_exc() = %q; want string containing 'ValueError' and 'sentinel'", result)
	}
}
