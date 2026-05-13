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

// TestImportSignal is the spec 1702 signal gate: `import signal` must
// resolve end-to-end via Lib/signal.py wrapping the Go _signal backend,
// produce a Signals IntEnum with the correct SIGINT value (2 on macOS),
// and a Handlers IntEnum with SIG_DFL==0.
//
// CPython: Lib/signal.py, Modules/signalmodule.c
func TestImportSignal(t *testing.T) {
	installSignalFinder(t)

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := `
import signal
print(signal.Signals.SIGINT.value)
print(signal.Handlers.SIG_DFL.value)
`
	if _, err := pythonrun.RunString(ts, src, "<signal-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import signal: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "2\n0"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// installSignalFinder sets up PathFinder for signal and its transitive
// deps (enum, keyword, types, abc, _py_abc, _weakrefset, _collections_abc).
func installSignalFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"signal", "enum", "keyword", "types",
			"abc", "_py_abc", "_weakrefset", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})
}
