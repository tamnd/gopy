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

// TestImportFunctools pins the spec 1702 functools gate: with the
// inittab shim removed, `import functools` must resolve via
// stdlib/functools.py on top of the built-in _functools. The vendored
// module pulls in abc, collections, operator, reprlib, types and
// _thread.RLock so all of those need to load cleanly too.
//
// CPython: Lib/functools.py
func TestImportFunctools(t *testing.T) {
	installFunctoolsFinder(t)

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
import functools
print(functools.reduce(lambda a, b: a + b, [1, 2, 3, 4]))
print(functools.partial(lambda a, b: a * b, 3)(4))
key = functools.cmp_to_key(lambda a, b: a - b)
print(sorted([3, 1, 2], key=key))
`
	if _, err := pythonrun.RunString(ts, src, "<functools-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import functools: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "10\n12\n[1, 2, 3]"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func installFunctoolsFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"functools", "abc", "collections", "operator",
			"reprlib", "types", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})
}
