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

// TestImportIO is the spec 1702 io gate: `import io` must resolve
// end-to-end via stdlib/io.py wrapping the Go _io backend.
// BytesIO.write + getvalue must round-trip correctly.
//
// CPython: Lib/io.py, Modules/_io/
func TestImportIO(t *testing.T) {
	installIOFinder(t)

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
import io
b = io.BytesIO()
b.write(b'hello')
b.seek(0)
print(b.read())
`
	if _, err := pythonrun.RunString(ts, src, "<io-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import io: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "b'hello'"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// installIOFinder sets up PathFinder for io and its transitive deps
// (abc, _collections_abc).
func installIOFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"io", "abc", "_py_abc", "_weakrefset", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})
}
