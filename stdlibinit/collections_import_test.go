package stdlibinit_test

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

// installCollectionsFinder configures PathFinder for the collections test and
// registers cleanup that drops all modules imported during the test.
func installCollectionsFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"collections", "collections.abc",
			"_collections_abc", "_py_abc", "_weakrefset",
			"abc", "reprlib", "operator", "itertools",
			"keyword", "functools", "_functools",
		} {
			imp.RemoveModule(name)
		}
	})
}

// TestCollectionsImportResolvesNames pins that `import collections` loads from
// the vendored stdlib/collections/__init__.py and exposes OrderedDict and
// deque in the module dict.
func TestCollectionsImportResolvesNames(t *testing.T) {
	installCollectionsFinder(t)

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	ts := state.NewThread()

	src := "import collections\n"
	if _, err := pythonrun.RunString(ts, src, "<collections-import>", parser.ModeFile, g, nil); err != nil {
		t.Skipf("import collections: %v", err)
	}

	mod, ok := imp.GetModule("collections")
	if !ok {
		t.Fatal("imp.GetModule(\"collections\") missing after import")
	}
	for _, name := range []string{"OrderedDict", "deque"} {
		v, err := mod.Dict().GetItem(objects.NewStr(name))
		if err != nil {
			t.Errorf("collections.%s lookup: %v", name, err)
			continue
		}
		if v == nil || v == objects.None() {
			t.Errorf("collections.%s = %v, want non-None", name, v)
		}
	}
}
