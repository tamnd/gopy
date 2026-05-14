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

// TestImportWeakref is the spec 1702 weakref gate: `import weakref` must
// resolve end-to-end via Lib/weakref.py wrapping the Go _weakref backend.
// Verifies that ref() wraps an object and the reference can be retrieved.
//
// CPython: Lib/weakref.py, Modules/_weakref.c
func TestImportWeakref(t *testing.T) {
	installWeakrefFinder(t)

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
import weakref
class C:
    pass
obj = C()
r = weakref.ref(obj)
print(r() is obj)
print(weakref.getweakrefcount(obj))
`
	if _, err := pythonrun.RunString(ts, src, "<weakref-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import weakref: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "True\n1"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// installWeakrefFinder sets up PathFinder for weakref and its transitive deps.
func installWeakrefFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"weakref", "_weakrefset", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})
}
